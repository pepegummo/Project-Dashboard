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

// GetLatestTelemetry returns the current readings for one machine.
func (tk *ToolKit) GetLatestTelemetry(ctx context.Context, orgID string, raw json.RawMessage) (any, error) {
	var args MachineArg
	_ = json.Unmarshal(raw, &args)
	id, ok := resolveMachineID(ctx, orgID, strings.TrimSpace(args.MachineID))
	if !ok {
		return nil, fmt.Errorf("machine %q not found", args.MachineID)
	}
	snap, err := tk.tel.GetLatest(ctx, id, &orgID)
	if err != nil {
		return nil, fmt.Errorf("no telemetry recorded for %q yet", args.MachineID)
	}
	return map[string]any{
		"machine":   args.MachineID,
		"timestamp": snap.Timestamp,
		"values":    snap.Data,
	}, nil
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

// GetActiveAlerts lists every open alert event for the org.
func (tk *ToolKit) GetActiveAlerts(ctx context.Context, orgID string) (any, error) {
	events, err := tk.alert.GetActiveEvents(ctx, &orgID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"count": len(events), "alerts": events}, nil
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

// ── Category D: factory-wide analysis ────────────────────────────────────────

// GetFactoryOverview returns a one-shot snapshot of every machine — status,
// latest values, and open-alert count — for the LLM to summarise or reason over.
func (tk *ToolKit) GetFactoryOverview(ctx context.Context, orgID string) (any, error) {
	rows, err := database.Pool.Query(ctx, `
		SELECT m.id, m.name, m.type, m.status
		FROM machines m
		JOIN production_lines pl ON pl.id = m.production_line_id
		JOIN factories f ON f.id = pl.factory_id
		WHERE f.organization_id = $1
		ORDER BY m.name
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type mrow struct{ id, name, mtype, status string }
	var ms []mrow
	ids := []string{}
	for rows.Next() {
		var m mrow
		if err := rows.Scan(&m.id, &m.name, &m.mtype, &m.status); err != nil {
			continue
		}
		ms = append(ms, m)
		ids = append(ids, m.id)
	}

	latest, _ := tk.tel.GetMultiMachineLatest(ctx, ids, &orgID)
	events, _ := tk.alert.GetActiveEvents(ctx, &orgID)
	alertCount := map[string]int{}
	for _, e := range events {
		alertCount[e.MachineID]++
	}

	out := make([]map[string]any, 0, len(ms))
	for _, m := range ms {
		entry := map[string]any{
			"machine":    m.name,
			"type":       m.mtype,
			"status":     m.status,
			"openAlerts": alertCount[m.id],
		}
		if snap, ok := latest[m.id]; ok && snap != nil {
			entry["latest"] = snap.Data
		}
		out = append(out, entry)
	}
	return map[string]any{
		"machineCount":    len(ms),
		"totalOpenAlerts": len(events),
		"machines":        out,
	}, nil
}

// ── Category E: canvas locate ────────────────────────────────────────────────

// LocateWidget searches the org's dashboard widgets for one matching the user's
// description and returns its id so the frontend can spotlight it.
func (tk *ToolKit) LocateWidget(ctx context.Context, orgID string, raw json.RawMessage) (LocateWidgetResult, error) {
	var args struct {
		WidgetTitle   string `json:"widget_title"`
		DashboardName string `json:"dashboard_name"`
	}
	_ = json.Unmarshal(raw, &args)
	term := strings.TrimSpace(args.WidgetTitle)
	if term == "" {
		return LocateWidgetResult{Found: false, Summary: "widget_title is required"}, nil
	}

	query := `
		SELECT dw.id, COALESCE(dw.title, dw.widget_type)
		FROM dashboard_widgets dw
		JOIN dashboards d ON d.id = dw.dashboard_id
		WHERE d.organization_id = $1
		  AND (LOWER(COALESCE(dw.title, '')) LIKE '%' || LOWER($2) || '%'
		       OR LOWER(dw.config->>'field') LIKE '%' || LOWER($2) || '%'
		       OR LOWER(dw.widget_type)      LIKE '%' || LOWER($2) || '%')
	`
	args2 := []any{orgID, term}
	if dn := strings.TrimSpace(args.DashboardName); dn != "" {
		query += " AND LOWER(d.name) = LOWER($3)"
		args2 = append(args2, dn)
	}
	query += " ORDER BY d.created_at DESC LIMIT 1"

	var widgetID, widgetTitle string
	err := database.Pool.QueryRow(ctx, query, args2...).Scan(&widgetID, &widgetTitle)
	if err != nil {
		return LocateWidgetResult{Found: false, Summary: fmt.Sprintf("No widget matching %q found.", term)}, nil
	}
	return LocateWidgetResult{
		Found:       true,
		WidgetID:    widgetID,
		WidgetTitle: widgetTitle,
		Summary:     fmt.Sprintf("Found widget %q — highlighting it on your canvas.", widgetTitle),
	}, nil
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
