package ai

// Complex, realistic, MULTI-INTENT live-Groq tests — the way a real user types, in
// Thai, often with two intents in one message. These exercise the exact decision path
// Chat() runs (router -> dispatchIntent -> callAIModel) and assert, per round, the
// tool the model picks and its JSON args. Those args ARE the "highlight signal": the
// frontend flashes whichever widget matches the tool's machine/metric/fields/widget_title
// (see AIAssistantPage.vue's resolver), so asserting them here verifies the RIGHT widget
// would light up — without a browser.
//
// Multi-round (compound) flows are simulated by fabricating a tool-result message and
// feeding it back (nextRound), so no live Postgres is needed — real tool execution goes
// through database.Pool, which panics when nil in a test process.
//
// Model output varies run-to-run, so assertions check MEMBERSHIP + arg presence, not exact
// strings — same philosophy as eval_test.go / router_eval_test.go.
//
// Skips without GROQ_API_KEY. Run (needs a key; space is paced for the free-tier limit —
// raise the model's token limit for a faster/steadier run):
//   cd backend && go test ./internal/modules/ai/ -run ComplexFlows -v

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"iot-dashboard/internal/config"

	"github.com/joho/godotenv"
)

// ── harness ─────────────────────────────────────────────────────────────────────

func liveKeyOrSkip(t *testing.T) {
	t.Helper()
	_ = godotenv.Load("../../../../.env", "../../../.env")
	key := os.Getenv("GROQ_API_KEY")
	if key == "" {
		t.Skip("GROQ_API_KEY not set — skipping live complex-flow test")
	}
	config.Env = &config.Config{AIApiKey: key}
}

// pace dodges the free-tier 8k tok/min limit between model calls. Modest on purpose —
// bump the model's limit and this becomes irrelevant.
func pace() { time.Sleep(30 * time.Second) }

type toolCallView struct {
	id   string
	name string
	args map[string]any
}

func parseCalls(m aiMessage) []toolCallView {
	var out []toolCallView
	for _, c := range m.ToolCalls {
		var args map[string]any
		_ = json.Unmarshal([]byte(c.Function.Arguments), &args)
		out = append(out, toolCallView{id: c.ID, name: c.Function.Name, args: args})
	}
	return out
}

// turn is the outcome of one decide-then-call, carrying enough to chain another round.
type turn struct {
	intent    IntentResult
	routerOK  bool
	choice    string // tool_choice dispatchIntent decided
	assistant aiMessage
	calls     []toolCallView
	text      string // assistant prose when it answered instead of calling a tool
	msgs      []aiMessage
	tools     []map[string]any
}

// decideAndCall mirrors controller.Chat's first turn exactly: router classifies,
// dispatchIntent decides tool_choice, then the main model is called with it. The
// "required"/"none" auto-retry matches Chat's graceful fallback (controller.go:452-461).
func decideAndCall(t *testing.T, msg, ctxText, role string, machineValid, chartExists bool) turn {
	t.Helper()
	ctx := context.Background()

	focused := strings.Contains(msg, "@")
	inlineData := strings.Contains(ctxText, "on-screen data")

	res, ok, _ := classifyIntentWithModel(ctx, routerModel(), msg, focusedContextSummary(ctxText))
	choice, _ := dispatchIntent(res, ok, focused, inlineData, role, machineValid, chartExists)

	sp := systemPromptUnified
	msgs := []aiMessage{
		{Role: "system", Content: &sp},
		{Role: "user", Content: strPtr(msg)},
	}
	if ctxText != "" {
		cc := "Authoritative current dashboard state (overrides anything said earlier):\n" + ctxText + "\n" + dateLineForRequest()
		msgs = append(msgs, aiMessage{Role: "system", Content: &cc})
	} else {
		dl := dateLineForRequest()
		msgs = append(msgs, aiMessage{Role: "system", Content: &dl})
	}
	tools := buildAITools(role)

	resp, _, err := callAIModel(ctx, aiModel(), msgs, tools, choice)
	// Same graceful fallback as Chat: a forced tool_choice the model declines is a valid
	// (prose) answer — retry auto so it can ask/clarify.
	if err != nil && (strings.Contains(err.Error(), "Tool choice is required") || strings.Contains(err.Error(), "Tool choice is none")) {
		resp, _, err = callAIModel(ctx, aiModel(), msgs, tools, "")
	}
	if err != nil {
		t.Fatalf("[%s] groq error: %v", msg, err)
	}
	if len(resp.Choices) == 0 {
		t.Fatalf("[%s] no choices", msg)
	}
	ch := resp.Choices[0]
	tr := turn{intent: res, routerOK: ok, choice: choice, assistant: ch.Message, msgs: msgs, tools: tools}
	if ch.FinishReason == "tool_calls" {
		tr.calls = parseCalls(ch.Message)
	}
	if ch.Message.Content != nil {
		tr.text = *ch.Message.Content
	}
	return tr
}

