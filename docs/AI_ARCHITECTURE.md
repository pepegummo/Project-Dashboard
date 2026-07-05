# IotVision AI Assistant — Architecture, Workflow & Model Choice

The AI assistant lets a factory operator talk to IotVision in plain Thai or English
("speed ของ CW-01 เท่าไหร่", "สร้าง dashboard ของ CW-01") to read live telemetry and
build dashboards. It is a **tool-calling LLM agent**: the model decides *what the user
wants*, the backend runs real database queries as tools, and the model turns the results
into a short natural reply.

All references below point at the real code under `backend/internal/modules/ai/`.

---

## 1. AI Architecture

### Components

| Layer | What | Where |
|-------|------|-------|
| **UI** | Vue 3 chat page; sends the message plus the on-screen dashboard/widget context | `frontend/src/pages/AIAssistantPage.vue`, `frontend/src/services/api.service.ts` |
| **API / Backend** | Fiber routes under `/api/ai` (JWT-gated); the `Chat` handler orchestrates the tool loop | `routes.go`, `controller.go` |
| **Tool layer** | read / preview / write tools exposed to the model (`AllTools()`); a dispatch switch runs each against the DB | `schema.go`, `tool_actions.go`, `dashboard_action.go` |
| **LLM (external)** | Groq, OpenAI-compatible chat-completions API | `https://api.groq.com/openai/v1/chat/completions`, model `openai/gpt-oss-20b` |
| **Database** | TimescaleDB / Postgres — telemetry + dashboards + AI conversation history | shared `database.Pool` |

**External services / secrets:** the only AI secret is `GROQ_API_KEY` (`config/env.go`).
The model id itself is a hardcoded constant (`controller.go:23`), not an environment
variable. If the key is empty the endpoint returns `503 AI_UNAVAILABLE`.

### Tool catalog

`AllTools()` (`schema.go:186`) hands the model the read / preview / write tools below.
Write tools require admin/editor (`writeTools`, `schema.go:206`); preview tools are only
sent when a dashboard context is present. `create_custom_dashboard` is deliberately **not**
in `AllTools()` — the UI calls it directly via `POST /api/ai/tools/execute` only after the
user clicks Confirm.

| Tool | What it does | Group | Flow |
|------|--------------|-------|------|
| `get_machines` | List machines (name, type, status, numeric field keys) | read | 2b |
| `show_metric` | Resolve a machine + metric to a widget spec / current value | read | 2b |
| `get_telemetry_trend` | avg / min / max over a time range | read | 2b |
| `get_telemetry_series` | Time-bucketed series (what a line chart shows) | read | 2b / 2d |
| `get_production_count` | Bucketed piece counts (daily-count widget) | read | 2b |
| `get_skus` | Distinct SKU values for a machine | read | 2b |
| `list_dashboards` | Dashboards with names + widget counts | read | 2b |
| `preview_dashboard` | Build a preview plan from a template (no DB write) | preview | 2c |
| `preview_add_widget` | Stage a widget onto the preview / open active dashboard (no DB write) | preview | 2c |
| `preview_update_widget` | Stage an edit (rename / metric / bucket / …) on the preview / active dashboard | preview | 2c |
| `preview_remove_widget` | Stage a widget removal from the preview / active dashboard | preview | 2c |
| `create_custom_dashboard` | Persist a confirmed preview as a new dashboard (frontend-only, post-Confirm) | write | 2c |

### Data flow

1. The browser POSTs `{ conversationId, message, context }` to `POST /api/ai/chat`
   (120 s client timeout).
2. The backend classifies the request, picks a system prompt, and calls Groq with the
   message history + a **slimmed tool catalog**.
3. When Groq asks for a tool, the backend runs the corresponding SQL against TimescaleDB,
   compacts the result, and feeds it back to Groq.
4. Groq produces a final natural-language reply; the backend persists the new messages and
   returns them to the UI.

Groq calls are **non-streaming** (single POST, whole body read), sent with
`reasoning_format: hidden`, and retried up to 3× on HTTP 429 with `Retry-After` backoff.

### Architecture diagram

