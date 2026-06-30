package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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

// GetDailyCount returns per-day production counts for one machine.
func (tk *ToolKit) GetDailyCount(ctx context.Context, orgID string, raw json.RawMessage) (any, error) {
	var args DailyCountArgs
	_ = json.Unmarshal(raw, &args)
	id, ok := resolveMachineID(ctx, orgID, strings.TrimSpace(args.MachineID))
	if !ok {
		return nil, fmt.Errorf("machine %q not found", args.MachineID)
	}
	days := args.Days
	if days <= 0 {
		days = 7
	}
	return tk.tel.GetDailyCount(ctx, id, days, &orgID)
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

// AddWidget appends a single widget to an existing dashboard (referenced by name).
func (tk *ToolKit) AddWidget(ctx context.Context, orgID string, raw json.RawMessage) (any, error) {
	var args AddWidgetArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("malformed arguments")
	}
	// Gate: this tool only writes into a dashboard the user explicitly named. If none was
	// given, steer the model to show_metric (preview card) instead of asking which dashboard.
	if strings.TrimSpace(args.DashboardName) == "" {
		return map[string]any{"error": "No dashboard named. To just show a metric widget to the user, call show_metric instead. Use add_widget_to_dashboard only when the user names an existing dashboard."}, nil
	}
	dashID, ok := resolveDashboardID(ctx, orgID, strings.TrimSpace(args.DashboardName))
	if !ok {
		return nil, fmt.Errorf("dashboard %q not found", args.DashboardName)
	}
	if !isAllowedType(args.Widget.Type) {
		return nil, fmt.Errorf("unsupported widget type %q", args.Widget.Type)
	}

	// Position the new widget after the current ones using the same flow layout.
	dash, err := tk.dash.GetDashboardByID(ctx, dashID, orgID)
	if err != nil {
		return nil, err
	}

	widget := dashboards.Widget{
		WidgetType: args.Widget.Type,
		Layout:     flowLayout(len(dash.Widgets)),
		Config:     buildConfig(args.Widget),
	}
	if t := strings.TrimSpace(args.Widget.Title); t != "" {
		widget.Title = &t
	}
	if name := strings.TrimSpace(args.Widget.MachineID); name != "" {
		if id, ok := resolveMachineID(ctx, orgID, name); ok {
			widget.MachineID = &id
		}
	}
	if _, err := tk.dash.AddWidget(ctx, dashID, orgID, widget); err != nil {
		return nil, err
	}
	return ToolResult{
		Success:     true,
		DashboardID: dashID,
		URL:         "/dashboards/" + dashID,
		Summary:     fmt.Sprintf("Added a %s widget to %q.", args.Widget.Type, args.DashboardName),
	}, nil
}

// RemoveWidget deletes a widget from a dashboard by its title (case-insensitive).
func (tk *ToolKit) RemoveWidget(ctx context.Context, orgID string, raw json.RawMessage) (any, error) {
	var args RemoveWidgetArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("malformed arguments")
	}
	dashID, ok := resolveDashboardID(ctx, orgID, strings.TrimSpace(args.DashboardName))
	if !ok {
		return nil, fmt.Errorf("dashboard %q not found", args.DashboardName)
	}
	dash, err := tk.dash.GetDashboardByID(ctx, dashID, orgID)
	if err != nil {
		return nil, err
	}
	target := strings.ToLower(strings.TrimSpace(args.WidgetTitle))
	for _, w := range dash.Widgets {
		title := ""
		if w.Title != nil {
			title = *w.Title
		}
		if strings.ToLower(title) == target {
			if err := tk.dash.DeleteWidget(ctx, w.ID, orgID); err != nil {
				return nil, err
			}
			return ToolResult{
				Success:     true,
				DashboardID: dashID,
				URL:         "/dashboards/" + dashID,
				Summary:     fmt.Sprintf("Removed widget %q from %q.", args.WidgetTitle, args.DashboardName),
			}, nil
		}
	}
	return nil, fmt.Errorf("no widget titled %q found on %q", args.WidgetTitle, args.DashboardName)
}

// ── Category C: manage alert rules & events ──────────────────────────────────

// CreateAlert defines a new threshold alert rule on a machine metric.
func (tk *ToolKit) CreateAlert(ctx context.Context, orgID string, raw json.RawMessage) (any, error) {
	var args CreateAlertArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("malformed arguments")
	}
	id, ok := resolveMachineID(ctx, orgID, strings.TrimSpace(args.MachineID))
	if !ok {
		return nil, fmt.Errorf("machine %q not found", args.MachineID)
	}
	if strings.TrimSpace(args.Metric) == "" {
		return nil, fmt.Errorf("metric is required")
	}
	if !isValidCondition(args.Condition) {
		return nil, fmt.Errorf("invalid condition %q (use gt, lt, gte, lte, eq, neq, between, outside)", args.Condition)
	}
	if (args.Condition == "between" || args.Condition == "outside") && args.ThresholdHi == nil {
		return nil, fmt.Errorf("condition %q requires threshold_hi", args.Condition)
	}
	severity := strings.TrimSpace(args.Severity)
	if severity == "" {
		severity = "warning"
	}
	name := strings.TrimSpace(args.Name)
	if name == "" {
		name = fmt.Sprintf("%s %s %g", args.Metric, args.Condition, args.Threshold)
	}
	a := alerts.Alert{
		MachineID:   id,
		Name:        name,
		Field:       args.Metric,
		Condition:   args.Condition,
		Threshold:   args.Threshold,
		ThresholdHi: args.ThresholdHi,
		Severity:    severity,
	}
	if args.CooldownSec != nil {
		a.CooldownSec = *args.CooldownSec
	}
	created, err := tk.alert.CreateAlert(ctx, orgID, a)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"success": true,
		"alertId": created.ID,
		"summary": fmt.Sprintf("Created alert %q on %s.", name, args.MachineID),
	}, nil
}

// AckAlert marks an open alert event as acknowledged.
func (tk *ToolKit) AckAlert(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
	var args AlertEventArg
	_ = json.Unmarshal(raw, &args)
	if strings.TrimSpace(args.EventID) == "" {
		return nil, fmt.Errorf("event_id is required (call get_active_alerts first)")
	}
	if err := tk.alert.AcknowledgeEvent(ctx, args.EventID, userID); err != nil {
		return nil, err
	}
	return map[string]any{"success": true, "summary": "Alert acknowledged."}, nil
}

// ResolveAlert marks an open alert event as resolved.
func (tk *ToolKit) ResolveAlert(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
	var args AlertEventArg
	_ = json.Unmarshal(raw, &args)
	if strings.TrimSpace(args.EventID) == "" {
		return nil, fmt.Errorf("event_id is required (call get_active_alerts first)")
	}
	if err := tk.alert.ResolveEvent(ctx, args.EventID, userID); err != nil {
		return nil, err
	}
	return map[string]any{"success": true, "summary": "Alert resolved."}, nil
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// resolveDashboardID does a case-insensitive, org-scoped dashboard name lookup.
func resolveDashboardID(ctx context.Context, orgID, name string) (string, bool) {
	var id string
	err := database.Pool.QueryRow(ctx, `
		SELECT id FROM dashboards
		WHERE organization_id = $1 AND LOWER(name) = LOWER($2)
		ORDER BY created_at DESC
		LIMIT 1
	`, orgID, name).Scan(&id)
	if err != nil {
		return "", false
	}
	return id, true
}

func isValidCondition(c string) bool {
	switch c {
	case "gt", "lt", "gte", "lte", "eq", "neq", "between", "outside":
		return true
	}
	return false
}
