package ai

var allowedWidgetTypes = []string{
	"line-chart", "gauge", "kpi-card", "status-card", "table", "alarm-panel", "daily-count",
}

// widgetItemSchema is the per-widget object shared by create/preview tools.
// ponytail: enum removed from nested items.type — Groq's parser fails on enums inside array items schemas.
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

var dashboardWidgetsInput = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"dashboard_name": map[string]any{"type": "string"},
		"widgets": map[string]any{
			"type":  "array",
			"items": widgetItemSchema,
		},
	},
	"required": []string{"dashboard_name", "widgets"},
}

var GetMachinesTool = map[string]any{
	"name":         "get_machines",
	"description":  "List all machines with their names, types, status, and available metric fields.",
	"input_schema": map[string]any{"type": "object", "properties": map[string]any{}},
}

var GetLatestTelemetryTool = map[string]any{
	"name":        "get_latest_telemetry",
	"description": "Get current sensor readings for one machine.",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"machine_id"},
		"properties": map[string]any{
			"machine_id": map[string]any{"type": "string"},
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
			"machine_id": map[string]any{"type": "string"},
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
			"machine_id": map[string]any{"type": "string"},
			"days":       map[string]any{"type": "integer"},
		},
	},
}

var GetFactoryOverviewTool = map[string]any{
	"name":         "get_factory_overview",
	"description":  "Snapshot of every machine: status, latest values, open-alert count. Use for broad 'what's wrong' questions.",
	"input_schema": map[string]any{"type": "object", "properties": map[string]any{}},
}

var ListDashboardsTool = map[string]any{
	"name":         "list_dashboards",
	"description":  "List existing dashboards with names and widget counts.",
	"input_schema": map[string]any{"type": "object", "properties": map[string]any{}},
}

var LocateWidgetTool = map[string]any{
	"name":        "locate_widget",
	"description": "Find a widget on the canvas by title, metric, or machine name so the UI can spotlight it.",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"widget_title"},
		"properties": map[string]any{
			"widget_title":   map[string]any{"type": "string"},
			"dashboard_name": map[string]any{"type": "string"},
		},
	},
}

var PreviewDashboardTool = map[string]any{
	"name":         "preview_dashboard",
	"description":  "Plan a dashboard WITHOUT creating it. Always call this first; ask user to confirm before calling create_custom_dashboard.",
	"input_schema": dashboardWidgetsInput,
}

var CreateDashboardTool = map[string]any{
	"name":         "create_custom_dashboard",
	"description":  "Create a new dashboard. Only call after the user confirms the preview_dashboard plan.",
	"input_schema": dashboardWidgetsInput,
}

var AddWidgetTool = map[string]any{
	"name":        "add_widget_to_dashboard",
	"description": "Add one widget to an existing dashboard (by name).",
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
			"machine_id":   map[string]any{"type": "string"},
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

var AcknowledgeAlertTool = map[string]any{
	"name":        "acknowledge_alert",
	"description": "Acknowledge an open alert event by event_id.",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"event_id"},
		"properties": map[string]any{
			"event_id": map[string]any{"type": "string"},
		},
	},
}

var ResolveAlertTool = map[string]any{
	"name":        "resolve_alert",
	"description": "Resolve (close) an open alert event by event_id.",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"event_id"},
		"properties": map[string]any{
			"event_id": map[string]any{"type": "string"},
		},
	},
}

// AllTools is the complete set handed to the LLM and exposed via GET /api/ai/tools.
func AllTools() []map[string]any {
	return []map[string]any{
		GetMachinesTool,
		GetLatestTelemetryTool,
		GetTelemetryTrendTool,
		GetActiveAlertsTool,
		GetDailyCountTool,
		GetFactoryOverviewTool,
		ListDashboardsTool,
		LocateWidgetTool,
		PreviewDashboardTool,
		CreateDashboardTool,
		AddWidgetTool,
		RemoveWidgetTool,
		CreateAlertTool,
		AcknowledgeAlertTool,
		ResolveAlertTool,
	}
}

// writeTools are the mutating tools that require admin/editor role.
var writeTools = map[string]bool{
	"create_custom_dashboard": true,
	"add_widget_to_dashboard": true,
	"remove_widget":           true,
	"create_alert":            true,
	"acknowledge_alert":       true,
	"resolve_alert":           true,
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

type CreateDashboardArgs struct {
	DashboardName string       `json:"dashboard_name"`
	Widgets       []ToolWidget `json:"widgets"`
}

type MachineArg struct {
	MachineID string `json:"machine_id"`
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
	Metric      string  `json:"metric"`
	Unit        string  `json:"unit"`
	Min         float64 `json:"min,omitempty"`
	Max         float64 `json:"max,omitempty"`
}

type PreviewDashboardResult struct {
	Preview       bool            `json:"preview"`
	DashboardName string          `json:"dashboardName"`
	Widgets       []PreviewWidget `json:"widgets"`
	Summary       string          `json:"summary"`
}

// ── Locate type ───────────────────────────────────────────────────────────────

type LocateWidgetResult struct {
	Found       bool   `json:"found"`
	WidgetID    string `json:"widgetId,omitempty"`
	WidgetTitle string `json:"widgetTitle,omitempty"`
	Summary     string `json:"summary"`
}
