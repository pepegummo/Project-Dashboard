package ai

// TestRouterBakeOff — live model comparison for the classify_intent router (Phase 2).
// Standalone from TestBakeOff (eval_test.go): it does not exercise the chat pipeline or
// tool dispatch, only classifyIntentWithModel's single forced tool call. Skips without
// GROQ_API_KEY, same .env-loading pattern as TestBakeOff, and continues past rate limits
// (classifyIntentWithModel/callGroqModel already retry quick 429 blips internally; a long
// wait surfaces as an error here and is just logged, not retried, per the brief).
//
// Run:  cd backend && GROQ_API_KEY=... go test ./internal/modules/ai/ -run RouterBakeOff -v

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"iot-dashboard/internal/config"

	"github.com/joho/godotenv"
)

// routerBakeModels: llama-3.1-8b-instant is ClassifyIntent's shipped default (routerModel);
// gpt-oss-20b is the main chat model's own pick (controller.go) offered here as a second
// data point on a fast/cheap model. Both sit in a different Groq rate bucket than the
// qwen/gpt-oss models TestBakeOff exercises, so this should see fewer 429s than TestBakeOff.
var routerBakeModels = []string{
	"llama-3.1-8b-instant",
	"openai/gpt-oss-20b",
}

type routerCase struct {
	label       string
	message     string
	contextLine string // optional one-line context summary passed as ClassifyIntent's contextSummary
	wantIntent  string
}

