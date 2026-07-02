package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"iot-dashboard/internal/database"
	"iot-dashboard/internal/modules/alerts"
	"iot-dashboard/internal/modules/dashboards"
	"iot-dashboard/internal/modules/telemetry"
)

// ToolKit holds the domain services the AI tools delegate to. It reuses the
// existing module services so every tool inherits the same org-scoping and
// validation the REST API already enforces — no new business logic, just a
// thin LLM-facing layer.
type ToolKit struct {
	tel   *telemetry.Service
	alert *alerts.Service
	dash  *dashboards.Service
}

func NewToolKit() *ToolKit {
	return &ToolKit{
		tel:   telemetry.NewService(),
		alert: alerts.NewService(),
		dash:  dashboards.NewService(),
	}
}

// ── Category A: read telemetry & alerts ──────────────────────────────────────

// ShowMetric resolves a machine + field to a concrete widget spec the UI renders
// directly (no frontend guessing). When the field doesn't exist, returns fallback
// widgets for all available fields: non-status fields as gauge/kpi, status fields
// as exactly 1 kpi-card.
func (tk *ToolKit) ShowMetric(ctx context.Context, orgID string, raw json.RawMessage) (any, error) {
	var args ShowMetricArgs
	_ = json.Unmarshal(raw, &args)
	id, ok := resolveMachineID(ctx, orgID, strings.TrimSpace(args.Machine))
	if !ok {
		return nil, fmt.Errorf("machine %q not found", args.Machine)
	}
	metric := strings.TrimSpace(args.Metric)
	if metric == "" {
		return nil, fmt.Errorf("metric is required")
	}

	// Fetch machine name first — needed by both happy path and fallback.
	var machineName string
	_ = database.Pool.QueryRow(ctx, `SELECT name FROM machines WHERE id = $1`, id).Scan(&machineName)
	if machineName == "" {
		machineName = args.Machine
	}

	if metric == "count" || metric == "daily-count" || metric == "counter" {
		bucket := "1h"
		status := "all"
		sku := ""

		var configBytes []byte
		dbErr := database.Pool.QueryRow(ctx, `
			SELECT config
			FROM dashboard_widgets
			WHERE machine_id = $1 AND widget_type = 'daily-count'
			ORDER BY updated_at DESC LIMIT 1
		`, id).Scan(&configBytes)
		if dbErr == nil {
			var cfg map[string]any
			if json.Unmarshal(configBytes, &cfg) == nil {
				if b, ok := cfg["bucket"].(string); ok && b != "" {
					bucket = b
				}
				if s, ok := cfg["status"].(string); ok && s != "" {
					status = s
				}
				if k, ok := cfg["sku"].(string); ok {
					sku = k
				}
			}
		}

		return map[string]any{
			"widget": map[string]any{
				"type":        "daily-count",
				"title":       fmt.Sprintf("%s — Count", machineName),
				"machine":     machineName,
				"machineUuid": id,
				"metric":      "",
				"unit":        "pcs",
				"bucket":      bucket,
				"status":      status,
				"sku":         sku,
			},
		}, nil
	}

	var label string
	var unit *string
	var wmin, wmax *float64
	err := database.Pool.QueryRow(ctx, `
		SELECT mf.label, mf.unit, mf.min, mf.max
		FROM machine_fields mf
		WHERE mf.machine_id = $1 AND mf.key = $2
	`, id, metric).Scan(&label, &unit, &wmin, &wmax)
	if err != nil {
		// Metric not found — return all available fields as fallback widgets.
		// Non-status: gauge if has min+max, else kpi-card. Status: exactly 1 kpi-card.
		rows, _ := database.Pool.Query(ctx, `
			SELECT key, label, unit, min, max, key ILIKE '%status%' AS is_status
			FROM machine_fields
			WHERE machine_id = $1 AND data_type = 'number'
			ORDER BY is_key DESC, key
		`, id)
		defer rows.Close()
		var fallbackWidgets []map[string]any
		statusAdded := false
		for rows.Next() {
			var key, lbl string
			var u *string
			var fmin, fmax *float64
			var isStatus bool
			if err2 := rows.Scan(&key, &lbl, &u, &fmin, &fmax, &isStatus); err2 != nil {
				continue
			}
			if isStatus {
				if statusAdded {
					continue
				}
				statusAdded = true
				fallbackWidgets = append(fallbackWidgets, map[string]any{
					"type": "kpi-card", "title": fmt.Sprintf("%s — %s", machineName, lbl),
					"machine": machineName, "machineUuid": id,
					"metric": key, "unit": deref(u),
				})
				continue
			}
			wtype := "kpi-card"
			if fmin != nil && fmax != nil {
				wtype = "gauge"
			}
			w := map[string]any{
				"type": wtype, "title": fmt.Sprintf("%s — %s", machineName, lbl),
				"machine": machineName, "machineUuid": id,
				"metric": key, "unit": deref(u),
			}
			if fmin != nil {
				w["min"] = *fmin
			}
			if fmax != nil {
				w["max"] = *fmax
			}
			fallbackWidgets = append(fallbackWidgets, w)
		}
		return map[string]any{
			"fallback":         true,
			"requested_metric": metric,
			"message":          fmt.Sprintf("Machine %q has no metric %q. Showing available metrics.", machineName, metric),
			"widgets":          fallbackWidgets,
		}, nil
	}

	wtype := "kpi-card"
	switch args.Viz {
	case "trend":
		wtype = "line-chart"
	case "gauge":
		wtype = "gauge"
	case "value":
		wtype = "kpi-card"
	default:
		if wmin != nil && wmax != nil {
			wtype = "gauge"
		}
	}

	widget := map[string]any{
		"type":        wtype,
		"title":       fmt.Sprintf("%s — %s", machineName, label),
		"machine":     machineName,
		"machineUuid": id,
		"metric":      metric,
		"unit":        deref(unit),
	}
	if wmin != nil {
		widget["min"] = *wmin
	}
	if wmax != nil {
		widget["max"] = *wmax
	}
	trendWidget := map[string]any{
		"type":        "line-chart",
		"title":       fmt.Sprintf("%s — %s (trend)", machineName, label),
		"machine":     machineName,
		"machineUuid": id,
		"metric":      metric,
		"unit":        deref(unit),
	}
	return map[string]any{"widgets": []map[string]any{widget, trendWidget}}, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// GetTelemetryTrend returns avg/min/max of a metric over a time range.
func (tk *ToolKit) GetTelemetryTrend(ctx context.Context, orgID string, raw json.RawMessage) (any, error) {
	var args TrendArgs
	_ = json.Unmarshal(raw, &args)
	id, ok := resolveMachineID(ctx, orgID, strings.TrimSpace(args.MachineID))
	if !ok {
		return nil, fmt.Errorf("machine %q not found", args.MachineID)
	}
	if strings.TrimSpace(args.Metric) == "" {
		return nil, fmt.Errorf("metric is required")
	}
	period := strings.TrimSpace(args.TimeRange)
	if period == "" {
		period = "1h"
	}
	return tk.tel.GetAggregate(ctx, id, args.Metric, period, orgID)
}

// GetActiveAlerts lists every open alert event for the org. The result is fed
// back to the LLM (and re-sent on each agentic-loop iteration), so we project
// each event down to only the fields the model needs to reason or ack/resolve.
func (tk *ToolKit) GetActiveAlerts(ctx context.Context, orgID string) (any, error) {
	events, err := tk.alert.GetActiveEvents(ctx, &orgID)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(events))
	for _, e := range events {
		out = append(out, map[string]any{
			"event_id":    e.ID,
			"machine":     e.MachineName,
			"metric":      e.Field,
			"value":       e.Value,
			"severity":    e.Severity,
			"status":      e.Status,
			"message":     e.Message,
			"triggeredAt": e.TriggeredAt,
		})
	}
	return map[string]any{"count": len(out), "alerts": out}, nil
}

