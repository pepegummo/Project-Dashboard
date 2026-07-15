package ai

// Non-live unit tests for Phase 5's verify-then-repair loop — deterministic
// checks, VerifyResult parsing, and the pure cap-logic decision functions. No
// network, no GROQ_API_KEY needed: the machine-fields lookup is faked via
// machineFieldsLookup so these run without a DB.

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

// ── Deterministic checks ─────────────────────────────────────────────────────

func TestCheckFieldsExist(t *testing.T) {
	cases := []struct {
		name      string
		want      []string
		available []string
		wantFail  bool
	}{
		{"exists", []string{"speed"}, []string{"speed", "temp"}, false},
		{"exists-case-insensitive", []string{"Speed"}, []string{"speed", "temp"}, false},
		{"absent", []string{"xyz"}, []string{"speed", "temp"}, true},
		{"multi-one-bad", []string{"speed", "bogus"}, []string{"speed", "temp"}, true},
		{"empty-want", []string{}, []string{"speed"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problem, failed := checkFieldsExist(tc.want, "CW-01", tc.available)
			if failed != tc.wantFail {
				t.Errorf("failed = %v, want %v (problem=%q)", failed, tc.wantFail, problem)
			}
			if tc.wantFail && problem == "" {
				t.Error("wantFail but problem string is empty")
			}
		})
	}
}

func TestCheckPreviewPlanWidgets(t *testing.T) {
	cases := []struct {
		name     string
		widgets  []PreviewWidget
		wantFail bool
	}{
		{"complete", []PreviewWidget{{Title: "Speed", Metric: "speed"}, {Title: "Overlay", Fields: []string{"a", "b"}}}, false},
		{"missing-metric", []PreviewWidget{{Title: "Speed", Metric: "speed"}, {Title: "Blank"}}, true},
		{"empty-plan", []PreviewWidget{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problem, failed := checkPreviewPlanWidgets(tc.widgets)
			if failed != tc.wantFail {
				t.Errorf("failed = %v, want %v (problem=%q)", failed, tc.wantFail, problem)
			}
		})
	}
}

func TestMachineForWidgetTitle(t *testing.T) {
	ctxText := "Current dashboard preview \"CW-01 Overview\" on screen:\n" +
		`- [FOCUSED] line-chart "Trend" — machine CW-01, metric weight, bucket 1h` + "\n" +
		`- kpi-card "Speed KPI" — machine CW-02, metric speed`
	cases := []struct {
		name  string
		title string
		want  string
	}{
		{"focused-widget", "Trend", "CW-01"},
		{"non-focused-widget", "Speed KPI", "CW-02"},
		{"unknown-title", "Nope", ""},
		{"empty-context", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctxArg := ctxText
			if tc.name == "empty-context" {
				ctxArg = ""
			}
			got := machineForWidgetTitle(ctxArg, tc.title)
			if got != tc.want {
				t.Errorf("machineForWidgetTitle() = %q, want %q", got, tc.want)
			}
		})
	}
}

// fakeLookup returns a fixed field set for any machine ID, and lets a test force
// a "lookup failed" (nil) response.
func fakeLookup(fields []string) machineFieldsLookup {
	return func(ctx context.Context, machineID string) []string { return fields }
}

// fakeResolver mirrors resolveMachineID without touching database.Pool (which is
// nil — and panics on use — outside a real server process).
func fakeResolver(id string, ok bool) machineIDResolver {
	return func(ctx context.Context, orgID, name string) (string, bool) { return id, ok }
}

func TestRunDeterministicChecksPreviewUpdateMetricExists(t *testing.T) {
	result := `{"updated":true,"widgetTitle":"Trend","changes":{"metric":"speed","machineUuid":"m-1","machine":"CW-01"}}`
	log := []toolExecution{{name: "preview_update_widget", resultJSON: result}}
	problem, failed := runDeterministicChecks(context.Background(), "org-1", "", log, fakeResolver("", false), fakeLookup([]string{"speed", "temp"}))
	if failed {
		t.Errorf("failed = true, want false (metric exists); problem=%q", problem)
	}
}

