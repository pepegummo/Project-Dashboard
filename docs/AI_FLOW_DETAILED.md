# IotVision AI — Ultra-detailed Flow (Mermaid)

ไดอะแกรมชุดนี้ลงลึกทุกจุดของ `Chat` handler (`backend/internal/modules/ai/controller.go`)
พร้อม tool layer (`schema.go`, `tool_actions.go`, `dashboard_action.go`).
render ได้บน GitHub / VS Code (Markdown Preview Mermaid) / mermaid.live

---

## 1. Master flow — end to end (ทุก branch, ทุก guard)

```mermaid
flowchart TB
    %% ============ CLIENT ============
    subgraph CLIENT["🌐 Frontend · AIAssistantPage.vue"]
        U(["ผู้ใช้พิมพ์ข้อความ<br/>ไทย / อังกฤษ"])
        CTX["buildDashboardContext()<br/>serialize dashboard · preview · [FOCUSED] widget<br/>+ inline 'on-screen data' series ถ้าเป็นคำถามเชิงวิเคราะห์"]
        POST["POST /api/ai/chat<br/>{ conversationId, message, context }<br/>client timeout 120s"]
        U --> CTX --> POST
    end

    %% ============ AUTH ============
    POST --> JWT{"middleware/auth.go<br/>JWT ถูกต้อง?"}
    JWT -- "ไม่" --> E401(["401 Unauthorized"])
    JWT -- "ใช่ · inject userId/orgId/role" --> KEY{"GROQ_API_KEY ว่าง?"}
    KEY -- "ว่าง" --> E503(["503 AI_UNAVAILABLE"])

    %% ============ CLASSIFY ============
    KEY -- "มีคีย์" --> CLS
    subgraph CLASSIFY["🧠 Classify — deterministic regex (controller.go:693-712)"]
        direction TB
        CLS["needsToolsFlag = needsTools(msg)<br/>regex: metric/action keyword หรือมี @mention"]
        HC["hasContext = body.Context != &quot;&quot;"]
        INL["inlineData = context มี 'on-screen data'"]
        CR["contextRead = @-focus ที่ไม่ใช่ edit/range/sku<br/>(ไม่แมตช์ editRe / rangeRe / skuRe)"]
        AFC["answerFromContext = inlineData OR contextRead"]
        CLS --> HC --> INL --> CR --> AFC
    end

    %% ============ PROMPT SELECT ============
    AFC --> SEL{"เลือก system prompt<br/>(controller.go:387-394)"}
    SEL -- "needsTools == false" --> P1["📄 systemPromptMinimal<br/>identity + ภาษา + 'ตอบ 1 ประโยค'<br/>NO tools · ~300 tok เบากว่า base"]
    SEL -- "answerFromContext == true" --> P2["📄 systemPromptContextAnswer<br/>'Do NOT call any tool — ตอบจาก context'<br/>NO tools"]
    SEL -- "needsTools && hasContext<br/>&& !answerFromContext" --> P3["📄 systemPromptBase + systemPromptContextExt<br/>TOOL SELECTION + SLOT FILLING + WIDGET<br/>+ preview-staging + @Widget/[FOCUSED] routing<br/>slim tools"]
    SEL -- "needsTools · no context" --> P4["📄 systemPromptBase<br/>TOOL SELECTION + SLOT FILLING + WIDGET<br/>slim tools"]

    %% ============ ASSEMBLE ============
    P1 --> ASM
    P2 --> ASM
    P3 --> ASM
    P4 --> ASM
    ASM["ประกอบ messages[]<br/>system + history (3 เทิร์นล่าสุด) + context ต่อท้ายสุด (recency wins)<br/>tools = toGroqToolSlim (name+desc; full schema เฉพาะ widget-nested)<br/>role gate: writeTools เฉพาะ admin/editor<br/>roundCap = 0 ถ้า @widget focus, มิฉะนั้น 1"]

    %% ============ GROQ CALL ============
    ASM --> CALL
    subgraph LOOP["🔁 Tool-calling loop · สูงสุด 5 รอบ"]
        direction TB
        CALL["callGroqModel()<br/>POST api.groq.com/openai/v1/chat/completions<br/>model=openai/gpt-oss-20b · reasoning_format=hidden · non-stream"]
        R429{"HTTP 429?"}
        RETRY["parseRetryAfter → sleep<br/>retry สูงสุด 3×"]
        FIN{"finish_reason"}
        CALL --> R429
        R429 -- "ใช่ (≤3×)" --> RETRY --> CALL
        R429 -- "ไม่" --> FIN
    end

    FIN -- "tool_calls" --> DISP
    FIN -- "stop / text" --> TXT

    %% ============ DISPATCH ============
    subgraph DISPATCH["⚙️ Tool dispatch (tool_actions.go / dashboard_action.go)"]
        direction TB
        DISP{"tool ไหน?"}
        DISP -- "read" --> RD["get_machines · show_metric · get_telemetry_trend<br/>get_telemetry_series · get_production_count<br/>get_skus · get_active_alerts · list_dashboards"]
        DISP -- "preview" --> PV["preview_dashboard · preview_add_widget<br/>preview_update_widget · preview_remove_widget<br/>➜ สร้าง plan/patch เท่านั้น · NO DB write"]
        RD --> SQL[("org-scoped SQL<br/>TimescaleDB")]
        SQL --> CMP["compactSeriesResult / compactBucketResult<br/>→ column+tuple ลด token"]
        PV --> STG["stage บน card (in-memory)"]
        CMP --> APP["append เป็น role:tool message"]
        STG --> APP
    end

    APP --> CAP{"roundCap ถึงเพดาน<br/>หรือครบ 5 รอบ?"}
    CAP -- "ยัง" --> CALL
    CAP -- "ถึงแล้ว" --> TXT

    %% ============ FINALIZE ============
    TXT["Groq final text<br/>(1 ประโยค read · 2-4 ประโยค analytical)<br/>SLOT FILLING: ขาด slot → ถาม 1 คำถาม ไม่เรียก tool"]
    TXT --> SAVE[("persist turns → ai_messages")]
    SAVE --> RESP["ตอบ messages[] → UI<br/>+ ไฮไลต์ widget / แสดง preview card"]

    %% ============ USER ACTION (deferred write) ============
    RESP --> ACT{"มี preview staged?"}
    ACT -- "read เฉย ๆ" --> DONE(["จบ"])
    ACT -- "staged · ผู้ใช้ตัดสินใจภายหลัง" --> CONF{"ผู้ใช้กดอะไร?"}
    CONF -- "Confirm (dashboard ใหม่)" --> W1["POST /api/ai/tools/execute<br/>create_custom_dashboard (admin/editor)"]
    CONF -- "Save (dashboard เดิม)" --> W2["saveDashboardCard → REST<br/>api.addWidget/updateWidget/deleteWidget"]
    CONF -- "ไม่ทำอะไร" --> DONE
    W1 --> WDB[("เขียน dashboards / dashboard_widgets")]
    W2 --> WDB
    WDB --> DONE

    %% ============ STYLES ============
    classDef read stroke:#4fc3d9,stroke-width:2px;
    classDef write stroke:#f2765c,stroke-width:2px;
    classDef prev stroke:#b58cff,stroke-width:2px;
    classDef acc stroke:#ffb54d,stroke-width:2px;
    classDef err stroke:#f2765c,fill:#3a1f1f,color:#fff;
    class RD,SQL,CMP read;
    class W1,W2,WDB write;
    class PV,STG prev;
    class CLS,SEL,CALL acc;
    class E401,E503 err;
```