// GetTelemetrySeries returns time-bucketed avg/min/max data points — mirrors what a line chart shows.
func (tk *ToolKit) GetTelemetrySeries(ctx context.Context, orgID string, raw json.RawMessage) (any, error) {
	var args SeriesArgs
	_ = json.Unmarshal(raw, &args)
	id, ok := resolveMachineID(ctx, orgID, strings.TrimSpace(args.MachineID))
	if !ok {
		return nil, fmt.Errorf("machine %q not found", args.MachineID)
	}
	if strings.TrimSpace(args.Metric) == "" {
		return nil, fmt.Errorf("metric is required")
	}
	tr := strings.TrimSpace(args.TimeRange)
	if tr == "" {
		tr = "1h"
	}
	result, err := tk.tel.GetSeries(ctx, id, args.Metric, tr, "", "", "", 0, &orgID)
	if err != nil {
		return nil, err
	}
	return compactSeriesResult(result), nil
}

// GetProductionCount returns bucket-level piece counts — mirrors what a daily-count widget shows.
func (tk *ToolKit) GetProductionCount(ctx context.Context, orgID string, raw json.RawMessage) (any, error) {
	var args ProductionCountArgs
	_ = json.Unmarshal(raw, &args)
	id, ok := resolveMachineID(ctx, orgID, strings.TrimSpace(args.MachineID))
	if !ok {
		return nil, fmt.Errorf("machine %q not found", args.MachineID)
	}
	bucket := strings.TrimSpace(args.Bucket)
	if bucket == "" {
		bucket = "1h"
	}
	points := args.Points
	if points <= 0 {
		points = 48
	}
	result, err := tk.tel.GetBucketCount(ctx, id, args.Sku, args.Status, bucket, points, &orgID)
	if err != nil {
		return nil, err
	}
	return compactBucketResult(result), nil
}