// nextRound feeds fabricated tool results back and returns the model's next tool calls —
// how one compound prompt produces multiple tool calls across rounds (Chat's for-loop).
// results maps tool name -> the JSON payload to return for it; unnamed tools get {ok:true}.
func nextRound(t *testing.T, tr turn, results map[string]any) turn {
	t.Helper()
	ctx := context.Background()

	msgs := append([]aiMessage{}, tr.msgs...)
	msgs = append(msgs, tr.assistant)
	for _, c := range tr.calls {
		payload, ok := results[c.name]
		if !ok {
			payload = map[string]any{"ok": true}
		}
		b, _ := json.Marshal(payload)
		s := string(b)
		msgs = append(msgs, aiMessage{Role: "tool", ToolCallID: c.id, Content: &s})
	}

	resp, _, err := callAIModel(ctx, aiModel(), msgs, tr.tools, "")
	if err != nil {
		t.Fatalf("nextRound groq error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatalf("nextRound no choices")
	}
	ch := resp.Choices[0]
	out := turn{assistant: ch.Message, msgs: msgs, tools: tr.tools}
	if ch.FinishReason == "tool_calls" {
		out.calls = parseCalls(ch.Message)
	}
	if ch.Message.Content != nil {
		out.text = *ch.Message.Content
	}
	return out
}

// ── assertion helpers (membership + arg presence, not exact strings) ──────────────

func assertToolIn(t *testing.T, tag string, calls []toolCallView, allowed ...string) toolCallView {
	t.Helper()
	if len(calls) == 0 {
		t.Fatalf("[%s] expected a tool call in %v, got none", tag, allowed)
	}
	for _, c := range calls {
		for _, a := range allowed {
			if c.name == a {
				return c
			}
		}
	}
	t.Fatalf("[%s] tool = %q, want one of %v", tag, calls[0].name, allowed)
	return toolCallView{}
}

func argStr(c toolCallView, key string) string {
	if v, ok := c.args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// widgetArg returns the nested "widget" object (preview_add_widget) or the flat args
// (preview_update_widget puts fields/type at the top level).
func widgetArg(c toolCallView) map[string]any {
	if w, ok := c.args["widget"].(map[string]any); ok {
		return w
	}
	return c.args
}

func fieldsOf(m map[string]any) []string {
	var out []string
	if raw, ok := m["fields"].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				out = append(out, strings.ToLower(s))
			}
		}
	}
	return out
}

func assertArgContains(t *testing.T, tag string, c toolCallView, key, want string) {
	t.Helper()
	got := strings.ToLower(argStr(c, key))
	if !strings.Contains(got, strings.ToLower(want)) {
		t.Errorf("[%s] %s arg %q = %q, want to contain %q", tag, c.name, key, got, want)
	}
}

func assertHasFields(t *testing.T, tag string, got []string, want ...string) {
	t.Helper()
	set := map[string]bool{}
	for _, g := range got {
		set[g] = true
	}
	for _, w := range want {
		if !set[strings.ToLower(w)] {
			t.Errorf("[%s] fields = %v, want to include %q", tag, got, w)
		}
	}
}

var blindWriteTools = []string{"preview_add_widget", "preview_update_widget", "preview_remove_widget", "create_custom_dashboard"}

func assertNoBlindWrite(t *testing.T, tag string, tr turn) {
	t.Helper()
	// An ambiguous request must NOT be answered by a forced/hallucinated write — either
	// the model asks a clarifying question (no tool) or reads; never edits blindly.
	for _, c := range tr.calls {
		for _, w := range blindWriteTools {
			if c.name == w {
				t.Errorf("[%s] ambiguous request wrote blindly via %q (args %v); expected a clarifying question or a read", tag, c.name, c.args)
			}
		}
	}
	if len(tr.calls) == 0 && strings.TrimSpace(tr.text) == "" {
		t.Errorf("[%s] no tool and no clarifying text — expected the model to ask", tag)
	}
}

// ── 1. Compound read + write (two intents, one message) ───────────────────────────

func TestComplexFlowsCompoundReadWrite(t *testing.T) {
	liveKeyOrSkip(t)

	// "How much is CW-01's speed, and also add a gauge for temp." Serve the read first
	// (controller.go:61), then the write across a second round.
	msg := "speed ของ CW-01 เท่าไหร่ แล้วเพิ่ม gauge วัด temperature ด้วย"
	tr := decideAndCall(t, msg, "", "editor", true, false)

	// Round 0 = the READ. show_metric(machine=CW-01, metric=speed) is exactly the widget
	// the frontend highlights for a metric read. get_machines is tolerated (fan-out first).
	c := assertToolIn(t, "compound r0", tr.calls, "show_metric", "get_machines")
	if c.name == "show_metric" {
		assertArgContains(t, "compound r0", c, "machine", "CW-01")
		assertArgContains(t, "compound r0", c, "metric", "speed")
	}

	// Round 1 = the WRITE (or a clarifying question about it). Fabricate the read result.
	pace()
	nr := nextRound(t, tr, map[string]any{
		"show_metric":  map[string]any{"widgets": []any{map[string]any{"type": "gauge", "machine": "CW-01", "metric": "speed", "unit": "rpm"}}},
		"get_machines": map[string]any{"machines": []any{map[string]any{"name": "CW-01"}}},
	})
	if len(nr.calls) > 0 {
		w := assertToolIn(t, "compound r1", nr.calls, "preview_add_widget", "show_metric")
		if w.name == "preview_add_widget" {
			assertArgContains(t, "compound r1", toolCallView{name: w.name, args: widgetArg(w)}, "type", "gauge")
		}
	} else if strings.TrimSpace(nr.text) == "" {
		t.Errorf("[compound r1] model neither staged the write nor asked about it")
	}
}

