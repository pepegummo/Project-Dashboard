package ai

import "testing"

func TestToDatetimeLocal(t *testing.T) {
	cases := []struct {
		in       string
		endOfDay bool
		want     string
	}{
		{"2026-06-20", false, "2026-06-20T00:00"},
		{"2026-06-22", true, "2026-06-22T23:59"},
		{"2026-06-20T08:30:00Z", false, "2026-06-20T08:30"},
		{" 2026-06-20 ", false, "2026-06-20T00:00"},
		{"", false, ""},
	}
	for _, c := range cases {
		if got := toDatetimeLocal(c.in, c.endOfDay); got != c.want {
			t.Errorf("toDatetimeLocal(%q, %v) = %q, want %q", c.in, c.endOfDay, got, c.want)
		}
	}
}


