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

// DashboardAction turns a parsed create_custom_dashboard tool call into real
// dashboard + widget rows, reusing the existing dashboards.Service (no new SQL
// for the inserts themselves — only a read to resolve machine names → UUIDs).
type DashboardAction struct {
	dash *dashboards.Service
}

func NewDashboardAction() *DashboardAction {
	return &DashboardAction{dash: dashboards.NewService()}
}

// ToolResult is the structured payload returned to the caller. It is fed back to
// the LLM as the tool_result and surfaced to the frontend as msg.toolResult, so
// the UI can render a confirmation card + link without ever showing raw JSON.
type ToolResult struct {
	Success     bool   `json:"success"`
	DashboardID string `json:"dashboardId,omitempty"`
	URL         string `json:"url,omitempty"`
	Summary     string `json:"summary"`
}

// Handle parses the raw tool arguments emitted by the LLM and builds the dashboard.
// rawArgs is the JSON from tool_use.input (Anthropic) / function_call.arguments (OpenAI),
// or the `params` object when called directly via POST /api/ai/tools/execute.
func (a *DashboardAction) Handle(ctx context.Context, orgID, userID string, rawArgs json.RawMessage) (ToolResult, error) {
	var args CreateDashboardArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return ToolResult{}, middleware.NewAppError(400, "VALIDATION_ERROR", "Malformed tool arguments")
	}
	if strings.TrimSpace(args.DashboardName) == "" || len(args.Widgets) == 0 {
		return ToolResult{}, middleware.NewAppError(400, "VALIDATION_ERROR", "dashboard_name and at least one widget are required")
	}

	// 1. Create the dashboard shell via the existing service (private by default).
	dash, err := a.dash.CreateDashboard(ctx, orgID, userID, args.DashboardName, nil, false, nil)
	if err != nil {
		return ToolResult{}, err
	}

	// 2. Resolve + persist each widget through the existing AddWidget path.
	created := 0
	for _, w := range args.Widgets {
		if !isAllowedType(w.Type) {
			continue // skip anything the LLM hallucinated outside the enum
		}

		widget := dashboards.Widget{
			WidgetType: w.Type,
			Layout:     flowLayout(created), // deterministic 2-col grid so widgets render immediately
		}
		if t := strings.TrimSpace(w.Title); t != "" {
			widget.Title = &t
		}

		// Resolve the machine NAME the LLM used → internal UUID (org-scoped).
		if name := strings.TrimSpace(w.MachineID); name != "" {
			if id, ok := resolveMachineID(ctx, orgID, name); ok {
				widget.MachineID = &id
			}
		}

		widget.Config = buildConfig(w)

		if _, err := a.dash.AddWidget(ctx, dash.ID, orgID, widget); err != nil {
			return ToolResult{}, err
		}
		created++
	}

	return ToolResult{
		Success:     true,
		DashboardID: dash.ID,
		URL:         "/dashboards/" + dash.ID,
		Summary:     fmt.Sprintf("Created dashboard %q with %d widget(s).", args.DashboardName, created),
	}, nil
}

// flowLayout positions widgets in a 12-col GridStack as a 2-column grid
// (w:6 h:4), matching the size the editor uses when adding a widget by hand.
func flowLayout(index int) json.RawMessage {
	const w, h, cols = 6, 4, 12
	perRow := cols / w
	x := (index % perRow) * w
	y := (index / perRow) * h
	b, _ := json.Marshal(map[string]int{"x": x, "y": y, "w": w, "h": h})
	return b
}

// buildConfig maps the tool's flat metric/min/max/unit onto the WidgetConfig shape
// the frontend widgets read (config.field is the metric key).
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
	b, _ := json.Marshal(cfg)
	return b
}

func isAllowedType(t string) bool {
	for _, a := range allowedWidgetTypes {
		if a == t {
			return true
		}
	}
	return false
}

// resolveMachineID does a case-insensitive, org-scoped name lookup.
// machines → production_lines → factories carries the organization_id.
// Returns (id, true) on a unique match.
func resolveMachineID(ctx context.Context, orgID, name string) (string, bool) {
	var id string
	err := database.Pool.QueryRow(ctx, `
		SELECT m.id
		FROM machines m
		JOIN production_lines pl ON pl.id = m.production_line_id
		JOIN factories f ON f.id = pl.factory_id
		WHERE f.organization_id = $1 AND LOWER(m.name) = LOWER($2)
		LIMIT 1
	`, orgID, name).Scan(&id)
	if err != nil {
		return "", false
	}
	return id, true
}
