package ai

// Phase 5 — verify-then-repair loop. Runs ONLY when at least one tool executed
// during the request (scope rule): pure chat costs nothing extra. Order is
// deterministic checks (free) -> LLM verify (router model, bounded) -> at most
// ONE repair round -> a clarifying question if still wrong. Every failure path in
// this file degrades to "deliver the original answer" — verification must never
// break or block Chat(). See docs/AI_ARCHITECTURE.md and controller.go's Chat().

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// toolExecution is one tool call/result pair from this request's Groq round-trips
// (main loop or the single repair round) — the raw material both the deterministic
// checks and VerifyAnswer's "tools used" summary are built from.
type toolExecution struct {
	name       string
	args       string // raw JSON arguments the model sent
	resultJSON string // raw JSON result dispatch() returned
}

// machineFieldsLookup abstracts the org-scoped machine_fields query
// (getMachineFieldsForMachine in dashboard_action.go) so the deterministic checks
// are table-testable without a DB.
type machineFieldsLookup func(ctx context.Context, machineID string) []string

// machineIDResolver abstracts the org-scoped machine name -> UUID lookup
// (resolveMachineID in dashboard_action.go) — same rationale as
// machineFieldsLookup: keeps the deterministic checks unit-testable without a
// live database.Pool (resolveMachineID panics on a nil pool outside a real
// server process).
type machineIDResolver func(ctx context.Context, orgID, name string) (string, bool)

// ── Deterministic checks (free, pure core) ──────────────────────────────────

// checkFieldsExist is the pure core of the preview_update_widget metric/fields
// check: every key in `want` must appear (case-insensitively) in `available`.
// Returns the first missing key's problem string; ok=false means nothing to
// check (want is empty) or everything matched.
func checkFieldsExist(want []string, machineLabel string, available []string) (problem string, failed bool) {
	for _, w := range want {
		found := false
		for _, f := range available {
			if strings.EqualFold(f, w) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Sprintf("metric %q does not exist on machine %s; available: %v", w, machineLabel, available), true
		}
	}
	return "", false
}

// checkPreviewPlanWidgets is the pure core of the preview_dashboard check: every
// widget in the plan must carry a non-empty metric (fields[] counts for chart
// widgets, which use fields instead of metric).
func checkPreviewPlanWidgets(widgets []PreviewWidget) (problem string, failed bool) {
	for _, w := range widgets {
		if strings.TrimSpace(w.Metric) == "" && len(w.Fields) == 0 {
			label := w.Title
			if label == "" {
				label = w.Type
			}
			return fmt.Sprintf("widget %q has no metric assigned", label), true
		}
	}
	return "", false
}

// widgetTitleLineRe finds the dashboard-context line naming a given widget title
// (quoted, as the frontend renders it — see AIAssistantPage.vue buildDashboardContext)
// so a preview_update_widget metric change can be checked against the widget's
// CURRENT machine even when the tool call itself carried no "machine" argument
// (the common case: only the metric changes).
func widgetTitleLine(contextText, title string) string {
	if contextText == "" || title == "" {
		return ""
	}
	re := regexp.MustCompile(`(?m)^.*"` + regexp.QuoteMeta(title) + `".*$`)
	return re.FindString(contextText)
}

// machineForWidgetTitle resolves the machine NAME (not UUID) shown on the
// dashboard-context line for the given widget title, reusing focusedMachineRe
// (controller.go) since both [FOCUSED] and plain widget lines share the same
// "machine <name>" token shape. Returns "" if the title/line/token isn't found.
func machineForWidgetTitle(contextText, title string) string {
	line := widgetTitleLine(contextText, title)
	if line == "" {
		return ""
	}
	m := focusedMachineRe.FindStringSubmatch(line)
	if m == nil {
		return ""
	}
	return m[1]
}

// runDeterministicChecks inspects this request's tool executions for a
// preview_update_widget metric/fields change or an incomplete preview_dashboard
// plan. It NEVER fails the request on a lookup error — a check it can't resolve
// (no machine on hand, DB error) is simply skipped, per the brief's "never fail
// the request on a check error" rule. Returns the first failing check's problem
// string, or ("", false) when everything checked out (or nothing was checkable).
func runDeterministicChecks(ctx context.Context, orgID string, contextText string, log []toolExecution, resolveID machineIDResolver, lookup machineFieldsLookup) (problem string, failed bool) {
	for _, t := range log {
		switch t.name {
		case "preview_update_widget":
			if p, bad := checkPreviewUpdateResult(ctx, orgID, contextText, t.resultJSON, resolveID, lookup); bad {
				return p, true
			}
		case "preview_dashboard":
			var res PreviewDashboardResult
			if err := json.Unmarshal([]byte(t.resultJSON), &res); err != nil {
				continue
			}
			if p, bad := checkPreviewPlanWidgets(res.Widgets); bad {
				return p, true
			}
		}
	}
	return "", false
}

