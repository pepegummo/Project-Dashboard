package ai

var allowedWidgetTypes = []string{
	"line-chart", "gauge", "kpi-card", "status-card", "table", "alarm-panel", "daily-count", "chart",
}

// machineIDProp is the shared schema for a required machine_id slot. Read-only and
// reused across read/alert tools — nudges the model to ask rather than guess a name.
var machineIDProp = map[string]any{
	"type":        "string",
	"description": "Machine name (e.g. CW-01). Ask user if unknown — never guess.",
}

// widgetItemSchema is the nested widget object used by preview_add_widget.
var widgetItemSchema = map[string]any{
	"type":     "object",
	"required": []string{"type"},
	"properties": map[string]any{
		"type":       map[string]any{"type": "string", "description": "Widget type. Use 'daily-count' for production/piece counts, 'kpi-card' for single numeric metric, 'line-chart' for trend, 'gauge' for dials, 'chart' for a multi-metric overlay chart (set 'fields' array, not 'metric')."},
		"title":      map[string]any{"type": "string"},
		"machine_id": map[string]any{"type": "string"},
		"metric":     map[string]any{"type": "string"},
		"min":        map[string]any{"type": "number"},
		"max":        map[string]any{"type": "number"},
		"unit":       map[string]any{"type": "string"},
		"bucket":     map[string]any{"type": "string", "description": "Time bucket size for daily-count and chart widgets, e.g. '30m', '1h', '1d'."},
		"sku":        map[string]any{"type": "string", "description": "SKU filter for daily-count widgets (empty = all SKUs)."},
		"status":     map[string]any{"type": "string", "enum": []string{"all", "good", "reject"}, "description": "Piece status filter for daily-count widgets."},
		"fields":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Chart widget: metric field keys to overlay (e.g. ['speed','throughput'])."},
		"chartType":  map[string]any{"type": "string", "enum": []string{"line", "bar", "area"}, "description": "Chart widget render style."},
		"points":     map[string]any{"type": "integer", "description": "Chart widget: number of buckets/bars to show (window = bucket × points)."},
		"scaling":    map[string]any{"type": "string", "enum": []string{"shared", "dual", "normalized"}, "description": "Chart widget y-axis scaling. Use 'normalized' when overlaying 3+ fields or mixed units."},
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
			"metric":  map[string]any{"type": "string", "description": "The English sensor field key (e.g. speed, weight, temp, rejects, throughput). Map the user's word in any language to it. Never pass a display-style word here (value/gauge/trend) — those belong in viz, not metric."},
			"viz":     map[string]any{"type": "string", "enum": []string{"value", "gauge", "trend"}, "description": "OPTIONAL display style only — value = current number, gauge = dial, trend = line chart over time. Never confuse this with metric."},
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

var GetTelemetrySeriesTool = map[string]any{
	"name":        "get_telemetry_series",
	"description": "Get all time-bucketed data points (avg/min/max per bucket) for a metric — use this to read what a line chart is showing. Result's \"data\" is compact rows ordered oldest→newest; \"columns\" names each row's fields in order, e.g. columns:[\"time\",\"avg\",\"min\",\"max\"], data:[[\"2026-06-27T10:00\",55.2,50.1,60.3],...].",
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

var GetProductionCountTool = map[string]any{
	"name":        "get_production_count",
	"description": "Get production/piece counts bucketed over time — use to read what a daily-count widget is showing. bucket: '30m','1h','1d'; points: how many buckets back (default 48). Result's \"data\" is compact [time, count] rows ordered oldest→newest, e.g. data:[[\"2026-06-27T10:00\",123],...].",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"machine_id", "bucket"},
		"properties": map[string]any{
			"machine_id": machineIDProp,
			"bucket":     map[string]any{"type": "string", "description": "Time bucket size, e.g. '30m', '1h', '1d'."},
			"points":     map[string]any{"type": "integer", "description": "Number of buckets to return (default 48, max 500)."},
			"sku":        map[string]any{"type": "string", "description": "SKU filter (empty = all SKUs)."},
			"status":     map[string]any{"type": "string", "enum": []string{"all", "good", "reject"}},
		},
	},
}

var GetSkusTool = map[string]any{
	"name":        "get_skus",
	"description": "List the SKU values available for a machine (so the user can pick one, or to map a loose SKU name to a real one).",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"machine_id"},
		"properties": map[string]any{
			"machine_id": machineIDProp,
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
			"widget_title": map[string]any{"type": "string", "description": "Exact displayed title of the widget as shown in the dashboard context (e.g. \"CW-01 Count\"), not the widget type."},
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
			"machine":      map[string]any{"type": "string", "description": "New machine name to reassign this widget (e.g. CW-02). Resolves to machineUuid automatically."},
			"type":         map[string]any{"type": "string"},
			"metric":       map[string]any{"type": "string"},
			"unit":         map[string]any{"type": "string"},
			"min":          map[string]any{"type": "number"},
			"max":          map[string]any{"type": "number"},
			"start_date":   map[string]any{"type": "string", "description": "Absolute window start as YYYY-MM-DD (chart widgets). Convert any DD/MM/YYYY the user gives."},
			"end_date":     map[string]any{"type": "string", "description": "Absolute window end as YYYY-MM-DD (chart widgets)."},
			"bucket":       map[string]any{"type": "string", "description": "Time bucket size for count/chart widgets, e.g. '25m', '1h', '1d'. Format: <number><m|h|d>."},
			"sku":          map[string]any{"type": "string", "description": "SKU filter for count widgets (empty = all SKUs)."},
			"status":       map[string]any{"type": "string", "enum": []string{"all", "good", "reject"}, "description": "Piece status filter for count widgets."},
			"fields":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Chart widget: replace the overlaid metric field keys, e.g. ['speed','throughput']."},
			"chartType":    map[string]any{"type": "string", "enum": []string{"line", "bar", "area"}, "description": "Chart widget render style."},
			"points":       map[string]any{"type": "integer", "description": "Chart widget: number of buckets/bars to show."},
			"scaling":      map[string]any{"type": "string", "enum": []string{"shared", "dual", "normalized"}, "description": "Chart widget y-axis scaling."},
		},
	},
}

