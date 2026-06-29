package ai

var allowedWidgetTypes = []string{
	"line-chart", "gauge", "kpi-card", "status-card", "table", "alarm-panel", "daily-count",
}

// machineIDProp is the shared schema for a required machine_id slot. Read-only and
// reused across read/alert tools — nudges the model to ask rather than guess a name.
var machineIDProp = map[string]any{
	"type":        "string",
	"description": "Machine name (e.g. CW-01). Ask user if unknown — never guess.",
}

// widgetItemSchema is used by add_widget_to_dashboard only.
var widgetItemSchema = map[string]any{
	"type":     "object",
	"required": []string{"type"},
	"properties": map[string]any{
		"type":       map[string]any{"type": "string"},
		"title":      map[string]any{"type": "string"},
		"machine_id": map[string]any{"type": "string"},
		"metric":     map[string]any{"type": "string"},
		"min":        map[string]any{"type": "number"},
		"max":        map[string]any{"type": "number"},
		"unit":       map[string]any{"type": "string"},
	},
}

// templateDashboardInput is the minimal schema for template-based dashboard creation.
var templateDashboardInput = map[string]any{
	"type":     "object",
	"required": []string{"machine", "template"},
	"properties": map[string]any{
		"machine":  map[string]any{"type": "string"},
		"template": map[string]any{"type": "string", "enum": []string{"machine_overview", "machine_production", "machine_maintenance"}},
	},
}

var GetMachinesTool = map[string]any{
	"name":         "get_machines",
	"description":  "List all machines with their names, types, status, and available metric fields.",
	"input_schema": map[string]any{"type": "object", "properties": map[string]any{}},
}

var ShowMetricTool = map[string]any{
	"name":        "show_metric",
	"description":  "Show one metric as a live widget for the user — call this whenever they ask the current value of, or to see, a metric. Returns a widget the UI renders.",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"machine", "metric"},
		"properties": map[string]any{
			"machine": machineIDProp,
			"metric":  map[string]any{"type": "string", "description": "The English field key (e.g. speed, weight, temp). Map the user's word in any language to it."},
			"viz":     map[string]any{"type": "string", "enum": []string{"value", "gauge", "trend"}, "description": "value = current number, gauge = dial, trend = line chart over time."},
		},
	},
}

var GetTelemetryTrendTool = map[string]any{
	"name":        "get_telemetry_trend",
	"description": "Get avg/min/max of one metric over a time window (5m…30d).",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"machine_id", "metric"},
		"properties": map[string]any{
			"machine_id": machineIDProp,
			"metric":     map[string]any{"type": "string"},
			"time_range": map[string]any{"type": "string", "enum": []string{"5m", "15m", "30m", "1h", "6h", "24h", "7d", "15d", "30d"}},
		},
	},
}

var GetActiveAlertsTool = map[string]any{
	"name":         "get_active_alerts",
	"description":  "List all open alert events (each has an event_id for ack/resolve).",
	"input_schema": map[string]any{"type": "object", "properties": map[string]any{}},
}

var GetDailyCountTool = map[string]any{
	"name":        "get_daily_count",
	"description": "Get per-day production count for one machine.",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"machine_id"},
		"properties": map[string]any{
			"machine_id": machineIDProp,
			"days":       map[string]any{"type": "integer"},
		},
	},
}

var ListDashboardsTool = map[string]any{
	"name":         "list_dashboards",
	"description":  "List existing dashboards with names and widget counts.",
	"input_schema": map[string]any{"type": "object", "properties": map[string]any{}},
}

var PreviewDashboardTool = map[string]any{
	"name":         "preview_dashboard",
	"description":  "STEP 1: Preview a template dashboard. Call first; do NOT call create_custom_dashboard the same turn.",
	"input_schema": templateDashboardInput,
}

var CreateDashboardTool = map[string]any{
	"name":         "create_custom_dashboard",
	"description":  "STEP 2: Create the dashboard, only after the user confirms the preview.",
	"input_schema": templateDashboardInput,
}

