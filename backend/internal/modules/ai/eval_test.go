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
	"strings"
	"testing"
	"time"

	"iot-dashboard/internal/config"

	"github.com/joho/godotenv"
)

var bakeModels = []string{
	"qwen/qwen3-32b",     // current
	"openai/gpt-oss-20b", // cache-supported + cheap
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
	{label: "greeting-informal", message: "หวัดดี", expect: "no tool, Thai reply", want: ""},

	// ── Ask detail (read / analytical) ───────────────────────────────────────
	{label: "read-speed", message: "speed ของ CW-01 เท่าไหร่", expect: "show_metric", want: "show_metric"},
	{label: "read-speed-thai", message: "CW-01 ตอนนี้เร็วเท่าไหร่", expect: "show_metric", want: "show_metric"},
	{label: "read-temp-informal", message: "ดูอุณหภูมิ CW-01 หน่อย", expect: "show_metric (value or trend)", want: "show_metric"},
	{label: "english-read", message: "what's the speed of CW-01", expect: "show_metric, English reply", want: "show_metric"},
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
	{label: "create", message: "สร้าง dashboard ของ CW-01 ให้หน่อย", expect: "preview_dashboard (NOT create)", want: "preview_dashboard"},

	// ── Other (list / skus) ──────────────────────────────────────────────────
	{label: "list-dashboards", message: "มี dashboard อะไรบ้าง", expect: "list_dashboards", want: "list_dashboards"},
	{label: "list-skus", message: "CW-01 มี SKU อะไรบ้าง", expect: "get_skus", want: "get_skus"},

	// ── Slot-filling traps: a read needs a machine but none is named ──────────
	{label: "trap-action-but-read", message: "สร้าง dashboard สิ แล้วตอนนี้มีเครื่องอะไรบ้าง", expect: "get_machines (read) — NOT preview/create", want: "get_machines"},
	{label: "ambiguous-fix", message: "แก้ให้หน่อย", expect: "clarifying question in Thai, no tool", want: ""},
	{label: "ambiguous-change", message: "เปลี่ยนหน่อย", expect: "clarifying question in Thai, no tool", want: ""},
	{label: "read-no-machine", message: "speed เท่าไหร่", expect: "ask which machine in Thai — NO tool, NO guessed machine_id", want: ""},
	{label: "read-no-machine-en", message: "show me the temperature", expect: "ask which machine in English — NO tool, NO guessed machine_id", want: ""},
}

func TestBakeOff(t *testing.T) {
	// Load GROQ_API_KEY from .env (repo root or backend/) or the ambient env.
	_ = godotenv.Load("../../../../.env", "../../../.env")
	key := os.Getenv("GROQ_API_KEY")
	if key == "" {
		t.Skip("GROQ_API_KEY not set — skipping live model bake-off")
	}
	config.Env = &config.Config{GroqApiKey: key}

	tools := buildGroqTools("admin", true) // full tool set for bake-off

	type tally struct{ score, total int }
	scores := map[string]tally{}

	for _, model := range bakeModels {
		fmt.Printf("\n========== MODEL: %s ==========\n", model)
		for _, tc := range bakeCases {
			sp := systemPromptBase + systemPromptContextExt // bake-off always uses full prompt
			msgs := []groqMessage{{Role: "system", Content: &sp}}
			msgs = append(msgs, tc.history...)
			msgs = append(msgs, groqMessage{Role: "user", Content: strPtr(tc.message)})
			if tc.context != "" {
				ctxContent := "Authoritative current dashboard state (overrides anything said earlier):\n" + tc.context
				msgs = append(msgs, groqMessage{Role: "system", Content: &ctxContent})
			}

			fmt.Printf("\n[%s] %q\n  expect: %s\n", tc.label, tc.message, tc.expect)

			time.Sleep(10 * time.Second) // dodge free-tier rate limits (8k tokens/min)
			resp, err := callGroqModel(model, msgs, tools, "")
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
			status := "FAIL"
			if got == tc.want {
				status = "PASS"
				t.score++
			}
			scores[model] = t
			fmt.Printf("  %s (want %q, got %q)\n", status, tc.want, got)
			if resp.Usage != nil {
				fmt.Printf("  tokens: prompt=%d completion=%d total=%d\n",
					resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
			}
		}
	}

	fmt.Printf("\n========== SCOREBOARD ==========\n")
	for _, model := range bakeModels {
		t := scores[model]
		fmt.Printf("%-24s %d/%d\n", model, t.score, t.total)
	}
}
