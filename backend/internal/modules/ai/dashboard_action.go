package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"iot-dashboard/internal/database"
	"iot-dashboard/internal/middleware"
	"iot-dashboard/internal/modules/dashboards"
)

type DashboardAction struct {
	dash *dashboards.Service
}

func NewDashboardAction() *DashboardAction {
	return &DashboardAction{dash: dashboards.NewService()}
}

// ToolResult is returned to the LLM and surfaced to the frontend as msg.toolResult.
type ToolResult struct {
	Success     bool   `json:"success"`
	DashboardID string `json:"dashboardId,omitempty"`
	URL         string `json:"url,omitempty"`
	Summary     string `json:"summary"`
}

// ── Templates ─────────────────────────────────────────────────────────────────

type templateWidgetDef struct {
	widgetType     string
	title          string
	preferredField string // try this metric key first; falls back to first machine field
	unit           string
	min, max       float64
}

var dashboardTemplates = map[string][]templateWidgetDef{
	"machine_overview": {
		{widgetType: "kpi-card",   title: "Speed",       preferredField: "speed",      unit: "rpm"},
		{widgetType: "gauge",      title: "Speed Gauge",  preferredField: "speed",      unit: "rpm", max: 3000},
		{widgetType: "kpi-card",   title: "Throughput",   preferredField: "throughput"},
		{widgetType: "line-chart", title: "Trend",        preferredField: "speed"},
	},
	"machine_production": {
		{widgetType: "kpi-card",    title: "Count",  preferredField: "count"},
		{widgetType: "line-chart",  title: "Output", preferredField: "throughput"},
		{widgetType: "status-card", title: "Status"},
	},
	"machine_maintenance": {
		{widgetType: "alarm-panel", title: "Alarms"},
		{widgetType: "daily-count", title: "Downtime"},
		{widgetType: "table",       title: "History"},
	},
}

// expandTemplate converts a template definition + machine fields into concrete ToolWidgets.
func expandTemplate(defs []templateWidgetDef, machineName string, fields []string) []ToolWidget {
	fieldSet := make(map[string]bool, len(fields))
	for _, f := range fields {
		fieldSet[f] = true
	}
	firstField := ""
	if len(fields) > 0 {
		firstField = fields[0]
	}

	out := make([]ToolWidget, 0, len(defs))
	for _, def := range defs {
		w := ToolWidget{
			Type:      def.widgetType,
			Title:     def.title,
			MachineID: machineName,
			Unit:      def.unit,
		}
		if def.preferredField != "" {
			if fieldSet[def.preferredField] {
				w.Metric = def.preferredField
			} else if firstField != "" {
				w.Metric = firstField
			}
		}
		if def.max != 0 {
			max := def.max
			w.Max = &max
		}
		if def.min != 0 {
			min := def.min
			w.Min = &min
		}
		out = append(out, w)
	}
	return out
}

// getMachineFieldsForMachine returns numeric field keys for a machine UUID.
func getMachineFieldsForMachine(ctx context.Context, machineID string) []string {
	rows, err := database.Pool.Query(ctx,
		`SELECT key FROM machine_fields WHERE machine_id = $1 AND data_type = 'number' ORDER BY key`,
		machineID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var fields []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err == nil {
			fields = append(fields, k)
		}
	}
	return fields
}

func templateDashboardName(template, machine string) string {
	label := strings.ReplaceAll(template, "_", " ")
	words := strings.Fields(label)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ") + " — " + machine
}