var PreviewAddWidgetTool = map[string]any{
	"name":        "preview_add_widget",
	"description": "Add a widget to the in-progress preview plan (no DB write).",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"machine", "widget"},
		"properties": map[string]any{
			"machine": map[string]any{"type": "string"},
			"widget":  widgetItemSchema,
		},
	},
}

var PreviewRemoveWidgetTool = map[string]any{
	"name":        "preview_remove_widget",
	"description": "Remove a widget from the in-progress preview plan (no DB write).",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"widget_title"},
		"properties": map[string]any{
			"widget_title": map[string]any{"type": "string"},
		},
	},
}

var PreviewUpdateWidgetTool = map[string]any{
	"name":        "preview_update_widget",
	"description": "Edit a widget in the in-progress preview plan (no DB write). Locate it by its current title; pass only the fields to change.",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"widget_title"},
		"properties": map[string]any{
			"widget_title": map[string]any{"type": "string", "description": "Current title of the widget to edit."},
			"new_title":    map[string]any{"type": "string"},
			"type":         map[string]any{"type": "string"},
			"metric":       map[string]any{"type": "string"},
			"unit":         map[string]any{"type": "string"},
			"min":          map[string]any{"type": "number"},
			"max":          map[string]any{"type": "number"},
			"start_date":   map[string]any{"type": "string", "description": "Absolute window start as YYYY-MM-DD (chart widgets). Convert any DD/MM/YYYY the user gives."},
			"end_date":     map[string]any{"type": "string", "description": "Absolute window end as YYYY-MM-DD (chart widgets)."},
			"bucket":       map[string]any{"type": "string", "description": "Time bucket size for count widgets, e.g. '25m', '1h', '1d'. Format: <number><m|h|d>."},
			"sku":          map[string]any{"type": "string", "description": "SKU filter for count widgets (empty = all SKUs)."},
			"status":       map[string]any{"type": "string", "enum": []string{"all", "good", "reject"}, "description": "Piece status filter for count widgets."},
		},
	},
}

var AddWidgetTool = map[string]any{
	"name":        "add_widget_to_dashboard",
	"description": "Add one widget to an EXISTING dashboard (by name).",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"dashboard_name", "widget"},
		"properties": map[string]any{
			"dashboard_name": map[string]any{"type": "string"},
			"widget":         widgetItemSchema,
		},
	},
}

var RemoveWidgetTool = map[string]any{
	"name":        "remove_widget",
	"description": "Remove a widget from a dashboard by its title.",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"dashboard_name", "widget_title"},
		"properties": map[string]any{
			"dashboard_name": map[string]any{"type": "string"},
			"widget_title":   map[string]any{"type": "string"},
		},
	},
}

var CreateAlertTool = map[string]any{
	"name":        "create_alert",
	"description": "Create a threshold alert rule on a machine metric.",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"machine_id", "metric", "condition", "threshold"},
		"properties": map[string]any{
			"machine_id":   machineIDProp,
			"metric":       map[string]any{"type": "string"},
			"condition":    map[string]any{"type": "string", "enum": []string{"gt", "lt", "gte", "lte", "eq", "neq", "between", "outside"}},
			"threshold":    map[string]any{"type": "number"},
			"threshold_hi": map[string]any{"type": "number"},
			"severity":     map[string]any{"type": "string", "enum": []string{"info", "warning", "critical"}},
			"name":         map[string]any{"type": "string"},
			"cooldown_sec": map[string]any{"type": "integer"},
		},
	},
}

var ManageAlertEventTool = map[string]any{
	"name":        "manage_alert_event",
	"description": "Acknowledge or resolve an open alert event. action: ack | resolve.",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"event_id", "action"},
		"properties": map[string]any{
			"event_id": map[string]any{"type": "string"},
			"action":   map[string]any{"type": "string", "enum": []string{"ack", "resolve"}},
		},
	},
}

