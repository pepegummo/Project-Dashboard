package ai

// Non-live unit tests for dispatchIntent — the pure, deterministic replacement for
// the old regex-based tool-choice heuristics (Task 3). No network, no GROQ_API_KEY
// needed: ClassifyIntent's result is supplied directly as an IntentResult/ok pair,
// exactly as the router would hand it to Chat().

import "testing"

func TestDispatchIntentFallbackIsAuto(t *testing.T) {
	// !ok (router failed/declined/ambiguous) must always fall back to plain auto —
	// indistinguishable from pre-router behavior — regardless of any other signal.
	cases := []struct {
		name    string
		focused bool
		inline  bool
		role    string
		machine bool
		wantCap int
	}{
		{"plain", false, false, "viewer", false, 1},
		{"focused-inline-editor", true, true, "editor", true, 0},
		{"focused-editor", true, false, "editor", false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := IntentResult{Intent: "read_metric", Machine: "CW-01", Confidence: 0.9}
			toolChoice, roundCap := dispatchIntent(res, false, tc.focused, tc.inline, tc.role, tc.machine)
			if toolChoice != "" {
				t.Errorf("toolChoice = %q, want \"\" (auto)", toolChoice)
			}
			if roundCap != tc.wantCap {
				t.Errorf("roundCap = %d, want %d", roundCap, tc.wantCap)
			}
		})
	}
}

func TestDispatchIntentChat(t *testing.T) {
	res := IntentResult{Intent: "chat", Confidence: 0.9}
	toolChoice, _ := dispatchIntent(res, true, false, false, "viewer", false)
	if toolChoice != "" {
		t.Errorf("chat intent toolChoice = %q, want \"\" (auto)", toolChoice)
	}
}

func TestDispatchIntentFocusedInlineReadIsNone(t *testing.T) {
	// intent in {chat, read_metric, read_agg} AND focused AND inlineData -> "none"
	// (the Task-1 answer-from-context path, now router-decided).
	for _, intent := range []string{"chat", "read_metric", "read_agg"} {
		t.Run(intent, func(t *testing.T) {
			res := IntentResult{Intent: intent, Machine: "CW-01", Confidence: 0.9}
			toolChoice, roundCap := dispatchIntent(res, true, true, true, "viewer", true)
			if toolChoice != "none" {
				t.Errorf("toolChoice = %q, want %q", toolChoice, "none")
			}
			if roundCap != 0 {
				t.Errorf("roundCap = %d, want 0 (focused)", roundCap)
			}
		})
	}
}

func TestDispatchIntentReadNoMachineNoFocusIsRequired(t *testing.T) {
	// read_metric / read_agg / production with no machine slot and no focused widget
	// must NOT force by name (fan-out case needs get_machines first) -> "required".
	for _, intent := range []string{"read_metric", "read_agg", "production"} {
		t.Run(intent, func(t *testing.T) {
			res := IntentResult{Intent: intent, Confidence: 0.9} // no Machine
			toolChoice, _ := dispatchIntent(res, true, false, false, "viewer", false)
			if toolChoice != "required" {
				t.Errorf("toolChoice = %q, want %q", toolChoice, "required")
			}
		})
	}
}

func TestDispatchIntentReadForcesByNameWithMachineSlot(t *testing.T) {
	cases := []struct {
		intent   string
		wantFunc string
	}{
		{"read_metric", "show_metric"},
		{"read_agg", "get_telemetry_series"},
		{"production", "get_production_count"},
	}
	for _, tc := range cases {
		t.Run(tc.intent, func(t *testing.T) {
			res := IntentResult{Intent: tc.intent, Machine: "CW-01", Confidence: 0.9}
			toolChoice, _ := dispatchIntent(res, true, false, false, "viewer", true)
			want := forceFunc(tc.wantFunc)
			if toolChoice != want {
				t.Errorf("toolChoice = %q, want %q", toolChoice, want)
			}
		})
	}
}

func TestDispatchIntentReadForcesByNameWhenFocusedNoMachine(t *testing.T) {
	// No machine slot, but a focused widget on screen supplies one implicitly.
	res := IntentResult{Intent: "read_metric", Confidence: 0.9}
	toolChoice, _ := dispatchIntent(res, true, true, false, "viewer", false)
	want := forceFunc("show_metric")
	if toolChoice != want {
		t.Errorf("toolChoice = %q, want %q", toolChoice, want)
	}
}

func TestDispatchIntentReadDegradesOnInvalidMachine(t *testing.T) {
	// Router named a machine that did NOT resolve in the DB — never force with a
	// hallucinated machine, degrade to "required" even though focused is true.
	res := IntentResult{Intent: "read_metric", Machine: "NOT-A-REAL-MACHINE", Confidence: 0.9}
	toolChoice, _ := dispatchIntent(res, true, true, false, "viewer", false)
	if toolChoice != "required" {
		t.Errorf("toolChoice = %q, want %q (invalid machine slot must degrade)", toolChoice, "required")
	}
}

func TestDispatchIntentAlertsAlwaysForces(t *testing.T) {
	// alerts is org-scoped — no machine-slot exception, forces regardless of focus.
	res := IntentResult{Intent: "alerts", Confidence: 0.9}
	toolChoice, _ := dispatchIntent(res, true, false, false, "viewer", false)
	want := forceFunc("get_active_alerts")
	if toolChoice != want {
		t.Errorf("toolChoice = %q, want %q", toolChoice, want)
	}
}