// Handle creates the dashboard from a template (or custom widget list). Called only after user confirms Preview.
func (a *DashboardAction) Handle(ctx context.Context, orgID, userID string, rawArgs json.RawMessage) (ToolResult, error) {
	var args TemplateDashboardArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return ToolResult{}, middleware.NewAppError(400, "VALIDATION_ERROR", "Malformed tool arguments")
	}

	dashName := templateDashboardName(args.Template, args.Machine)
	if n := strings.TrimSpace(args.Name); n != "" {
		dashName = n
	}
	dash, err := a.dash.CreateDashboard(ctx, orgID, userID, dashName, nil, false, nil)
	if err != nil {
		return ToolResult{}, err
	}

	created := 0

	if len(args.Widgets) > 0 {
		// Use the custom widget list from the preview plan (may include user-added widgets).
		for _, pw := range args.Widgets {
			if !isAllowedType(pw.Type) {
				continue
			}
			cfg := map[string]any{}
			if pw.Metric != "" {
				cfg["field"] = pw.Metric
			}
			if pw.Unit != "" {
				cfg["unit"] = pw.Unit
			}
			if pw.Type == "gauge" {
				if pw.Min != 0 {
					cfg["min"] = pw.Min
				}
				if pw.Max != 0 {
					cfg["max"] = pw.Max
				}
			}
			if pw.Type == "line-chart" {
				// Absolute date window takes precedence over the live (rolling) window.
				if pw.StartDateTime != "" || pw.EndDateTime != "" {
					cfg["liveMode"] = false
					if pw.StartDateTime != "" {
						cfg["startDateTime"] = pw.StartDateTime
					}
					if pw.EndDateTime != "" {
						cfg["endDateTime"] = pw.EndDateTime
					}
				} else {
					cfg["liveMode"] = true
				}
			}
			cfgJSON, _ := json.Marshal(cfg)
			mid := pw.MachineUUID
			widget := dashboards.Widget{
				WidgetType: pw.Type,
				Layout:     flowLayout(created),
				MachineID:  &mid,
				Config:     cfgJSON,
			}
			if t := strings.TrimSpace(pw.Title); t != "" {
				widget.Title = &t
			}
			if _, err := a.dash.AddWidget(ctx, dash.ID, orgID, widget); err != nil {
				return ToolResult{}, err
			}
			created++
		}
	} else {
		// Template path.
		defs, ok := dashboardTemplates[args.Template]
		if !ok {
			return ToolResult{}, middleware.NewAppError(400, "VALIDATION_ERROR", fmt.Sprintf("unknown template %q", args.Template))
		}
		machineID, found := resolveMachineID(ctx, orgID, strings.TrimSpace(args.Machine))
		if !found {
			return ToolResult{}, middleware.NewAppError(404, "NOT_FOUND", fmt.Sprintf("machine %q not found", args.Machine))
		}
		fields := getMachineFieldsForMachine(ctx, machineID)
		for _, w := range expandTemplate(defs, args.Machine, fields) {
			if !isAllowedType(w.Type) {
				continue
			}
			widget := dashboards.Widget{
				WidgetType: w.Type,
				Layout:     flowLayout(created),
				MachineID:  &machineID,
				Config:     buildConfig(w),
			}
			if t := strings.TrimSpace(w.Title); t != "" {
				widget.Title = &t
			}
			if _, err := a.dash.AddWidget(ctx, dash.ID, orgID, widget); err != nil {
				return ToolResult{}, err
			}
			created++
		}
	}

	return ToolResult{
		Success:     true,
		DashboardID: dash.ID,
		URL:         "/dashboards/" + dash.ID,
		Summary:     fmt.Sprintf("Created %q with %d widget(s).", dashName, created),
	}, nil
}

// PreviewAddWidget validates a widget spec and returns a PreviewWidget without any DB write.
func (a *DashboardAction) PreviewAddWidget(ctx context.Context, orgID string, rawArgs json.RawMessage) (PreviewWidget, error) {
	var args struct {
		Machine string     `json:"machine"`
		Widget  ToolWidget `json:"widget"`
	}
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return PreviewWidget{}, middleware.NewAppError(400, "VALIDATION_ERROR", "Malformed tool arguments")
	}
	if !isAllowedType(args.Widget.Type) {
		return PreviewWidget{}, middleware.NewAppError(400, "VALIDATION_ERROR", fmt.Sprintf("unknown widget type %q", args.Widget.Type))
	}
	machineID, found := resolveMachineID(ctx, orgID, strings.TrimSpace(args.Machine))
	if !found {
		return PreviewWidget{}, middleware.NewAppError(404, "NOT_FOUND", fmt.Sprintf("machine %q not found", args.Machine))
	}
	pw := PreviewWidget{
		Type:        args.Widget.Type,
		Title:       args.Widget.Title,
		Machine:     args.Machine,
		MachineUUID: machineID,
		Metric:      strings.TrimSpace(args.Widget.Metric),
		Unit:        args.Widget.Unit,
	}
	if args.Widget.Min != nil {
		pw.Min = *args.Widget.Min
	}
	if args.Widget.Max != nil {
		pw.Max = *args.Widget.Max
	}
	return pw, nil
}