// legacyIntentCases re-labels the 24 bakeCases (eval_test.go) with the router's expected
// intent. There is no clean 1:1 mapping from "first tool called" to the router's 8-value
// intent enum, so the following judgment calls were made:
//   - get_machines / list_dashboards / get_skus (pure listing queries, no single metric or
//     widget target) and the two genuinely topic-less no-tool cases (greeting, ambiguous
//     "what machines are there" aside) → chat. None of the 8 intents cover "list X"; chat is
//     the router's catch-all for "no dashboard data/action implied by a specific slot".
//   - show_metric, and a slot-missing read ("speed เท่าไหร่") → read_metric (missing the
//     machine slot doesn't change the READ intent — slots are left empty, never guessed).
//   - get_telemetry_series (analytical/trend reads) → read_agg.
//   - get_production_count (a focused count-widget's current value) → production, since the
//     underlying tool and topic are specifically piece/production counts.
//   - get_active_alerts, and the alert-RULE-management redirect (no tool exists for rule
//     creation) → alerts, on topic match — Task 3's chat handler still needs its own redirect
//     logic for rule management; the router only tags the topic.
//   - preview_add_widget / preview_remove_widget / preview_update_widget, and the ambiguous
//     "แก้ให้หน่อย" ("please fix/change it") clarify-case → edit_widget. The router's
//     edit_widget definition explicitly covers add/remove/change of an on-screen widget.
//     add-custom-chart (2-metric overlay via preview_add_widget, a NEW widget) is kept as
//     edit_widget rather than compare — compare is reserved for reassigning fields[] on an
//     ALREADY-focused chart, not staging a brand new one.
//   - preview_dashboard (incl. the typo'd variant) → create_dashboard.
var legacyIntentCases = []routerCase{
	{label: "greeting", message: "สวัสดีครับ", wantIntent: "chat"},
	{label: "read-speed", message: "speed ของ CW-01 เท่าไหร่", wantIntent: "read_metric"},
	{label: "english-read", message: "what's the speed of CW-01", wantIntent: "read_metric"},
	{label: "all-metrics", message: "ขอดูทุกค่าของ CW-01 หน่อย", wantIntent: "read_metric"},
	{
		label:       "detail-analytical-focused",
		message:     "@Speed Trend แนวโน้มเป็นยังไง วิเคราะห์หน่อย",
		contextLine: "focused widget: Speed Trend (line-chart, machine CW-01, metric speed)",
		wantIntent:  "read_agg",
	},
	{
		label:       "change-preview-edit",
		message:     "เปลี่ยน metric เป็น temperature",
		contextLine: "preview dashboard CW-01 Overview, widget: Trend (line-chart, machine CW-01, metric speed)",
		wantIntent:  "edit_widget",
	},
	{
		label:       "add-preview-widget",
		message:     "เพิ่ม widget อุณหภูมิ CW-01 ด้วย",
		contextLine: "preview dashboard CW-01 Overview, widget: Trend (line-chart, machine CW-01, metric speed)",
		wantIntent:  "edit_widget",
	},
	{
		label:       "delete-preview-widget",
		message:     "ลบ widget Trend ออก",
		contextLine: "preview dashboard CW-01 Overview, widget: Trend (line-chart, machine CW-01, metric speed)",
		wantIntent:  "edit_widget",
	},
	{
		label:       "add-to-active-dashboard",
		message:     "เพิ่ม widget speed ของ CW-01 ด้วย",
		contextLine: "active dashboard: Production Line, widget: Speed Gauge (gauge, machine CW-01, metric speed)",
		wantIntent:  "edit_widget",
	},
	{
		label:       "remove-from-active-dashboard",
		message:     "ลบ widget Speed Gauge ออก",
		contextLine: "active dashboard: Production Line, widget: Speed Gauge (gauge, machine CW-01, metric speed)",
		wantIntent:  "edit_widget",
	},
	{
		label:       "add-custom-chart",
		message:     "เพิ่มกราฟรวม speed กับ throughput ของ CW-01",
		contextLine: "active dashboard: Production Line, widget: Speed Gauge (gauge, machine CW-01, metric speed)",
		wantIntent:  "edit_widget",
	},
	{label: "create", message: "สร้าง dashboard ของ CW-01 ให้หน่อย", wantIntent: "create_dashboard"},
	{label: "typo-create", message: "ส้างแดชบอด cw-01 ให้หน่อย", wantIntent: "create_dashboard"},
	{label: "list-dashboards", message: "มี dashboard อะไรบ้าง", wantIntent: "chat"},
	{label: "list-skus", message: "CW-01 มี SKU อะไรบ้าง", wantIntent: "chat"},
	{label: "active-alerts", message: "ตอนนี้มีแจ้งเตือนอะไรบ้าง", wantIntent: "alerts"},
	{label: "alert-rule-trap", message: "ตั้ง alert ให้หน่อย ถ้า speed ของ CW-01 เกิน 100 ให้เตือน", wantIntent: "alerts"},
	{label: "trap-action-but-read", message: "ถ้าฉันอยากสร้าง dashboard แล้วตอนนี้มีเครื่องอะไรบ้าง", wantIntent: "chat"},
	{label: "ambiguous-fix", message: "แก้ให้หน่อย", wantIntent: "edit_widget"},
	{label: "read-no-machine", message: "speed เท่าไหร่", wantIntent: "read_metric"},
	{
		label:       "focused-gauge-analytical",
		message:     "แนวโน้มเป็นยังไง วิเคราะห์หน่อย",
		contextLine: "focused widget: Speed Gauge (gauge, machine CW-01, metric speed)",
		wantIntent:  "read_agg",
	},
	{
		label:       "focused-count-now",
		message:     "ตอนนี้เท่าไหร่",
		contextLine: "focused widget: CW-01 Count (daily-count, machine CW-01, bucket 1h)",
		wantIntent:  "production",
	},
	{
		label:       "focused-alarm-panel",
		message:     "ตอนนี้เป็นยังไงบ้าง",
		contextLine: "focused widget: Alarms (alarm-panel, machine CW-01)",
		wantIntent:  "alerts",
	},
	{
		label:       "compound-read-write",
		message:     "เพิ่ม widget อุณหภูมิ CW-01 ด้วย แต่ก่อนอื่นบอกหน่อยตอนนี้ speed เท่าไหร่",
		contextLine: "active dashboard: Production Line, widget: Speed Gauge (gauge, machine CW-01, metric speed)",
		wantIntent:  "read_metric",
	},
}

