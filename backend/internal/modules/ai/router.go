package ai

// Structured intent router (Phase 2). Standalone in this task — built and evaluated
// but NOT wired into the Chat handler yet (Task 3 does that). ClassifyIntent makes one
// forced tool call to a small/fast Groq model (classify_intent, schema.go) and returns
// strict JSON, never prose. Callers treat a false second return as "fall back to the
// existing auto-tools chat path" — any error, invalid JSON, unknown intent, or
// low-confidence result is treated as a non-answer rather than guessed at.

import (
	"context"
	"encoding/json"
	"time"
)

// routerModel is ClassifyIntent's default model — openai/gpt-oss-20b per the live
// TestRouterBakeOff run (2026-07-10): 20/32 vs llama-3.1-8b-instant's 0/32 (Groq's
// function-call validator rejected llama-3.1-8b-instant's forced tool_choice output for
// this schema on every case, tripping callGroqModel's existing no-tools fallback).
// TestRouterBakeOff compares candidates by calling classifyIntentWithModel directly with
// an explicit model string — it does not mutate this constant. llama stays in that
// comparison for the record.
const routerModel = "openai/gpt-oss-20b"

// routerConfidenceFloor: results below this are treated as "not confident enough" —
// the caller falls back to auto tools rather than acting on a shaky guess.
const routerConfidenceFloor = 0.5

// validRouterIntents is the strict enum classify_intent must return. Anything else
// (typo, hallucinated category, empty string) fails ClassifyIntent.
var validRouterIntents = map[string]bool{
	"chat":             true,
	"read_metric":      true,
	"read_agg":         true,
	"edit_widget":      true,
	"compare":          true,
	"create_dashboard": true,
	"alerts":           true,
	"production":       true,
}

// IntentResult is the strict JSON contract returned by the classify_intent tool call
// (schema.go: ClassifyIntentTool). Slot fields are empty/zero when the message doesn't
// explicitly state them — the router never invents a slot value.
type IntentResult struct {
	Intent    string   `json:"intent"`
	Machine   string   `json:"machine,omitempty"`
	Metric    string   `json:"metric,omitempty"`
	Fields    []string `json:"fields,omitempty"`
	Bucket    string   `json:"bucket,omitempty"`
	DateRange struct {
		Start string `json:"start,omitempty"`
		End   string `json:"end,omitempty"`
	} `json:"dateRange,omitempty"`
	TargetWidget string  `json:"targetWidget,omitempty"`
	Status       string  `json:"status,omitempty"`
	Sku          string  `json:"sku,omitempty"`
	Confidence   float64 `json:"confidence"`
}

// routerSystemPrompt is a small, static-first prompt (~600 tokens max) so Groq can
// prompt-cache it across calls, like systemPromptUnified. Kept tight on purpose — one
// short example per intent, slot rules, nothing else. classify_intent's schema (not
// prose here) carries the output contract.
const routerSystemPrompt = `Classify one factory-dashboard chat message (Thai or English, often with typos) by calling classify_intent. Always call the tool — never reply in prose.

INTENTS (one example each):
- chat: greeting / small talk / general question, no dashboard data needed. Also use for a HYPOTHETICAL or conditional question about performing an action ("ถ้าฉันอยากสร้าง...", "if I wanted to create...", "how would I...") — the user is asking ABOUT an action, not requesting it now. "สวัสดีครับ" -> chat
- read_metric: a live/current single-value read. "speed ของ CW-01 เท่าไหร่" -> read_metric
- read_agg: a statistical aggregate or trend of a SENSOR METRIC over time — avg/min/max/แนวโน้ม — never a piece/production count. "ค่าเฉลี่ย speed เมื่อวานเท่าไหร่" -> read_agg
- edit_widget: change an on-screen widget's date window, bucket size, metric, or add/remove it. "อยากดู 22 นาที" (widget focused) -> edit_widget
- compare: overlay or compare two or more metrics on a chart. "เปรียบเทียบ speed กับ temp" -> compare
- create_dashboard: create a new dashboard — classify by meaning even through typos. "ส้างแดชบอด cw-01" -> create_dashboard
- alerts: active alerts/alarms (NOT alert-rule setup, which is a redirect elsewhere). "ตอนนี้มีแจ้งเตือนอะไรบ้าง" -> alerts
- production: counting units PRODUCED — piece counts, production counts, SKU counts. "ผลิตกี่ชิ้นใน 22 นาที" -> production

SLOTS — fill a slot only when the message explicitly states it. Never invent or guess a value; leave it empty if absent:
- machine: machine name/code, e.g. CW-01.
- metric: sensor field key, e.g. speed, temperature.
- fields: 2+ metric keys, compare intent only.
- bucket: interval size as <number><m|h|d>, e.g. "15m"; "22 นาที" -> "22m".
- dateRange.start / dateRange.end: YYYY-MM-DD, only if a date is explicit or trivially resolvable (today/yesterday).
- targetWidget: widget title, only if the user names or @-mentions one.
- status, sku: only if explicitly named.

confidence: 0..1, how sure you are of the INTENT (not the slots). Use below 0.5 when the message is genuinely ambiguous.`

// ClassifyIntent makes one forced-tool-call request to routerModel and parses the
// result. ctx is bounded to ~6s beyond whatever the caller already set, and there are
// no retries beyond what callGroqModel already does internally for quick 429 blips.
// Returns (zero, false) on any error, invalid JSON, unknown intent, or confidence
// below routerConfidenceFloor — callers treat false as "fall back to auto tools".
func ClassifyIntent(ctx context.Context, userMessage string, contextSummary string) (IntentResult, bool) {
	r, ok, _ := classifyIntentWithModel(ctx, routerModel, userMessage, contextSummary)
	return r, ok
}

