package telemetry

import (
	"testing"
	"time"
)

func TestParseBucket(t *testing.T) {
	ok := []struct {
		in       string
		interval string
		every    time.Duration
	}{
		{"1m", "1 minutes", time.Minute},
		{"30m", "30 minutes", 30 * time.Minute},
		{"2h", "2 hours", 2 * time.Hour},
		{"1d", "1 days", 24 * time.Hour},
		{"7m", "7 minutes", 7 * time.Minute},
	}
	for _, c := range ok {
		gotI, gotE, err := parseBucket(c.in)
		if err != nil || gotI != c.interval || gotE != c.every {
			t.Errorf("parseBucket(%q) = (%q, %v, %v); want (%q, %v, nil)", c.in, gotI, gotE, err, c.interval, c.every)
		}
	}

	// Garbage and injection attempts must be rejected — these would otherwise reach the SQL string.
	bad := []string{"", "m", "1", "1s", "abc", "1 minute", "1; DROP TABLE telemetry_raw", "1 year'); --", "99999d", "0m", "-5m"}
	for _, in := range bad {
		if _, _, err := parseBucket(in); err == nil {
			t.Errorf("parseBucket(%q) should have errored", in)
		}
	}
}

func TestNormalizeStatus(t *testing.T) {
	for in, want := range map[string]string{"": "all", "all": "all", "good": "good", "reject": "reject"} {
		got, err := normalizeStatus(in)
		if err != nil || got != want {
			t.Errorf("normalizeStatus(%q) = %q, %v; want %q, nil", in, got, err, want)
		}
	}
	for _, in := range []string{"bad", "GOOD", "all;--", "0", "rejected"} {
		if _, err := normalizeStatus(in); err == nil {
			t.Errorf("normalizeStatus(%q) should have errored", in)
		}
	}
}
