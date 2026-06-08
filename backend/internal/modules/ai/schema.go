package ai

// allowedWidgetTypes is the single source of truth for which widgets the AI may
// place on a dashboard. These strings MUST stay in sync with the frontend
// WidgetType union (frontend/src/types/index.ts) and the widgetComponents map in
// useWidgetComponents.ts, so anything the LLM emits renders with no extra mapping.
var allowedWidgetTypes = []string{
	"line-chart", "gauge", "kpi-card", "status-card", "table", "alarm-panel", "daily-count",
}

// CreateDashboardTool is the JSON schema handed to the LLM (Anthropic `tools` /
// OpenAI `functions`). input_schema is plain JSON Schema. Defining it here keeps
// the enum from drifting away from the widgets we can actually render.
var CreateDashboardTool = map[string]any{
	"name": "create_custom_dashboard",
	"description": "Create a new dashboard composed of pre-built widgets based on the " +
		"user's monitoring request. Use the machine's human name in `machine_id`; the " +
		"backend resolves it to an internal id. Choose the widget `type` that best fits " +
		"the request (line-chart for trends, gauge for a single bounded metric, kpi-card " +
		"for a headline number, daily-count for production totals).",
	"input_schema": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dashboard_name": map[string]any{
				"type":        "string",
				"description": "Concise title for the dashboard, e.g. 'CNC Temperature Monitor'.",
			},
			"widgets": map[string]any{
				"type":        "array",
				"minItems":    1,
				"description": "Widgets to place on the dashboard.",
				"items": map[string]any{
					"type":     "object",
					"required": []string{"type"},
					"properties": map[string]any{
						"type": map[string]any{
							"type":        "string",
							"enum":        allowedWidgetTypes,
							"description": "Which pre-built widget component to render.",
						},
						"title": map[string]any{
							"type":        "string",
							"description": "Optional widget header. Defaults to '<machine> — <metric>'.",
						},
						"machine_id": map[string]any{
							"type":        "string",
							"description": "Machine name as the user referred to it (e.g. 'CNC Mill 01'). Optional for alarm-panel.",
						},
						"metric": map[string]any{
							"type":        "string",
							"description": "Telemetry field key to display, e.g. 'temperature', 'vibration', 'speed'.",
						},
						"min":  map[string]any{"type": "number", "description": "Gauge lower bound (gauge only)."},
						"max":  map[string]any{"type": "number", "description": "Gauge upper bound (gauge only)."},
						"unit": map[string]any{"type": "string", "description": "Display unit, e.g. '°C'."},
					},
				},
			},
		},
		"required": []string{"dashboard_name", "widgets"},
	},
}

// ToolWidget is one element of the `widgets` array as emitted by the LLM.
type ToolWidget struct {
	Type      string   `json:"type"`
	Title     string   `json:"title"`
	MachineID string   `json:"machine_id"` // human name; resolved to a UUID server-side
	Metric    string   `json:"metric"`
	Min       *float64 `json:"min"`
	Max       *float64 `json:"max"`
	Unit      string   `json:"unit"`
}

// GetMachinesTool returns all machines with their names and numeric fields.
// The AI must call this before create_custom_dashboard so it can use exact machine names.
var GetMachinesTool = map[string]any{
	"name":        "get_machines",
	"description": "Get the list of all machines with their exact names, types, current status, and available numeric metric field keys. Use this to answer questions about machines, and to obtain correct machine names/metrics when the user has asked to build a dashboard.",
	"input_schema": map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	},
}