```mermaid
flowchart LR
    subgraph Browser
        UI["Vue chat UI<br/>AIAssistantPage.vue"]
    end

    subgraph Backend["Go / Fiber backend"]
        CHAT["POST /api/ai/chat<br/>Chat handler"]
        CLASS["Prompt classify<br/>needsTools / hasContext"]
        LOOP["Tool-calling loop<br/>(max 5 rounds)"]
        TOOLS["Tool executors<br/>tool_actions.go / dashboard_action.go"]
    end

    subgraph External
        GROQ["Groq LLM<br/>openai/gpt-oss-20b"]
    end

    subgraph Data["TimescaleDB / Postgres"]
        TEL[("telemetry_raw<br/>machines · machine_fields")]
        DASH[("dashboards<br/>dashboard_widgets")]
        HIST[("ai_conversations<br/>ai_messages · ai_preview_drafts")]
    end

    UI -- "message + on-screen context" --> CHAT
    CHAT --> CLASS --> LOOP
    LOOP -- "tool decision + summary" --> GROQ
    GROQ -- "tool_calls / text" --> LOOP
    LOOP --> TOOLS
    TOOLS --> TEL
    TOOLS --> DASH
    CHAT -- "persist turns" --> HIST
    CHAT -- "reply messages[]" --> UI
```

---

## 2. AI Workflow

How the architecture actually operates, per request. The stages map onto the classic
`User Request → Intent Detection → Tool Selection → Data Retrieval → LLM Reasoning →
Dashboard Generation → User Feedback` shape, driven by `Chat` in `controller.go`
(≈ lines 335–508).

1. **User Request** — the UI sends the message plus a serialized snapshot of the
   on-screen dashboard/preview/focused widget (`buildDashboardContext`). For analytical
   questions about a focused chart it even inlines the rendered data series, so the model
   can answer without a second fetch.
2. **Intent Detection** — the backend computes `needsTools`, `answerFromContext`, and
   `hasContext`, then selects **one of four system prompts**:
   `minimal` (greeting/chit-chat), `contextAnswer` (the on-screen data already answers),
   `base` (default actionable rules), or `base + contextExt` (preview-editing rules added).
   This is where a bare "สวัสดีครับ" is routed to a no-tool path and a metric read is
   routed to `show_metric`.
3. **Tool Selection** — Groq's first turn returns either `tool_calls` (e.g.
   `show_metric`, `preview_dashboard`, `get_production_count`) or plain text if no tool is
   needed.
4. **Data Retrieval** — the dispatch switch runs each requested tool against TimescaleDB
   (org-scoped SQL via the domain services). Large series/count results are compacted
   (`compactSeriesResult` / `compactBucketResult`) into column+tuple form to cut tokens,
   then appended back as `tool` messages.
5. **LLM Reasoning** — results are fed back and the model runs again. The loop allows up
   to 5 rounds but a `roundCap` (0 for a focused `@widget` question, 1 otherwise) forces a
   text summary early to stay under Groq's 8k-tokens/min rate limit.
6. **Dashboard Generation** — for create/edit intents the model returns `preview_*`
   specs; the UI **stages** them on the card but writes nothing. A **new** dashboard
   persists only when the user clicks **Confirm** (`POST /api/ai/tools/execute` →
   `create_custom_dashboard`, gated to admin/editor); an **existing** dashboard persists
   only on **Save** (`saveDashboardCard` → the plain widget REST endpoints). The retired
   `add_widget_to_dashboard` / `remove_widget` tools mean the model can never mutate a saved
   dashboard on its own.
7. **User Feedback** — the assistant's reply is shown and the new turns
   (user + tool + assistant) are persisted to `ai_messages`.

### Sequence diagram

Every request starts the same way — the UI POSTs to `/api/ai/chat`, the backend
classifies intent and picks 1 of 4 system prompts — then follows one of four flow shapes:

- **2a Greeting** — no tool, no DB.
- **2b Read** — any read (metric value/trend, SKUs, dashboard list) → read tool → DB → summary.
- **2c Change / Add / Delete** — stage via `preview_*` → Confirm (new) / Save (existing) → write.
- **2d Answer-from-context** — the on-screen data already answers → no tool, no DB.

One behavior is **not** a separate shape but a **cross-cutting guard**: if a read or an
edit is missing a required slot (which machine? which widget? change it to what?), the
`SLOT FILLING` rules (`controller.go:54-57`) make the model **ask one clarifying question
and call no tool** — e.g. `"speed เท่าไหร่"` (no machine) or `"แก้ให้หน่อย"` (no target).
It applies to both 2b and 2c, so it is shown once as a note rather than its own diagram.

All flows persist the turn to `ai_messages` at the end (omitted from each diagram for
brevity).

#### 2a. Greeting / chit-chat  — `"สวัสดีครับ"`