---

## 2. Classification internals — ลำดับการตัดสิน (ทำไมได้ prompt นั้น)

```mermaid
flowchart TD
    M(["message + context เข้ามา"]) --> A{"needsTools(msg)?<br/>metric/action regex OR @mention"}
    A -- "ไม่ (ทักทาย/คุยเล่น)" --> PA["Minimal prompt · no tools"]
    A -- "ใช่" --> B{"context มี 'on-screen data'?<br/>(inlineData)"}
    B -- "ใช่" --> PC["ContextAnswer prompt · no tools"]
    B -- "ไม่" --> C{"@-focus อยู่ไหม?"}
    C -- "ไม่" --> D{"hasContext?"}
    C -- "ใช่" --> E{"ตรง editRe?<br/>(แก้ไข)"}
    E -- "ใช่" --> PE["Base + ContextExt<br/>→ preview_update/remove"]
    E -- "ไม่" --> F{"ตรง rangeRe หรือ skuRe?<br/>(ช่วงเวลา/SKU ที่ไม่ได้อยู่บนจอ)"}
    F -- "ใช่ · ต้อง fetch" --> D
    F -- "ไม่ · ค่าปัจจุบัน/config" --> PC2["ContextAnswer (contextRead) · no tools"]
    D -- "hasContext = true" --> PBE["Base + ContextExt · slim tools"]
    D -- "hasContext = false" --> PB["Base · slim tools"]

    classDef notool stroke:#6ec98a,stroke-width:2px;
    classDef tool stroke:#ffb54d,stroke-width:2px;
    class PA,PC,PC2 notool;
    class PE,PBE,PB tool;
```

---

## 3. Read path — sequence เต็ม (2b) พร้อม compaction & round cap

