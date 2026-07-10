package ai

// Throwaway model bake-off harness (Phase 1). Compares candidate Groq models on
// Thai-first intent understanding for the IotVision AI assistant.
//
// It only inspects each model's FIRST decision — which tool it picks (or whether
// it answers/clarifies in plain text) — because that IS the "decide what the user
// wants" step. It does not execute tools or make the summary call.
//
// Run:  cd backend && GROQ_API_KEY=... go test ./internal/modules/ai/ -run BakeOff -v
// Delete this file once a model is chosen.

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"iot-dashboard/internal/config"

	"github.com/joho/godotenv"
)

var bakeModels = []string{
	"qwen/qwen3-32b",      // heaviest tokens, most rate-limited — replaced
	"openai/gpt-oss-20b",  // now live (controller.go:23) — cheapest + cache-supported
	"openai/gpt-oss-120b", // cache-supported step-up (kimi-k2 not accessible on this account)
}

type bakeCase struct {
	label   string
	message string
	context string        // optional on-screen preview context
	history []groqMessage // optional prior turns (e.g. a preview was just shown)
	expect  string        // human-readable expectation
	want    string        // expected first tool name, or "" for the no-tool path (greeting/clarify/redirect)
}

// Thai-first cases mirroring the sequence diagram (greeting / ask-detail /
// change-add-delete / other) + the hard slot-filling traps. `want` is the expected
// first tool ("" = no-tool path); TestBakeOff scores got==want per model.
var bakeCases = []bakeCase{
	// ── Greeting ─────────────────────────────────────────────────────────────
	{label: "greeting", message: "สวัสดีครับ", expect: "no tool, Thai reply", want: ""},

	// ── Ask detail (read / analytical) ───────────────────────────────────────
	{label: "read-speed", message: "speed ของ CW-01 เท่าไหร่", expect: "show_metric", want: "show_metric"},
	{label: "english-read", message: "what's the speed of CW-01", expect: "show_metric, English reply", want: "show_metric"},
	// hard: "see ALL metrics" routes to get_machines first (base prompt), not show_metric.
	{label: "all-metrics", message: "ขอดูทุกค่าของ CW-01 หน่อย", expect: "get_machines (all fields) — NOT show_metric", want: "get_machines"},
	{
		label:   "detail-analytical-focused",
		message: "@Speed Trend แนวโน้มเป็นยังไง วิเคราะห์หน่อย",
		context: `[FOCUSED] widget: Speed Trend | type line-chart | machine CW-01 | metric speed`,
		expect:  "get_telemetry_series (analytical focused read)",
		want:    "get_telemetry_series",
	},

	// ── Change / Add / Delete ────────────────────────────────────────────────
	{
		label:   "change-preview-edit",
		message: "เปลี่ยน metric เป็น temperature",
		context: `{"dashboardName":"CW-01 Overview","widgets":[{"type":"line-chart","title":"Trend","machine":"CW-01","metric":"speed"}]}`,
		history: []groqMessage{
			{Role: "user", Content: strPtr("สร้าง dashboard ของ CW-01")},
			{Role: "assistant", Content: strPtr("นี่คือ preview dashboard ของ CW-01 ครับ กดยืนยันเพื่อสร้าง")},
		},
		expect: "preview_update_widget",
		want:   "preview_update_widget",
	},
	{
		label:   "add-preview-widget",
		message: "เพิ่ม widget อุณหภูมิ CW-01 ด้วย",
		context: `{"dashboardName":"CW-01 Overview","widgets":[{"type":"line-chart","title":"Trend","machine":"CW-01","metric":"speed"}]}`,
		history: []groqMessage{
			{Role: "user", Content: strPtr("สร้าง dashboard ของ CW-01")},
			{Role: "assistant", Content: strPtr("นี่คือ preview dashboard ของ CW-01 ครับ")},
		},
		expect: "preview_add_widget",
		want:   "preview_add_widget",
	},
	{
		label:   "delete-preview-widget",
		message: "ลบ widget Trend ออก",
		context: `{"dashboardName":"CW-01 Overview","widgets":[{"type":"line-chart","title":"Trend","machine":"CW-01","metric":"speed"}]}`,
		history: []groqMessage{
			{Role: "user", Content: strPtr("สร้าง dashboard ของ CW-01")},
			{Role: "assistant", Content: strPtr("นี่คือ preview dashboard ของ CW-01 ครับ")},
		},
		expect: "preview_remove_widget",
		want:   "preview_remove_widget",
	},
	{
		label:   "add-to-active-dashboard",
		message: "เพิ่ม widget speed ของ CW-01 ด้วย",
		context: `{"activeDashboard":"Production Line","widgets":[{"type":"gauge","title":"Speed Gauge","machine":"CW-01","metric":"speed"}]}`,
		expect:  "preview_add_widget (stage on the open Active dashboard — no direct write)",
		want:    "preview_add_widget",
	},
	{
		label:   "remove-from-active-dashboard",
		message: "ลบ widget Speed Gauge ออก",
		context: `{"activeDashboard":"Production Line","widgets":[{"type":"gauge","title":"Speed Gauge","machine":"CW-01","metric":"speed"}]}`,
		expect:  "preview_remove_widget (stage removal — persisted only on Save)",
		want:    "preview_remove_widget",
	},
	{
		label:   "add-custom-chart",
		message: "เพิ่มกราฟรวม speed กับ throughput ของ CW-01",
		context: `{"activeDashboard":"Production Line","widgets":[{"type":"gauge","title":"Speed Gauge","machine":"CW-01","metric":"speed"}]}`,
		expect:  "preview_add_widget (custom chart overlaying 2 metrics — stage, no direct write)",
		want:    "preview_add_widget",
	},
	{label: "create", message: "สร้าง dashboard ของ CW-01 ให้หน่อย", expect: "preview_dashboard (NOT create)", want: "preview_dashboard"},
	// Typo'd create — the needsTools gate misses this at runtime (sentinel
	// escalates it); here we verify the model still picks the right tool once
	// tools are present.
	{label: "typo-create", message: "ส้างแดชบอด cw-01 ให้หน่อย", expect: "preview_dashboard despite typos", want: "preview_dashboard"},

	// ── Other (list / skus / alerts) ─────────────────────────────────────────
	{label: "list-dashboards", message: "มี dashboard อะไรบ้าง", expect: "list_dashboards", want: "list_dashboards"},
	{label: "list-skus", message: "CW-01 มี SKU อะไรบ้าง", expect: "get_skus", want: "get_skus"},
	{label: "active-alerts", message: "ตอนนี้มีแจ้งเตือนอะไรบ้าง", expect: "get_active_alerts", want: "get_active_alerts"},
	// hard trap: alert-RULE management is a plain-text redirect (Alerts page), NOT a tool call.
	{label: "alert-rule-trap", message: "ตั้ง alert ให้หน่อย ถ้า speed ของ CW-01 เกิน 100 ให้เตือน", expect: "no tool — redirect to Alerts page in Thai", want: ""},

	// ── Slot-filling traps: a read needs a machine but none is named ──────────
	{label: "trap-action-but-read", message: "ถ้าฉันอยากสร้าง dashboard แล้วตอนนี้มีเครื่องอะไรบ้าง", expect: "get_machines (read) — NOT preview/create", want: "get_machines"},
	{label: "ambiguous-fix", message: "แก้ให้หน่อย", expect: "clarifying question in Thai, no tool", want: ""},
	{label: "read-no-machine", message: "speed เท่าไหร่", expect: "ask which machine in Thai — NO tool, NO guessed machine_id", want: ""},

	// ── Focused-widget routing traps (weak models default to show_metric) ────
	{
		label:   "focused-gauge-analytical",
		message: "แนวโน้มเป็นยังไง วิเคราะห์หน่อย",
		context: `[FOCUSED] widget: Speed Gauge | type gauge | machine CW-01 | metric speed`,
		expect:  "get_telemetry_series (analytical → full series, NOT show_metric)",
		want:    "get_telemetry_series",
	},
	{
		label:   "focused-count-now",
		message: "ตอนนี้เท่าไหร่",
		context: `[FOCUSED] widget: CW-01 Count | type daily-count | machine CW-01 | bucket 1h`,
		expect:  "get_production_count (count widget → NEVER show_metric)",
		want:    "get_production_count",
	},
	{
		label:   "focused-alarm-panel",
		message: "ตอนนี้เป็นยังไงบ้าง",
		context: `[FOCUSED] widget: Alarms | type alarm-panel | machine CW-01`,
		expect:  "get_active_alerts (alarm-panel widget)",
		want:    "get_active_alerts",
	},
	{
		label:   "compound-read-write",
		message: "เพิ่ม widget อุณหภูมิ CW-01 ด้วย แต่ก่อนอื่นบอกหน่อยตอนนี้ speed เท่าไหร่",
		context: `{"activeDashboard":"Production Line","widgets":[{"type":"gauge","title":"Speed Gauge","machine":"CW-01","metric":"speed"}]}`,
		expect:  "show_metric (serve the read first, then ask about the add)",
		want:    "show_metric",
	},
}