```mermaid
sequenceDiagram
    actor User
    participant UI as Vue UI
    participant BE as Backend (Chat)
    participant LLM as Groq LLM

    User->>UI: "สวัสดีครับ"
    UI->>BE: POST /api/ai/chat
    BE->>BE: classify → minimal prompt, no tools
    BE->>LLM: message (no tools)
    LLM-->>BE: one short Thai/English sentence
    BE-->>UI: reply (no tool, no DB read)
    UI-->>User: shows reply
```

#### 2b. Read — metric / analytical / list  — `"speed ของ CW-01 เท่าไหร่"`, `"CW-01 มี SKU อะไรบ้าง"`

All reads share one shape — the model picks a **read tool**, the backend queries the DB,
the model summarizes. Only the tool differs by what's asked:
`show_metric` / `get_telemetry_trend` / `get_telemetry_series` (metric value or trend),
`get_production_count` (piece counts), `get_skus` (SKU list), `list_dashboards`,
`get_machines` (machine list). Missing a machine → the clarify guard fires (no tool).

```mermaid
sequenceDiagram
    actor User
    participant UI as Vue UI
    participant BE as Backend (Chat)
    participant LLM as Groq LLM
    participant DB as TimescaleDB

    User->>UI: "speed ของ CW-01 เท่าไหร่" / "CW-01 มี SKU อะไรบ้าง" / "มี dashboard อะไรบ้าง"
    UI->>BE: POST /api/ai/chat (+ on-screen context)
    BE->>BE: classify → base prompt + tools
    alt required slot missing (e.g. no machine named)
        BE->>LLM: message + tools
        LLM-->>BE: one clarifying question, no tool
        BE-->>UI: reply (no DB)
    else slots present
        BE->>LLM: message + slim tools
        loop tool-calling (≤ 5 rounds, capped)
            LLM-->>BE: show_metric / get_telemetry_series / get_skus / list_dashboards
            BE->>DB: query machines / machine_fields / telemetry / skus / dashboards
            DB-->>BE: rows
            BE->>BE: compact result → tool message
            BE->>LLM: tool result appended
        end
        LLM-->>BE: 1–4 sentence summary
        BE-->>UI: reply + highlight widget
    end
    UI-->>User: shows reply
```

#### 2c. Change / Add / Delete widget  — `"เพิ่ม / ลบ / เปลี่ยน widget"`