func TestRunDeterministicChecksPreviewUpdateMetricAbsent(t *testing.T) {
	result := `{"updated":true,"widgetTitle":"Trend","changes":{"metric":"bogus","machineUuid":"m-1","machine":"CW-01"}}`
	log := []toolExecution{{name: "preview_update_widget", resultJSON: result}}
	problem, failed := runDeterministicChecks(context.Background(), "org-1", "", log, fakeResolver("", false), fakeLookup([]string{"speed", "temp"}))
	if !failed {
		t.Fatal("failed = false, want true (metric absent)")
	}
	if problem == "" {
		t.Error("problem string is empty, want a specific reason")
	}
}

func TestRunDeterministicChecksPreviewUpdateResolvesMachineFromContext(t *testing.T) {
	// No machineUuid in changes (machine wasn't reassigned this call) — must fall
	// back to the widget's current machine on the dashboard-context line, then
	// resolve that name to an ID via the injected resolver.
	result := `{"updated":true,"widgetTitle":"Trend","changes":{"metric":"bogus"}}`
	ctxText := `- [FOCUSED] line-chart "Trend" — machine CW-01, metric weight`
	log := []toolExecution{{name: "preview_update_widget", resultJSON: result}}
	problem, failed := runDeterministicChecks(context.Background(), "org-1", ctxText, log, fakeResolver("m-1", true), fakeLookup([]string{"speed"}))
	if !failed {
		t.Fatal("failed = false, want true (metric resolved via context machine, then found absent)")
	}
	if problem == "" {
		t.Error("problem string is empty, want a specific reason")
	}
}

func TestRunDeterministicChecksPreviewUpdateUnresolvableMachineSkips(t *testing.T) {
	// No machineUuid in changes AND either no context line or the resolver can't
	// find the name — must SKIP (never false-fail) since there's nothing to check
	// against.
	result := `{"updated":true,"widgetTitle":"Trend","changes":{"metric":"bogus"}}`
	log := []toolExecution{{name: "preview_update_widget", resultJSON: result}}
	problem, failed := runDeterministicChecks(context.Background(), "org-1", "" /* no context */, log, fakeResolver("m-1", true), fakeLookup([]string{"speed"}))
	if failed {
		t.Errorf("failed = true, want false (no context line — nothing to resolve against); problem=%q", problem)
	}

	ctxText := `- [FOCUSED] line-chart "Trend" — machine CW-01, metric weight`
	problem, failed = runDeterministicChecks(context.Background(), "org-1", ctxText, log, fakeResolver("", false) /* resolver fails */, fakeLookup([]string{"speed"}))
	if failed {
		t.Errorf("failed = true, want false (resolver failed to resolve — must skip); problem=%q", problem)
	}
}

func TestRunDeterministicChecksPreviewUpdateNoMetricChangeSkips(t *testing.T) {
	result := `{"updated":true,"widgetTitle":"Trend","changes":{"title":"New Title"}}`
	log := []toolExecution{{name: "preview_update_widget", resultJSON: result}}
	problem, failed := runDeterministicChecks(context.Background(), "org-1", "", log, fakeResolver("", false), fakeLookup([]string{"speed"}))
	if failed {
		t.Errorf("failed = true, want false (no metric/fields change); problem=%q", problem)
	}
}

func TestRunDeterministicChecksPreviewUpdateLookupFailureSkips(t *testing.T) {
	result := `{"updated":true,"widgetTitle":"Trend","changes":{"metric":"speed","machineUuid":"m-1"}}`
	log := []toolExecution{{name: "preview_update_widget", resultJSON: result}}
	problem, failed := runDeterministicChecks(context.Background(), "org-1", "", log, fakeResolver("", false), fakeLookup(nil))
	if failed {
		t.Errorf("failed = true, want false (lookup error must skip, never false-fail); problem=%q", problem)
	}
}