func TestDispatchIntentViewerEditNeverForces(t *testing.T) {
	// A viewer's tool set has no preview_* tools (Task 1 gating) — forcing would
	// 400. edit_widget and compare must both return auto for a viewer.
	for _, intent := range []string{"edit_widget", "compare"} {
		for _, focused := range []bool{true, false} {
			res := IntentResult{Intent: intent, TargetWidget: "Trend", Confidence: 0.9}
			toolChoice, _ := dispatchIntent(res, true, focused, false, "viewer", false)
			if toolChoice != "" {
				t.Errorf("intent=%s focused=%v: toolChoice = %q, want \"\" (viewer must never be forced)", intent, focused, toolChoice)
			}
		}
	}
}

func TestDispatchIntentEditWidgetFocusedForcesByName(t *testing.T) {
	for _, role := range []string{"editor", "admin"} {
		res := IntentResult{Intent: "edit_widget", TargetWidget: "Trend", Confidence: 0.9}
		toolChoice, _ := dispatchIntent(res, true, true, false, role, false)
		want := forceFunc("preview_update_widget")
		if toolChoice != want {
			t.Errorf("role=%s: toolChoice = %q, want %q", role, toolChoice, want)
		}
	}
}

func TestDispatchIntentEditWidgetNotFocusedIsRequired(t *testing.T) {
	res := IntentResult{Intent: "edit_widget", Confidence: 0.9}
	toolChoice, _ := dispatchIntent(res, true, false, false, "editor", false)
	if toolChoice != "required" {
		t.Errorf("toolChoice = %q, want %q", toolChoice, "required")
	}
}

func TestDispatchIntentCompareFocusedForcesByName(t *testing.T) {
	res := IntentResult{Intent: "compare", Fields: []string{"speed", "temp"}, Confidence: 0.9}
	toolChoice, _ := dispatchIntent(res, true, true, false, "editor", false)
	want := forceFunc("preview_update_widget")
	if toolChoice != want {
		t.Errorf("toolChoice = %q, want %q", toolChoice, want)
	}
}

func TestDispatchIntentCreateDashboardForcesForEditor(t *testing.T) {
	res := IntentResult{Intent: "create_dashboard", Machine: "CW-01", Confidence: 0.9}
	toolChoice, _ := dispatchIntent(res, true, false, false, "editor", false)
	want := forceFunc("preview_dashboard")
	if toolChoice != want {
		t.Errorf("toolChoice = %q, want %q", toolChoice, want)
	}
}

func TestDispatchIntentCreateDashboardViewerIsAuto(t *testing.T) {
	res := IntentResult{Intent: "create_dashboard", Confidence: 0.9}
	toolChoice, _ := dispatchIntent(res, true, false, false, "viewer", false)
	if toolChoice != "" {
		t.Errorf("toolChoice = %q, want \"\" (viewer cannot create)", toolChoice)
	}
}

func TestDispatchIntentRoundCapFollowsFocused(t *testing.T) {
	res := IntentResult{Intent: "chat", Confidence: 0.9}
	if _, cap := dispatchIntent(res, true, true, false, "viewer", false); cap != 0 {
		t.Errorf("roundCap = %d, want 0 when focused", cap)
	}
	if _, cap := dispatchIntent(res, true, false, false, "viewer", false); cap != 1 {
		t.Errorf("roundCap = %d, want 1 when not focused", cap)
	}
}

// ── canWrite / hasMachineSlot / focusedContextSummary ───────────────────────────

func TestCanWrite(t *testing.T) {
	if !canWrite("admin") || !canWrite("editor") {
		t.Error("canWrite(admin/editor) = false, want true")
	}
	if canWrite("viewer") {
		t.Error("canWrite(viewer) = true, want false")
	}
}

func TestHasMachineSlot(t *testing.T) {
	cases := []struct {
		name     string
		machine  string
		focused  bool
		valid    bool
		wantSlot bool
	}{
		{"no-slot-no-focus", "", false, false, false},
		{"no-slot-focused", "", true, false, true},
		{"slot-valid", "CW-01", false, true, true},
		{"slot-invalid", "CW-01", true, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := IntentResult{Machine: tc.machine}
			got := hasMachineSlot(res, tc.focused, tc.valid)
			if got != tc.wantSlot {
				t.Errorf("hasMachineSlot() = %v, want %v", got, tc.wantSlot)
			}
		})
	}
}

func TestFocusedContextSummary(t *testing.T) {
	cases := []struct {
		name    string
		context string
		want    string
	}{
		{"empty", "", ""},
		{"no-marker", "some plain context, no focus here", ""},
		{
			"frontend-shape",
			"Current dashboard preview \"CW-01 Overview\" on screen:\n" +
				`- [FOCUSED] line-chart "Trend" — machine CW-01, metric weight, bucket 1h`,
			"focused widget: Trend (line-chart, machine CW-01, metric weight, bucket 1h)",
		},
		{
			"dateedit-live-test-shape",
			`- [FOCUSED] line-chart "Trend", machine CW-01, metric weight, window 2026-07-05T22:55 -> 2026-07-06T21:02`,
			"focused widget: Trend (line-chart, machine CW-01, metric weight)",
		},
		{
			"no-metric-alarm-panel",
			`- [FOCUSED] alarm-panel "Alarms" — machine CW-01`,
			"focused widget: Alarms (alarm-panel, machine CW-01)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := focusedContextSummary(tc.context)
			if got != tc.want {
				t.Errorf("focusedContextSummary() = %q, want %q", got, tc.want)
			}
		})
	}
}