"Change a widget" and "edit a widget setting" are the **same** operation here:
`preview_update_widget` patches any of `new_title` (rename), `metric`, `bucket`
(time bucket), `unit`, `min`, `max`, `start_date`/`end_date`, `sku`, `status`, `machine`,
`type` (`schema.go`). It **stages** the change — on a new preview *or* an open Active
dashboard — and nothing is written until the user clicks Confirm/Save (see "Preview vs
Active dashboard" below).

```mermaid
sequenceDiagram
    actor User
    participant UI as Vue UI
    participant BE as Backend (Chat)
    participant LLM as Groq LLM
    participant DB as TimescaleDB

    User->>UI: "เปลี่ยน metric เป็น temperature"
    UI->>BE: POST /api/ai/chat (+ preview / active-dashboard context)
    BE->>BE: classify → base + contextExt prompt
    BE->>LLM: message + tools
    LLM-->>BE: preview_update_widget / preview_add_widget / preview_remove_widget
    BE-->>UI: staged on the card (NO DB write yet)
    User->>UI: click Confirm (new) / Save (existing)
    UI->>BE: create_custom_dashboard (new)  ·  api.addWidget/updateWidget/deleteWidget (existing)
    BE->>DB: persist widgets
    BE-->>UI: open / refresh dashboard
```

**Preview vs Active dashboard.** 2c edits two targets, and **both stage first — nothing is
written to the DB until the user acts.** A **preview** is a new, unsaved plan; an **Active
dashboard** is an existing saved one the user opened in the AI page (card `kind: 'dashboard'`,
labelled `"Active dashboard"` — `AIAssistantPage.vue:207,495`). Chat edits to either use the
same `preview_*` staging tools; the only difference is the persistence action.

| | Preview (new, unsaved) | Active dashboard (existing, saved) |
|---|---|---|
| Card kind | `preview` | `dashboard` |
| Comes from | "สร้าง dashboard" / create flow | selected from the dashboard list into the AI page |
| AI edit tools (chat) | `preview_add_widget` / `preview_update_widget` / `preview_remove_widget` | same `preview_*` tools |
| Edit a widget's settings? | ✅ `preview_update_widget` | ✅ `preview_update_widget` |
| DB write | on **Confirm** → `create_custom_dashboard` | on **Save** → `saveDashboardCard` diffs via `api.addWidget/updateWidget/deleteWidget` |

There is **no immediate-write path** anymore: the old `add_widget_to_dashboard` /
`remove_widget` tools (which wrote on the spot) have been retired, so a chat request can
never mutate a saved dashboard before Save. If the dashboard the user names isn't the one
open on screen, the model asks them to open it first (`controller.go` `systemPromptBase`),
and writes nothing.

The card's **"+ Add widget" button** is the manual counterpart: it calls the frontend
`addPreviewWidget` (stages in memory) and also persists only on **Save** — same guarantee as
the chat path.

#### 2d. Answer from context — no tool, no DB  — `"@Speed Trend แนวโน้มเป็นยังไง"`

The difference from 2b is **not** "is a widget focused" — it is **"is the answer already
on screen."** The backend sets `answerFromContext` (`controller.go:374-382`) when either
`inlineData` (an analytical question whose focused chart's rendered series the UI inlined
as `"on-screen data"`) **or** `contextRead` (an `@`-focused plain current-value/config
question) holds. It then selects `systemPromptContextAnswer` — *"Do NOT call any tool"* —
and the model answers from the injected context, skipping the redundant fetch-then-
summarize round (a token + rate-limit optimization).

Guardrails: an `@`-focus alone does **not** force this path. If the focused question is an
**edit** (`editRe`) it goes to 2c; if it asks a **range/aggregate** (`rangeRe`) or **SKU**
(`skuRe`) not shown on screen, it falls back to the 2b tool path to fetch the data.

```mermaid
sequenceDiagram
    actor User
    participant UI as Vue UI
    participant BE as Backend (Chat)
    participant LLM as Groq LLM

    User->>UI: "@Speed Trend แนวโน้มเป็นยังไง"
    UI->>UI: read on-screen series from widgetViewStateStore
    UI->>BE: POST /api/ai/chat (context includes the rendered data)
    BE->>BE: answerFromContext = true → systemPromptContextAnswer, no tools
    BE->>LLM: message + injected on-screen data (no tools)
    LLM-->>BE: trend summary reasoned from context
    BE-->>UI: reply (no tool call, no DB read)
    UI-->>User: shows reply
```

---

## 3. AI Model Comparison

The model choice is **data-driven**: the repo contains a bake-off harness,
`eval_test.go` (`TestBakeOff`), that runs **21 Thai-first intent cases** (greeting /
reads / change-add-delete / slot-fill traps, including a custom-chart add) against the candidate Groq models and
auto-scores each model's **first decision** — the tool it picks, or `""` for a correct
no-tool reply (`got == want`). First-decision accuracy is the metric that matters here,
because "understand what the user wants" *is* the assistant's hard problem; the rest is
deterministic SQL. Run it with:

```
GROQ_API_KEY=… go test ./internal/modules/ai/ -run TestBakeOff -v -timeout 1800s
```

The harness sleeps **10 s between cases** and **120 s between models** so the shared
8k-tokens/min Groq budget recovers before each model starts; it also times every call and
prints per-model average latency. A full run is ~20 min.

### Candidates and results

Measured on the live Groq API (run of 2026-07-05, 21 cases, 10 s inter-case + 120 s inter-model
throttle; scoreboard printed by the harness). This run includes the custom-chart add case against
the enlarged widget schema — the `chart` type plus its `fields`/`chartType`/`points`/`scaling`
properties add ~240 tokens/prompt. **Rate-limited cases (⏳) are excluded from the denominator,
not counted as failures**, so sample sizes differ. Two headline results:
**custom-chart (#13) now passes on all three models**, but the **compound "create + what machines"
trap (#17) was missed by *both* gpt-oss models** — `20b` answered with a clarifying text (no tool),
`120b` called `preview_dashboard` with an empty machine — and only `qwen` routed it to
`get_machines`. The 120 s inter-model cooldown paid off: `120b` completed **all 21** cases (0 rate
limits); `qwen`'s ~3.3k-token prompts still made it the biggest 429 victim (8 of 21 lost).

| Model | Score | Completed / 21 | Avg prompt tok | Median latency | Verdict |
|-------|-------|----------------|----------------|----------------|---------|
| `qwen/qwen3-32b` | **13 / 13** | 13 (8 ⏳) | ~3,280 | ~0.9 s | **replaced** — heaviest tokens, most rate-limited (completed only 13) |
| `openai/gpt-oss-20b` | **18 / 19** | 19 (2 ⏳) | ~2,690 | ~0.7 s | **live** (`controller.go:23`) — cheapest + fastest; missed compound trap #17 |
| `openai/gpt-oss-120b` | **20 / 21** | 21 (0 ⏳) | ~2,700 | ~0.8 s | step-up; completed every case, but also missed compound trap #17 |

#### Full per-case results

`✅` = picked the expected first tool (or correctly answered with no tool); `❌` = wrong
decision (shown); `⏳` = case lost to a rate limit, excluded from the denominator. **Two `❌`
this run — both on the compound trap (#17):** `gpt-oss-20b` and `gpt-oss-120b`.

| # | Case | Expected | qwen3-32b | gpt-oss-20b | gpt-oss-120b |
|---|------|----------|-----------|-------------|--------------|
| 1 | greeting | (no tool) | ✅ | ✅ | ✅ |
| 2 | greeting-informal | (no tool) | ✅ | ✅ | ✅ |
| 3 | read-speed | `show_metric` | ⏳ | ✅ | ✅ |
| 4 | read-speed-thai | `show_metric` | ✅ | ✅ | ✅ |
| 5 | read-temp-informal | `show_metric` | ⏳ | ✅ | ✅ |
| 6 | english-read | `show_metric` | ✅ | ✅ | ✅ |
| 7 | detail-analytical-focused | `get_telemetry_series` | ✅ | ✅ | ✅ |
| 8 | change-preview-edit | `preview_update_widget` | ⏳ | ⏳ | ✅ |
| 9 | add-preview-widget | `preview_add_widget` | ✅ | ✅ | ✅ |
| 10 | delete-preview-widget | `preview_remove_widget` | ⏳ | ✅ | ✅ |
| 11 | add-to-active-dashboard | `preview_add_widget` | ✅ | ✅ | ✅ |
| 12 | remove-from-active-dashboard | `preview_remove_widget` | ✅ | ✅ | ✅ |
| 13 | add-custom-chart | `preview_add_widget` | ✅ | ✅ | ✅ |
| 14 | create | `preview_dashboard` | ⏳ | ⏳ | ✅ |
| 15 | list-dashboards | `list_dashboards` | ✅ | ✅ | ✅ |
| 16 | list-skus | `get_skus` | ⏳ | ✅ | ✅ |
| 17 | trap-action-but-read | `get_machines` | ✅ | ❌ | ❌ |
| 18 | ambiguous-fix | (no tool) | ⏳ | ✅ | ✅ |
| 19 | ambiguous-change | (no tool) | ✅ | ✅ | ✅ |
| 20 | read-no-machine | (no tool) | ⏳ | ✅ | ✅ |
| 21 | read-no-machine-en | (no tool) | ✅ | ✅ | ✅ |

Reading the results — three things stand out this run:
- **The compound trap (#17) is the genuinely hard case — both gpt-oss models missed it.** Asked
  "create a dashboard, and what machines are there now?", `20b` replied with a clarifying text and
  called **no tool**, while `120b` called `preview_dashboard` with an **empty** `machine` — neither
  answered the read half with `get_machines`. Only `qwen` got it right. Both misses are *safe* (no
  DB write — a preview stages nothing), but this is a real routing weakness on compound
  create-plus-read sentences across the whole gpt-oss family, not a one-model fluke.
- **The custom-chart case now passes on all three.** `add-custom-chart` (#13) routed to
  `preview_add_widget` with a valid multi-field spec (`type: chart`,
  `fields: ["speed","throughput"]`, `chartType`/`bucket`/`points`) on `qwen`, `20b`, and `120b` —
  the earlier `20b` `tool_choice` abort did not recur, so the enlarged chart schema is confirmed
  working on the live model.
- **The inter-model cooldown cut rate-limit noise.** With 120 s between models, `120b` completed
  all 21 cases (0 ⏳) and `20b` lost only 2; `qwen` still lost 8, because its ~3.3k-token prompts
  burn the shared 8k/min budget fastest.

Net: no *dangerous* actions (no wrong writes/previews) from any model. The clean separator is the
compound trap (#17) — only `qwen` handled it, and `qwen` is the model being retired for its token
cost and rate-limit fragility. Among the gpt-oss pair, `120b` completed every case and `20b` is
cheapest and fastest; neither is reliable on #17 yet.

### The requested axes

- **Quality** — the differentiator is not whether a model *can* call functions (all three
  can) but whether it picks the *correct* tool on a Thai sentence the first time. This run
  `qwen` scored **13/13**, `120b` **20/21**, and `20b` **18/19** — and the only case any model
  got wrong was the compound "create + read" trap (#17), which **both** gpt-oss models missed
  (`20b` → text, `120b` → `preview_dashboard` with empty machine) and only `qwen` handled. Every
  other completed case, including the custom-chart add (#13), routed correctly on all three. So
  raw accuracy separates the models only on that one compound case — and it favors the model being
  retired, not either gpt-oss candidate.
- **Cost** — `gpt-oss-20b` has the smallest prompts (~2,690 vs ~3,280 tokens for qwen) and
  is the cheapest per token; because the large `base` prompt prefix stays byte-stable, Groq's
  prompt cache is reused across turns, cutting effective input cost further. Adding the `chart`
  tool schema lifted every prompt ~240 tokens vs the prior run, but the ranking is unchanged.
  Qwen's larger prompts also made it the biggest free-tier rate-limit victim in the run.
- **Latency** — the harness now times each `callGroqModel`. **Typical (median) first-decision
  latency is ~0.7 s for `20b`, ~0.8 s for `120b`, ~0.9 s for `qwen`** — all fast for a single-turn
  tool decision, and `20b` is the quickest. The per-model *mean* is higher (2.8 s / 3.8 s / 3.9 s)
  only because a handful of calls absorbed a **429 retry/backoff** inside the timed window (those
  outliers run 7–17 s); they are retry waits, not model compute. Calls are non-streaming with a
  90 s client timeout and a 3× 429 retry (`parseRetryAfter`). To stay under Groq's 8k-tokens/min
  limit the backend replays only the **last 3 turns**, sends **slim tool schemas** (name +
  description for simple tools, full schema only for widget-nested ones), and caps tool rounds —
  all of which also reduce per-request latency.
- **Context window** — the Groq `gpt-oss` family offers a long context, but the design
  deliberately does **not** rely on it: history is trimmed to 3 turns and past tool
  payloads are not replayed (the assistant's prior summary already captured them), so the
  effective prompt stays small and cache-friendly.
- **Tool calling** — all candidates support OpenAI-compatible function calling, which is
  why the OpenAI-compatible Groq endpoint is used unchanged.

### Decision (applied)

The decision stands after this run (2026-07-05), with one known weakness:

- The live constant (`controller.go:23`) runs **`openai/gpt-oss-20b`**. With the direct-write
  tools retired, the case for `20b` rests on **cost, cache-friendliness, and speed** — cheapest
  per token, prompt-cache-friendly, and the fastest median first-decision latency (~0.7 s). It is
  not flawless: it missed the compound trap (#17) this run — but so did `120b`, so paying for the
  larger model would **not** buy reliability on that case. Every other completed case routed
  correctly on `20b`.
- **Known weakness: the compound "create + read" trap (#17).** No gpt-oss model handled it this
  run — `20b` asked a clarifying question (safe: no tool), `120b` staged an empty-machine
  `preview_dashboard`. Both are non-destructive (a preview writes nothing), but if this pattern
  matters, the fix is a prompt/routing rule, not a bigger model.
- The "ask the user to open the dashboard" rule in `systemPromptBase` is **scoped** to fire only
  when no preview/Active-dashboard context is on screen. The preview-edit cases (#9, #11, #12) all
  routed correctly on `20b` this run, so that scoping holds.
- The custom-chart capability (new `chart` widget type across `schema.go` /
  `dashboard_action.go` and the frontend preview pipeline) is exercised by the `add-custom-chart`
  case (#13) and now passes on **all three** models, including the live `20b` — the enlarged chart
  schema routes and populates correctly.

> Reproduce: `GROQ_API_KEY=… go test ./internal/modules/ai/ -run TestBakeOff -v -timeout 1800s`
> (~20 min). The harness sleeps 10 s between cases and 120 s between models, times each call, and
> prints a per-model scoreboard with average latency. Because it hits the live free-tier API, some
> cases can still be lost to rate limits on any given run (they are skipped, not failed) — for a
> clean denominator, re-run or raise the inter-case `time.Sleep`.
> Exact model spec numbers (parameter counts, per-token pricing, hard context-window token
> limits) are per Groq's published docs; the ranking here rests on this in-repo bake-off.
