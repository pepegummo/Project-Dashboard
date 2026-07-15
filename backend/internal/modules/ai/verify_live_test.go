package ai

// Live fixtures for Phase 5's VerifyAnswer (LLM verify step) — exercises the real
// Groq call (routerModel, forced tool_choice on verify_answer) with known-wrong,
// known-right, and ambiguous-from-start cases. Skips without GROQ_API_KEY. Run:
//   cd backend; go test ./internal/modules/ai/ -run TestVerifyAnswerLive -v
//
// Prompt-only judging is probabilistic like the other live tests in this
// package — run a few times to gauge reliability, not a single pass/fail.

import (
	"context"
	"os"
	"testing"
	"time"

	"iot-dashboard/internal/config"

	"github.com/joho/godotenv"
)

func TestVerifyAnswerLive(t *testing.T) {
	_ = godotenv.Load("../../../../.env", "../../../.env")
	key := os.Getenv("GROQ_API_KEY")
	if key == "" {
		t.Skip("GROQ_API_KEY not set — skipping live verify-answer test")
	}
	config.Env = &config.Config{AIApiKey: key}

	t.Run("known-wrong", func(t *testing.T) {
		// User asked for CW-01's speed; the answer describes CW-02's temperature —
		// wrong metric AND wrong machine.
		vr, ok := VerifyAnswer(context.Background(),
			"ความเร็ว CW-01 เท่าไหร่",
			"read_metric (machine CW-01, metric speed)",
			"อุณหภูมิของ CW-02 ตอนนี้อยู่ที่ 78 องศาเซลเซียส",
			"show_metric({\"machine\":\"CW-02\",\"metric\":\"temperature\"})")
		if !ok {
			t.Fatal("VerifyAnswer returned no verdict — infra failure, can't judge this fixture")
		}
		if vr.MatchesIntent {
			t.Errorf("MatchesIntent = true, want false (wrong machine/metric); problem=%q", vr.Problem)
		}
	})

	time.Sleep(3 * time.Second) // dodge free-tier 8k tok/min limit

	t.Run("known-right", func(t *testing.T) {
		vr, ok := VerifyAnswer(context.Background(),
			"ความเร็ว CW-01 เท่าไหร่",
			"read_metric (machine CW-01, metric speed)",
			"ความเร็วของ CW-01 ตอนนี้อยู่ที่ 42 m/min",
			"show_metric({\"machine\":\"CW-01\",\"metric\":\"speed\"})")
		if !ok {
			t.Fatal("VerifyAnswer returned no verdict — infra failure, can't judge this fixture")
		}
		if !vr.MatchesIntent {
			t.Errorf("MatchesIntent = false, want true (correct machine/metric); problem=%q", vr.Problem)
		}
	})

	time.Sleep(3 * time.Second)

	t.Run("ambiguous", func(t *testing.T) {
		// Router declined (genuinely vague message); the answer is a generic,
		// non-committal reply that doesn't itself ask for clarification. Expect a
		// mismatch verdict WITH a non-empty clarifying_question when the verifier
		// flags it.
		vr, ok := VerifyAnswer(context.Background(),
			"แก้ให้หน่อย",
			"router declined",
			"รับทราบครับ",
			"none")
		if !ok {
			t.Fatal("VerifyAnswer returned no verdict — infra failure, can't judge this fixture")
		}
		if !vr.MatchesIntent && vr.ClarifyingQuestion == "" {
			t.Errorf("mismatch verdict with empty clarifying_question — want a follow-up question when flagging ambiguity (problem=%q)", vr.Problem)
		}
	})
}