func TestRunDeterministicChecksPreviewDashboardComplete(t *testing.T) {
	result := `{"preview":true,"dashboardName":"CW-01 Overview","widgets":[{"type":"kpi-card","title":"Speed","metric":"speed"}]}`
	log := []toolExecution{{name: "preview_dashboard", resultJSON: result}}
	problem, failed := runDeterministicChecks(context.Background(), "org-1", "", log, fakeResolver("", false), fakeLookup(nil))
	if failed {
		t.Errorf("failed = true, want false (complete plan); problem=%q", problem)
	}
}

func TestRunDeterministicChecksPreviewDashboardIncomplete(t *testing.T) {
	result := `{"preview":true,"dashboardName":"CW-01 Overview","widgets":[{"type":"kpi-card","title":"Speed","metric":"speed"},{"type":"gauge","title":"Blank"}]}`
	log := []toolExecution{{name: "preview_dashboard", resultJSON: result}}
	problem, failed := runDeterministicChecks(context.Background(), "org-1", "", log, fakeResolver("", false), fakeLookup(nil))
	if !failed {
		t.Fatal("failed = false, want true (widget missing metric)")
	}
	if problem == "" {
		t.Error("problem string is empty, want a specific reason")
	}
}

func TestRunDeterministicChecksIgnoresOtherTools(t *testing.T) {
	log := []toolExecution{{name: "show_metric", resultJSON: `{"value":42}`}}
	problem, failed := runDeterministicChecks(context.Background(), "org-1", "", log, fakeResolver("", false), fakeLookup(nil))
	if failed {
		t.Errorf("failed = true, want false (no checkable tool ran); problem=%q", problem)
	}
}

func TestRunDeterministicChecksMalformedResultSkips(t *testing.T) {
	log := []toolExecution{{name: "preview_update_widget", resultJSON: `not json`}}
	problem, failed := runDeterministicChecks(context.Background(), "org-1", "", log, fakeResolver("", false), fakeLookup([]string{"speed"}))
	if failed {
		t.Errorf("failed = true, want false (malformed result must skip, never false-fail); problem=%q", problem)
	}
}

// ── VerifyResult parsing ──────────────────────────────────────────────────────

func TestParseVerifyResultMismatch(t *testing.T) {
	raw := `{"matches_intent":false,"problem":"answered temperature, user asked speed","clarifying_question":""}`
	got, ok := parseVerifyResult(raw)
	if !ok {
		t.Fatalf("parseVerifyResult(%s) ok = false, want true", raw)
	}
	if got.MatchesIntent {
		t.Error("MatchesIntent = true, want false")
	}
	if got.Problem == "" {
		t.Error("Problem is empty, want a reason")
	}
}

func TestParseVerifyResultMatch(t *testing.T) {
	raw := `{"matches_intent":true}`
	got, ok := parseVerifyResult(raw)
	if !ok {
		t.Fatalf("parseVerifyResult(%s) ok = false, want true", raw)
	}
	if !got.MatchesIntent {
		t.Error("MatchesIntent = false, want true")
	}
}

func TestParseVerifyResultMalformedIsNoVerdict(t *testing.T) {
	raw := `{"matches_intent": tru`
	_, ok := parseVerifyResult(raw)
	if ok {
		t.Fatalf("parseVerifyResult(%s) ok = true, want false (malformed -> no-verdict)", raw)
	}
}

// ── Cap logic ─────────────────────────────────────────────────────────────────

func matchVerdict() *VerifyResult { return &VerifyResult{MatchesIntent: true} }
func mismatchVerdict() *VerifyResult {
	return &VerifyResult{MatchesIntent: false, Problem: "wrong thing"}
}

