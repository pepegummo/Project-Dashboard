# AI Assistant — Token / Cost / Latency Optimization

_Summary of changes made to `backend/internal/modules/ai/`. Read top-to-bottom._

## The core problem

Every user message runs an **agentic loop** — 2+ calls to Groq (`qwen/qwen3-32b`). Groq does **not** cache the static prefix, so on *every* call you re-pay for: system prompt + all tool schemas + message history + tool results. Cost and latency multiply across the loop.

Levers: shrink the prefix, send fewer tools, trim what comes back, don't re-send what you don't need.

---

## Phase 1 — output tokens & latency (measured)

| # | Change | File | Why |
|---|--------|------|-----|
| 1 | Append `/no_think` to system prompt | `controller.go` | `qwen3-32b` is a *reasoning* model — it generated a wall of hidden "thinking" tokens (billed as output) before every reply. `reasoning_format: hidden` only hid them; you still paid. `/no_think` disables it. |
| 2 | Role-gated tools | `controller.go: buildGroqTools` | The 5 write-tools were sent to viewers even though `dispatch()` blocks them — wasted tokens + denied-call apologies. |
| 3 | Trimmed tool descriptions | `schema.go` | `preview_*`/add/remove descriptions were paragraph-length and duplicated system-prompt rule #5. Cut to one line each. |
| 4 | Projected `GetActiveAlerts` payload | `tool_actions.go` | Was dumping full alert structs; now `{event_id, machine, metric, value, severity, status, message, triggeredAt}`. Results get re-sent each loop pass, so this compounds. |
| 5 | Deleted dead code | `schema.go`, `tool_actions.go`, `AIAssistantPage.vue` | `GetFactoryOverview` / `LocateWidget` + tool defs + orphaned frontend handler — never dispatched, never sent. |

**Result (confirmed in Groq logs):** output tokens **278 → 37 (~8×)**, latency **~1.0s → 0.19s (~5×)**.

---

## Phase 2 — input tokens via intent gating

**Read-only intent gate** — `controller.go: wantsBuilderTools`, `schema.go: isBuilderTool`

A question like _"what's the speed of CW-01?"_ doesn't need the 7 builder tools (dashboard/widget/alert) — only the 6 read tools. A keyword check (no extra API call) drops the builder tools when the message shows no build/edit intent.

- Read-only message: **13 tools → 6** (~−1,000 tok/message).
- Builder prompts ("create a dashboard…") **correctly still get the full set** — this is why a create-dashboard test still showed ~1.5–1.9k input. Working as designed, not a bug.

Tool split:
- **Read (6, always):** `get_machines`, `get_latest_telemetry`, `get_telemetry_trend`, `get_active_alerts`, `get_daily_count`, `list_dashboards`
- **Builder (7, gated):** `preview_dashboard`, `preview_add_widget`, `preview_remove_widget`, `add_widget_to_dashboard`, `remove_widget`, `create_alert`, `manage_alert_event`

---

## Phase 3 — input tokens on the builder path

**Drop tools on continuation calls** — `controller.go: Chat`

Logs showed 2 calls per message:
- **call 1** (~1526 tok): makes the tool call (e.g. `preview_dashboard`).
- **call 2** (~1875 tok): just writes _"Here's the preview, confirm?"_ — yet re-paid for the full ~1,000-token tool array it never uses (confirmation is a **button**, not a tool).

Fix: `callTools = nil` after the first tool round, so the summary call carries no tools.

- **~−1,000 tok off call 2 of every message.** Builder example: ~3,401 → ~2,401 (**~28%**).
- Untouched: parallel tool calls in one response, the preview→confirm→create flow.
- Only lost: cross-call tool chaining — already discouraged by system-prompt rule #3.

---

## Net effect per message

| | before | after |
|---|--------|-------|
| Output tokens | ~300 | ~37 |
| Latency | ~1.0s | ~0.19s |
| Read-only input | 13 tools × 2 calls | 6 tools (call 1), 0 tools (call 2) |
| Builder input | ~3,400 | ~2,400 |

---

## Deliberately NOT done (and why)

- **LLM-based router** — adds an extra call, defeats the purpose.
- **Enum trimming** (`time_range`, `condition`) — ~80 tok, but enums improve tool-call accuracy. Bad trade.
- **`GetMessages` SQL `LIMIT`** — that query also feeds the UI history; limiting it would truncate what users see.
- **Sub-gating builder tools** (to cut builder call-1 too) — deferred; keyword classification is brittle and a misroute could break dashboard creation. Measure Phase 3 first.

---

## TODO tomorrow

1. **Deploy Phase 3:** `docker compose up --build -d`.
2. **Verify:** send "create a dashboard for CW-01" → the **second** Groq request should drop ~1,000 input tokens (~1875 → ~875). Preview card + confirm button must still work.
3. Send "add a gauge and remove the table from Overview" → both happen, summary in call 2. Behavior unchanged.
4. If builder **call 1** input still hurts after measuring, consider the deferred sub-gating lever (flagship-flow risk — proceed carefully).

_Diagram of the full flow: `docs/ai-flow.html` (open in a browser)._
