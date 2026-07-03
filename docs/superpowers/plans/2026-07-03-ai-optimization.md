# AI Optimization Implementation Plan (understanding > token usage > latency)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Improve the AI assistant's understanding first (correct tool routing, correct machine/metric mapping, multi-turn memory), then reduce token spend, then latency — without regressing any existing behavior.

**Architecture:** Extract the per-turn routing decision (prompt tier, tool list, tool_choice, round cap) from `Controller.Chat` into a pure, unit-testable `planRequest` function. Make tool gating data-driven from the org's real machines/fields (cached in-process), inject a compact machine/metric inventory into the system prompt so the model stops guessing names and burning `get_machines` rounds, switch the history cap from 3 rows to a token budget, and converge the live eval harness onto the production routing path so every future change is measurable.

**Tech Stack:** Go (Fiber), Groq API (`openai/gpt-oss-120b`), existing eval harness in `eval_test.go`.

## Global Constraints

- **Priority order is understanding > token usage > latency.** When a change trades one for another, a false *negative* on tool routing (model can't act) is never acceptable to save tokens; a false positive (tools sent unnecessarily) is acceptable.
- **No behavior regression:** `cd backend && go test ./internal/modules/ai/` must pass after every task; the live eval (`go test -run RoutingEval -v`, requires `GROQ_API_KEY`) must not score lower than before the change.
- **Thai + English parity** in all keyword/routing logic — the user base is Thai-first (see `eval_test.go` cases).
- **No new dependencies, no Redis, no new tables.** In-process caching only (single backend instance today).
- Current baseline facts (verified): model `openai/gpt-oss-120b` (`controller.go:23`); history capped to last 3 rows text-only (`controller.go:726`); tool gating regex `needsToolsRe` (`controller.go:692`); free-tier limit 8k tokens/min.

## What is already optimized (do not redo)

Tiered system prompts (minimal/base/contextAnswer/contextExt), slim tool schemas (`toGroqToolSlim`), context-gated preview tools, tool-result compaction to `columns`+tuples (`compactSeriesResult`), answer-from-context no-tool path, chained round caps, 429 retry with `Retry-After` parsing.

## Non-Goals (deferred, with reason)

- **SSE streaming of the chat reply** — the single biggest *perceived* latency win, but it reshapes the API contract and the frontend message loop; latency is priority 3. Do it as its own plan if still wanted after this one.
- **System-prompt diet** (`systemPromptContextExt` is ~800 tokens with some overlap against `systemPromptContextAnswer`) — only safe with the eval harness from Task 5 in place; measure first, cut second. Candidate follow-up, not in this plan.
- **Model switch** — the bake-off already chose `gpt-oss-120b`; Task 5 keeps the harness so re-running a bake-off stays cheap.
- **Parallel tool dispatch** — DB tool calls are ~ms; the LLM round-trip dominates. YAGNI.

---

### Task 1: Extract `planRequest` — pure routing function + characterization tests

Zero behavior change. This creates the seam every later task tests through, and adds token-usage logging so improvements are measurable.

**Files:**
- Create: `backend/internal/modules/ai/request_plan.go`
- Test: `backend/internal/modules/ai/request_plan_test.go`
- Modify: `backend/internal/modules/ai/controller.go` (`Chat`, lines ~362–421 and the `roundCap` block at ~497–503)

**Interfaces:**
- Consumes: existing `needsTools`, `editRe`, `rangeRe`, `skuRe`, `buildGroqTools`, prompt constants (all package-private, same package).
- Produces: `type requestPlan struct { systemPrompt string; tools []map[string]any; firstToolChoice string; includeContext bool; roundCap int }` and `func planRequest(message, context, role string) requestPlan` — Tasks 2, 3, 5 extend/consume this exact signature (Task 2 adds a `machines []machineInfo` parameter).

- [ ] **Step 1: Write the characterization tests (current behavior, will fail only on `undefined: planRequest`)**

Create `backend/internal/modules/ai/request_plan_test.go`:

```go
package ai

import (
	"strings"
	"testing"
)

func TestPlanGreetingMinimalPromptNoTools(t *testing.T) {
	p := planRequest("สวัสดีครับ", "", "admin")
	if p.systemPrompt != systemPromptMinimal {
		t.Fatalf("greeting should get minimal prompt")
	}
	if p.tools != nil || p.includeContext {
		t.Fatalf("greeting should get no tools/context")
	}
}

func TestPlanMetricReadGetsToolsAndBasePrompt(t *testing.T) {
	p := planRequest("what's the speed of CW-01", "", "viewer")
	if len(p.tools) == 0 {
		t.Fatalf("metric read must include tools")
	}
	if p.systemPrompt != systemPromptBase {
		t.Fatalf("no-context read should get base prompt")
	}
	if p.roundCap != 1 {
		t.Fatalf("non-focused read keeps 2 tool rounds (cap 1), got %d", p.roundCap)
	}
}

func TestPlanFocusedPlainReadAnswersFromContext(t *testing.T) {
	p := planRequest("@Speed Gauge what is this", "widget context line", "admin")
	if p.systemPrompt != systemPromptContextAnswer {
		t.Fatalf("focused plain read should use context-answer prompt")
	}
	if p.tools != nil {
		t.Fatalf("focused plain read needs no tools")
	}
}

func TestPlanFocusedEditForcesToolCall(t *testing.T) {
	p := planRequest("@Trend เปลี่ยน metric เป็น temp", "widget context line", "admin")
	if len(p.tools) == 0 || p.firstToolChoice != "required" {
		t.Fatalf("focused edit must force a tool call, got tools=%d choice=%q", len(p.tools), p.firstToolChoice)
	}
	if p.roundCap != 0 {
		t.Fatalf("focused message gets 1 round (cap 0), got %d", p.roundCap)
	}
	if !p.includeContext {
		t.Fatalf("tool path with context must inject the context block")
	}
	if !strings.HasSuffix(p.systemPrompt, systemPromptContextExt) {
		t.Fatalf("tool path with context should append the context extension")
	}
}

func TestPlanInlineSeriesAnswersFromContext(t *testing.T) {
	p := planRequest("วิเคราะห์แนวโน้มหน่อย", "widget…\n  on-screen data — columns …", "admin")
	if p.systemPrompt != systemPromptContextAnswer || p.tools != nil {
		t.Fatalf("injected series should answer with no tool")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/modules/ai/ -run TestPlan -v`
Expected: FAIL — `undefined: planRequest`

- [ ] **Step 3: Create `request_plan.go` (logic moved verbatim from `Chat`)**

```go
package ai

import "strings"

// requestPlan is the routing decision for one chat turn: which prompt tier,
// which tools, and whether the on-screen context can answer without a tool.
// Pure function of the inputs — keep it free of I/O so it stays unit-testable.
type requestPlan struct {
	systemPrompt    string
	tools           []map[string]any // nil = no-tool call
	firstToolChoice string           // "" = auto, "required" = force tool on first call
	includeContext  bool             // append dashboard context as trailing system message
	roundCap        int              // chained tool rounds before forcing a text summary
}

func planRequest(message, context, role string) requestPlan {
	hasContext := context != ""
	needsToolsFlag := needsTools(message)

	// The frontend injects the focused widget's full series only for analytical
	// questions — when present, answer from it in one no-tool call.
	inlineData := hasContext && strings.Contains(context, "on-screen data")

	// A focused-widget read the injected context already answers needs no tool
	// round either. Guardrail: edits and aggregate/range/SKU questions still
	// take the tool path.
	focused := hasContext && strings.Contains(message, "@")
	contextRead := focused && !editRe.MatchString(message) && !rangeRe.MatchString(message) && !skuRe.MatchString(message)
	answerFromContext := inlineData || contextRead

	sp := systemPromptBase
	switch {
	case !needsToolsFlag:
		sp = systemPromptMinimal
	case answerFromContext:
		sp = systemPromptContextAnswer
	case hasContext:
		sp += systemPromptContextExt
	}

	var tools []map[string]any
	if needsToolsFlag && !answerFromContext {
		tools = buildGroqTools(role, hasContext)
	}

	tc := ""
	if focused && !answerFromContext {
		tc = "required"
	}
	roundCap := 1
	if focused {
		roundCap = 0
	}
	return requestPlan{
		systemPrompt:    sp,
		tools:           tools,
		firstToolChoice: tc,
		includeContext:  hasContext && needsToolsFlag,
		roundCap:        roundCap,
	}
}
```

- [ ] **Step 4: Rewire `Chat` to use it**

In `controller.go`, replace the block from `hasContext := body.Context != ""` (~line 363) through the `firstToolChoice` computation (~line 421) with:

```go
	plan := planRequest(body.Message, body.Context, middleware.GetUser(c).Role)

	msgs := []groqMessage{{Role: "system", Content: &plan.systemPrompt}}
	msgs = append(msgs, buildGroqMessages(history)...)
	// Inject the on-screen dashboard state AFTER history so the current state is
	// the last thing the model sees — recency wins over stale earlier turns.
	if plan.includeContext {
		ctxContent := "Authoritative current dashboard state (overrides anything said earlier):\n" + body.Context
		msgs = append(msgs, groqMessage{Role: "system", Content: &ctxContent})
	}

	tools := plan.tools
	firstToolChoice := plan.firstToolChoice
```

Then in the loop, replace the `roundCap := 1 / if focused { roundCap = 0 }` block (~lines 497–500) with a direct use of `plan.roundCap`:

```go
		if i >= plan.roundCap {
			callTools = nil
		}
```

Delete the now-unused local computations (`hasContext`, `needsToolsFlag`, `inlineData`, `focused`, `contextRead`, `answerFromContext`, `sp`). The comments they carried now live in `request_plan.go`.

- [ ] **Step 5: Add token-usage observability (measure before optimizing)**

In the loop in `Chat`, right after the `if len(resp.Choices) == 0` guard, add:

```go
		if resp.Usage != nil {
			fmt.Printf("🤖 groq call %d: prompt=%d completion=%d tokens\n",
				i, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
		}
```

- [ ] **Step 6: Run tests and vet**

Run: `cd backend && go build ./... && go vet ./... && go test ./internal/modules/ai/ -v`
Expected: all PASS (TestPlan* and pre-existing tests).

- [ ] **Step 7: Commit**

```bash
git add backend/internal/modules/ai/request_plan.go backend/internal/modules/ai/request_plan_test.go backend/internal/modules/ai/controller.go
git commit -m "refactor(ai): extract planRequest routing seam + token usage logging"
```

---

### Task 2: Data-driven tool gating (fixes the biggest understanding bug)

`needsToolsRe` only knows a hardcoded word list. Real machine fields like `load`, `rpm`, `vibration`, `defect_rate`, `dew_point`, and any machine name not matching a keyword (e.g. "CB-01 โหลดเท่าไหร่") fall through to the **no-tool minimal-prompt path** — the model cannot answer and either fabricates or apologizes. Gate on the org's *actual* machines and fields instead.

**Files:**
- Create: `backend/internal/modules/ai/inventory.go`
- Modify: `backend/internal/modules/ai/request_plan.go` (signature + gating)
- Modify: `backend/internal/modules/ai/request_plan_test.go` (update call sites, add cases)
- Modify: `backend/internal/modules/ai/controller.go` (`Chat` passes inventory)

**Interfaces:**
- Consumes: existing `getMachinesForOrg(ctx, orgID) ([]machineInfo, error)` (`dashboard_action.go:497`) and `machineInfo{Name, Type, Status, Fields}`.
- Produces: `func orgInventory(ctx context.Context, orgID string) []machineInfo` (cached) and `planRequest(message, context, role string, machines []machineInfo) requestPlan` — Task 3 reuses both.

- [ ] **Step 1: Write the failing tests**

Append to `request_plan_test.go` (and change all existing `planRequest(...)` calls in this file to pass `nil` as the 4th argument):

```go
var testInventory = []machineInfo{
	{Name: "CB-01", Type: "conveyor", Fields: []string{"speed", "load", "rpm", "vibration"}},
	{Name: "TS-01", Type: "temperature_sensor", Fields: []string{"temp", "humidity", "dew_point"}},
}

func TestPlanFieldNotInStaticRegexStillGetsTools(t *testing.T) {
	// "โหลด"/"load"/"CB-01" match nothing in needsToolsRe — before this fix the
	// message misrouted to the minimal no-tool path and the model couldn't answer.
	p := planRequest("โหลดของ CB-01 เท่าไหร่", "", "viewer", testInventory)
	if len(p.tools) == 0 {
		t.Fatalf("machine-name match must enable tools")
	}
	p = planRequest("vibration เท่าไหร่ตอนนี้", "", "viewer", testInventory)
	if len(p.tools) == 0 {
		t.Fatalf("field-key match must enable tools")
	}
}

func TestPlanGreetingStaysMinimalWithInventory(t *testing.T) {
	p := planRequest("สวัสดีครับ", "", "admin", testInventory)
	if p.systemPrompt != systemPromptMinimal || p.tools != nil {
		t.Fatalf("greeting must stay on the minimal no-tool path")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/modules/ai/ -run TestPlan -v`
Expected: compile error (wrong arity) → after mechanical call-site fix, `TestPlanFieldNotInStaticRegexStillGetsTools` FAILS.

- [ ] **Step 3: Implement**

Create `backend/internal/modules/ai/inventory.go`:

```go
package ai

import (
	"context"
	"strings"
	"sync"
	"time"
)

// orgInventory returns the org's machines + numeric field keys for routing and
// prompt injection. ponytail: 5-min in-process TTL — machines change rarely;
// move to Redis (already provisioned) only if the backend ever runs multi-instance.
type invEntry struct {
	machines []machineInfo
	expires  time.Time
}

var invCache sync.Map // orgID → invEntry

func orgInventory(ctx context.Context, orgID string) []machineInfo {
	if v, ok := invCache.Load(orgID); ok {
		if e := v.(invEntry); time.Now().Before(e.expires) {
			return e.machines
		}
	}
	ms, err := getMachinesForOrg(ctx, orgID)
	if err != nil {
		return nil // routing falls back to the static regex — never blocks a chat
	}
	invCache.Store(orgID, invEntry{machines: ms, expires: time.Now().Add(5 * time.Minute)})
	return ms
}

// needsToolsFor extends the static keyword gate with the org's real machine
// names and field keys, so a read like "โหลดของ CB-01" routes to the tool path
// even though neither word is in needsToolsRe. False positives only cost the
// tool-list tokens; false negatives make the assistant unable to act — so
// when in doubt, include tools (understanding > token usage).
func needsToolsFor(msg string, machines []machineInfo) bool {
	if needsToolsRe.MatchString(msg) {
		return true
	}
	lower := strings.ToLower(msg)
	for _, m := range machines {
		if m.Name != "" && strings.Contains(lower, strings.ToLower(m.Name)) {
			return true
		}
		for _, f := range m.Fields {
			// ponytail: 3-char floor keeps 1–2 char keys from matching everywhere.
			if len(f) >= 3 && strings.Contains(lower, strings.ToLower(f)) {
				return true
			}
		}
	}
	return false
}
```

In `request_plan.go`, change the signature and the gate:

```go
func planRequest(message, context, role string, machines []machineInfo) requestPlan {
	hasContext := context != ""
	needsToolsFlag := needsToolsFor(message, machines)
```

(everything else unchanged).

In `controller.go` `Chat`, before the `plan := ...` line:

```go
	machines := orgInventory(ctx, middleware.GetUser(c).OrgId)
	plan := planRequest(body.Message, body.Context, middleware.GetUser(c).Role, machines)
```

- [ ] **Step 4: Run tests**

Run: `cd backend && go test ./internal/modules/ai/ -v && go vet ./...`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/modules/ai/inventory.go backend/internal/modules/ai/request_plan.go backend/internal/modules/ai/request_plan_test.go backend/internal/modules/ai/controller.go
git commit -m "feat(ai): gate tools on real org machines/fields, not a static word list"
```

---

### Task 3: Inject a machine/metric inventory line into the system prompt

Today the model must either guess metric keys (understanding failure: Thai wording → wrong key) or spend a whole `get_machines` round (~2 extra API calls re-sending everything — tokens *and* latency). A compact `MACHINES` glossary (~15 tokens/machine) kills both. This is the deliberate trade: small fixed token cost for a large understanding + latency win — consistent with the priority order.

**Files:**
- Modify: `backend/internal/modules/ai/inventory.go` (add `inventoryLine`)
- Modify: `backend/internal/modules/ai/request_plan.go` (append when on the tool path)
- Modify: `backend/internal/modules/ai/request_plan_test.go`
- Modify: `backend/internal/modules/ai/controller.go` (`systemPromptBase` wording)

**Interfaces:**
- Consumes: `machines []machineInfo` already flowing through `planRequest` (Task 2).
- Produces: `func inventoryLine(machines []machineInfo) string`; the `MACHINES (name(type): metric keys):` block in the system prompt that Task 5's eval cases reference.

- [ ] **Step 1: Write the failing tests**

Append to `request_plan_test.go`:

```go
func TestPlanToolPathIncludesMachineInventory(t *testing.T) {
	p := planRequest("โหลดของ CB-01 เท่าไหร่", "", "viewer", testInventory)
	if !strings.Contains(p.systemPrompt, "MACHINES") ||
		!strings.Contains(p.systemPrompt, "CB-01(conveyor): speed,load,rpm,vibration") {
		t.Fatalf("tool path should carry the machine glossary, got:\n%s", p.systemPrompt)
	}
}

func TestPlanNoInventoryOnGreetingOrContextAnswer(t *testing.T) {
	if p := planRequest("สวัสดี", "", "admin", testInventory); strings.Contains(p.systemPrompt, "MACHINES") {
		t.Fatalf("greeting must not pay for the glossary")
	}
	p := planRequest("@Gauge what is this", "widget context", "admin", testInventory)
	if strings.Contains(p.systemPrompt, "MACHINES") {
		t.Fatalf("answer-from-context must not pay for the glossary")
	}
}

func TestInventoryLineCapsAtThirty(t *testing.T) {
	many := make([]machineInfo, 35)
	for i := range many {
		many[i] = machineInfo{Name: "M", Type: "t", Fields: []string{"f1"}}
	}
	line := inventoryLine(many)
	if !strings.Contains(line, "call get_machines for the full list") {
		t.Fatalf("oversized inventory must truncate with a hint")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/modules/ai/ -run 'TestPlan|TestInventory' -v`
Expected: FAIL — `undefined: inventoryLine` / missing MACHINES block.

- [ ] **Step 3: Implement**

Append to `inventory.go`:

```go
// inventoryLine renders a compact per-org machine/metric glossary for the
// system prompt, so the model maps user wording (Thai or English) to real
// machine names and field keys without a get_machines round.
// ponytail: capped at 30 machines (~450 tokens worst case) — truncates with a
// get_machines hint beyond that; add pagination only if a real org exceeds it.
func inventoryLine(machines []machineInfo) string {
	if len(machines) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("MACHINES (name(type): metric keys):")
	for i, m := range machines {
		if i >= 30 {
			b.WriteString("\n- …more machines exist — call get_machines for the full list")
			break
		}
		b.WriteString("\n- " + m.Name + "(" + m.Type + "): " + strings.Join(m.Fields, ","))
	}
	return b.String()
}
```

In `request_plan.go`, after the `tools` assignment inside `planRequest`:

```go
	if len(tools) > 0 {
		if inv := inventoryLine(machines); inv != "" {
			sp += "\n\n" + inv
		}
	}
```

(Place this AFTER the `sp` switch so the glossary lands at the end of the stable prompt prefix — per-org but stable for 5 minutes, so Groq prompt caching still applies within a conversation.)

- [ ] **Step 4: Teach the prompt to use it**

In `controller.go` `systemPromptBase`, make two edits:

a) Replace the line
`- User asks to see ALL metrics of a machine → get_machines first, then show_metric once per field.`
with:
`- User asks to see ALL metrics of a machine → show_metric once per field key from the MACHINES list (no get_machines call needed). get_machines is for machine STATUS questions.`

b) In SLOT FILLING, replace
`- Machine unknown → ask which machine in ONE question. Never guess. Never call get_machines just to list them back.`
with:
`- Machine unknown → ask which machine in ONE question, offering the names from MACHINES. Never guess. Map the user's wording to the closest metric key in MACHINES; if nothing matches, say that metric doesn't exist for that machine — never invent a key.`

- [ ] **Step 5: Run tests, then live smoke**

Run: `cd backend && go test ./internal/modules/ai/ -v && go vet ./...`
Expected: PASS.

Live smoke (stack running, `GROQ_API_KEY` set): in the AI page ask `โหลดของ CB-01 เท่าไหร่` — expect a `show_metric` call with `metric: "load"` on the first round, no `get_machines` round. Check the backend log's `🤖 groq call` lines: the whole turn should be 2 calls.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/modules/ai/inventory.go backend/internal/modules/ai/request_plan.go backend/internal/modules/ai/request_plan_test.go backend/internal/modules/ai/controller.go
git commit -m "feat(ai): inject machine/metric glossary into prompt, drop get_machines round"
```

---

### Task 4: History cap by budget, not a flat 3 rows

The flat 3-row cap (`controller.go:731`) loses multi-turn context: after one clarify round ("which machine?" → "CW-01"), the original question is already at the edge; anything older ("แล้ว CW-02 ล่ะ" follow-ups) is gone. Keep up to 8 user/assistant rows within a fixed character budget — better understanding with a *bounded* worst-case token cost, and strictly better than today when a tool-heavy turn ate the 3 slots.

**Files:**
- Modify: `backend/internal/modules/ai/controller.go` (`buildGroqMessages`, ~line 713)
- Test: `backend/internal/modules/ai/history_test.go` (create)

**Interfaces:**
- Consumes: `Message{Role, Content}` rows in DESC order (newest first) from `repo.GetMessages`.
- Produces: same `[]groqMessage` contract (oldest-first, user/assistant text only) — no caller changes.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/modules/ai/history_test.go`:

```go
package ai

import (
	"fmt"
	"strings"
	"testing"
)

// rows arrive DESC (newest first), mirroring repo.GetMessages.
func descRows(contents ...string) []Message {
	var out []Message
	for i, c := range contents {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		out = append(out, Message{Role: role, Content: c})
	}
	return out
}

func TestHistoryKeepsUpToEightRowsOldestFirst(t *testing.T) {
	var contents []string
	for i := 9; i >= 0; i-- { // 10 short rows, newest first: "msg9" … "msg0"
		contents = append(contents, fmt.Sprintf("msg%d", i))
	}
	got := buildGroqMessages(descRows(contents...))
	if len(got) != historyRowCap {
		t.Fatalf("want %d rows, got %d", historyRowCap, len(got))
	}
	if *got[0].Content != "msg2" || *got[len(got)-1].Content != "msg9" {
		t.Fatalf("want oldest-first msg2…msg9, got %s…%s", *got[0].Content, *got[len(got)-1].Content)
	}
}

func TestHistoryBudgetDropsOldLongRows(t *testing.T) {
	long := strings.Repeat("x", historyCharBudget) // one row eats the whole budget
	got := buildGroqMessages(descRows("newest", long, "oldest"))
	if len(got) != 1 || *got[0].Content != "newest" {
		t.Fatalf("budget should keep only the newest row, got %d rows", len(got))
	}
}

func TestHistorySkipsToolRows(t *testing.T) {
	rows := []Message{
		{Role: "tool", Content: "Tool executed: show_metric"},
		{Role: "assistant", Content: "a"},
		{Role: "user", Content: "q"},
	}
	got := buildGroqMessages(rows)
	if len(got) != 2 || *got[0].Content != "q" || *got[1].Content != "a" {
		t.Fatalf("tool rows must not consume history slots, got %v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/modules/ai/ -run TestHistory -v`
Expected: FAIL — `undefined: historyRowCap`, then row-count mismatches (current cap is 3 and counts tool rows).

- [ ] **Step 3: Replace `buildGroqMessages`**

In `controller.go`, replace the whole function (and its comment block) with:

```go
// buildGroqMessages converts recent DB rows (DESC, newest first) to Groq format,
// oldest-first. History is capped by BOTH a row count and a character budget:
// the flat 3-row cap saved tokens but amputated multi-turn follow-ups, and it
// counted tool rows that were then discarded. Budget math: ~1600 UTF-8 bytes
// ≈ 400 tokens worst case — bounded, and only long transcripts pay it.
//
// Past tool calls/results are deliberately NOT replayed — the assistant's text
// reply already summarizes them; a follow-up that needs fresh data makes its
// own tool call in the current turn.
const historyRowCap = 8
const historyCharBudget = 1600

func buildGroqMessages(msgs []Message) []groqMessage {
	var kept []Message
	budget := historyCharBudget
	for _, m := range msgs { // newest → oldest
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		if len(kept) >= historyRowCap || budget < len(m.Content) {
			break
		}
		budget -= len(m.Content)
		kept = append(kept, m)
	}
	result := make([]groqMessage, 0, len(kept))
	for i := len(kept) - 1; i >= 0; i-- { // reverse back to oldest-first
		m := kept[i]
		result = append(result, groqMessage{Role: m.Role, Content: strPtr(m.Content)})
	}
	return result
}
```

- [ ] **Step 4: Run all tests**

Run: `cd backend && go test ./internal/modules/ai/ -v && go vet ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/modules/ai/controller.go backend/internal/modules/ai/history_test.go
git commit -m "feat(ai): budget-based history window restores multi-turn context"
```

---

### Task 5: Permanent routing eval on the production path

`eval_test.go` is a throwaway bake-off that hand-builds its own prompt/tool combo — it does **not** exercise the production routing, so none of Tasks 1–4 are covered by it, and its header says "delete once a model is chosen". Convert it into a permanent single-model routing eval that goes through `planRequest`, and add cases for the newly fixed behaviors. This is the regression gate every future prompt/token change must pass.

**Files:**
- Modify: `backend/internal/modules/ai/eval_test.go` (repurpose; keep filename for git history)

**Interfaces:**
- Consumes: `planRequest(message, context, role, machines)` (Task 2 signature), `callGroqModel`, `groqModel`, `testInventory` fixture pattern from `request_plan_test.go`.
- Produces: `go test -run RoutingEval` — skipped without `GROQ_API_KEY`; prints a scoreboard and FAILS the test if the pass rate drops below the recorded baseline.

- [ ] **Step 1: Rewrite the harness**

In `eval_test.go`: delete `bakeModels` and `TestBakeOff`; update the header comment to say this is the permanent routing eval; keep `bakeCase` and `bakeCases` but apply these changes:

a) Add new cases to `bakeCases`:

```go
	// ── Fixed by data-driven gating + MACHINES glossary (Tasks 2–3) ─────────
	{label: "read-load-thai", message: "โหลดของ CB-01 เท่าไหร่", expect: "show_metric with metric load", want: "show_metric"},
	{label: "read-vibration", message: "CB-01 vibration เท่าไหร่", expect: "show_metric", want: "show_metric"},
	{label: "machines-from-prompt", message: "มีเครื่องอะไรบ้าง", expect: "answers from MACHINES glossary, no tool", want: ""},
```

b) Update the existing `trap-action-but-read` case — with the MACHINES glossary in the prompt, listing machines no longer needs a tool:

```go
	{label: "trap-action-but-read", message: "สร้าง dashboard สิ แล้วตอนนี้มีเครื่องอะไรบ้าง", expect: "answer machine list from MACHINES (no get_machines) or preview flow", want: ""},
```

c) Replace the per-case setup inside the loop so it runs the PRODUCTION path:

```go
func TestRoutingEval(t *testing.T) {
	_ = godotenv.Load("../../../../.env", "../../../.env")
	key := os.Getenv("GROQ_API_KEY")
	if key == "" {
		t.Skip("GROQ_API_KEY not set — skipping live routing eval")
	}
	config.Env = &config.Config{GroqApiKey: key}

	evalInventory := []machineInfo{
		{Name: "CW-01", Type: "checkweigher", Fields: []string{"weight", "speed", "rejects", "throughput"}},
		{Name: "CB-01", Type: "conveyor", Fields: []string{"speed", "load", "rpm", "vibration"}},
		{Name: "TS-01", Type: "temperature_sensor", Fields: []string{"temp", "humidity", "dew_point"}},
	}

	score, total := 0, 0
	for _, tc := range bakeCases {
		plan := planRequest(tc.message, tc.context, "admin", evalInventory)

		msgs := []groqMessage{{Role: "system", Content: &plan.systemPrompt}}
		msgs = append(msgs, tc.history...)
		msgs = append(msgs, groqMessage{Role: "user", Content: strPtr(tc.message)})
		if plan.includeContext {
			ctxContent := "Authoritative current dashboard state (overrides anything said earlier):\n" + tc.context
			msgs = append(msgs, groqMessage{Role: "system", Content: &ctxContent})
		}

		fmt.Printf("\n[%s] %q\n  expect: %s\n", tc.label, tc.message, tc.expect)
		time.Sleep(evalSleep()) // dodge free-tier 8k tokens/min
		resp, err := callGroqModel(groqModel, msgs, plan.tools, plan.firstToolChoice)
		if err != nil || len(resp.Choices) == 0 {
			fmt.Printf("  ERROR: %v\n", err)
			total++
			continue
		}
		ch := resp.Choices[0]
		got := ""
		if ch.FinishReason == "tool_calls" && len(ch.Message.ToolCalls) > 0 {
			got = ch.Message.ToolCalls[0].Function.Name
			fmt.Printf("  -> TOOL: %s(%s)\n", got, ch.Message.ToolCalls[0].Function.Arguments)
		} else if ch.Message.Content != nil {
			fmt.Printf("  -> TEXT: %s\n", strings.TrimSpace(*ch.Message.Content))
		}
		total++
		if got == tc.want {
			score++
			fmt.Printf("  PASS\n")
		} else {
			fmt.Printf("  FAIL (want %q, got %q)\n", tc.want, got)
		}
	}

	fmt.Printf("\n========== ROUTING EVAL: %d/%d ==========\n", score, total)
	// Baseline gate: record the score of the first full run here and never let
	// a later change ship below it. Start at 80%% of cases; tighten as it climbs.
	if score*100 < total*80 {
		t.Fatalf("routing eval below baseline: %d/%d", score, total)
	}
}

// evalSleep is tunable so a paid-tier key can run the suite fast.
func evalSleep() time.Duration {
	if s := os.Getenv("EVAL_SLEEP_SECONDS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 10 * time.Second
}
```

(Add `strconv` to the file's imports. Note `plan.firstToolChoice` replaces the old hardcoded `""` — the eval now also covers the forced-tool-call path, and the retry-on-"Tool choice is required" behavior lives in `callGroq`, not here, so a plain-text answer on a forced case scores as `""`.)

- [ ] **Step 2: Compile check**

Run: `cd backend && go vet ./... && go test ./internal/modules/ai/ -run TestRoutingEval -v`
Expected without `GROQ_API_KEY`: SKIP. With a key: scoreboard prints; expect ≥80% (the new cases pass because of Tasks 2–3).

- [ ] **Step 3: Record the baseline**

Run the live eval once, then update the `80` in the gate to the actual achieved percentage minus one case's worth of slack, and note the date + score in the file header comment.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/modules/ai/eval_test.go
git commit -m "test(ai): permanent routing eval through the production planRequest path"
```

---

### Task 6: Reuse one HTTP client (latency, one-line fix)

`callGroqModel` builds a new `http.Client` per call (`controller.go:617`), so every Groq call pays a fresh TCP+TLS handshake (~100–300 ms). A typical tool turn makes 2–3 calls; connection keep-alive makes all but the first nearly free.

**Files:**
- Modify: `backend/internal/modules/ai/controller.go` (~line 617)

**Interfaces:** none — internal to `callGroqModel`.

- [ ] **Step 1: Hoist the client**

Replace `httpClient := &http.Client{Timeout: 90 * time.Second}` inside `callGroqModel` with a package-level var above the function:

```go
// groqHTTP is shared so keep-alive reuses the TLS connection across the 2–3
// calls of a tool turn instead of re-handshaking each time.
var groqHTTP = &http.Client{Timeout: 90 * time.Second}
```

and use `groqHTTP.Do(req)` in the loop.

- [ ] **Step 2: Build, vet, test**

Run: `cd backend && go build ./... && go vet ./... && go test ./internal/modules/ai/`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/modules/ai/controller.go
git commit -m "perf(ai): reuse HTTP client for Groq keep-alive"
```

---

## Expected impact summary

| Task | Understanding | Tokens | Latency |
|---|---|---|---|
| 1 planRequest seam + usage logs | testable routing (foundation) | measurable | — |
| 2 data-driven gating | fixes misrouted metric reads (real bug) | small cost on rare false positives | — |
| 3 MACHINES glossary | correct machine/metric mapping, fewer clarify turns | +~15/machine per tool turn, −1 full `get_machines` round when it fires | −2 API calls on fan-out flows |
| 4 budget history | restores multi-turn follow-ups | bounded (+~400 tok worst case vs 3-row) | — |
| 5 production-path eval | regression gate for every future change | enables the deferred prompt diet | — |
| 6 shared HTTP client | — | — | −100–300 ms per call after the first |

## Self-Review Notes

- **Priority coverage:** understanding gets Tasks 1–5; token usage gets the observability (T1), bounded budgets (T3/T4) and the eval gate that unlocks the deferred prompt diet; latency gets T3 (fewer rounds) and T6. Streaming deliberately deferred (priority 3, big blast radius).
- **Type consistency:** `planRequest(message, context, role string, machines []machineInfo)` is introduced in Task 2 and used identically in Tasks 3 and 5; `historyRowCap`/`historyCharBudget` defined and tested in Task 4 only. `machineInfo` already exists at `dashboard_action.go:490`.
- **Ordering constraint:** Tasks must run 1 → 2 → 3 (each extends `planRequest`); 4 and 6 are independent; 5 requires 2–3 (its new cases depend on the glossary).