// checkPreviewUpdateResult resolves the target machine for a preview_update_widget
// result (from its own "changes.machineUuid" when the call reassigned the machine,
// otherwise from the widget's current machine on the dashboard-context line) and
// checks any new metric/fields against that machine's known field keys. Skips
// (never fails) when there's no metric/fields change, or the machine can't be
// resolved — deterministic checks only run against data already in hand plus at
// most one org-scoped lookup.
func checkPreviewUpdateResult(ctx context.Context, orgID string, contextText string, resultJSON string, resolveID machineIDResolver, lookup machineFieldsLookup) (problem string, failed bool) {
	var res struct {
		WidgetTitle string         `json:"widgetTitle"`
		Changes     map[string]any `json:"changes"`
	}
	if err := json.Unmarshal([]byte(resultJSON), &res); err != nil || res.Changes == nil {
		return "", false
	}

	var want []string
	if m, ok := res.Changes["metric"].(string); ok && m != "" {
		want = append(want, m)
	}
	if fs, ok := res.Changes["fields"].([]any); ok {
		for _, f := range fs {
			if s, ok := f.(string); ok && s != "" {
				want = append(want, s)
			}
		}
	}
	if len(want) == 0 {
		return "", false
	}

	machineID, _ := res.Changes["machineUuid"].(string)
	machineLabel, _ := res.Changes["machine"].(string)
	if machineID == "" {
		name := machineForWidgetTitle(contextText, res.WidgetTitle)
		if name == "" {
			return "", false // no machine to resolve against — skip, never fail
		}
		id, ok := resolveID(ctx, orgID, name)
		if !ok {
			return "", false
		}
		machineID, machineLabel = id, name
	}

	fields := lookup(ctx, machineID)
	if fields == nil {
		return "", false // lookup error/empty — skip rather than false-fail
	}
	return checkFieldsExist(want, machineLabel, fields)
}

// ── Cap logic (pure) ─────────────────────────────────────────────────────────

type verifyOutcome int

const (
	outcomeDeliver verifyOutcome = iota
	outcomeRepair
	outcomeAskBack
)

// decideVerifyOutcome is the pre-repair decision: given the deterministic-check
// result, the LLM verify verdict (nil = no-verdict, i.e. treated as pass), whether
// a repair round has already happened, and whether the router confidently
// classified the intent, decide deliver / repair / ask-back. Mirrors brief §3
// rules 1, 2, 3 and 5. `repaired` is always false on Chat()'s one real call site —
// it's part of the signature so the "repair happens at most once" property is
// directly testable (repaired=true never yields outcomeRepair here).
func decideVerifyOutcome(detOK bool, verifyVerdict *VerifyResult, repaired bool, routerOK bool) verifyOutcome {
	if !detOK {
		if repaired {
			return outcomeAskBack
		}
		return outcomeRepair
	}
	if verifyVerdict == nil || verifyVerdict.MatchesIntent {
		return outcomeDeliver
	}
	// mismatch
	if repaired {
		return outcomeAskBack
	}
	if !routerOK {
		return outcomeAskBack // rule 5: ambiguous-from-start shortcut
	}
	return outcomeRepair
}

// decidePostRepairOutcome is rule 4: after the single repair round, only the
// deterministic checks run again (no second LLM verify — hard cap one verify +
// one repair). By construction this never returns outcomeRepair, which is what
// enforces the one-repair cap for the post-repair half of the flow.
func decidePostRepairOutcome(detOKAfterRepair bool, firstClarifyingQuestion string, repairHadToolActivity bool) verifyOutcome {
	if !detOKAfterRepair {
		return outcomeAskBack
	}
	if firstClarifyingQuestion != "" && !repairHadToolActivity {
		return outcomeAskBack
	}
	return outcomeDeliver
}

// ── Small helpers ────────────────────────────────────────────────────────────

// summarizeToolLog renders a compact "name(args); name(args)" string for
// VerifyAnswer's toolsUsed input — args truncated so a chatty round doesn't blow
// the verify prompt's token budget.
func summarizeToolLog(log []toolExecution) string {
	if len(log) == 0 {
		return ""
	}
	parts := make([]string, 0, len(log))
	for _, t := range log {
		args := t.args
		if len(args) > 150 {
			args = args[:150]
		}
		parts = append(parts, t.name+"("+args+")")
	}
	return strings.Join(parts, "; ")
}

// clarifyingQuestionOrFallback returns the verifier's clarifying question, or a
// generic Thai+English ask-clarify line when the verifier didn't supply one
// (empty verdict, no-verdict, or infrastructure failure).
func clarifyingQuestionOrFallback(verdict *VerifyResult) string {
	if verdict != nil && strings.TrimSpace(verdict.ClarifyingQuestion) != "" {
		return verdict.ClarifyingQuestion
	}
	return "ขอโทษค่ะ ช่วยระบุรายละเอียดเพิ่มเติมได้ไหมคะว่าต้องการให้ทำอะไร / Sorry, could you clarify what you'd like me to do?"
}

func detLabel(ok bool) string {
	if ok {
		return "ok"
	}
	return "fail"
}

func verdictLabel(o verifyOutcome) string {
	switch o {
	case outcomeRepair:
		return "repair"
	case outcomeAskBack:
		return "askback"
	default:
		return "pass"
	}
}

func intentLabel(res IntentResult, ok bool) string {
	if !ok {
		return "none"
	}
	return res.Intent
}