// PreviewUpdateWidget returns the partial changes to apply to a preview widget
// (located client-side by title). No DB write — only the provided fields are returned.
func (a *DashboardAction) PreviewUpdateWidget(ctx context.Context, orgID string, rawArgs json.RawMessage) (map[string]any, error) {
	var args struct {
		WidgetTitle string   `json:"widget_title"`
		NewTitle    string   `json:"new_title"`
		Type        string   `json:"type"`
		Metric      string   `json:"metric"`
		Unit        string   `json:"unit"`
		Min         *float64 `json:"min"`
		Max         *float64 `json:"max"`
		StartDate   string   `json:"start_date"`
		EndDate     string   `json:"end_date"`
	}
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return nil, middleware.NewAppError(400, "VALIDATION_ERROR", "Malformed tool arguments")
	}
	if strings.TrimSpace(args.WidgetTitle) == "" {
		return nil, middleware.NewAppError(400, "VALIDATION_ERROR", "widget_title is required")
	}

	changes := map[string]any{}
	if strings.TrimSpace(args.NewTitle) != "" {
		changes["title"] = args.NewTitle
	}
	if t := strings.TrimSpace(args.Type); t != "" {
		if !isAllowedType(t) {
			return nil, middleware.NewAppError(400, "VALIDATION_ERROR", fmt.Sprintf("unknown widget type %q", t))
		}
		changes["type"] = t
	}
	if m := strings.TrimSpace(args.Metric); m != "" {
		changes["metric"] = m
	}
	if args.Unit != "" {
		changes["unit"] = args.Unit
	}
	if args.Min != nil {
		changes["min"] = *args.Min
	}
	if args.Max != nil {
		changes["max"] = *args.Max
	}
	if s := strings.TrimSpace(args.StartDate); s != "" {
		changes["startDateTime"] = toDatetimeLocal(s, false)
	}
	if e := strings.TrimSpace(args.EndDate); e != "" {
		changes["endDateTime"] = toDatetimeLocal(e, true)
	}

	return map[string]any{
		"updated":     true,
		"widgetTitle": args.WidgetTitle,
		"changes":     changes,
	}, nil
}

// Preview builds the dashboard plan without any DB writes.
func (a *DashboardAction) Preview(ctx context.Context, orgID string, rawArgs json.RawMessage) (PreviewDashboardResult, error) {
	var args TemplateDashboardArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return PreviewDashboardResult{}, middleware.NewAppError(400, "VALIDATION_ERROR", "Malformed tool arguments")
	}
	defs, ok := dashboardTemplates[args.Template]
	if !ok {
		return PreviewDashboardResult{}, middleware.NewAppError(400, "VALIDATION_ERROR", fmt.Sprintf("unknown template %q", args.Template))
	}

	machineID, found := resolveMachineID(ctx, orgID, strings.TrimSpace(args.Machine))
	if !found {
		return PreviewDashboardResult{}, middleware.NewAppError(404, "NOT_FOUND", fmt.Sprintf("machine %q not found", args.Machine))
	}

	fields := getMachineFieldsForMachine(ctx, machineID)
	widgets := expandTemplate(defs, args.Machine, fields)
	dashName := templateDashboardName(args.Template, args.Machine)

	previewWidgets := make([]PreviewWidget, 0, len(widgets))
	for _, w := range widgets {
		pw := PreviewWidget{
			Type:        w.Type,
			Title:       w.Title,
			Machine:     args.Machine,
			MachineUUID: machineID,
			Metric:      w.Metric,
			Unit:        w.Unit,
		}
		if w.Max != nil {
			pw.Max = *w.Max
		}
		if w.Min != nil {
			pw.Min = *w.Min
		}
		previewWidgets = append(previewWidgets, pw)
	}

	return PreviewDashboardResult{
		Preview:       true,
		DashboardName: dashName,
		Widgets:       previewWidgets,
		Summary:       fmt.Sprintf("Will create %q with %d widget(s).", dashName, len(previewWidgets)),
	}, nil
}