func TestBakeOff(t *testing.T) {
	// Load GROQ_API_KEY from .env (repo root or backend/) or the ambient env.
	_ = godotenv.Load("../../../../.env", "../../../.env")
	key := os.Getenv("GROQ_API_KEY")
	if key == "" {
		t.Skip("GROQ_API_KEY not set — skipping live model bake-off")
	}
	config.Env = &config.Config{GroqApiKey: key}

	tools := buildGroqTools("admin") // full tool set for bake-off

	type tally struct {
		score, total int
		lats         []time.Duration // wall time of each completed (non-error) call
	}
	scores := map[string]tally{}

	for mi, model := range bakeModels {
		if mi > 0 {
			time.Sleep(120 * time.Second) // let the shared 8k-tok/min budget recover between models
		}
		fmt.Printf("\n========== MODEL: %s ==========\n", model)
		for _, tc := range bakeCases {
			sp := systemPromptUnified // bake-off always uses full prompt
			msgs := []groqMessage{{Role: "system", Content: &sp}}
			msgs = append(msgs, tc.history...)
			msgs = append(msgs, groqMessage{Role: "user", Content: strPtr(tc.message)})
			if tc.context != "" {
				ctxContent := "Authoritative current dashboard state (overrides anything said earlier):\n" + tc.context + "\n" + dateLineForRequest()
				msgs = append(msgs, groqMessage{Role: "system", Content: &ctxContent})
			} else {
				dateContent := dateLineForRequest()
				msgs = append(msgs, groqMessage{Role: "system", Content: &dateContent})
			}

			fmt.Printf("\n[%s] %q\n  expect: %s\n", tc.label, tc.message, tc.expect)

			time.Sleep(10 * time.Second) // dodge free-tier rate limits (8k tokens/min)
			// httpLat is the successful HTTP round only — excludes callGroqModel's internal
			// 429 retry sleeps, so latency reflects model speed, not rate-limit backoff.
			resp, httpLat, err := callGroqModel(model, msgs, tools, "")
			if err != nil {
				fmt.Printf("  ERROR: %v\n", err)
				continue
			}
			if len(resp.Choices) == 0 {
				fmt.Printf("  ERROR: no choices\n")
				continue
			}
			ch := resp.Choices[0]
			got := "" // first tool name, or "" for the no-tool (text) path
			if ch.FinishReason == "tool_calls" && len(ch.Message.ToolCalls) > 0 {
				got = ch.Message.ToolCalls[0].Function.Name
				var picks []string
				for _, tcall := range ch.Message.ToolCalls {
					picks = append(picks, tcall.Function.Name+"("+tcall.Function.Arguments+")")
				}
				fmt.Printf("  -> TOOL: %s\n", strings.Join(picks, ", "))
			} else {
				txt := ""
				if ch.Message.Content != nil {
					txt = strings.TrimSpace(*ch.Message.Content)
				}
				fmt.Printf("  -> TEXT: %s\n", txt)
			}
			t := scores[model]
			t.total++
			t.lats = append(t.lats, httpLat)
			status := "FAIL"
			if got == tc.want {
				status = "PASS"
				t.score++
			}
			scores[model] = t
			fmt.Printf("  %s (want %q, got %q)\n", status, tc.want, got)
			fmt.Printf("  latency (http): %.2fs\n", httpLat.Seconds())
			if resp.Usage != nil {
				fmt.Printf("  tokens: prompt=%d completion=%d total=%d\n",
					resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
			}
		}
	}

	fmt.Printf("\n========== SCOREBOARD ==========\n")
	for _, model := range bakeModels {
		t := scores[model]
		n := len(t.lats)
		medLat := 0.0
		if n > 0 {
			sort.Slice(t.lats, func(i, j int) bool { return t.lats[i] < t.lats[j] })
			if n%2 == 1 {
				medLat = t.lats[n/2].Seconds()
			} else {
				medLat = (t.lats[n/2-1] + t.lats[n/2]).Seconds() / 2
			}
		}
		fmt.Printf("%-24s %d/%d   median latency %.2fs (n=%d)\n", model, t.score, t.total, medLat, n)
	}
}