// CreateDashboardArgs is the full argument object for the create_custom_dashboard tool.
type CreateDashboardArgs struct {
	DashboardName string       `json:"dashboard_name"`
	Widgets       []ToolWidget `json:"widgets"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Category A — read telemetry & alerts
// ─────────────────────────────────────────────────────────────────────────────

var GetLatestTelemetryTool = map[string]any{
	"name":        "get_latest_telemetry",
	"description": "Get the most recent telemetry values (current readings) for one machine. Use when the user asks for the current value of a metric or 'what is X right now'.",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"machine_id"},
		"properties": map[string]any{
			"machine_id": map[string]any{"type": "string", "description": "Machine name as the user refers to it, e.g. 'Checkweigher CW-01'."},
		},
	},
}

var GetTelemetryTrendTool = map[string]any{
	"name":        "get_telemetry_trend",
	"description": "Get the average / min / max of one metric over a time range. Use for questions about trends or how a value has behaved over the last hour/day/week.",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"machine_id", "metric"},
		"properties": map[string]any{
			"machine_id": map[string]any{"type": "string", "description": "Machine name, e.g. 'Temp Sensor TS-01'."},
			"metric":     map[string]any{"type": "string", "description": "Telemetry field key, e.g. 'temp', 'weight', 'vibration'."},
			"time_range": map[string]any{"type": "string", "description": "One of 5m, 15m, 30m, 1h, 6h, 24h, 7d, 15d, 30d. Defaults to 1h.", "enum": []string{"5m", "15m", "30m", "1h", "6h", "24h", "7d", "15d", "30d"}},
		},
	},
}

var GetActiveAlertsTool = map[string]any{
	"name":        "get_active_alerts",
	"description": "List all currently open (unresolved) alert events for the organization. Each item includes an event id you can pass to acknowledge_alert or resolve_alert.",
	"input_schema": map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	},
}

var GetDailyCountTool = map[string]any{
	"name":        "get_daily_count",
	"description": "Get the number of telemetry records (production count) per day for one machine. Use for 'how much did X produce' questions.",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"machine_id"},
		"properties": map[string]any{
			"machine_id": map[string]any{"type": "string", "description": "Machine name."},
			"days":       map[string]any{"type": "integer", "description": "How many days back to include. Defaults to 7."},
		},
	},
}

// ─────────────────────────────────────────────────────────────────────────────
// Category B — manage existing dashboards
// ─────────────────────────────────────────────────────────────────────────────

var ListDashboardsTool = map[string]any{
	"name":        "list_dashboards",
	"description": "List the user's existing dashboards with their names and widget counts. Call this before add_widget_to_dashboard or remove_widget if you are unsure of the exact dashboard name.",
	"input_schema": map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	},
}

var AddWidgetTool = map[string]any{
	"name":        "add_widget_to_dashboard",
	"description": "Add a single widget to an EXISTING dashboard (referenced by name). Use this to extend a dashboard the user already has — do not create a new one.",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"dashboard_name", "widget"},
		"properties": map[string]any{
			"dashboard_name": map[string]any{"type": "string", "description": "Exact name of the existing dashboard."},
			"widget": map[string]any{
				"type":     "object",
				"required": []string{"type"},
				"properties": map[string]any{
					"type":       map[string]any{"type": "string", "enum": allowedWidgetTypes, "description": "Which pre-built widget component to render."},
					"title":      map[string]any{"type": "string", "description": "Optional widget header."},
					"machine_id": map[string]any{"type": "string", "description": "Machine name the widget shows. Optional for alarm-panel."},
					"metric":     map[string]any{"type": "string", "description": "Telemetry field key to display, e.g. 'temperature'."},
					"min":        map[string]any{"type": "number", "description": "Gauge lower bound (gauge only)."},
					"max":        map[string]any{"type": "number", "description": "Gauge upper bound (gauge only)."},
					"unit":       map[string]any{"type": "string", "description": "Display unit, e.g. '°C'."},
				},
			},
		},
	},
}

var RemoveWidgetTool = map[string]any{
	"name":        "remove_widget",
	"description": "Remove a widget from an existing dashboard, identified by the widget's title. Call list_dashboards first if unsure of the dashboard name.",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"dashboard_name", "widget_title"},
		"properties": map[string]any{
			"dashboard_name": map[string]any{"type": "string", "description": "Exact name of the dashboard."},
			"widget_title":   map[string]any{"type": "string", "description": "Title of the widget to remove."},
		},
	},
}

// ─────────────────────────────────────────────────────────────────────────────
// Category C — manage alert rules & events
// ─────────────────────────────────────────────────────────────────────────────

var CreateAlertTool = map[string]any{
	"name":        "create_alert",
	"description": "Create a threshold alert rule that fires when a machine's metric crosses a value. Use when the user asks to be notified or warned about a condition.",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"machine_id", "metric", "condition", "threshold"},
		"properties": map[string]any{
			"machine_id":   map[string]any{"type": "string", "description": "Machine name."},
			"metric":       map[string]any{"type": "string", "description": "Telemetry field key, e.g. 'temp'."},
			"condition":    map[string]any{"type": "string", "enum": []string{"gt", "lt", "gte", "lte", "eq", "neq", "between", "outside"}, "description": "Comparison operator. 'gt' = greater than, 'lt' = less than, etc."},
			"threshold":    map[string]any{"type": "number", "description": "The value to compare against (lower bound for between/outside)."},
			"threshold_hi": map[string]any{"type": "number", "description": "Upper bound — required only for 'between' and 'outside'."},
			"severity":     map[string]any{"type": "string", "enum": []string{"info", "warning", "critical"}, "description": "Alert severity. Defaults to 'warning'."},
			"name":         map[string]any{"type": "string", "description": "Optional human-readable rule name."},
			"cooldown_sec": map[string]any{"type": "integer", "description": "Minimum seconds between firings. Defaults to 300."},
		},
	},
}

var AcknowledgeAlertTool = map[string]any{
	"name":        "acknowledge_alert",
	"description": "Acknowledge an open alert event (mark that someone is handling it). First call get_active_alerts to obtain the event_id.",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"event_id"},
		"properties": map[string]any{
			"event_id": map[string]any{"type": "string", "description": "The alert event id from get_active_alerts."},
		},
	},
}

var ResolveAlertTool = map[string]any{
	"name":        "resolve_alert",
	"description": "Resolve (close) an open alert event. First call get_active_alerts to obtain the event_id.",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"event_id"},
		"properties": map[string]any{
			"event_id": map[string]any{"type": "string", "description": "The alert event id from get_active_alerts."},
		},
	},
}

// ─────────────────────────────────────────────────────────────────────────────
// Category D — factory-wide analysis
// ─────────────────────────────────────────────────────────────────────────────

var GetFactoryOverviewTool = map[string]any{
	"name":        "get_factory_overview",
	"description": "Get a one-shot snapshot of every machine — status, latest values, and open-alert count. Use this for broad questions like 'summarize the factory', 'what's wrong', or 'which machines need attention', then reason over the result in text.",
	"input_schema": map[string]any{
		"type":       "object",
		"properties": map[string]any{},
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