// newRouterCases: 8 additional cases from the Task 2 brief, targeting typo tolerance,
// synonym reads, and the router-specific EDIT slots (bucket/relative-date/compare) that
// TestBakeOff's cases don't isolate.
var newRouterCases = []routerCase{
	{label: "typo-create-th", message: "ส้างแดชบอด cw-01", wantIntent: "create_dashboard"},
	{label: "typo-create-en", message: "creat dashbord for cw-01", wantIntent: "create_dashboard"},
	{label: "synonym-read", message: "how fast is CW-01 running", wantIntent: "read_metric"},
	{
		label:       "bucket-edit",
		message:     "อยากดู 22 นาที",
		contextLine: "focused widget: CW-01 Count (daily-count, machine CW-01, bucket 15m)",
		wantIntent:  "edit_widget",
	},
	{
		label:       "relative-date-edit",
		message:     "ดูของเมื่อวาน",
		contextLine: "focused widget: Trend (line-chart, machine CW-01, metric weight)",
		wantIntent:  "edit_widget",
	},
	// ผลิตกี่ชิ้นใน 22 นาที = "how many pieces produced in 22 minutes" — a piece-count
	// aggregate, so production (topic match) over the more generic read_agg.
	{label: "agg-production-read", message: "ผลิตกี่ชิ้นใน 22 นาที", wantIntent: "production"},
	{label: "compare-metrics", message: "เปรียบเทียบ speed กับ temp", wantIntent: "compare"},
	{label: "greeting-short", message: "สวัสดี", wantIntent: "chat"},
}

func TestRouterBakeOff(t *testing.T) {
	_ = godotenv.Load("../../../../.env", "../../../.env")
	key := os.Getenv("GROQ_API_KEY")
	if key == "" {
		t.Skip("GROQ_API_KEY not set — skipping live router bake-off")
	}
	config.Env = &config.Config{GroqApiKey: key}

	cases := make([]routerCase, 0, len(legacyIntentCases)+len(newRouterCases))
	cases = append(cases, legacyIntentCases...)
	cases = append(cases, newRouterCases...)

	type tally struct {
		score, total int
		lats         []time.Duration
	}
	scores := map[string]tally{}

	for mi, model := range routerBakeModels {
		if mi > 0 {
			time.Sleep(60 * time.Second) // let the shared per-model TPM budget recover
		}
		fmt.Printf("\n========== ROUTER MODEL: %s ==========\n", model)
		for _, tc := range cases {
			fmt.Printf("\n[%s] %q (want %s)\n", tc.label, tc.message, tc.wantIntent)
			time.Sleep(5 * time.Second) // dodge free-tier rate limits

			result, ok, lat := classifyIntentWithModel(context.Background(), model, tc.message, tc.contextLine)

			tt := scores[model]
			if !ok {
				fmt.Printf("  ERROR / not-ok (invalid JSON, unknown intent, or confidence < %.1f)\n", routerConfidenceFloor)
				tt.total++
				scores[model] = tt
				continue
			}
			tt.total++
			tt.lats = append(tt.lats, lat)
			status := "FAIL"
			if result.Intent == tc.wantIntent {
				status = "PASS"
				tt.score++
			}
			scores[model] = tt
			fmt.Printf("  -> intent=%s confidence=%.2f machine=%q metric=%q bucket=%q targetWidget=%q\n",
				result.Intent, result.Confidence, result.Machine, result.Metric, result.Bucket, result.TargetWidget)
			fmt.Printf("  %s (want %q, got %q)  latency %.2fs\n", status, tc.wantIntent, result.Intent, lat.Seconds())
		}
	}

	fmt.Printf("\n========== ROUTER SCOREBOARD ==========\n")
	for _, model := range routerBakeModels {
		tt := scores[model]
		n := len(tt.lats)
		medLat := 0.0
		if n > 0 {
			sort.Slice(tt.lats, func(i, j int) bool { return tt.lats[i] < tt.lats[j] })
			if n%2 == 1 {
				medLat = tt.lats[n/2].Seconds()
			} else {
				medLat = (tt.lats[n/2-1] + tt.lats[n/2]).Seconds() / 2
			}
		}
		fmt.Printf("%-24s %d/%d   median latency %.2fs (n=%d)\n", model, tt.score, tt.total, medLat, n)
	}
}