// ── Layout & config helpers ───────────────────────────────────────────────────

func flowLayout(index int) json.RawMessage {
	const w, h, cols = 6, 4, 12
	perRow := cols / w
	x := (index % perRow) * w
	y := (index / perRow) * h
	b, _ := json.Marshal(map[string]int{"x": x, "y": y, "w": w, "h": h})
	return b
}

func buildConfig(w ToolWidget) json.RawMessage {
	cfg := map[string]any{}
	if m := strings.TrimSpace(w.Metric); m != "" {
		cfg["field"] = m
	}
	if w.Unit != "" {
		cfg["unit"] = w.Unit
	}
	if w.Type == "gauge" {
		if w.Min != nil {
			cfg["min"] = *w.Min
		}
		if w.Max != nil {
			cfg["max"] = *w.Max
		}
	}
	if w.Type == "line-chart" {
		cfg["liveMode"] = true
	}
	b, _ := json.Marshal(cfg)
	return b
}

// toDatetimeLocal normalizes a model-supplied date into a "YYYY-MM-DDTHH:MM"
// datetime-local string (what LineChartWidget's config.startDateTime expects).
// A date-only value expands to start-of-day, or end-of-day when endOfDay is set.
func toDatetimeLocal(s string, endOfDay bool) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) == 10 { // YYYY-MM-DD
		if endOfDay {
			return s + "T23:59"
		}
		return s + "T00:00"
	}
	s = strings.TrimSuffix(s, "Z") // datetime-local has no timezone
	if i := strings.Index(s, "T"); i >= 0 && len(s) >= 16 {
		return s[:16]
	}
	return s
}

func isAllowedType(t string) bool {
	for _, a := range allowedWidgetTypes {
		if a == t {
			return true
		}
	}
	return false
}

type machineInfo struct {
	Name   string   `json:"name"`
	Type   string   `json:"type"`
	Status string   `json:"status"`
	Fields []string `json:"fields"`
}

func getMachinesForOrg(ctx context.Context, orgID string) ([]machineInfo, error) {
	rows, err := database.Pool.Query(ctx, `
		SELECT m.name, m.type, m.status,
		       COALESCE(
		           array_agg(mf.key ORDER BY mf.key) FILTER (WHERE mf.id IS NOT NULL AND mf.data_type = 'number'),
		           '{}'::text[]
		       ) AS field_keys
		FROM machines m
		LEFT JOIN machine_fields mf ON mf.machine_id = m.id
		JOIN production_lines pl ON pl.id = m.production_line_id
		JOIN factories f ON f.id = pl.factory_id
		WHERE f.organization_id = $1
		GROUP BY m.id, m.name, m.type, m.status
		ORDER BY m.name
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var machines []machineInfo
	for rows.Next() {
		var m machineInfo
		if err := rows.Scan(&m.Name, &m.Type, &m.Status, &m.Fields); err != nil {
			return nil, err
		}
		machines = append(machines, m)
	}
	return machines, nil
}

func resolveMachineID(ctx context.Context, orgID, name string) (string, bool) {
	var id string
	err := database.Pool.QueryRow(ctx, `
		SELECT m.id
		FROM machines m
		JOIN production_lines pl ON pl.id = m.production_line_id
		JOIN factories f ON f.id = pl.factory_id
		WHERE f.organization_id = $1
		  AND LOWER(m.name) LIKE '%' || LOWER($2) || '%'
		ORDER BY
		  CASE WHEN LOWER(m.name) = LOWER($2) THEN 0 ELSE 1 END,
		  m.name
		LIMIT 1
	`, orgID, name).Scan(&id)
	if err != nil {
		return "", false
	}
	return id, true
}