func TestDecideVerifyOutcome(t *testing.T) {
	cases := []struct {
		name     string
		detOK    bool
		verdict  *VerifyResult
		repaired bool
		routerOK bool
		want     verifyOutcome
	}{
		{"det-fail-first-pass", false, nil, false, true, outcomeRepair},
		{"det-fail-already-repaired", false, nil, true, true, outcomeAskBack},
		{"det-fail-already-repaired-routerdecline", false, nil, true, false, outcomeAskBack},
		{"det-ok-no-verdict", true, nil, false, true, outcomeDeliver},
		{"det-ok-match", true, matchVerdict(), false, true, outcomeDeliver},
		{"det-ok-mismatch-first-pass", true, mismatchVerdict(), false, true, outcomeRepair},
		{"det-ok-mismatch-already-repaired", true, mismatchVerdict(), true, true, outcomeAskBack},
		{"det-ok-mismatch-router-declined", true, mismatchVerdict(), false, false, outcomeAskBack},
		{"det-ok-match-router-declined", true, matchVerdict(), false, false, outcomeDeliver},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decideVerifyOutcome(tc.detOK, tc.verdict, tc.repaired, tc.routerOK)
			if got != tc.want {
				t.Errorf("decideVerifyOutcome(%v, %v, %v, %v) = %v, want %v",
					tc.detOK, tc.verdict, tc.repaired, tc.routerOK, got, tc.want)
			}
		})
	}
}

// TestDecideVerifyOutcomeRepairAtMostOnce asserts the structural cap: whenever
// repaired=true is passed in, the result is NEVER outcomeRepair — across every
// combination of the other three inputs.
func TestDecideVerifyOutcomeRepairAtMostOnce(t *testing.T) {
	verdicts := []*VerifyResult{nil, matchVerdict(), mismatchVerdict()}
	for _, detOK := range []bool{true, false} {
		for _, v := range verdicts {
			for _, routerOK := range []bool{true, false} {
				got := decideVerifyOutcome(detOK, v, true /* repaired */, routerOK)
				if got == outcomeRepair {
					t.Errorf("decideVerifyOutcome(%v, %v, repaired=true, %v) = outcomeRepair, want never repeat", detOK, v, routerOK)
				}
			}
		}
	}
}

func TestDecidePostRepairOutcome(t *testing.T) {
	cases := []struct {
		name             string
		detOKAfterRepair bool
		firstClarify     string
		hadToolActivity  bool
		want             verifyOutcome
	}{
		{"det-still-fails", false, "", false, outcomeAskBack},
		{"det-still-fails-with-clarify", false, "ระบุ?", true, outcomeAskBack},
		{"det-ok-no-clarify", true, "", false, outcomeDeliver},
		{"det-ok-clarify-but-tool-ran", true, "ระบุ?", true, outcomeDeliver},
		{"det-ok-clarify-no-tool-activity", true, "ระบุ?", false, outcomeAskBack},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decidePostRepairOutcome(tc.detOKAfterRepair, tc.firstClarify, tc.hadToolActivity)
			if got != tc.want {
				t.Errorf("decidePostRepairOutcome(%v, %q, %v) = %v, want %v",
					tc.detOKAfterRepair, tc.firstClarify, tc.hadToolActivity, got, tc.want)
			}
		})
	}
}

// TestDecidePostRepairOutcomeNeverRepairs asserts the second structural half of
// the one-repair cap: decidePostRepairOutcome's return type never includes
// outcomeRepair, across every input combination — the repair round can only run
// once per request, by construction.
func TestDecidePostRepairOutcomeNeverRepairs(t *testing.T) {
	for _, detOK := range []bool{true, false} {
		for _, clarify := range []string{"", "ระบุ?"} {
			for _, activity := range []bool{true, false} {
				if got := decidePostRepairOutcome(detOK, clarify, activity); got == outcomeRepair {
					t.Errorf("decidePostRepairOutcome(%v, %q, %v) = outcomeRepair, want never", detOK, clarify, activity)
				}
			}
		}
	}
}

// ── Small helpers ─────────────────────────────────────────────────────────────