// AllTools is the complete set handed to the LLM and exposed via GET /api/ai/tools.
func AllTools() []map[string]any {
	return []map[string]any{
		GetMachinesTool,
		ShowMetricTool,
		GetTelemetryTrendTool,
		GetActiveAlertsTool,
		GetDailyCountTool,
		ListDashboardsTool,
		PreviewDashboardTool,
		PreviewAddWidgetTool,
		PreviewRemoveWidgetTool,
		PreviewUpdateWidgetTool,
		AddWidgetTool,
		RemoveWidgetTool,
		CreateAlertTool,
		ManageAlertEventTool,
	}
}

// writeTools are the mutating tools that require admin/editor role.
var writeTools = map[string]bool{
	"create_custom_dashboard": true,
	"add_widget_to_dashboard": true,
	"remove_widget":           true,
	"create_alert":            true,
	"manage_alert_event":      true,
}

func isWriteTool(name string) bool { return writeTools[name] }

// ── Tool argument structs ─────────────────────────────────────────────────────

type ToolWidget struct {
	Type      string   `json:"type"`
	Title     string   `json:"title"`
	MachineID string   `json:"machine_id"`
	Metric    string   `json:"metric"`
	Min       *float64 `json:"min"`
	Max       *float64 `json:"max"`
	Unit      string   `json:"unit"`
}

// TemplateDashboardArgs is the minimal payload for preview/create via template.
type TemplateDashboardArgs struct {
	Machine  string          `json:"machine"`
	Template string          `json:"template"`
	Name     string          `json:"name,omitempty"`    // user-edited dashboard name
	Widgets  []PreviewWidget `json:"widgets,omitempty"` // optional override from preview plan
}

type ShowMetricArgs struct {
	Machine string `json:"machine"`
	Metric  string `json:"metric"`
	Viz     string `json:"viz"`
}

type TrendArgs struct {
	MachineID string `json:"machine_id"`
	Metric    string `json:"metric"`
	TimeRange string `json:"time_range"`
}

type DailyCountArgs struct {
	MachineID string `json:"machine_id"`
	Days      int    `json:"days"`
}

type AddWidgetArgs struct {
	DashboardName string     `json:"dashboard_name"`
	Widget        ToolWidget `json:"widget"`
}

type RemoveWidgetArgs struct {
	DashboardName string `json:"dashboard_name"`
	WidgetTitle   string `json:"widget_title"`
}

type CreateAlertArgs struct {
	MachineID   string   `json:"machine_id"`
	Name        string   `json:"name"`
	Metric      string   `json:"metric"`
	Condition   string   `json:"condition"`
	Threshold   float64  `json:"threshold"`
	ThresholdHi *float64 `json:"threshold_hi"`
	Severity    string   `json:"severity"`
	CooldownSec *int     `json:"cooldown_sec"`
}

type AlertEventArg struct {
	EventID string `json:"event_id"`
}

// ── Preview types ─────────────────────────────────────────────────────────────

type PreviewWidget struct {
	Type        string  `json:"type"`
	Title       string  `json:"title"`
	Machine     string  `json:"machine"`
	MachineUUID string  `json:"machineUuid,omitempty"` // resolved UUID — enables live data in preview
	Metric        string  `json:"metric"`
	Unit          string  `json:"unit"`
	Min           float64 `json:"min,omitempty"`
	Max           float64 `json:"max,omitempty"`
	StartDateTime string  `json:"startDateTime,omitempty"` // absolute window start (datetime-local) for chart widgets
	EndDateTime   string  `json:"endDateTime,omitempty"`
	Bucket        string  `json:"bucket,omitempty"` // count widget: bucket size, e.g. "30m"
	Sku           string  `json:"sku,omitempty"`    // count widget: SKU filter ("" = all)
	Status        string  `json:"status,omitempty"` // count widget: all | good | reject
}

type PreviewDashboardResult struct {
	Preview       bool            `json:"preview"`
	DashboardName string          `json:"dashboardName"`
	Widgets       []PreviewWidget `json:"widgets"`
	Summary       string          `json:"summary"`
}