// classifyIntentWithModel is ClassifyIntent with an explicit model and the successful
// HTTP round-trip duration exposed — used by TestRouterBakeOff to compare candidates
// (score + latency) without duplicating the request/parse logic.
func classifyIntentWithModel(ctx context.Context, model string, userMessage string, contextSummary string) (IntentResult, bool, time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	sp := routerSystemPrompt
	msgs := []groqMessage{{Role: "system", Content: &sp}}
	if contextSummary != "" {
		cs := contextSummary
		msgs = append(msgs, groqMessage{Role: "system", Content: &cs})
	}
	msgs = append(msgs, groqMessage{Role: "user", Content: strPtr(userMessage)})

	tools := []map[string]any{toGroqTool(ClassifyIntentTool)}
	resp, lat, err := callGroqModel(ctx, model, msgs, tools, forceFunc("classify_intent"))
	if err != nil {
		return IntentResult{}, false, lat
	}
	if len(resp.Choices) == 0 {
		return IntentResult{}, false, lat
	}
	calls := resp.Choices[0].Message.ToolCalls
	if len(calls) == 0 {
		return IntentResult{}, false, lat
	}
	r, ok := parseIntentResult(calls[0].Function.Arguments)
	return r, ok, lat
}

// parseIntentResult is separated from the HTTP call so it's unit-testable without the
// network: valid JSON + known intent + confidence >= floor -> (result, true); anything
// else -> (zero, false).
func parseIntentResult(rawJSON string) (IntentResult, bool) {
	var r IntentResult
	if err := json.Unmarshal([]byte(rawJSON), &r); err != nil {
		return IntentResult{}, false
	}
	if !validRouterIntents[r.Intent] {
		return IntentResult{}, false
	}
	if r.Confidence < routerConfidenceFloor {
		return IntentResult{}, false
	}
	return r, true
}

// ── Verify-then-repair (Phase 5) ──────────────────────────────────────────────

// VerifyResult is the strict JSON contract returned by the verify_answer tool
// call (schema.go: VerifyAnswerTool).
type VerifyResult struct {
	MatchesIntent      bool   `json:"matches_intent"`
	Problem            string `json:"problem,omitempty"`
	ClarifyingQuestion string `json:"clarifying_question,omitempty"`
}

// verifySystemPrompt is a small, static prompt (~250 tok) so Groq can
// prompt-cache it across verify calls, mirroring routerSystemPrompt. The schema
// (not prose here) carries the output contract.
const verifySystemPrompt = `You check whether an assistant's answer actually addresses the user's request in a factory-dashboard chat app. Always call verify_answer — never reply in prose.

MISMATCH (matches_intent: false) when the answer:
- performed or staged a DIFFERENT action than the one requested (e.g. edited the wrong widget, created instead of previewing, changed the wrong thing)
- answers about a different metric or machine than the user asked about
- states or implies a value that the tool results don't actually show (fabrication)

MATCH (matches_intent: true) when the answer correctly addresses the request — including a partial answer that HONESTLY says what it could not do.

If mismatch AND the request was genuinely ambiguous (not simply answered wrong), set clarifying_question to ONE short question in the user's language (Thai or English, matching the user's message) that would resolve it. Leave clarifying_question empty when the fix is obvious and needs no user input.`

// VerifyAnswer makes one forced-tool-call request to routerModel judging whether
// finalAnswer actually addresses userMessage, given the router's intent summary
// and which tools ran. Mirrors ClassifyIntent: 6s bounded timeout, static
// system prompt, forced tool_choice. Returns (zero, false) on any error, timeout,
// or malformed JSON — callers MUST treat false as "no verdict" (pass), never as
// a mismatch; the verifier's own infrastructure failing must never block or
// repair an otherwise-fine answer.
func VerifyAnswer(ctx context.Context, userMessage string, intentSummary string, finalAnswer string, toolsUsed string) (VerifyResult, bool) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	finalAnswer = truncateRunes(finalAnswer, 1500)
	if intentSummary == "" {
		intentSummary = "router declined"
	}
	if toolsUsed == "" {
		toolsUsed = "none"
	}

	sp := verifySystemPrompt
	userContent := "User message: " + userMessage +
		"\nRouter intent: " + intentSummary +
		"\nTools used: " + toolsUsed +
		"\nAssistant's final answer: " + finalAnswer
	msgs := []groqMessage{
		{Role: "system", Content: &sp},
		{Role: "user", Content: strPtr(userContent)},
	}

	tools := []map[string]any{toGroqTool(VerifyAnswerTool)}
	resp, _, err := callGroqModel(ctx, routerModel, msgs, tools, forceFunc("verify_answer"))
	if err != nil {
		return VerifyResult{}, false
	}
	if len(resp.Choices) == 0 {
		return VerifyResult{}, false
	}
	calls := resp.Choices[0].Message.ToolCalls
	if len(calls) == 0 {
		return VerifyResult{}, false
	}
	return parseVerifyResult(calls[0].Function.Arguments)
}

// parseVerifyResult is separated from the HTTP call so it's unit-testable
// without the network: valid JSON -> (result, true); malformed -> (zero, false)
// ("no verdict", never treated as a mismatch by callers).
func parseVerifyResult(rawJSON string) (VerifyResult, bool) {
	var r VerifyResult
	if err := json.Unmarshal([]byte(rawJSON), &r); err != nil {
		return VerifyResult{}, false
	}
	return r, true
}
