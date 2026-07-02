package ai

import (
	"testing"
	"time"
)

// shortTimePtr must render UTC telemetry in plant-local (+7) wall-clock,
// including the day roll-over that caused the AI/chart mismatch.
func TestShortTimePtrBangkok(t *testing.T) {
	utc := time.Date(2026, 7, 1, 21, 0, 0, 0, time.UTC)
	if got := shortTimePtr(&utc); got != "2026-07-02T04:00" {
		t.Fatalf("shortTimePtr = %q, want 2026-07-02T04:00", got)
	}
	if got := shortTimePtr(nil); got != "" {
		t.Fatalf("shortTimePtr(nil) = %q, want empty", got)
	}
}