// GetSkus lists the distinct SKU values seen for a machine — so the AI can answer
// "which SKUs can I select?" and map a user's loose casing to a real SKU.
func (tk *ToolKit) GetSkus(ctx context.Context, orgID string, raw json.RawMessage) (any, error) {
	var args struct {
		MachineID string `json:"machine_id"`
	}
	_ = json.Unmarshal(raw, &args)
	id, ok := resolveMachineID(ctx, orgID, strings.TrimSpace(args.MachineID))
	if !ok {
		return nil, fmt.Errorf("machine %q not found", args.MachineID)
	}
	skus, err := tk.tel.GetMachineSkus(ctx, id, &orgID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"machine": args.MachineID, "skus": skus}, nil
}

// ── LLM token-footprint compaction ───────────────────────────────────────────
// get_telemetry_series / get_production_count return up to 500 points; the
// object-per-point shape (repeating "bucket"/"avg"/"min"/"max" keys on every
// row) is what the REST API/frontend charts need, but it's the single biggest
// cost in the tool-result tokens fed back into the model on every turn that
// uses it — and again on every later turn that re-sends conversation history.
// Reshape into [time, values...] tuples + a "columns" legend here, tool-side
// only, so the shared telemetry service (and the REST API) are untouched.

// bkkZone: telemetry is stored UTC but the chart renders in plant-local time.
// ponytail: Thailand-only, no DST — a fixed +7 offset needs no tzdata.
var bkkZone = time.FixedZone("+07", 7*3600)

func shortTime(v any) string {
	if t, ok := v.(time.Time); ok {
		return t.In(bkkZone).Format("2006-01-02T15:04")
	}
	return ""
}

func shortTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.In(bkkZone).Format("2006-01-02T15:04")
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

func compactSeriesResult(result map[string]interface{}) map[string]any {
	rows, _ := result["data"].([]telemetry.TelemetryPoint)
	columns := []string{"time"}
	if len(rows) > 0 {
		if rows[0].Avg != nil {
			columns = append(columns, "avg")
		}
		if rows[0].Min != nil {
			columns = append(columns, "min")
		}
		if rows[0].Max != nil {
			columns = append(columns, "max")
		}
		if rows[0].Value != nil {
			columns = append(columns, "value")
		}
	}
	data := make([][]any, 0, len(rows))
	for _, p := range rows {
		ts := p.Bucket
		if ts == nil {
			ts = p.Ts
		}
		row := []any{shortTimePtr(ts)}
		if p.Avg != nil {
			row = append(row, round2(*p.Avg))
		}
		if p.Min != nil {
			row = append(row, round2(*p.Min))
		}
		if p.Max != nil {
			row = append(row, round2(*p.Max))
		}
		if p.Value != nil {
			row = append(row, round2(*p.Value))
		}
		data = append(data, row)
	}
	return map[string]any{
		"field":   result["field"],
		"from":    shortTime(result["from"]),
		"to":      shortTime(result["to"]),
		"columns": columns,
		"data":    data,
	}
}

func compactBucketResult(result map[string]interface{}) map[string]any {
	rows, _ := result["data"].([]telemetry.BucketCount)
	data := make([][2]any, 0, len(rows))
	for _, r := range rows {
		data = append(data, [2]any{r.Bucket.In(bkkZone).Format("2006-01-02T15:04"), r.Count})
	}
	return map[string]any{
		"sku":     result["sku"],
		"status":  result["status"],
		"bucket":  result["bucket"],
		"from":    shortTime(result["from"]),
		"to":      shortTime(result["to"]),
		"columns": []string{"time", "count"},
		"data":    data,
	}
}

// ── Category B: manage existing dashboards ───────────────────────────────────

// ListDashboards returns the org's dashboards with names + widget counts so the
// AI can reference an existing dashboard by name before modifying it.
func (tk *ToolKit) ListDashboards(ctx context.Context, orgID, userID string) (any, error) {
	ds, err := tk.dash.GetDashboards(ctx, orgID, userID)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(ds))
	for _, d := range ds {
		wc := 0
		if d.Count != nil {
			wc = d.Count.Widgets
		}
		out = append(out, map[string]any{
			"name":    d.Name,
			"widgets": wc,
			"url":     "/dashboards/" + d.ID,
		})
	}
	return map[string]any{"count": len(out), "dashboards": out}, nil
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// resolveDashboardID does a case-insensitive substring match, consistent with resolveMachineID.
// Exact matches are ranked first so a short query like "Overview" doesn't shadow "CW-01 Overview".
func resolveDashboardID(ctx context.Context, orgID, name string) (string, bool) {
	var id string
	err := database.Pool.QueryRow(ctx, `
		SELECT id FROM dashboards
		WHERE organization_id = $1 AND LOWER(name) LIKE '%' || LOWER($2) || '%'
		ORDER BY
		  CASE WHEN LOWER(name) = LOWER($2) THEN 0 ELSE 1 END,
		  created_at DESC
		LIMIT 1
	`, orgID, name).Scan(&id)
	if err != nil {
		return "", false
	}
	return id, true
}


