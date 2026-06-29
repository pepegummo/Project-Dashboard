# IotVision AI — Workflow & Optimization Notes

Model: **`openai/gpt-oss-120b`** (Groq) — upgraded from `gpt-oss-20b` (bake-off winner) after 20b occasionally hallucinated tool args in chained calls; 120b is also Groq prompt-cached so the discount and rate-limit benefit are unchanged. Both beat `qwen3-32b` (flaky/rate-limited) in the Thai-first bake-off (11/11).

## What changed (and why)

Goal priority (from the user): **(1) AI understands intent — Thai-first > (2) token cost > (3) latency.**

| Change | File | Why |
|---|---|---|
| Model swap `qwen/qwen3-32b` → `openai/gpt-oss-20b` → `openai/gpt-oss-120b` | `controller.go` | 20b won the Thai bake-off (11/11, no language leaks, cheapest, Groq-cached). Upgraded to 120b when 20b hallucinated tool args on chained calls. Prompt-cache discount and rate-limit exemption unchanged. |
| New `show_metric` tool + rule 11 (see/show/**add** a metric → focus card) | `schema.go`, `tool_actions.go`, `controller.go` | The AI maps the user's word (any language) → the English field key; backend resolves machine+field to a render-ready widget spec; the UI shows a live single-metric "focus" card (with an **Add to dashboard** button) or highlights the widget if it already exists. Replaces fragile frontend text-scraping of which metric was asked. |
| Server-side gate on `add_widget_to_dashboard` (empty `dashboard_name` → steer to `show_metric`) | `tool_actions.go` | Belt-and-suspenders for rule 11: a bare "add weight widget" must show a preview card, never ask "which dashboard?". |
| Machine matched by **substring**, not exact name | `tool_actions.go` `resolveMachineID` (and frontend `machineMatches`) | Display names are `"Checkweigher CW-01"` but users/AI say `"CW-01"`; exact match silently failed. |
| Deleted the unused telemetry simulator; reconciled docs Anthropic → Groq | `internal/simulator/` (removed), `config/env.go`, `CLAUDE.md` | Runs on backfill + live ingest via the broadcaster; the AI proxy is Groq, not Anthropic. |
| `show_metric` fallback: unknown metric → return all available field widgets | `tool_actions.go` | When user misspells a metric or says "show all", the model gets renderable widget specs for every field instead of a dead error. Non-status fields are gauge (if min+max exist) or kpi-card; status fields collapse to one kpi-card. |
| Tool chaining: turns i=0 and i=1 may both call tools; only i≥2 forces summary (no tools) | `controller.go` | Enables `get_machines → show_metric × N` in a single response — required for rule 11's "show all metrics" flow. Previously any second call was always tool-free. |
| `tool_choice: "required"` on turn i=0 when `context` is present; graceful retry with auto on `"Tool choice is required"` error | `controller.go` | Forces a live `show_metric` call when the user already has a widget card on screen, preventing the model from answering from stale context text. If the model legitimately wants to reply in plain text, the retry lets it. |
| `add_widget_to_dashboard` gate: empty `dashboard_name` returns a soft error directing the model to `show_metric` | `tool_actions.go` | Belt-and-suspenders for rule 11: a bare "add weight widget" can never silently fail or prompt "which dashboard?" — it hits this gate and falls back to the preview card flow. |
| `daily-count` widget gains `bucket`, `sku`, `status` config fields | `schema.go`, `dashboard_action.go` | Production count widget is now filterable by SKU and reject/good/all status; `bucket` controls time resolution (e.g. `"30m"`). |
| `callGroq` split into `callGroq` + `callGroqModel(model,…)` | `controller.go` | Lets the bake-off harness compare models without touching the request path. |
| Removed `/no_think` from system prompt | `controller.go` | qwen-specific directive; meaningless to gpt-oss. |
| Tightened rule 6 (default to `machine_overview` preview, never ask which template) | `controller.go` | "create a dashboard" should show a preview immediately — the preview *is* the confirmation step. Easier to use. |
| Rule 10 → explicit slot-filling (ask for the missing **machine**, never guess, never `get_machines` just to echo) | `controller.go` | The user's "ask if not clear", made precise: a read/alert needs a machine; if none is named, ask one short question instead of guessing a name. Still never asks which dashboard *template* (rule 6). |
| Shared `machineIDProp` description on the 4 required-`machine_id` tools | `schema.go` | `required` already forces the field; the description nudges the model to *ask* rather than invent a name. JSON Schema can't express "ask the user", so this pairs with rule 10. |
| New `eval_test.go` bake-off harness (+ `read-no-machine` / `read-no-machine-en` slot-filling cases) | `eval_test.go` | Throwaway model comparison; skips without `GROQ_API_KEY`. |

### Approaches considered and rejected
- **Semantic / embedding router** — Groq has no embeddings endpoint; for only 14 small tools the extra provider + per-request embedding call costs more than it saves.
- **Keyword / hybrid tool-gating router** — matches words, not meaning (e.g. "create dashboard, what machines do I have?" looks like an action but is a read). With *understanding* as priority #1, the model itself is the better intent-decider.
- **Greeting-skip (no tools on "hello")** — marginal once caching is on, and a bilingual matcher risks wrongly withholding tools.

### Token win without routing code
gpt-oss-20b is **prompt-cached by Groq automatically**. The request prefix
(system prompt + the 15 tool schemas, fixed order) is identical across turns, so
it gets a **50% cached-token discount and stops counting against the rate limit**.
The per-request dashboard `context` is appended *last*, after history, so it never
breaks the cached prefix.

## Tools offered to the model (role-filtered only) — 14 total

- **READ (6):** `get_machines`, `show_metric`, `get_telemetry_trend`, `get_active_alerts`, `get_daily_count`, `list_dashboards`
- **PREVIEW (4, no DB write):** `preview_dashboard`, `preview_add_widget`, `preview_remove_widget`, `preview_update_widget`
- **WRITE (4, admin/editor only):** `add_widget_to_dashboard`, `remove_widget`, `create_alert`, `manage_alert_event`

`show_metric` is read-only/display: it resolves a machine + field server-side and
returns a widget spec the UI renders as an ephemeral **focus card** (not persisted —
the user clicks **Add to dashboard** to keep it). If a widget for that metric already
exists on screen, the frontend highlights it instead of showing a card. If the
requested metric doesn't exist, the backend returns **all available field widgets**
as a fallback (gauge or kpi-card per field; status fields collapsed to one kpi-card)
so the model always has something renderable to show.

`get_latest_telemetry` (the old raw-values endpoint) has been removed — `show_metric`
is the only way to surface live sensor data; the model cannot fabricate values.

`create_custom_dashboard` is **not** offered to the model — it only runs when the
user clicks **Confirm** (`POST /tools/execute`). So the AI can never build a
dashboard on its own; the worst it can do is show a preview.

## Core chat flow

```mermaid
sequenceDiagram
    actor U as User
    participant FE as Frontend
    participant BE as Backend (Go)
    participant Groq as Groq · gpt-oss-120b
    participant DB as Database

    U->>FE: type a message (Thai/English)
    FE->>BE: POST /chat { conversationId, message, context? }
    BE->>DB: INSERT ai_messages (role=user)
    BE->>DB: SELECT history (last 8, user+assistant only)
    note over BE: build prompt = system(rules 1–11)<br/>+ history + context(last, if any)<br/>tools = all 14 (role-filtered)<br/>⟵ stable prefix → Groq cache hit<br/>if context present → tool_choice:"required" on turn 0

    loop up to 5 turns (i=0..4)
        note over BE: i=0: tool_choice = "required" if context present, else auto<br/>i=1: tool_choice = auto (chaining allowed)<br/>i≥2: callTools = nil → summary only
        BE->>Groq: POST /completions { messages, tools?, tool_choice? }
        alt finish_reason = "stop"
            Groq-->>BE: plain-text answer / clarifying question
            BE->>DB: INSERT ai_messages (role=assistant)
        else finish_reason = "tool_calls"
            Groq-->>BE: tool_calls[]
            BE->>BE: dispatch each tool (READ/PREVIEW/WRITE)
            BE->>DB: read or write per tool · INSERT ai_messages (role=tool)
            note over BE: error "Tool choice is none" → retry with full toolset<br/>error "Tool choice is required" → retry with auto
        end
    end

    BE-->>FE: { messages (+ toolResult for previews) }
    FE-->>U: render answer / preview panel
```

## The "decide what the user wants" step

The model — not a keyword layer — decides, guided by the system prompt:

```mermaid
flowchart TD
    A[User message] --> B{Model reads intent}
    B -->|greeting / general| C[Plain-text reply · no tool]
    B -->|see / show / add a metric| K[show_metric → focus card<br/>· highlight if it already exists]
    B -->|other data: machines / alerts / trend| D[READ tool → summarize]
    B -->|create dashboard| E[preview_dashboard → preview panel → user Confirm]
    B -->|edit on-screen preview| F[preview_update/add/remove_widget → local apply]
    B -->|add to a NAMED existing dashboard| G[add_widget_to_dashboard / remove_widget]
    B -->|create / manage alert| H[create_alert / manage_alert_event]
    B -->|genuinely vague 'fix it'| I[Ask ONE clarifying question · no tool]
    B -->|read but no machine named| J[Ask which machine · no tool, no guessed id]
```

## Case examples (verified in the bake-off, Thai-first)

| User says | Model does |
|---|---|
| "สวัสดีครับ" | plain Thai reply, no tool |
| "speed ของ CW-01 เท่าไหร่" / "ความเร็ว CW-01" | `show_metric({machine:"CW-01", metric:"speed"})` → focus card (or highlight if it exists) |
| "add weight widget" / "ขอ widget weight CW-01" | `show_metric({machine:"CW-01", metric:"weight"})` → focus card — **not** `add_widget_to_dashboard`, never asks which dashboard |
| "เทรนด์ speed CW-01 ย้อนหลัง" | `show_metric({…, viz:"trend"})` → line-chart focus card |
| "add a weight widget to CW-01 Overview" | `add_widget_to_dashboard({dashboard_name:"CW-01 Overview", …})` — names a dashboard |
| "มีเครื่องอะไรบ้าง" | `get_machines()` |
| "สร้าง dashboard ของ CW-01 ให้หน่อย" | `preview_dashboard({machine:"CW-01", template:"machine_overview"})` |
| "เปลี่ยน metric เป็น temperature" (preview on screen) | `preview_update_widget({widget_title:"Trend", metric:"temperature"})` |
| "สร้าง dashboard สิ แล้วตอนนี้มีเครื่องอะไรบ้าง" (trap) | `get_machines()` — reads, does **not** build |
| "แก้ให้หน่อย" (vague) | asks a clarifying question in Thai, no tool |
| "speed เท่าไหร่" (no machine named) | asks *which machine* in Thai — no tool, no guessed `machine_id` |
| "ดูทั้งหมด CW-01" / "show all metrics" | `get_machines()` → `show_metric(CW-01, field1)` + `show_metric(CW-01, field2)` + … (chained, all in one response) |
| "add weight widget" (no dashboard named) | `show_metric({machine:"CW-01", metric:"weight"})` — `add_widget_to_dashboard` gate returns soft error if `dashboard_name` is empty |
| field name wrong / typo | `show_metric` fallback returns all available field widgets for that machine |

## Two- or three-call pattern (data/tool turns)

A simple tool turn costs two Groq calls: one to pick the tool, one to summarize
the result. When the model chains a second tool (e.g. `get_machines` → `show_metric × N`
for "show all metrics"), a third call handles the summary. The tool array is dropped
only on the final summary call (i≥2). Streaming is a deferred follow-up.

```mermaid
sequenceDiagram
    participant BE as Backend
    participant Groq
    BE->>Groq: call 1 (i=0) { messages, tools, tool_choice? } → tool_calls
    BE->>BE: execute tool(s), append results
    BE->>Groq: call 2 (i=1) { messages + results, tools } → tool_calls OR stop
    alt chained (i=1 returned tool_calls)
        BE->>BE: execute chained tool(s), append results
        BE->>Groq: call 3 (i=2) { messages + all results, no tools } → final text
    else no chain (i=1 returned stop)
        note over Groq: final text already returned at call 2
    end
```

## Not done (future work)
- **Streaming** the final answer to the UI (cuts perceived latency on tool turns).
- Delete or keep `eval_test.go` — useful for re-validating model/prompt changes, but skip in CI (requires `GROQ_API_KEY`).
