# Widget Anatomy for AI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Decompose every widget into named semantic elements (name, value, x/y axis, scale, series, filters) so the AI can understand and reason about each part of each widget.

**Architecture:** No new table. Widget parts already exist in `dashboard_widgets.config` (JSONB) + `machine_fields` (label/unit/min/max/precision). We add one pure Go function `DescribeWidget` that merges the two into a semantic element map, expose it through a new AI tool `get_dashboard_widgets`, and enrich the on-screen dashboard context the frontend already injects.

**Tech Stack:** Go (Fiber, pgx raw SQL), Vue 3 / TypeScript, Groq tool-calling.

## Global Constraints

- **No new DB table, no migration.** Decomposition happens at read time — always in sync with `config`. (A `widget_elements` table was considered and rejected: it duplicates `config`, drifts on every widget edit, and adds no data that doesn't already exist. Revisit only if elements ever need an independent lifecycle — per-element permissions or per-element alert rules.)
- **No new dependencies.** Raw SQL via `database.Pool` (pgx), matching existing style in `tool_actions.go`.
- **Org scoping:** every dashboard lookup goes through the existing `resolveDashboardID(ctx, orgID, name)` — never query by name without orgID.
- **Token-lean tool results:** omit empty elements; follow the compaction philosophy documented at `backend/internal/modules/ai/tool_actions.go:318`.
- **Existing tests must keep passing:** `cd backend && go test ./internal/modules/ai/` and `go vet ./...`; frontend `npm run typecheck`.
- Widget types in play: `line-chart`, `gauge`, `kpi-card`, `status-card`, `table`, `alarm-panel`, `daily-count`, `chart` (custom multi-series).

## Element Vocabulary (the "split")

| Widget type | Elements |
|---|---|
| `kpi-card` | `value` {metric, label, unit} |
| `gauge` | `value` {metric, label, unit} · `scale` {min, max} · `thresholds` |
| `line-chart` | `x_axis` {kind: time, window: live \| absolute range} · `y_axis` {metric, label, unit} |
| `daily-count` | `x_axis` {kind: time-bucket, bucket} · `y_axis` {kind: piece-count, unit: pcs} · `filters` {sku, status} |
| `chart` (custom) | `series` [{metric, label, unit} …] · `x_axis` {kind: time-bucket, bucket, points} · `y_axis` {scaling} · `chart_style` |
| `status-card` | `value` {kind: machine-status} |
| `table` | `columns` (all machine fields, latest values) |
| `alarm-panel` | `filters` {severities, maxItems} |

All types also carry `type`, `name` (title), `machine`. Label/unit come from `machine_fields`; widget config overrides (e.g. gauge `min`/`max`) win over field metadata.

## Non-Goals

- Letting the AI *create* custom `chart` widgets (`allowedWidgetTypes` in `schema.go:3` intentionally excludes `chart`) — read-only understanding only.
- Changing the REST API or widget rendering. Frontend widgets keep reading `config` directly.

---

### Task 1: `DescribeWidget` — pure decomposition function

**Files:**
- Create: `backend/internal/modules/ai/widget_anatomy.go`
- Test: `backend/internal/modules/ai/widget_anatomy_test.go`

**Interfaces:**
- Consumes: nothing (pure function; `config` is the unmarshalled `dashboard_widgets.config` JSONB, so numbers are `float64`).
- Produces: `type FieldMeta struct { Label string; Unit string; Min, Max *float64; Precision int }` and `func DescribeWidget(widgetType, title, machineName string, config map[string]any, fields map[string]FieldMeta) map[string]any` — Task 2 calls this per widget.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/modules/ai/widget_anatomy_test.go`:

```go
package ai

import (
	"encoding/json"
	"testing"
)

func cfgMap(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func fptr(v float64) *float64 { return &v }

var testFieldMeta = map[string]FieldMeta{
	"speed": {Label: "Speed", Unit: "rpm", Min: fptr(0), Max: fptr(3000), Precision: 1},
	"temp":  {Label: "Temperature", Unit: "°C"},
}

func TestDescribeGaugeConfigOverridesFieldMeta(t *testing.T) {
	d := DescribeWidget("gauge", "Speed Gauge", "CW-01",
		cfgMap(t, `{"field":"speed","min":0,"max":2500,"unit":"rpm"}`), testFieldMeta)
	val := d["value"].(map[string]any)
	if val["metric"] != "speed" || val["label"] != "Speed" || val["unit"] != "rpm" {
		t.Fatalf("value element wrong: %v", val)
	}
	scale := d["scale"].(map[string]any)
	if scale["max"] != 2500.0 { // widget config wins over field meta (3000)
		t.Fatalf("scale wrong: %v", scale)
	}
	if d["machine"] != "CW-01" || d["name"] != "Speed Gauge" {
		t.Fatalf("identity wrong: %v", d)
	}
}

func TestDescribeGaugeScaleFallsBackToFieldMeta(t *testing.T) {
	d := DescribeWidget("gauge", "g", "CW-01", cfgMap(t, `{"field":"speed"}`), testFieldMeta)
	scale := d["scale"].(map[string]any)
	if scale["min"] != 0.0 || scale["max"] != 3000.0 {
		t.Fatalf("expected field-meta fallback, got %v", scale)
	}
}

func TestDescribeLineChartLiveVsAbsoluteWindow(t *testing.T) {
	live := DescribeWidget("line-chart", "Trend", "CW-01",
		cfgMap(t, `{"field":"speed","liveMode":true}`), testFieldMeta)
	if live["x_axis"].(map[string]any)["window"] != "live (rolling)" {
		t.Fatalf("live window wrong: %v", live["x_axis"])
	}
	abs := DescribeWidget("line-chart", "Trend", "CW-01",
		cfgMap(t, `{"field":"speed","startDateTime":"2026-06-01T00:00","endDateTime":"2026-06-02T23:59"}`), testFieldMeta)
	if abs["x_axis"].(map[string]any)["window"] != "2026-06-01T00:00 → 2026-06-02T23:59" {
		t.Fatalf("absolute window wrong: %v", abs["x_axis"])
	}
	if abs["y_axis"].(map[string]any)["unit"] != "rpm" {
		t.Fatal("y_axis should carry field unit from machine_fields")
	}
}

func TestDescribeDailyCountDefaults(t *testing.T) {
	d := DescribeWidget("daily-count", "Count", "CW-01", cfgMap(t, `{}`), nil)
	if d["x_axis"].(map[string]any)["bucket"] != "1h" {
		t.Fatalf("bucket default missing: %v", d["x_axis"])
	}
	if d["filters"].(map[string]any)["status"] != "all" {
		t.Fatalf("status default missing: %v", d["filters"])
	}
	if d["y_axis"].(map[string]any)["kind"] != "piece-count" {
		t.Fatalf("y_axis wrong: %v", d["y_axis"])
	}
}

func TestDescribeCustomChartSeries(t *testing.T) {
	d := DescribeWidget("chart", "Multi", "TS-01",
		cfgMap(t, `{"fields":["speed","temp"],"chartType":"bar","bucket":"30m","points":40,"scaling":"dual"}`), testFieldMeta)
	series := d["series"].([]any)
	if len(series) != 2 {
		t.Fatalf("expected 2 series, got %d", len(series))
	}
	if series[1].(map[string]any)["unit"] != "°C" {
		t.Fatal("per-series unit from field meta missing")
	}
	if d["chart_style"] != "bar" {
		t.Fatalf("chart_style wrong: %v", d["chart_style"])
	}
	x := d["x_axis"].(map[string]any)
	if x["bucket"] != "30m" || x["points"] != 40.0 {
		t.Fatalf("x_axis wrong: %v", x)
	}
	if d["y_axis"].(map[string]any)["scaling"] != "dual" {
		t.Fatalf("y_axis scaling wrong: %v", d["y_axis"])
	}
}

func TestDescribeKpiAndStatusAndPanels(t *testing.T) {
	kpi := DescribeWidget("kpi-card", "Temp", "TS-01", cfgMap(t, `{"field":"temp"}`), testFieldMeta)
	if kpi["value"].(map[string]any)["label"] != "Temperature" {
		t.Fatalf("kpi value wrong: %v", kpi["value"])
	}
	st := DescribeWidget("status-card", "Status", "TS-01", cfgMap(t, `{}`), nil)
	if st["value"].(map[string]any)["kind"] != "machine-status" {
		t.Fatalf("status value wrong: %v", st["value"])
	}
	al := DescribeWidget("alarm-panel", "Alarms", "", cfgMap(t, `{"severities":["critical"],"maxItems":5}`), nil)
	f := al["filters"].(map[string]any)
	if f["maxItems"] != 5.0 {
		t.Fatalf("alarm filters wrong: %v", f)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/modules/ai/ -run TestDescribe -v`
Expected: FAIL — `undefined: DescribeWidget`, `undefined: FieldMeta`

- [ ] **Step 3: Write the implementation**

Create `backend/internal/modules/ai/widget_anatomy.go`:

```go
package ai

// FieldMeta carries the machine_fields metadata used to label widget elements.
type FieldMeta struct {
	Label     string
	Unit      string
	Min, Max  *float64
	Precision int
}

// DescribeWidget decomposes one widget into named semantic elements
// (name, value, x_axis/y_axis, scale, series, filters) so the LLM can reason
// about each part instead of guessing from an opaque config blob.
// config is the unmarshalled dashboard_widgets.config JSONB (numbers are float64).
// Widget-level config overrides (unit, min, max) win over machine_fields metadata.
func DescribeWidget(widgetType, title, machineName string, config map[string]any, fields map[string]FieldMeta) map[string]any {
	out := map[string]any{"type": widgetType, "name": title}
	if machineName != "" {
		out["machine"] = machineName
	}

	str := func(k string) string { s, _ := config[k].(string); return s }
	num := func(k string) (float64, bool) { f, ok := config[k].(float64); return f, ok }

	// metricEl builds a value/axis element for one metric key, merging the
	// machine_fields label/unit with any widget-level unit override.
	metricEl := func(key string) map[string]any {
		el := map[string]any{"metric": key}
		if f, ok := fields[key]; ok {
			if f.Label != "" {
				el["label"] = f.Label
			}
			if f.Unit != "" {
				el["unit"] = f.Unit
			}
		}
		if u := str("unit"); u != "" {
			el["unit"] = u
		}
		return el
	}

	switch widgetType {
	case "kpi-card":
		out["value"] = metricEl(str("field"))

	case "status-card":
		out["value"] = map[string]any{"kind": "machine-status"}

	case "gauge":
		key := str("field")
		out["value"] = metricEl(key)
		scale := map[string]any{}
		if v, ok := num("min"); ok {
			scale["min"] = v
		} else if f, ok := fields[key]; ok && f.Min != nil {
			scale["min"] = *f.Min
		}
		if v, ok := num("max"); ok {
			scale["max"] = v
		} else if f, ok := fields[key]; ok && f.Max != nil {
			scale["max"] = *f.Max
		}
		out["scale"] = scale
		if t, ok := config["thresholds"]; ok {
			out["thresholds"] = t
		}

	case "line-chart":
		x := map[string]any{"kind": "time"}
		if s := str("startDateTime"); s != "" {
			x["window"] = s + " → " + str("endDateTime")
		} else {
			x["window"] = "live (rolling)"
		}
		out["x_axis"] = x
		out["y_axis"] = metricEl(str("field"))

	case "daily-count":
		bucket := str("bucket")
		if bucket == "" {
			bucket = "1h"
		}
		out["x_axis"] = map[string]any{"kind": "time-bucket", "bucket": bucket}
		out["y_axis"] = map[string]any{"kind": "piece-count", "unit": "pcs"}
		filters := map[string]any{"status": "all"}
		if s := str("status"); s != "" {
			filters["status"] = s
		}
		if s := str("sku"); s != "" {
			filters["sku"] = s
		}
		out["filters"] = filters

	case "chart": // custom multi-series chart
		var series []any
		if fs, ok := config["fields"].([]any); ok {
			for _, f := range fs {
				if k, ok := f.(string); ok {
					series = append(series, metricEl(k))
				}
			}
		}
		out["series"] = series
		style := str("chartType")
		if style == "" {
			style = "line"
		}
		out["chart_style"] = style
		bucket := str("bucket")
		if bucket == "" {
			bucket = "1h"
		}
		x := map[string]any{"kind": "time-bucket", "bucket": bucket}
		if p, ok := num("points"); ok {
			x["points"] = p
		}
		out["x_axis"] = x
		scaling := str("scaling")
		if scaling == "" {
			scaling = "shared"
		}
		out["y_axis"] = map[string]any{"scaling": scaling}

	case "table":
		out["columns"] = "all machine fields with latest values"

	case "alarm-panel":
		filters := map[string]any{}
		if s, ok := config["severities"]; ok {
			filters["severities"] = s
		}
		if m, ok := num("maxItems"); ok {
			filters["maxItems"] = m
		}
		out["filters"] = filters
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/modules/ai/ -v`
Expected: all `TestDescribe*` PASS, and pre-existing tests (`eval_test.go`, `dashboard_action_test.go`, `timezone_test.go`) still PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/modules/ai/widget_anatomy.go backend/internal/modules/ai/widget_anatomy_test.go
git commit -m "feat(ai): DescribeWidget decomposes widgets into semantic elements"
```

---

### Task 2: `get_dashboard_widgets` AI tool

**Files:**
- Modify: `backend/internal/modules/ai/tool_actions.go` (add method after `ListDashboards`, ~line 433)
- Modify: `backend/internal/modules/ai/schema.go` (add tool def after `ListDashboardsTool` ~line 128; register in `AllTools()` ~line 195)
- Modify: `backend/internal/modules/ai/controller.go` (dispatch case after `list_dashboards` ~line 165; system-prompt line in `systemPromptBase` ~line 52)

**Interfaces:**
- Consumes: `DescribeWidget(widgetType, title, machineName string, config map[string]any, fields map[string]FieldMeta) map[string]any` and `FieldMeta` from Task 1; existing `resolveDashboardID(ctx, orgID, name) (string, bool)`.
- Produces: tool result `{"dashboard": string, "count": int, "widgets": []map[string]any}` — each widget is a `DescribeWidget` element map. Read-only tool (NOT added to `writeTools`).

- [ ] **Step 1: Add the ToolKit method and field-meta loader**

In `backend/internal/modules/ai/tool_actions.go`, add `"maps"` and `"slices"` to the imports, then append after `ListDashboards`:

```go
// GetDashboardWidgets returns every widget of a dashboard decomposed into
// semantic elements (name, value, axes, scale, series, filters) so the model
// can reason about each part of each widget — see widget_anatomy.go.
func (tk *ToolKit) GetDashboardWidgets(ctx context.Context, orgID string, raw json.RawMessage) (any, error) {
	var args struct {
		Dashboard string `json:"dashboard"`
	}
	_ = json.Unmarshal(raw, &args)
	id, ok := resolveDashboardID(ctx, orgID, strings.TrimSpace(args.Dashboard))
	if !ok {
		return nil, fmt.Errorf("dashboard %q not found", args.Dashboard)
	}

	var dashName string
	_ = database.Pool.QueryRow(ctx, `SELECT name FROM dashboards WHERE id = $1`, id).Scan(&dashName)

	rows, err := database.Pool.Query(ctx, `
		SELECT w.widget_type, COALESCE(w.title, ''), w.config,
		       COALESCE(m.id::text, ''), COALESCE(m.name, '')
		FROM dashboard_widgets w
		LEFT JOIN machines m ON m.id = w.machine_id
		WHERE w.dashboard_id = $1
		ORDER BY w."order", w.created_at
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type rawWidget struct {
		wtype, title, machineID, machineName string
		config                               map[string]any
	}
	var widgetsRaw []rawWidget
	machineIDs := map[string]bool{}
	for rows.Next() {
		var rw rawWidget
		var cfgBytes []byte
		if err := rows.Scan(&rw.wtype, &rw.title, &cfgBytes, &rw.machineID, &rw.machineName); err != nil {
			continue
		}
		_ = json.Unmarshal(cfgBytes, &rw.config)
		if rw.machineID != "" {
			machineIDs[rw.machineID] = true
		}
		widgetsRaw = append(widgetsRaw, rw)
	}

	fieldMeta := loadFieldMeta(ctx, slices.Collect(maps.Keys(machineIDs)))

	widgets := make([]map[string]any, 0, len(widgetsRaw))
	for _, rw := range widgetsRaw {
		widgets = append(widgets, DescribeWidget(rw.wtype, rw.title, rw.machineName, rw.config, fieldMeta[rw.machineID]))
	}
	return map[string]any{"dashboard": dashName, "count": len(widgets), "widgets": widgets}, nil
}

// loadFieldMeta returns machine_fields metadata grouped by machine UUID.
func loadFieldMeta(ctx context.Context, machineIDs []string) map[string]map[string]FieldMeta {
	out := map[string]map[string]FieldMeta{}
	if len(machineIDs) == 0 {
		return out
	}
	rows, err := database.Pool.Query(ctx, `
		SELECT machine_id::text, key, label, COALESCE(unit, ''), min, max, precision
		FROM machine_fields WHERE machine_id = ANY($1)
	`, machineIDs)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var mid, key string
		var fm FieldMeta
		if err := rows.Scan(&mid, &key, &fm.Label, &fm.Unit, &fm.Min, &fm.Max, &fm.Precision); err != nil {
			continue
		}
		if out[mid] == nil {
			out[mid] = map[string]FieldMeta{}
		}
		out[mid][key] = fm
	}
	return out
}
```

- [ ] **Step 2: Register the tool schema**

In `backend/internal/modules/ai/schema.go`, after `ListDashboardsTool` (~line 128), add:

```go
var GetDashboardWidgetsTool = map[string]any{
	"name":        "get_dashboard_widgets",
	"description": "Get every widget of a dashboard broken into semantic parts: name, machine, value (metric + label + unit), x_axis (time window or bucket), y_axis (metric or count), scale (gauge min/max), series (multi-field charts), filters (sku/status/severities). Use when the user asks what a dashboard contains or what a specific widget on it shows, and that dashboard is NOT open on screen.",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"dashboard"},
		"properties": map[string]any{
			"dashboard": map[string]any{"type": "string", "description": "Dashboard name (fuzzy matched). Ask the user if unknown — never guess."},
		},
	},
}
```

And in `AllTools()` add `GetDashboardWidgetsTool,` immediately after `ListDashboardsTool,`. Do NOT add it to `writeTools` — it is read-only.

- [ ] **Step 3: Dispatch + system prompt**

In `backend/internal/modules/ai/controller.go`:

a) Add a dispatch case after `case "list_dashboards":` (~line 165):

```go
	case "get_dashboard_widgets":
		return ctrl.tk.GetDashboardWidgets(ctx, user.OrgId, rawArgs)
```

b) In `systemPromptBase`, TOOL SELECTION section, add one line after the `- "List SKUs"...` line (~line 50):

```
- Questions about a saved dashboard's widgets when it is NOT open on screen ("what's on dashboard X", "what does the gauge on X show") → get_dashboard_widgets(dashboard). Each widget comes back split into parts: value (metric/label/unit), x_axis, y_axis, scale, series, filters — answer from those parts.
```

- [ ] **Step 4: Build, vet, test**

Run: `cd backend && go build ./... && go vet ./... && go test ./internal/modules/ai/`
Expected: build OK, vet clean, all tests PASS.

- [ ] **Step 5: End-to-end verification against the running stack**

```bash
docker compose up -d
TOKEN=$(curl -s -X POST http://localhost:4000/api/auth/login -H "Content-Type: application/json" \
  -d '{"email":"admin@acme-foods.com","password":"Admin@1234"}' | jq -r '.data.token')
curl -s -X POST http://localhost:4000/api/ai/tools/execute \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"toolName":"get_dashboard_widgets","params":{"dashboard":"Overview"}}' | jq
```

(If the login response nests the token differently, inspect `curl` output and adjust the `jq` path. If no dashboard named "Overview" exists, use any name from `list_dashboards`.)

Expected: JSON with `widgets[]`, each containing `type`, `name`, `machine`, and per-type elements (`value`, `x_axis`, `y_axis`, `scale`, `series`, `filters`). Note: the backend Docker image must be rebuilt first — `docker compose up -d --build backend`.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/modules/ai/tool_actions.go backend/internal/modules/ai/schema.go backend/internal/modules/ai/controller.go
git commit -m "feat(ai): get_dashboard_widgets tool exposes widget anatomy to the LLM"
```

---

### Task 3: Enrich the on-screen dashboard context with the same element vocabulary

The frontend already injects a per-widget context string (`buildDashboardContext` in `AIAssistantPage.vue`), but it omits gauge scale, per-widget units, and the entire custom-chart config (`fields`, `chartType`, `scaling`, `points`). Add those using the same names as the tool (`scale`, `series`) so the model sees one consistent vocabulary on-screen and off-screen.

**Files:**
- Modify: `frontend/src/pages/AIAssistantPage.vue` (function `buildDashboardContext`, the live-dashboard branch ~lines 526–550 and the preview branch ~lines 471–497)

**Interfaces:**
- Consumes: existing `w.config` (`WidgetConfig` from `frontend/src/types/index.ts:138`) and preview widget fields (`min`, `max`, `unit`).
- Produces: extra `scale …` / `series …` segments in the context string sent to `POST /api/ai/conversations/:id/messages`. No type changes.

- [ ] **Step 1: Extend the live-dashboard branch**

In `buildDashboardContext`, in the live branch (after the `if (w.config?.status) parts.push(...)` line, ~line 546), add:

```ts
      // Same element vocabulary as the get_dashboard_widgets tool (scale / series),
      // so the model reasons about widget parts identically on- and off-screen.
      if (w.config?.min !== undefined || w.config?.max !== undefined) {
        parts.push(`scale ${w.config.min ?? '?'}–${w.config.max ?? '?'}${w.config.unit ? ' ' + w.config.unit : ''}`);
      }
      if (Array.isArray(w.config?.fields) && w.config.fields.length) {
        parts.push(
          `series [${w.config.fields.join(', ')}], style ${w.config.chartType ?? 'line'}, ` +
          `scaling ${w.config.scaling ?? 'shared'}, window ${w.config.bucket ?? '1h'} × ${w.config.points ?? 20} buckets`,
        );
      }
```

- [ ] **Step 2: Extend the preview branch**

In the preview branch (after the `if (w.status) parts.push(...)` line, ~line 492), add:

```ts
      if (w.min !== undefined || w.max !== undefined) {
        parts.push(`scale ${w.min ?? '?'}–${w.max ?? '?'}${w.unit ? ' ' + w.unit : ''}`);
      }
```

(Preview widgets are flat `PreviewWidget` objects — no `config`, no multi-field charts.)

- [ ] **Step 3: Typecheck and lint**

Run: `cd frontend && npm run typecheck && npm run lint`
Expected: both clean.

- [ ] **Step 4: Manual verification**

Open the AI page with a dashboard containing a gauge and a custom chart, click the gauge, and ask "what is the max of this gauge?" — the model should answer from context (scale segment) without calling a tool. Ask "which fields does the custom chart overlay?" — answered from the `series` segment.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/AIAssistantPage.vue
git commit -m "feat(ai): send gauge scale and custom-chart series in dashboard context"
```

---

## Self-Review Notes

- **Spec coverage:** "split widget into elements (axis, name, value, other)" → element vocabulary table + `DescribeWidget` (Task 1); "for AI to understand" → tool exposure (Task 2) + on-screen context (Task 3). "Create new table or something else" → decided *something else*; rationale in Global Constraints.
- **Type consistency:** `FieldMeta` and `DescribeWidget` signatures are identical in Task 1 (definition) and Task 2 (call site). `slices.Collect(maps.Keys(...))` requires Go ≥1.23 — module is on Go 1.26.3.
- **Numbers from JSONB:** all numeric config assertions use `float64` (JSON unmarshal default) — tests assert `2500.0`, `40.0`, `5.0` accordingly.