// AllTools is the complete set handed to the LLM and exposed via GET /api/ai/tools.
func AllTools() []map[string]any {
	return []map[string]any{
		GetMachinesTool,
		ShowMetricTool,
		GetTelemetryTrendTool,
		GetTelemetrySeriesTool,
		GetActiveAlertsTool,
		GetProductionCountTool,
		GetSkusTool,
		ListDashboardsTool,
		PreviewDashboardTool,
		PreviewAddWidgetTool,
		PreviewRemoveWidgetTool,
		PreviewUpdateWidgetTool,
	}
}

// writeTools are the mutating tools that require admin/editor role. Editing dashboards is
// now staged via the preview_* tools (persisted only on the user's Save/Confirm), so the
// only remaining write tool is the post-Confirm dashboard creation.
var writeTools = map[string]bool{
	"create_custom_dashboard": true,
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
	Bucket    string   `json:"bucket,omitempty"`
	Sku       string   `json:"sku,omitempty"`
	Status    string   `json:"status,omitempty"`
	Fields    []string `json:"fields,omitempty"`    // chart widget: metric keys to overlay
	ChartType string   `json:"chartType,omitempty"` // chart widget: line | bar | area
	Points    int      `json:"points,omitempty"`    // chart widget: number of buckets/bars
	Scaling   string   `json:"scaling,omitempty"`   // chart widget: shared | dual | normalized
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

type SeriesArgs struct {
	MachineID string `json:"machine_id"`
	Metric    string `json:"metric"`
	TimeRange string `json:"time_range"`
}

type ProductionCountArgs struct {
	MachineID string `json:"machine_id"`
	Bucket    string `json:"bucket"`
	Points    int    `json:"points"`
	Sku       string `json:"sku"`
	Status    string `json:"status"`
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
	Bucket        string         `json:"bucket,omitempty"` // count/chart widget: bucket size, e.g. "30m"
	Sku           string         `json:"sku,omitempty"`    // count widget: SKU filter ("" = all)
	Status        string         `json:"status,omitempty"` // count widget: all | good | reject
	Fields        []string       `json:"fields,omitempty"`    // chart widget: metric keys to overlay
	ChartType     string         `json:"chartType,omitempty"` // chart widget: line | bar | area
	Points        int            `json:"points,omitempty"`    // chart widget: number of buckets/bars
	Scaling       string         `json:"scaling,omitempty"`   // chart widget: shared | dual | normalized
	Layout        map[string]any `json:"layout,omitempty"` // optional grid position from preview
}

type PreviewDashboardResult struct {
	Preview       bool            `json:"preview"`
	DashboardName string          `json:"dashboardName"`
	Widgets       []PreviewWidget `json:"widgets"`
	Summary       string          `json:"summary"`
}

