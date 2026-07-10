package ai

// Live check that a "view yesterday" phrasing on a focused line-chart routes to the
// EDIT tool (preview_update_widget), not a read tool (get_telemetry_series/_trend) — the
// bug where the AI summarized the current window instead of changing it. Exercises the
// exact prompt the Chat handler builds for this flow (systemPromptContextExt + dateEditRule
// + an authoritative dashboard-state message + forced tool_choice:"required").
// Skips without GROQ_API_KEY. Run:
//   cd backend; go test ./internal/modules/ai/ -run TestDateEditRoutesToUpdate -v
//
// Prompt-only routing is probabilistic — run a few times to gauge reliability.

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"iot-dashboard/internal/config"

	"github.com/joho/godotenv"
)

func TestDateEditRoutesToUpdate(t *testing.T) {
	_ = godotenv.Load("../../../../.env", "../../../.env")
	key := os.Getenv("GROQ_API_KEY")
	if key == "" {
		t.Skip("GROQ_API_KEY not set — skipping live date-edit routing test")
	}
	config.Env = &config.Config{GroqApiKey: key}

	// Same assembly as controller.Chat on the focused tool path.
	sp := systemPromptBase + systemPromptContextExt + dateEditRule()
	ctxContent := "Authoritative current dashboard state (overrides anything said earlier):\n" +
		`- [FOCUSED] line-chart "Trend", machine CW-01, metric weight, ` +
		"window 2026-07-05T22:55 → 2026-07-06T21:02"
	tools := buildGroqTools("editor", true)
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	// Both phrasings mean "yesterday". "วันก่อนหน้า" (previous day) once matched no classifier
	// regex and role-played the edit in prose; "เมื่อวาน" is the original bug phrasing.
	for i, msg := range []string{"อยากดูเวลาเมื่อวาน @Trend", "ดูวันก่อนหน้า @Trend"} {
		if i > 0 {
			time.Sleep(10 * time.Second) // dodge free-tier 8k tok/min limit
		}
		msgs := []groqMessage{
			{Role: "system", Content: &sp},
			{Role: "user", Content: strPtr(msg)},
			{Role: "system", Content: &ctxContent},
		}
		// Exercise the real deterministic path: force preview_update_widget BY NAME (object
		// tool_choice through callGroqModel), the same as Chat does for a focused relative-date.
		resp, _, err := callGroqModel(groqModel, msgs, tools, forceFunc("preview_update_widget"))
		if err != nil {
			t.Fatalf("[%s] groq error: %v", msg, err)
		}
		if len(resp.Choices) == 0 {
			t.Fatalf("[%s] no choices returned", msg)
		}
		calls := resp.Choices[0].Message.ToolCalls
		if len(calls) == 0 {
			t.Fatalf("[%s] model answered in prose instead of calling a tool: %v", msg, resp.Choices[0].Message.Content)
		}
		if name := calls[0].Function.Name; name != "preview_update_widget" {
			t.Errorf("[%s] routed to %q, want preview_update_widget", msg, name)
		}

		// The window should resolve to yesterday relative to today — not the on-screen window.
		var args struct {
			StartDate string `json:"start_date"`
			EndDate   string `json:"end_date"`
		}
		_ = json.Unmarshal([]byte(calls[0].Function.Arguments), &args)
		if !strings.HasPrefix(args.StartDate, yesterday) {
			t.Errorf("[%s] start_date = %q, want yesterday %s", msg, args.StartDate, yesterday)
		}
	}
}
