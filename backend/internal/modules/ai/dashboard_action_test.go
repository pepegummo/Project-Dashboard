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

func TestNeedsTools(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"create dashboard about cw-01", true},
		{"สร้างแดชบอร์ด cw-01 ให้หน่อย", true},
		{"สวัสดีครับ", false},
		{"hello", false},
		{"speed ของ CW-01 เท่าไหร่", true},
		{"what's the speed of CW-01", true},
		{"ลบ widget Trend ออก", true},
		{"how are you?", false},
	}
	for _, c := range cases {
		if got := needsTools(c.in); got != c.want {
			t.Errorf("needsTools(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

