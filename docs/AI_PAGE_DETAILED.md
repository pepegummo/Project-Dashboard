# IotVision — AI Page (very detailed)

Scope: the AI assistant page end-to-end — `frontend/src/pages/AIAssistantPage.vue` plus the
backend module it talks to (`backend/internal/modules/ai/`). Companion docs:
[`AI_ARCHITECTURE.md`](./AI_ARCHITECTURE.md) (design rationale, model bake-off) and
[`AI_FLOW_DETAILED.md`](./AI_FLOW_DETAILED.md) (backend `Chat` handler internals). This doc is
anchored on the **page**: what the browser holds, sends, and does with each tool result.

Sections: [Architecture](#1-system-architecture) · [Flowchart](#2-flowchart--sendmessage-pipeline-frontend) ·
[Sequence](#3-sequence-diagram--one-chat-turn--confirm) · [API](#4-api-reference--apiai) ·
[ERD](#5-data-model--erd) · [State](#6-state-diagram--canvas--persisted-draft)

---

## 1. System architecture

Components and who calls whom. The page never talks to Groq or the DB directly — everything
funnels through `/api/ai/*` (Fiber, JWT-authenticated at `routes.go:11`).

```mermaid
flowchart LR
    subgraph FE["🌐 Frontend"]
        PAGE["AIAssistantPage.vue<br/>canvasCards · mentions · undo/redo<br/>buildDashboardContext():494"]
        PCC["PreviewCanvasCard.vue<br/>(variants: preview / focus / dashboard)"]
        GSC["GridStackCanvas.vue<br/>live widget grid"]
        UH["useUndoHistory<br/>snapshot stack"]
        STORES["Pinia stores<br/>dashboard · machine · telemetry<br/>widget-view-state"]
        APISVC["api.service.ts<br/>Axios · JWT header"]
        PAGE --> PCC
        PAGE --> GSC
        PAGE --> UH
        PAGE --> STORES
        PAGE --> APISVC
    end

    subgraph BE["⚙️ Backend · internal/modules/ai"]
        RT["routes.go<br/>11 routes under /api/ai"]
        MW["middleware.Authenticate<br/>JWT → userId/orgId/role"]
        CTRL["controller.go<br/>Chat:331 · dispatch:141<br/>ExecuteTool:191 · draft handlers:276"]
        SCH["schema.go<br/>AllTools():194 · writeTools:214"]
        TK["tool_actions.go<br/>ToolKit — read tools"]
        DA["dashboard_action.go<br/>preview_* staging ·<br/>create_custom_dashboard"]
        REPO["repository.go<br/>conversations · messages ·<br/>preview drafts"]
        RT --> MW --> CTRL
        CTRL --> SCH
        CTRL --> TK
        CTRL --> DA
        CTRL --> REPO
    end

    GROQ["🧠 Groq API<br/>openai/gpt-oss-20b<br/>controller.go:24"]
    DB[("TimescaleDB<br/>telemetry_raw · dashboards ·<br/>ai_conversations · ai_messages ·<br/>ai_preview_drafts")]

    APISVC -->|"REST /api/ai/*"| RT
    CTRL -->|"POST chat/completions<br/>90s timeout · 3× 429 retry<br/>controller.go:621-648"| GROQ
    TK --> DB
    DA --> DB
    REPO --> DB
```

Key boundary rules:

- **Role gate** — `dispatch` (`controller.go:146`) rejects write tools for viewers; the only
  write tool is `create_custom_dashboard` (`schema.go:214`). It is deliberately **excluded from
  `AllTools()`** so the LLM can never call it — only the frontend does, via
  `POST /ai/tools/execute` after the user clicks Confirm (`controller.go:179-183`,
  `AIAssistantPage.vue:952`).
- **Token diet** — simple tools go to Groq slim (name + description only,
  `toGroqToolSlim` `controller.go:533`); only the three `preview_*` widget tools keep full
  schemas (`complexSchemaTools` `controller.go:556`), and they're hidden entirely when no
  dashboard context is on screen (`previewOnlyTools` `controller.go:564`).

---

## 2. Flowchart — `sendMessage` pipeline (frontend)

The backend half of this pipe (classify → prompt select → tool loop) is drawn in
[`AI_FLOW_DETAILED.md`](./AI_FLOW_DETAILED.md); here it's one box. This chart covers what the
**page** does before and after — `sendMessage` (`AIAssistantPage.vue:610-929`).

```mermaid
flowchart TB
    ST([User hits Enter]) --> G0{"text or mentions?<br/>not processing?<br/>:611-612"}
    G0 -- no --> EN0([ignore])
    G0 -- yes --> MEN["append @Title per mentioned widget<br/>snapshot + clear mention state<br/>:613-621"]
    MEN --> CONV{"conversationId set?"}
    CONV -- no --> NEWC["POST /ai/conversations<br/>:625-628"] --> CTX
    CONV -- yes --> CTX["buildDashboardContext(text, focusedIds):494<br/>widget lines + current values + bucket/sku/status<br/>[FOCUSED] marks clicked widgets<br/>analytical Q → inject stride-sampled series (~24 pts) :505-514<br/>focused chart → shown time window :517-522"]
    CTX --> CHAT["POST /api/ai/chat<br/>{conversationId, message, context}<br/>:630"]
    CHAT --> BE["backend Chat handler<br/>classify → prompt → Groq tool loop ≤5<br/>controller.go:331-508<br/>(see AI_FLOW_DETAILED.md)"]
    BE --> MSGS["messages[] returned<br/>(user + tool + assistant rows)"]

    MSGS --> TOAST["assistant text → showToast :632-635"]
    TOAST --> H1{"per tool message"}
    H1 -- "show_metric :636-692" --> SM{"widget for this<br/>machine+metric exists?"}
    SM -- "on live grid" --> HLL["highlight live widget<br/>setAiHighlight(...)"]
    SM -- "on focus card" --> HLF["highlight focus widget"]
    SM -- "preview open" --> HLP["highlight matching preview widget<br/>(substring/uuid machine match) :664-678"]
    SM -- "nowhere" --> FC["create/append focus card<br/>:679-690"]
    H1 -- "preview_dashboard :693-704" --> PD["replace or create preview card"]
    H1 -- "preview_add_widget :705-711" --> PA["push widget into preview card"]
    H1 -- "preview_remove_widget :712-723" --> PR{"title found?<br/>fuzzy match :977"}
    PR -- yes --> PRX["splice widget out"]
    PR -- no --> PRT["toast: not found"]
    H1 -- "preview_update_widget :724-746" --> PU["merge changes<br/>canonicalize SKU casing :733-738<br/>flash highlight"]

    PU --> CLR
    PD --> CLR
    PA --> CLR
    PRX --> CLR
    HLL --> CLR
    CLR["no show_metric this turn →<br/>drop ephemeral focus cards :750-752"]
    CLR --> BT{"builder tool used?<br/>:755-761"}
    BT -- yes --> FPV
    BT -- no --> HC["highlight cascade :763-907<br/>1· read-tool machine_id+metric match<br/>2· text-scan: metric/title words, then bare numbers<br/>vs live values (±1), then machine name<br/>3· fallback: the [FOCUSED] widget itself"]
    HC --> FPV{"nothing highlighted,<br/>no preview, no focus card?<br/>:917-918"}
    FPV -- yes --> DFW["deriveFocusWidget(messages, text):381<br/>resolve machine+field from tool input or text<br/>→ ephemeral focus card"]
    FPV -- no --> FIN
    DFW --> FIN["processing=false · scroll :926-928"]
    FIN --> EN([End])
```

Around the pipe, three `watch`ers on `canvasCards` keep state durable
(`AIAssistantPage.vue:268-282`):

- **Draft persistence** — any preview/focus card change is `PUT` to `/ai/preview-draft`
  (per-user, survives refresh; restored in `onMounted` `:240-263`).
- **Undo history** — every mutation pushes a JSON snapshot onto `useUndoHistory`
  (`:113-141`); applied DB writes reset the stack (`resetHistory` `:137`).
- The `restoring` flag (`:111`) stops the mount-time restore from echoing itself back as a
  save or a history entry.

---

## 3. Sequence diagram — one chat turn + Confirm

Full round trip including the staged-preview confirm. Error paths shown as alts.

```mermaid
sequenceDiagram
    autonumber
    actor U as User
    participant P as AIAssistantPage.vue
    participant A as api.service.ts
    participant M as auth middleware
    participant C as ai.Controller
    participant R as Repository
    participant G as Groq API
    participant D as TimescaleDB

    U->>P: type message (+ click widgets to @mention)
    P->>P: buildDashboardContext(text, focusedIds) :494
    P->>A: chat(conversationId, text, context)
    A->>M: POST /api/ai/chat (Bearer JWT)
    alt invalid JWT
        M-->>P: 401
    else GROQ_API_KEY empty (controller.go:332)
        M->>C: Chat
        C-->>P: 503 AI_UNAVAILABLE
    end
    M->>C: Chat (userId/orgId/role in Locals)
    C->>R: AddMessage(role=user) :351
    R->>D: INSERT ai_messages
    C->>R: GetMessages (DESC, LIMIT 20) :358
    C->>C: classify — needsTools :369, inlineData :375,<br/>contextRead :381, answerFromContext :383
    C->>C: pick prompt :387-394 · build tools :405<br/>(nil if no-tool path :410) · tool_choice required if @focus :419

    loop up to 5 rounds (controller.go:425)
        C->>G: POST chat/completions (90s timeout)
        alt HTTP 429
            G-->>C: 429 + Retry-After
            C->>C: sleep ≤8s, retry ≤3× :643-648
        else function-call parse error
            G-->>C: error body :658
            C->>G: retry once without tools :661
        end
        G-->>C: choice (finish_reason)
        alt finish_reason = tool_calls
            C->>C: dispatch(tool, args) :141 — role gate :146
            C->>D: org-scoped SQL (read tools)<br/>or stage preview patch (no write)
            C->>R: AddMessage(role=tool, input, result) :479
            R->>D: INSERT ai_messages
            Note over C: after round cap (1, or 0 when focused)<br/>tools = nil → forces text summary :498-504
        else finish_reason = stop
            C->>R: AddMessage(role=assistant, text) :459
            R->>D: INSERT ai_messages
        end
    end
    C-->>P: { data: newMessages[] }

    P->>P: toast assistant text · apply tool handlers<br/>(preview mutations, highlights) :632-746
    P->>A: putPreviewDraft (watch on canvasCards :268)
    A->>C: PUT /api/ai/preview-draft
    C->>R: UpsertDraft :116
    R->>D: UPSERT ai_preview_drafts (dashboard_id=NULL)

    opt user clicks Confirm on the preview card
        U->>P: Confirm(name, dragged layouts)
        P->>A: executeAiTool(create_custom_dashboard, args) :952
        A->>C: POST /api/ai/tools/execute
        C->>C: dispatch — write tool, admin/editor only :146
        C->>D: INSERT dashboards + dashboard_widgets
        C-->>P: { dashboardId }
        P->>A: deletePreviewDraft :964
        P->>P: reset cards/history · router.push(/dashboards/:id) :967
    end
```

Note the asymmetry: while chatting, **nothing** the LLM does writes to `dashboards` — the
`preview_*` tools only return patches the page applies to its in-memory card. The single DB
write happens on the user's explicit Confirm (or Save for an Active-dashboard card, which the
page translates into widget CRUD calls itself — `saveDashboardCard` `:167-238`).

---

## 4. API reference — `/api/ai`

All routes registered in `routes.go:13-26`, JWT required (`routes.go:11`).

| Method | Path | Handler (controller.go) | Notes |
|--------|------|--------------------------|-------|
| GET | `/api/ai/tools` | `ListTools` :209 | Returns `AllTools()` (12 tools; excludes `create_custom_dashboard`). |
| POST | `/api/ai/tools/execute` | `ExecuteTool` :191 | `{toolName, params}` → direct `dispatch`. The frontend's Confirm path; write tools need admin/editor. |
| GET | `/api/ai/conversations` | `GetConversations` :215 | Current user's conversations, newest-updated first, with message counts. |
| POST | `/api/ai/conversations` | `CreateConversation` :227 | `{title?}` (default "New Conversation") → 201. |
| GET | `/api/ai/conversations/:id/messages` | `GetMessages` :243 | Last 20 messages, DESC. ⚠️ No ownership check on `:id`. |
| POST | `/api/ai/conversations/:id/messages` | `AddMessage` :255 | Manual message insert `{role, content, toolName?, toolInput?, toolResult?}`. |
| GET | `/api/ai/preview-draft` | `GetPreviewDraft` :276 | Per-user view state: `{conversationId, dashboardId, data}` or `null`. |
| PUT | `/api/ai/preview-draft` | `PutPreviewDraft` :306 | `{conversationId?, data}` — upsert preview, clears `dashboard_id`. |
| DELETE | `/api/ai/preview-draft` | `DeletePreviewDraft` :321 | Drop the row (Clear chat / after Confirm). |
| PUT | `/api/ai/selected-dashboard` | `PutSelectedDashboard` :292 | `{dashboardId}` — upsert selected dashboard, clears `data`. |
| POST | `/api/ai/chat` | `Chat` :331 | `{conversationId, message, context?}` → runs the Groq tool loop, returns all new message rows. 503 if no `GROQ_API_KEY`, 502 on Groq failure. |

Response envelope everywhere: `{ "success": true, "data": … }`; errors go through the
shared error middleware as `{code, message}` with 4xx/5xx status.

---

## 5. Data model — ERD

Tables created in `internal/migrate/migrate.go:211-245`.

```mermaid
erDiagram
    users ||--o{ ai_conversations : "owns (CASCADE)"
    ai_conversations ||--o{ ai_messages : "has (CASCADE)"
    users ||--o| ai_preview_drafts : "at most one"
    ai_conversations |o..o| ai_preview_drafts : "conversation_id (no FK)"
    dashboards |o..o| ai_preview_drafts : "dashboard_id (no FK)"

    ai_conversations {
        uuid id PK
        uuid user_id FK "ON DELETE CASCADE"
        text title "default New Conversation"
        jsonb context "default {} (unused by handlers)"
        timestamptz created_at
        timestamptz updated_at "touched on every AddMessage"
    }
    ai_messages {
        uuid id PK
        uuid conversation_id FK "ON DELETE CASCADE"
        text role "user | assistant | tool"
        text content
        text tool_name "nullable"
        jsonb tool_input "nullable"
        jsonb tool_result "nullable"
        int tokens "nullable — column exists, never written"
        timestamptz created_at
    }
    ai_preview_drafts {
        uuid user_id PK "FK users, CASCADE"
        uuid conversation_id "nullable, no FK"
        uuid dashboard_id "nullable, no FK"
        jsonb data "nullable — preview card payload"
        timestamptz updated_at
    }
```

Semantics worth knowing:

- `ai_preview_drafts` is a **one-row-per-user XOR**: `UpsertDraft` (`repository.go:116`) sets
  `data` and nulls `dashboard_id`; `UpsertDashboard` (`:134`) does the opposite. `GetDraft`
  (`:147`) returns whichever side is set; the page restores a preview/focus card or re-fetches
  the dashboard accordingly (`AIAssistantPage.vue:243-258`).
- `conversation_id` / `dashboard_id` on the draft are plain UUID columns (no FK) — a deleted
  dashboard leaves a dangling draft that simply fails to restore.
- `ai_messages.tokens` is schema-only; `AddMessage` (`repository.go:96`) never writes it.
- History sent to Groq is much smaller than what's stored: 20 rows fetched
  (`repository.go:77`), but only the last **3** user/assistant text rows are replayed —
  tool rows are deliberately dropped (`buildGroqMessages` `controller.go:732-750`).

---

## 6. State diagram — canvas + persisted draft

The page's central state is `canvasCards` (`AIAssistantPage.vue:18-22`) — at most one card of
kind `preview` | `focus` | `dashboard` (plus transient `created`). Each in-memory state maps
to a persisted `ai_preview_drafts` shape.

```mermaid
stateDiagram-v2
    [*] --> Empty : mount, no draft (:259-262)
    [*] --> Preview : mount, draft.data (kind preview) :244-250
    [*] --> FocusCard : mount, draft.data (kind focus) :244-250
    [*] --> ActiveDashboard : mount, draft.dashboardId :251-258

    Empty --> Preview : preview_dashboard tool :693
    Empty --> FocusCard : show_metric with nothing to highlight :679<br/>or deriveFocusWidget fallback :917
    Empty --> ActiveDashboard : dashboard opened into AI page<br/>(PUT selected-dashboard)

    Preview --> Preview : preview_add / update / remove :705-746<br/>manual widget edits · drag · undo/redo
    FocusCard --> FocusCard : more show_metric widgets append :681
    ActiveDashboard --> ActiveDashboard : staged edits (same handlers, kind dashboard)

    FocusCard --> Empty : turn without show_metric clears it :750<br/>or user removes card :483
    Preview --> [*] : Confirm → create_custom_dashboard (DB write)<br/>delete draft · reset history · navigate :944-974
    FocusCard --> [*] : Create-from-focus → new dashboard + widgets<br/>navigate :450-481
    ActiveDashboard --> ActiveDashboard : Save → widget CRUD to DB,<br/>card rebuilt with fresh IDs, history reset :167-238

    Preview --> Empty : Clear chat :931
    FocusCard --> Empty : Clear chat :931
    ActiveDashboard --> Empty : Clear chat :931

    note right of Preview
        persisted as ai_preview_drafts.data
        (dashboard_id = NULL) — watch :268
    end note
    note right of ActiveDashboard
        persisted as ai_preview_drafts.dashboard_id
        (data = NULL)
    end note
    note right of Empty
        no draft row
        (DELETE /ai/preview-draft)
    end note
```

Undo/redo (`useUndoHistory`, `:113-141`) operates **within** a state — it snapshots
`canvasCards` on every deep change and restores previews/focus cards; anything already written
to the DB (Confirm, Save) is out of its reach, so those paths call `resetHistory()`.
