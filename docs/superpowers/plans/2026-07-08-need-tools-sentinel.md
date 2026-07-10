# NEED_TOOLS Sentinel Escalation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When the `needsTools()` keyword regex wrongly strips tools from an action message (typos like "ส้างแดชบอด cw-01"), let the model escalate via a `NEED_TOOLS` sentinel reply and retry once with the full prompt + tools — instead of role-playing an action it can't perform.

**Architecture:** The existing no-tool round doubles as the intent test (Option B from design discussion). `systemPromptMinimal` gains one rule: reply exactly `NEED_TOOLS` if the message actually asks for data/actions. The chat handler detects that sentinel on the no-tool path and re-enters the loop once with `systemPromptBase` (+ contextExt) and the full tool list. No new API round on any correctly-routed path.

**Tech Stack:** Go (Fiber), Groq chat completions (`openai/gpt-oss` via `groqModel` const), existing test harness in `backend/internal/modules/ai/`.

## Global Constraints

- Do NOT change `needsToolsRe` / `needsTools()` — the keyword gate stays as-is (the whitelist rewrite was tried and reverted; user chose sentinel escalation instead).
- Do NOT change the `groqModel` const (currently `openai/gpt-oss-120b`, set intentionally by the user).
- Sentinel escalation fires at most ONCE per request (guard flag) — never loop.
- The sentinel text `NEED_TOOLS` must never be saved as an assistant message in the DB.
- Token budget rationale (keep in comments): greetings pay only ~40 extra prompt tokens for the new rule; the escalation path pays one wasted minimal round (~200 tok) + the normal full round (~2.7k tok) — only on regex misses.
- All Go work happens in `backend/`; run commands from repo root as shown (PowerShell syntax: `cd backend; go test ...`).

## Current Repo State (read before starting)

- `backend/internal/modules/ai/dashboard_action_test.go:31` already contains a case `{"ส้างแดชบอด cw-01 ให้หน่อย", true}` added during diagnosis. **It currently FAILS** (the keyword regex correctly returns `false` for the typo). Task 1 fixes the expectation — under this design the gate is ALLOWED to miss typos; the sentinel covers them.
- `backend/internal/modules/ai/controller.go` — key locations (line numbers approximate, re-locate by content):
  - `systemPromptMinimal` const near line 28.
  - Gate + prompt selection: `needsToolsFlag := needsTools(body.Message)` (~line 370), prompt pick (~line 388), `tools` nil-out (~line 406–413).
  - Tool loop `for i := 0; i < 5; i++` (~line 426); plain-text exit branch `if choice.FinishReason != "tool_calls" ...` (~line 460).
- `backend/internal/modules/ai/eval_test.go` — live bake-off (`TestBakeOff`), skips without `GROQ_API_KEY`; loads `.env` via godotenv. `callGroqModel(model, msgs, tools, toolChoice)` returns `(resp, httpLat, err)`.

---

### Task 1: Fix the gate unit test to document intended behavior

**Files:**
- Modify: `backend/internal/modules/ai/dashboard_action_test.go` (TestNeedsTools cases, ~line 29–37)

**Interfaces:**
- Consumes: `needsTools(string) bool` (unchanged).
- Produces: nothing — documentation-by-test only.

- [ ] **Step 1: Update the typo case expectation**

Replace the existing typo line:

```go
		{"ส้างแดชบอด cw-01 ให้หน่อย", true}, // typo of the line above — must still get tools
```

with:

```go
		// Typo of the line above. The keyword gate is ALLOWED to miss typos —
		// the NEED_TOOLS sentinel in systemPromptMinimal escalates them to the
		// tool path at runtime (see controller.go). This case documents that
		// the gate returning false here is expected, not a bug.
		{"ส้างแดชบอด cw-01 ให้หน่อย", false},
```

- [ ] **Step 2: Run the test to verify it passes**

Run: `cd backend; go test ./internal/modules/ai/ -run TestNeedsTools -v`
Expected: `--- PASS: TestNeedsTools`

- [ ] **Step 3: Commit**

```bash
cd backend
git add internal/modules/ai/dashboard_action_test.go
git commit -m "test: document that needsTools gate may miss typos (sentinel covers them)"
```

---

### Task 2: Sentinel rule in systemPromptMinimal (test-first, live)

**Files:**
- Modify: `backend/internal/modules/ai/controller.go` (`systemPromptMinimal` const, ~line 28–32)
- Test: `backend/internal/modules/ai/sentinel_live_test.go` (create)

**Interfaces:**
- Consumes: `callGroqModel(model string, msgs []groqMessage, tools []map[string]any, toolChoice string) (*groqResponse, time.Duration, error)` and `groqModel`, `systemPromptMinimal`, `strPtr` — all already in package `ai`.
- Produces: `needToolsSentinel` const (value `"NEED_TOOLS"`) — Task 3 compares against it.

- [ ] **Step 1: Write the failing live test**

