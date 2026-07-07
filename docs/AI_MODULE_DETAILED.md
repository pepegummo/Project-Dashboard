# AI Module — Detailed Documentation

Covers the full AI assistant stack: `backend/internal/modules/ai/` (Go) and `frontend/src/pages/AIAssistantPage.vue` (Vue 3). Every claim is anchored to `file:line` as of this writing.

Sections: [Architecture](#1-ai-module-architecture) · [Prompt strategy](#2-prompt-strategy) · [Chat request flow](#3-chat-request-flow) · [Tool execution pipeline](#4-tool-execution-pipeline) · [Dashboard preview/apply flow](#5-dashboard-previewapply-flow) · [Conversation persistence](#6-conversation-persistence-erd) · [Tool catalog](#7-tool-catalog-reference) · [API endpoints](#8-api-endpoint-reference)

---

## 1. AI module architecture

> **What this diagram is for:** a component map — which pieces exist (frontend page, backend controller, toolkit, Groq, DB) and which talks to which. Use it to orient yourself before diving into any flow below.

```mermaid
flowchart LR
    subgraph FE["Frontend (Vue 3)"]
        PAGE["AIAssistantPage.vue<br/>chat UI, @mentions, canvas cards"]
        PREVIEW["PreviewCanvasCard /<br/>CreatedCanvasCard"]
        API["api.service.ts<br/>(Axios, JWT header)"]
        PAGE --> PREVIEW
        PAGE --> API
    end

    subgraph BE["Backend (Go / Fiber) — internal/modules/ai"]
        ROUTES["routes.go<br/>all routes behind middleware.Authenticate"]
        CTRL["controller.go<br/>Chat loop, dispatch, prompt selection"]
        TK["tool_actions.go — ToolKit<br/>read tools, result compaction"]
        DA["dashboard_action.go — DashboardAction<br/>preview + create"]
        REPO["repository.go<br/>conversations, messages, drafts"]
        ROUTES --> CTRL
        CTRL --> TK
        CTRL --> DA
        CTRL --> REPO
    end

    subgraph SVC["Reused domain services"]
        TEL["telemetry.Service"]
        AL["alerts.Service"]
        DASH["dashboards.Service"]
    end

    GROQ["Groq API<br/>openai/gpt-oss-20b"]
    DB[("TimescaleDB<br/>ai_* + domain tables")]

    API -- "POST /api/ai/chat<br/>+ conversations, drafts, tools/execute" --> ROUTES
    CTRL -- "chat completions,<br/>tool-call loop" --> GROQ
    TK --> TEL & AL & DASH
    DA --> DASH
    TEL & AL & DASH --> DB
    TK & DA -- "raw SQL<br/>(resolveMachineID, fields)" --> DB
    REPO --> DB
```

Key structural facts:

- **Entry point**: `routes.go:9-27` registers everything under `/api/ai` with JWT auth (`middleware.Authenticate`, `routes.go:11`). The controller is a single instance wiring `DashboardAction`, `ToolKit`, and `Repository` (`controller.go:127-139`).
- **ToolKit reuses domain services** rather than duplicating logic — every AI read inherits the same org-scoping and validation the REST API enforces (`tool_actions.go:17-33`).
- **Model choice**: `openai/gpt-oss-20b`, pinned after a bake-off (23/23 eval pass, ~0.83 s median, smallest prompts; see `controller.go:20-24` and `eval_test.go`).
- **The LLM never writes to the DB.** The only mutating tool, `create_custom_dashboard`, is excluded from the toolset sent to Groq — only the frontend can call it, via `POST /ai/tools/execute` after the user clicks Confirm (`controller.go:179-183`).

---

## 2. Prompt strategy

> **What this diagram is for:** the decision tree that picks which system prompt and which tools each chat message pays for. Use it to understand (or tune) token costs and why a given message did / didn't call a tool.

The module maintains **four system prompts**, chosen per message to keep token cost proportional to what the message actually needs:

| Prompt | Defined at | Sent when | Contents |
|---|---|---|---|
| `systemPromptMinimal` | `controller.go:31` | Message doesn't look tool-worthy (greeting, chit-chat) | Identity + language rule only (~300 tokens cheaper than base) |
| `systemPromptContextAnswer` | `controller.go:38` | The on-screen widget context already answers the question | "Answer from `[FOCUSED]` context, do NOT call tools, describe series shape" |
| `systemPromptBase` | `controller.go:42` | Any tool-worthy message | TOOL SELECTION + SLOT FILLING + WIDGET TYPES rules. Kept byte-stable so Groq prompt-caches it |
| `systemPromptBase + systemPromptContextExt` | `controller.go:65` | Tool-worthy **and** a dashboard/preview is on screen | Adds preview-editing rules and `@WidgetTitle` routing rules |

```mermaid
flowchart TD
    MSG["Incoming message<br/>(POST /api/ai/chat)"] --> NT{"needsTools regex?<br/>controller.go:698<br/>(@, metric words, EN+TH verbs)"}
    NT -- no --> MIN["systemPromptMinimal<br/>tools = nil<br/>controller.go:388-389, 410-412"]
    NT -- yes --> ILD{"context contains<br/>'on-screen data'?<br/>controller.go:375"}
    ILD -- yes --> CTXA["systemPromptContextAnswer<br/>tools = nil (answer in one call)"]
    ILD -- no --> FOC{"@mention present AND not<br/>edit / range / sku intent?<br/>controller.go:380-381"}
    FOC -- yes --> CTXA
    FOC -- no --> HASCTX{"dashboard/preview<br/>context present?"}
    HASCTX -- yes --> EXT["systemPromptBase + ContextExt<br/>+ context injected AFTER history<br/>controller.go:393, 400-403"]
    HASCTX -- no --> BASE["systemPromptBase"]
    EXT --> TOOLS["buildGroqTools(role, hasContext)<br/>controller.go:572"]
    BASE --> TOOLS
    TOOLS --> FORCE{"@mention focused?<br/>controller.go:419-422"}
    FORCE -- yes --> REQ["tool_choice = required<br/>(first call only)"]
    FORCE -- no --> AUTO["tool_choice = auto"]
```

Token-saving mechanisms beyond prompt selection:

- **Slim tool schemas** — simple tools are sent as name + description only (arg hints embedded in the description, `parameters` = open object), saving ~50–80 tokens per tool (`controller.go:523-552`). The three `preview_*` widget tools keep full schemas because they have nested objects the model must see (`complexSchemaTools`, `controller.go:556-560`).
- **Context-gated tools** — `preview_add/remove/update_widget` are omitted entirely when no dashboard context is on screen (`previewOnlyTools`, `controller.go:564-568, 580-582`). Write tools are omitted for viewer role (`controller.go:577-579`).
- **History cap** — only the last **3** user/assistant text rows are replayed; past tool calls/results are never reconstructed (the assistant's reply already summarizes them) (`buildGroqMessages`, `controller.go:719-750`).
- **Recency trick** — the authoritative dashboard state is injected as a system message *after* history, so the current widget config beats any stale earlier turn (`controller.go:398-403`).
- **Compact tool results** — series/count results are reshaped from object-per-point to `columns` + `[time, values…]` tuples before being fed back to the model (`tool_actions.go:318-409`); timestamps are rendered in fixed +07 plant-local time (`bkkZone`, `tool_actions.go:329`).

---

## 3. Chat request flow

> **What this diagram is for:** the time-ordered message exchange for one chat turn — who calls whom, in what order, including the agentic tool loop and every retry/failure path. Use it when debugging a bad answer or a 502.

```mermaid
sequenceDiagram
    autonumber
    participant U as Browser<br/>(AIAssistantPage.vue)
    participant B as Backend<br/>Chat (controller.go:331)
    participant R as Repository
    participant G as Groq API
    participant T as dispatch → ToolKit /<br/>DashboardAction

    U->>B: POST /api/ai/chat<br/>{conversationId, message, context}
    Note over B: 503 if GROQ_API_KEY empty (:332)<br/>400 if conversationId/message missing (:344)
    B->>R: AddMessage(role=user) (:351)
    B->>R: GetMessages — last 20 DESC (:358)
    Note over B: pick system prompt + tools + tool_choice (see §2)<br/>then append context after history (:400)

    loop up to 5 rounds (controller.go:425)
        B->>G: chat/completions (callGroq :430)
        alt "Tool choice is none" error (:434)
            B->>G: retry with full toolset
        else "Tool choice is required" but model answered in text (:441)
            B->>G: retry with tool_choice auto
        else HTTP 429 (:643)
            Note over B,G: sleep per Retry-After<br/>(parseRetryAfter, ≤8s) — 3 attempts max
        else function-call parse failure (:655)
            B->>G: retry once WITHOUT tools → plain-text reply
        end
        alt Groq error / no choices
            B-->>U: 502 AI_ERROR (:446, :449)
        else finish_reason ≠ tool_calls (:454)
            B->>R: AddMessage(role=assistant, text)
            Note over B: break — final answer
        else tool_calls present
            loop each tool call (:469)
                B->>T: dispatch(toolName, args) (:472)
                Note over T: error → {"error": …} fed back<br/>to the model, not the user (:474)
                B->>R: AddMessage(role=tool,<br/>toolName+input+result) (:479)
                B->>B: append tool result to msgs (:487)
            end
            Note over B: round cap — focused msg: 0 extra rounds,<br/>else 1 — then callTools=nil forces<br/>a text summary (:498-504)
        end
    end
    B-->>U: {success, data: newMessages[]} (:507)
    Note over U: render text + toolResult cards<br/>(preview/widget/highlight handlers,<br/>AIAssistantPage.vue:693-744)
```

Notes:

- The 5-iteration outer loop is the hard stop; the *round cap* (`controller.go:498-504`) is the practical one — a focused-widget message gets exactly 1 tool round because each round re-sends the ~3k-token context and would trip Groq's 8k tokens/min limit.
- Every Groq HTTP attempt has a 90 s client timeout (`controller.go:621`). Rate-limit sleeps are excluded from latency measurements (`controller.go:634-641`).
- The frontend appends `@Widget Title` mention tokens to the outgoing message and builds the `context` string itself (widget list, `[FOCUSED]` marker, current values, shown window, and — for analytical questions — the full `on-screen data` series) (`AIAssistantPage.vue:491-610`).

---

## 4. Tool execution pipeline

> **What this diagram is for:** the control flow inside a single tool call — role gate, name routing, and the branchy `show_metric` resolution. Use it to trace why a tool returned a fallback, an error, or a particular widget type.

```mermaid
flowchart TD
    IN["dispatch(toolName, rawArgs)<br/>controller.go:141"] --> ROLE{"isWriteTool AND role is<br/>viewer? (:146)"}
    ROLE -- yes --> DENY["error: permission denied<br/>(fed back to LLM as tool error)"]
    ROLE -- no --> SW{"switch toolName (:150)"}

    SW -- "get_machines" --> GM["getMachinesForOrg<br/>dashboard_action.go:534<br/>org-scoped SQL: machines + numeric field keys"]
    SW -- "show_metric" --> SM["ToolKit.ShowMetric<br/>tool_actions.go:41"]
    SW -- "get_telemetry_trend /<br/>get_telemetry_series /<br/>get_production_count /<br/>get_skus / get_active_alerts" --> RD["ToolKit reads →<br/>telemetry / alerts services<br/>+ compactSeriesResult /<br/>compactBucketResult (:347, :394)"]
    SW -- "list_dashboards" --> LD["ToolKit.ListDashboards<br/>tool_actions.go:415"]
    SW -- "preview_dashboard /<br/>preview_add_widget /<br/>preview_update_widget" --> PV["DashboardAction —<br/>validate only, NO DB write<br/>(see §5)"]
    SW -- "preview_remove_widget" --> PRW["echo {removed, widgetTitle}<br/>controller.go:171-176<br/>(frontend applies it)"]
    SW -- "create_custom_dashboard" --> CCD["DashboardAction.Handle —<br/>the ONLY DB write<br/>(frontend-only, :179-183)"]
    SW -- "unknown" --> UNK["error: unknown tool (:185)"]

    SM --> MRES{"resolveMachineID —<br/>fuzzy LIKE, exact-first<br/>dashboard_action.go:565"}
    MRES -- not found --> MERR["error: machine not found"]
    MRES -- found --> CNT{"metric is count /<br/>daily-count / counter? (:60)"}
    CNT -- yes --> DCW["daily-count widget; bucket/sku/status<br/>copied from the machine's most recent<br/>daily-count widget config, else 1h/all (:65-99)"]
    CNT -- no --> FLD{"metric exists in<br/>machine_fields? (:105)"}
    FLD -- no --> FB["fallback: one widget per available<br/>numeric field (gauge if min+max else kpi),<br/>status fields collapsed to 1 kpi (:111-164)"]
    FLD -- yes --> VIZ{"viz arg? (:168)"}
    VIZ -- trend --> LC["line-chart"]
    VIZ -- gauge --> GA["gauge"]
    VIZ -- "value / none" --> KPI["kpi-card<br/>(gauge if field has min+max)"]
    LC & GA & KPI --> OUT["widget + companion trend widget (:195-203)<br/>→ UI renders directly, no frontend guessing"]
```

- Tool errors never abort the chat: `dispatch` errors are marshalled as `{"error": …}` and returned to the model as the tool result (`controller.go:474-476`), letting it apologize or retry with better args.
- `resolveMachineID` / `resolveDashboardID` do case-insensitive substring matches with exact matches ranked first, so "Overview" can't shadow "CW-01 Overview" (`tool_actions.go:439-453`, `dashboard_action.go:565-583`).

---

## 5. Dashboard preview/apply flow

> **What this diagram is for:** the lifecycle of the preview canvas — every state a dashboard plan passes through from first request to persisted dashboard, and which actor (LLM, frontend, user) drives each transition. Use it to understand why nothing is saved until Confirm/Save.

```mermaid
stateDiagram-v2
    [*] --> NoCanvas

    NoCanvas --> PreviewStaged : LLM calls preview_dashboard<br/>(template expanded server-side,<br/>dashboard_action.go:406-450 — no DB write)
    NoCanvas --> ActiveDashboard : user opens an existing dashboard<br/>PUT /ai/selected-dashboard<br/>(repository.go:134 — clears any draft)

    state PreviewStaged {
        [*] --> Editing
        Editing --> Editing : preview_add_widget (validated, :274)<br/>preview_update_widget (changes echoed, :315)<br/>preview_remove_widget (title echoed)<br/>+ manual drags/edits — frontend mutates<br/>canvasCards + undo/redo snapshots
    }

    note right of PreviewStaged
        Draft auto-persisted per user:
        PUT /ai/preview-draft on each mutation
        (AIAssistantPage.vue:265-277 →
        ai_preview_drafts, repository.go:116)
        Survives refresh via GET /ai/preview-draft.
        One row per user: preview data XOR dashboard_id.
    end note

    PreviewStaged --> Created : user clicks Confirm →<br/>POST /ai/tools/execute create_custom_dashboard<br/>(AIAssistantPage.vue:952 → Handle,<br/>dashboard_action.go:129)
    PreviewStaged --> NoCanvas : user discards →<br/>DELETE /ai/preview-draft (repository.go:167)

    ActiveDashboard --> ActiveDashboard : preview_* tools stage edits<br/>on the open dashboard (in memory)
    ActiveDashboard --> Saved : user clicks Save<br/>(dashboard REST API persists widgets)

    Created --> [*] : dashboard + widgets written<br/>(CreateDashboard + AddWidget per widget,<br/>widgets from preview plan incl. user-added,<br/>layout from grid or flowLayout :454)
    Saved --> [*]
```

Enforcement of preview-then-confirm:

1. `create_custom_dashboard` is **not** in `AllTools()` (`schema.go:194-209`) — the LLM cannot invoke it; the system prompt additionally forbids it (`controller.go:48`).
2. It is the only entry in `writeTools` (`schema.go:214-218`), so even via `POST /ai/tools/execute` it requires admin/editor role (`controller.go:146`).
3. `Handle` honors the (possibly user-edited) preview plan: custom widget list with per-widget config — absolute date windows switch line-charts to `liveMode:false` (`dashboard_action.go:167-180`), count widgets carry bucket/sku/status, chart widgets carry fields/chartType/points/scaling — falling back to template expansion when no widget list is passed (`dashboard_action.go:146-263`).

---

## 6. Conversation persistence (ERD)

> **What this diagram is for:** the AI module's own tables and how they hang off `users`. Use it when querying chat history or debugging draft-restore behavior.

```mermaid
erDiagram
    users ||--o{ ai_conversations : "owns"
    ai_conversations ||--o{ ai_messages : "contains"
    users ||--o| ai_preview_drafts : "has at most one"

    ai_conversations {
        uuid id PK
        uuid user_id FK "ON DELETE CASCADE"
        text title "default 'New Conversation'"
        jsonb context "default {}"
        timestamptz created_at
        timestamptz updated_at "bumped on every AddMessage"
    }

    ai_messages {
        uuid id PK
        uuid conversation_id FK "ON DELETE CASCADE"
        text role "user | assistant | tool"
        text content
        text tool_name "nullable"
        jsonb tool_input "nullable"
        jsonb tool_result "nullable — rendered as widget/preview cards"
        integer tokens "nullable, unused"
        timestamptz created_at
    }

    ai_preview_drafts {
        uuid user_id PK "FK users, ON DELETE CASCADE"
        uuid conversation_id "set for preview, NULL for dashboard"
        uuid dashboard_id "set for selected dashboard, NULL for preview"
        jsonb data "the preview canvas state"
        timestamptz updated_at
    }
```

- Schema defined in `migrate/migrate.go:211-245`; indexes `idx_aiconv_user (user_id, updated_at DESC)` and `idx_aimsg_conv (conversation_id, created_at ASC)` at `migrate.go:349-350`.
- `ai_preview_drafts` is a one-row-per-user *view state*: either an in-progress preview (`data` set) **or** a selected dashboard (`dashboard_id` set) — each upsert clears the other side (`repository.go:116-145`).
- `GetMessages` reads newest-first with `LIMIT 20` (`repository.go:72-78`); the chat loop then replays only the last 3 text rows to Groq (§2).

---

## 7. Tool catalog reference

All tools handed to the LLM (`AllTools()`, `schema.go:194-209`) plus the one frontend-only tool. **Schema form**: *slim* = name + description only (`controller.go:533`); *full* = complete JSON schema.

| Tool | Kind | Schema form | Args (required in bold) | Returns |
|---|---|---|---|---|
| `get_machines` | read | slim | — | machines with name/type/status + numeric field keys |
| `show_metric` | read | slim | **machine**, **metric**, viz (`value\|gauge\|trend`) | widget spec(s) the UI renders directly; fallback widget list if metric unknown |
| `get_telemetry_trend` | read | slim | **machine_id**, **metric**, time_range (5m…30d) | avg/min/max aggregate |
| `get_telemetry_series` | read | slim | **machine_id**, **metric**, time_range | compact `columns` + `[time,avg,min,max]` rows |
| `get_active_alerts` | read | slim | — | open alert events (event_id, machine, metric, value, severity…) |
| `get_production_count` | read | slim | **machine_id**, **bucket**, points (≤500), sku, status | compact `[time,count]` rows |
| `get_skus` | read | slim | **machine_id** | distinct SKU values for the machine |
| `list_dashboards` | read | slim | — | dashboard names + widget counts + URLs |
| `preview_dashboard` | stage | slim | **machine**, **template** (`machine_overview\|machine_production\|machine_maintenance`) | `PreviewDashboardResult` — plan only, no DB write |
| `preview_add_widget` | stage | full | **machine**, **widget** (nested `widgetItemSchema`, `schema.go:15`) | validated `PreviewWidget` |
| `preview_remove_widget` | stage | full | **widget_title** | echo — frontend removes it |
| `preview_update_widget` | stage | full | **widget_title** + any patch fields (metric, bucket, sku, dates, fields, chartType, scaling…) | `{widgetTitle, changes}` — frontend applies |
| `create_custom_dashboard` | **write** | *not sent to LLM* | machine, template, name, widgets[] | creates dashboard + widgets; admin/editor only |

Context-gated: the three `preview_*` widget tools are omitted when no dashboard/preview is on screen. Allowed widget types: `line-chart, gauge, kpi-card, status-card, table, alarm-panel, daily-count, chart` (`schema.go:3-5`).

---

## 8. API endpoint reference

All routes from `routes.go:9-27`, prefixed `/api/ai`, JWT required (Fiber `Locals`: userId/orgId/role).

| Method | Path | Handler | Purpose |
|---|---|---|---|
| POST | `/chat` | `Chat` (`controller.go:331`) | Main chat turn — runs the Groq tool loop, returns all new messages |
| GET | `/tools` | `ListTools` | Full tool definitions (`AllTools()`) |
| POST | `/tools/execute` | `ExecuteTool` (`controller.go:191`) | Direct tool call `{toolName, params}` — how the frontend fires `create_custom_dashboard` on Confirm |
| GET | `/conversations` | `GetConversations` | Current user's conversations with message counts |
| POST | `/conversations` | `CreateConversation` | New conversation (default title "New Conversation") |
| GET | `/conversations/:id/messages` | `GetMessages` | Last 20 messages, newest first |
| POST | `/conversations/:id/messages` | `AddMessage` | Append a message row (used by the frontend for local events) |
| GET | `/preview-draft` | `GetPreviewDraft` | Restore the user's saved canvas state after refresh |
| PUT | `/preview-draft` | `PutPreviewDraft` | Persist the in-progress preview `{conversationId, data}` |
| DELETE | `/preview-draft` | `DeletePreviewDraft` | Discard the draft |
| PUT | `/selected-dashboard` | `PutSelectedDashboard` | Remember which existing dashboard is open on the AI page |

Error envelope: failures return the app-wide `middleware.NewAppError` shape; notable codes — `503 AI_UNAVAILABLE` (no `GROQ_API_KEY`), `400 VALIDATION_ERROR`, `502 AI_ERROR` (Groq failure after retries).