// ── 2. Compare -> custom chart (regression guard for the shipped fix) ──────────────

func TestComplexFlowsCompareToCustomChart(t *testing.T) {
	liveKeyOrSkip(t)

	// (a) No custom chart on the dashboard (line-chart only) -> ADD a type:"chart" widget
	// overlaying both metrics. This is the exact user bug that was fixed.
	noChartCtx := `Dashboard "CW-01 Line" is active on screen with widgets:
- line-chart "Trend", machine CW-01, metric weight, bucket 1h
- gauge "Speed Gauge", machine CW-01, metric speed`
	trAdd := decideAndCall(t, "อยากดูความเร็วเปรียบเทียบกับน้ำหนักหน่อย", noChartCtx, "editor", true, false)
	add := assertToolIn(t, "compare add", trAdd.calls, "preview_add_widget")
	w := widgetArg(add)
	assertArgContains(t, "compare add", toolCallView{name: add.name, args: w}, "type", "chart")
	assertHasFields(t, "compare add", fieldsOf(w), "speed", "weight")

	pace()

	// (b) A custom chart already exists -> UPDATE it (reassign fields). widget_title is the
	// highlight signal: the frontend flashes the chart titled "Metrics".
	chartCtx := `Dashboard "CW-01 Line" is active on screen with widgets:
- [FOCUSED] chart "Metrics", machine CW-01, fields weight, bucket 1h`
	trUpd := decideAndCall(t, "เปรียบเทียบ speed กับ weight หน่อย @Metrics", chartCtx, "editor", true, true)
	upd := assertToolIn(t, "compare update", trUpd.calls, "preview_update_widget")
	assertArgContains(t, "compare update", upd, "widget_title", "Metrics")
	assertHasFields(t, "compare update", fieldsOf(upd.args), "speed", "weight")
}

// ── 3. Fan-out + typos (router robustness) ────────────────────────────────────────

func TestComplexFlowsFanOutAndTypos(t *testing.T) {
	liveKeyOrSkip(t)

	// "Show every value of CW-01" -> either get_machines (then show_metric x N) or a direct
	// show_metric. Highlight signal: the machine the reads target.
	tr := decideAndCall(t, "ขอดูทุกค่าของ CW-01", "", "editor", true, false)
	first := assertToolIn(t, "fanout r0", tr.calls, "get_machines", "show_metric")
	if first.name == "show_metric" {
		assertArgContains(t, "fanout r0", first, "machine", "CW-01")
	} else {
		pace()
		nr := nextRound(t, tr, map[string]any{
			"get_machines": map[string]any{"machines": []any{map[string]any{"name": "CW-01", "fields": []any{"speed", "weight", "temperature"}}}},
		})
		assertToolIn(t, "fanout r1", nr.calls, "show_metric")
	}

	pace()

	// Typo'd Thai "create dashboard" ("ส้างแดชบอด") must still classify create_dashboard
	// -> preview_dashboard.
	trTypo := decideAndCall(t, "ส้างแดชบอด CW-01 ให้หน่อย", "", "editor", true, false)
	assertToolIn(t, "typo create", trTypo.calls, "preview_dashboard")
}

// ── 4. Production / alerts / ambiguous ────────────────────────────────────────────

func TestComplexFlowsProductionAlertsAmbiguous(t *testing.T) {
	liveKeyOrSkip(t)

	// Production count -> get_production_count. machine_id + bucket are the count-widget
	// highlight signal.
	trProd := decideAndCall(t, "ผลิตกี่ชิ้นวันนี้ CW-01", "", "editor", true, false)
	prod := assertToolIn(t, "production", trProd.calls, "get_production_count")
	assertArgContains(t, "production", prod, "machine_id", "CW-01")

	pace()

	// Active alerts -> get_active_alerts (org-scoped, no machine slot).
	trAlerts := decideAndCall(t, "ตอนนี้มีแจ้งเตือนอะไรบ้าง", "", "editor", true, false)
	assertToolIn(t, "alerts", trAlerts.calls, "get_active_alerts")

	pace()

	// Ambiguous "fix it for me" with no clear target -> must ask, not write blindly.
	trAmb := decideAndCall(t, "แก้ให้หน่อย", "", "editor", false, false)
	assertNoBlindWrite(t, "ambiguous", trAmb)
}