```mermaid
sequenceDiagram
    autonumber
    actor U as ผู้ใช้
    participant UI as Vue UI
    participant BE as Chat handler
    participant G as Groq (gpt-oss-20b)
    participant DB as TimescaleDB

    U->>UI: "speed ของ CW-01 เท่าไหร่"
    UI->>BE: POST /api/ai/chat (+ context)
    BE->>BE: classify → needsTools=true, answerFromContext=false → Base prompt
    BE->>BE: messages = system + history(3) + context(ท้ายสุด)<br/>tools = slim, roundCap=1
    BE->>G: chat.completions (tools)
    alt slot ขาด (ไม่ระบุเครื่อง)
        G-->>BE: text: "เครื่องไหนครับ?" (ไม่มี tool_call)
        BE-->>UI: reply (ไม่แตะ DB)
    else slot ครบ
        G-->>BE: tool_calls: show_metric(machine=CW-01, metric=speed)
        BE->>DB: SELECT ... machines · machine_fields · telemetry_raw (org-scoped)
        DB-->>BE: rows
        BE->>BE: compactSeriesResult → column+tuple
        BE->>G: append role:tool result
        Note over BE,G: roundCap=1 → บังคับสรุปเป็น text รอบถัดไป (คุม 8k tok/min)
        G-->>BE: "ตอนนี้ speed ของ CW-01 อยู่ที่ ... rpm" (finish_reason=stop)
        BE->>DB: INSERT ai_messages (user + tool + assistant)
        BE-->>UI: reply + ไฮไลต์ widget
    end
    UI-->>U: แสดงคำตอบ
```

---

## 4. Create / Edit path — staging → persist (2c) พร้อม Preview vs Active

```mermaid
sequenceDiagram
    autonumber
    actor U as ผู้ใช้
    participant UI as Vue UI
    participant BE as Chat handler
    participant G as Groq
    participant DB as TimescaleDB

    U->>UI: "สร้าง dashboard ของ CW-01" / "เปลี่ยน metric เป็น temperature"
    UI->>BE: POST /api/ai/chat (+ preview / Active-dashboard context)
    BE->>BE: classify → Base + ContextExt
    BE->>G: chat.completions (tools)
    G-->>BE: preview_dashboard / preview_add_widget / preview_update_widget / preview_remove_widget
    BE->>BE: dashboard_action.go → สร้าง plan/patch (NO DB write)
    BE-->>UI: staged บน card + ข้อความ "กด Save/Confirm เพื่อบันทึก"
    Note over UI: ยังไม่มีอะไรลง DB

    alt Preview ใหม่ → Confirm
        U->>UI: กด Confirm
        UI->>BE: POST /api/ai/tools/execute → create_custom_dashboard (admin/editor)
        BE->>DB: INSERT dashboards + dashboard_widgets
        BE-->>UI: เปิด dashboard ใหม่
    else Active dashboard เดิม → Save
        U->>UI: กด Save
        UI->>UI: saveDashboardCard diff
        UI->>BE: api.addWidget / updateWidget / deleteWidget (REST)
        BE->>DB: UPDATE widgets
        BE-->>UI: refresh dashboard
    else ไม่ทำอะไร
        Note over UI: preview ถูกทิ้ง · DB ไม่เปลี่ยน
    end
```

> **การันตีความปลอดภัย:** tool เขียนตรงของเดิม (`add_widget_to_dashboard` / `remove_widget`) ถูกปลดแล้ว —
> คำขอผ่านแชทเขียน dashboard ที่เซฟไว้เองไม่ได้ ต้องผ่าน Save/Confirm ของผู้ใช้เสมอ

---

## 5. Tool catalog + argument surface (mindmap)

```mermaid
mindmap
  root(("AllTools()<br/>12 tools"))
    READ
      get_machines
        รายชื่อ+field keys
      show_metric
        machine · metric → ค่า/widget spec
      get_telemetry_trend
        avg/min/max ตาม time_range
      get_telemetry_series
        time-bucketed series
      get_production_count
        bucketed piece count
        args: bucket · sku · status
      get_skus
        distinct SKU ของเครื่อง
      get_active_alerts
        alert ที่ active
      list_dashboards
        ชื่อ + จำนวน widget
    PREVIEW
      preview_dashboard
        template machine_overview
      preview_add_widget
      preview_update_widget
        new_title · metric · bucket · unit
        min · max · start/end_date
        sku · status · machine · type
        fields[] · chartType · points · scaling
      preview_remove_widget
    WRITE
      create_custom_dashboard
        ไม่อยู่ใน AllTools()
        UI เรียกตรงหลัง Confirm
        admin / editor เท่านั้น
```

---

**ไฟล์อ้างอิง:** `controller.go` (classify + loop + prompts) · `schema.go` (`AllTools`, `toGroqToolSlim`, `writeTools`) ·
`tool_actions.go` (read executors) · `dashboard_action.go` (preview/create) · `eval_test.go` (bake-off).
ภาพรวมย่อ: [`AI_WORKFLOW_SIMPLE.md`](./AI_WORKFLOW_SIMPLE.md) · เชิงลึก: [`AI_ARCHITECTURE.md`](./AI_ARCHITECTURE.md)