Create `backend/internal/modules/ai/sentinel_live_test.go`:

```go
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
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd backend; go test ./internal/modules/ai/ -run TestMinimalPromptSentinel -v`
Expected: compile error `undefined: needToolsSentinel` (the const doesn't exist yet). That is the failing state.

- [ ] **Step 3: Add the const and the prompt rule**

In `controller.go`, replace the `systemPromptMinimal` block:

```go
// systemPromptMinimal is sent on the no-tool path (greetings, chit-chat, "what
// can you do?"). It carries only identity + the language rule — the full
// TOOL SELECTION / SLOT FILLING / WIDGET rules are useless without tools, so a
// bare greeting no longer pays for them (~300 tokens saved vs systemPromptBase).
const systemPromptMinimal = `You are IotVision AI, assistant for an industrial IoT platform. Language: match the user's latest message exactly — Thai or English, never mix. Reply in one short, natural sentence. Plain text only — no markdown, no asterisks (**) or bold.`
```

with:

```go
// needToolsSentinel is the escape hatch for the needsTools() keyword gate: the
// regex can't see typos ("ส้างแดชบอด" for "สร้างแดชบอร์ด"), so when a message it
// stripped tools from actually asks for data or an action, the model replies
// with this sentinel and the handler retries once with the full prompt + tools.
// Without it the model role-played the action ("กำลังสร้างให้ครับ") and nothing
// happened. Costs ~40 extra prompt tokens on greetings; the full retry price is
// paid only on actual gate misses.
const needToolsSentinel = "NEED_TOOLS"

// systemPromptMinimal is sent on the no-tool path (greetings, chit-chat, "what
// can you do?"). It carries only identity + the language rule — the full
// TOOL SELECTION / SLOT FILLING / WIDGET rules are useless without tools, so a
// bare greeting no longer pays for them (~300 tokens saved vs systemPromptBase).
const systemPromptMinimal = `You are IotVision AI, assistant for an industrial IoT platform. Language: match the user's latest message exactly — Thai or English, never mix. Reply in one short, natural sentence. Plain text only — no markdown, no asterisks (**) or bold.
If the user's latest message asks to view data, metrics, machines, dashboards, alerts, or to create/add/change/remove anything — even misspelled or informal, Thai or English — reply with exactly NEED_TOOLS and nothing else. Greetings, thanks, and small talk get a normal short reply.`
```

- [ ] **Step 4: Run the live test to verify it passes**

Run: `cd backend; go test ./internal/modules/ai/ -run TestMinimalPromptSentinel -v`
Expected: `--- PASS: TestMinimalPromptSentinel` (4 cases, ~40s wall time due to rate-limit sleeps). If `typo-read` flakes (model answers text instead of sentinel), tighten the rule wording — do NOT loosen the comparison to `strings.Contains`; an exact match is what keeps false escalations impossible.

- [ ] **Step 5: Commit**

```bash
cd backend
git add internal/modules/ai/controller.go internal/modules/ai/sentinel_live_test.go
git commit -m "feat(ai): NEED_TOOLS sentinel rule in minimal prompt + live test"
```

---

### Task 3: Escalation retry in the chat handler loop

**Files:**
- Modify: `backend/internal/modules/ai/controller.go` (tools setup ~line 406–413 and the plain-text exit branch inside the `for i := 0; i < 5; i++` loop, ~line 458–471)

**Interfaces:**
- Consumes: `needToolsSentinel` (Task 2), existing locals `needsToolsFlag`, `answerFromContext`, `hasContext`, `body.Context`, `msgs`, `sp`, `buildGroqTools`.
- Produces: runtime behavior only — no new symbols.

- [ ] **Step 1: Keep a handle on the full tool list**

The current code builds tools then nils them, losing the full list. Replace:

```go
	tools := buildGroqTools(middleware.GetUser(c).Role, hasContext)

	// Pure conversational messages (greetings, "what can you do?") get no tools —
	// the model answers in plain text and tools are sent on the next actionable message.
	// answerFromContext likewise needs no tools: the on-screen context already has the answer.
	if !needsToolsFlag || answerFromContext {
		tools = nil
	}
```

with:

```go
	fullTools := buildGroqTools(middleware.GetUser(c).Role, hasContext)
	tools := fullTools

	// Pure conversational messages (greetings, "what can you do?") get no tools —
	// the model answers in plain text and tools are sent on the next actionable message.
	// answerFromContext likewise needs no tools: the on-screen context already has the answer.
	// fullTools is kept for the NEED_TOOLS sentinel escalation below.
	if !needsToolsFlag || answerFromContext {
		tools = nil
	}
```

- [ ] **Step 2: Add the one-shot escalation in the text-exit branch**

In the loop, replace:

```go
		if choice.FinishReason != "tool_calls" || len(choice.Message.ToolCalls) == 0 {
			text := ""
			if choice.Message.Content != nil {
				text = *choice.Message.Content
			}
			assistantMsg, err := ctrl.repo.AddMessage(ctx, body.ConversationID, "assistant", text, nil, nil, nil)
```

with:

```go
		if choice.FinishReason != "tool_calls" || len(choice.Message.ToolCalls) == 0 {
			text := ""
			if choice.Message.Content != nil {
				text = *choice.Message.Content
			}
			// The keyword gate stripped tools but the model says the message needs
			// them (typos the regex can't see — "ส้างแดชบอด"). Retry ONCE with the
			// full prompt + tools; the sentinel itself is never saved as a reply.
			// ponytail: one extra round only on gate misses, ~2.7k tok — same price
			// a correctly-routed message pays anyway.
			if !escalated && !needsToolsFlag && strings.TrimSpace(text) == needToolsSentinel {
				escalated = true
				sp = systemPromptBase
				if hasContext {
					sp += systemPromptContextExt
					ctxContent := "Authoritative current dashboard state (overrides anything said earlier):\n" + body.Context
					msgs = append(msgs, groqMessage{Role: "system", Content: &ctxContent})
				}
				tools = fullTools
				callTools = fullTools
				continue
			}
			assistantMsg, err := ctrl.repo.AddMessage(ctx, body.ConversationID, "assistant", text, nil, nil, nil)
```

And declare the guard flag just above the loop — replace:

```go
	callTools := tools
	for i := 0; i < 5; i++ {
```

with:

```go
	callTools := tools
	escalated := false
	for i := 0; i < 5; i++ {
```

**Why `sp = ...` works without touching `msgs[0]`:** `msgs[0].Content` is `&sp`, a pointer to the local — reassigning `sp` swaps the system prompt in place. Verify this is still true when editing (look for `msgs := []groqMessage{{Role: "system", Content: &sp}}`); if the code has changed to copy the string, set `msgs[0].Content` to the new prompt explicitly instead.

**Known, accepted trade-offs (do not "fix" these):**
- The escalation consumes one iteration of the 5-round loop and, because `roundCap` compares against `i`, an escalated request gets one fewer chained tool round. Typo requests are single-tool calls (preview_dashboard, show_metric) — irrelevant in practice.
- If the model ever replies `NEED_TOOLS` again AFTER escalation (tools present — near-impossible), the raw sentinel would be saved as the reply. Guarded only by `escalated`; deliberately not handled further.

- [ ] **Step 3: Compile and vet**

Run: `cd backend; go build ./...; go vet ./...`
Expected: no output (clean build).

- [ ] **Step 4: Run the full package unit tests**

Run: `cd backend; go test ./internal/modules/ai/ -v -run 'TestNeedsTools|TestToDatetimeLocal'`
Expected: all PASS.

- [ ] **Step 5: End-to-end verification against the running stack**

Start the stack if not running: `docker compose up -d --build backend`
Then in the frontend AI page (login `admin@acme-foods.com` / `Admin@1234` at http://localhost:5173), send exactly: `ส้างแดชบอด cw-01 ให้หน่อย`

Expected: a dashboard PREVIEW card appears (preview_dashboard tool ran) — not just a text reply claiming it was created. Also send `สวัสดีครับ` and confirm a normal one-sentence Thai greeting (no sentinel leak, no tool call).

- [ ] **Step 6: Commit**

```bash
cd backend
git add internal/modules/ai/controller.go
git commit -m "feat(ai): escalate NEED_TOOLS sentinel to full tool round (typo-proof intent gate)"
```

---

### Task 4: Bake-off eval case for typo intent

**Files:**
- Modify: `backend/internal/modules/ai/eval_test.go` (bakeCases slice, after the existing "create" case ~line 116)

**Interfaces:**
- Consumes: `bakeCase` struct, existing `bakeCases` slice.
- Produces: nothing — eval coverage only.

- [ ] **Step 1: Add the case**

After the line:

```go
	{label: "create", message: "สร้าง dashboard ของ CW-01 ให้หน่อย", expect: "preview_dashboard (NOT create)", want: "preview_dashboard"},
```

add:

```go
	// Typo'd create — the needsTools gate misses this at runtime (sentinel
	// escalates it); here we verify the model still picks the right tool once
	// tools are present.
	{label: "typo-create", message: "ส้างแดชบอด cw-01 ให้หน่อย", expect: "preview_dashboard despite typos", want: "preview_dashboard"},
```

- [ ] **Step 2: Compile check (bake-off is a long live run — do not run it now)**

Run: `cd backend; go vet ./internal/modules/ai/`
Expected: clean. The case gets exercised on the next full bake-off (`go test ./internal/modules/ai/ -run BakeOff -v`, ~15+ min live run — only run when deliberately re-evaluating models).

- [ ] **Step 3: Commit**

```bash
cd backend
git add internal/modules/ai/eval_test.go
git commit -m "test(ai): bake-off case for typo'd create intent"
```
