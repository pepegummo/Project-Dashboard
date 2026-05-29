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

// CreateDashboardArgs is the full argument object for the create_custom_dashboard tool.
type CreateDashboardArgs struct {
	DashboardName string       `json:"dashboard_name"`
	Widgets       []ToolWidget `json:"widgets"`
}
