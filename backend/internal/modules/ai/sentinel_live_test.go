package ai

// Live check for the NEED_TOOLS sentinel in systemPromptMinimal: an action
// message with typos (which the needsTools keyword gate misses) must come back
// as exactly NEED_TOOLS on the no-tool path, while a greeting must NOT.
// Skips without GROQ_API_KEY. Run:
//   cd backend; go test ./internal/modules/ai/ -run TestMinimalPromptSentinel -v

import (
	"os"
	"strings"
	"testing"
	"time"

	"iot-dashboard/internal/config"

	"github.com/joho/godotenv"
)

func TestMinimalPromptSentinel(t *testing.T) {
	_ = godotenv.Load("../../../../.env", "../../../.env")
	key := os.Getenv("GROQ_API_KEY")
	if key == "" {
		t.Skip("GROQ_API_KEY not set — skipping live sentinel test")
	}
	config.Env = &config.Config{GroqApiKey: key}

	cases := []struct {
		label        string
		message      string
		wantSentinel bool
	}{
		{"typo-create", "ส้างแดชบอด cw-01 ให้หน่อย", true},
		{"typo-read", "สปีด cw-01 ตอนนี้เท่าไหร่", true},
		{"greeting", "สวัสดีครับ", false},
		{"chitchat", "what is u doing", false},
	}
	for i, tc := range cases {
		if i > 0 {
			time.Sleep(10 * time.Second) // dodge free-tier 8k tok/min limit
		}
		sp := systemPromptMinimal
		msgs := []groqMessage{
			{Role: "system", Content: &sp},
			{Role: "user", Content: strPtr(tc.message)},
		}
		resp, _, err := callGroqModel(groqModel, msgs, nil, "")
		if err != nil {
			t.Fatalf("[%s] groq error: %v", tc.label, err)
		}
		if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == nil {
			t.Fatalf("[%s] no text choice returned", tc.label)
		}
		got := strings.TrimSpace(*resp.Choices[0].Message.Content)
		if (got == needToolsSentinel) != tc.wantSentinel {
			t.Errorf("[%s] reply %q, wantSentinel=%v", tc.label, got, tc.wantSentinel)
		}
	}
}
