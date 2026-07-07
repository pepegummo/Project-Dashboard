package ai

import (
	"testing"
	"time"
)

// parseRetryAfter must honor Groq's "try again in 16.7475s" hint (not clamp it to
// the old 8s), since that value drives whether we retry server-side or surface to
// the user. +0.3s buffer, capped at 30s.
func TestParseRetryAfter(t *testing.T) {
	body := []byte(`{"error":{"message":"Rate limit reached ... Please try again in 16.7475s. Need more tokens?"}}`)
	got := parseRetryAfter("", body)
	if want := time.Duration((16.7475 + 0.3) * float64(time.Second)); got != want {
		t.Fatalf("body hint: got %v, want %v", got, want)
	}

	if got := parseRetryAfter("2", nil); got != 2*time.Second {
		t.Fatalf("Retry-After header: got %v, want 2s", got)
	}

	if got := parseRetryAfter("120", nil); got != 30*time.Second {
		t.Fatalf("cap: got %v, want 30s", got)
	}
}