func TestClarifyingQuestionOrFallback(t *testing.T) {
	if got := clarifyingQuestionOrFallback(&VerifyResult{ClarifyingQuestion: "ต้องการอะไร?"}); got != "ต้องการอะไร?" {
		t.Errorf("got %q, want the verdict's clarifying_question", got)
	}
	if got := clarifyingQuestionOrFallback(&VerifyResult{}); got == "" {
		t.Error("fallback must not be empty when verdict has no clarifying_question")
	}
	if got := clarifyingQuestionOrFallback(nil); got == "" {
		t.Error("fallback must not be empty when verdict is nil")
	}
}

func TestSummarizeToolLog(t *testing.T) {
	if got := summarizeToolLog(nil); got != "" {
		t.Errorf("summarizeToolLog(nil) = %q, want empty", got)
	}
	log := []toolExecution{
		{name: "show_metric", args: `{"machine":"CW-01","metric":"speed"}`},
		{name: "get_active_alerts", args: `{}`},
	}
	got := summarizeToolLog(log)
	if got == "" {
		t.Fatal("summarizeToolLog returned empty for non-empty log")
	}
	for _, want := range []string{"show_metric", "get_active_alerts"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q missing %q", got, want)
		}
	}
}

func TestTruncateRunesRuneSafe(t *testing.T) {
	// Every rune here ("ความเร็ว...") is a multi-byte UTF-8 sequence — a naive
	// s[:n] byte slice at an odd n would split one and corrupt the string
	// (produces invalid UTF-8 / a replacement-char mangled string).
	thai := strings.Repeat("ความเร็ว", 50) // well over any of our caps, all multi-byte runes
	for _, max := range []int{1, 2, 3, 5, 7, 10, 50, 150} {
		got := truncateRunes(thai, max)
		if n := len([]rune(got)); n != max {
			t.Errorf("truncateRunes(_, %d): got %d runes, want %d", max, n, max)
		}
		if !utf8.ValidString(got) {
			t.Errorf("truncateRunes(_, %d) produced invalid UTF-8: %q", max, got)
		}
	}

	// Shorter than max — returned unchanged.
	if got := truncateRunes("สั้น", 100); got != "สั้น" {
		t.Errorf("truncateRunes(short, 100) = %q, want unchanged", got)
	}
}

func TestBuildRepairMessagesIncludesOriginalAnswer(t *testing.T) {
	base := []aiMessage{
		{Role: "system", Content: strPtr("system prompt")},
		{Role: "user", Content: strPtr("ความเร็ว CW-01 เท่าไหร่")},
	}
	originalAnswer := "อุณหภูมิของ CW-02 ตอนนี้อยู่ที่ 78 องศา"
	got := buildRepairMessages(base, originalAnswer, "answered the wrong machine/metric")

	if len(got) != len(base)+2 {
		t.Fatalf("len(got) = %d, want %d (base + assistant answer + VERIFIER message)", len(got), len(base)+2)
	}

	// The assistant's original (mismatched) answer must be present so the
	// VERIFIER instruction that follows refers to something the model can see.
	answerMsg := got[len(base)]
	if answerMsg.Role != "assistant" {
		t.Errorf("message before VERIFIER = role %q, want %q", answerMsg.Role, "assistant")
	}
	if answerMsg.Content == nil || *answerMsg.Content != originalAnswer {
		t.Errorf("assistant message content = %v, want %q", answerMsg.Content, originalAnswer)
	}

	verifierMsg := got[len(base)+1]
	if verifierMsg.Role != "system" {
		t.Errorf("last message role = %q, want %q", verifierMsg.Role, "system")
	}
	if verifierMsg.Content == nil || !strings.Contains(*verifierMsg.Content, "answered the wrong machine/metric") {
		t.Errorf("VERIFIER message = %v, want it to contain the problem string", verifierMsg.Content)
	}

	// base itself must be untouched (no aliasing surprises for the caller).
	if len(base) != 2 {
		t.Errorf("base was mutated: len = %d, want 2", len(base))
	}
}
