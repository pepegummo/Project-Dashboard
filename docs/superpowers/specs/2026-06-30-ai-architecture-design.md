# AI Module — Architecture & Flow

IotVision AI is a Groq-backed agentic loop that reads live telemetry and manages dashboards via tool calls. This document covers the full flow from user input to frontend rendering.

---

## Full Architecture Diagram

```mermaid
flowchart TD
    subgraph FE["Frontend — AIAssistantPage.vue"]
        U([User types message]) --> AT["Append @mention tokens\ne.g. @Widget Title"]
        AT --> DC["buildDashboardContext()\nlive values · widget configs · active dashboard"]
        DC --> POST["POST /api/ai/chat\n{ message, context, conversationId }"]
    end

    subgraph BE["Backend — Chat Loop  (controller.go)"]
        B1["Save user msg → ai_messages"] --> B2["Load last 8 messages from DB"]
        B2 --> B3["Build Groq payload\nsystem prompt + history + context injection"]
        B3 --> B4["Filter tools by role\nviewer → read-only tools only"]
        B4 --> GROQ["Groq API  openai/gpt-oss-20b\nmax 5 iterations · retry on 429"]
        GROQ --> DEC{finish_reason}
        DEC -->|tool_calls| RC["Role check\nadmin / editor → write tools allowed"]
        RC --> DISP["Tool dispatch"]
        DISP --> RESULT["Append tool result to message thread"]
        RESULT --> GROQ
        DEC -->|stop| SAVE["Save assistant reply → ai_messages"]
        SAVE --> RET["Return all new messages to frontend"]
    end

    subgraph TOOLS["Tool Catalogue  (tool_actions.go)"]
        subgraph RT["Read — Telemetry"]
            T1["show_metric\nresolve machine + field → widget spec\nhandles count / daily-count / fallback"]
            T2["get_telemetry_trend\navg / min / max over time period"]
            T3["get_daily_count\nper-day production counts"]
            T4["get_active_alerts\nopen alert events for org"]
        end
        subgraph RS["Read — Structure"]
            T5["get_machines\norg machine list + field schema"]
            T6["list_dashboards\nname + widget count + URL"]
        end
        subgraph WD["Write — Dashboards  (admin / editor only)"]
            T7["preview_dashboard\ngenerate template widget set"]
            T8["preview_add_widget\npreview_update_widget\npreview_remove_widget"]
            T9["add_widget_to_dashboard\nnamed existing dashboard only"]
            T10["remove_widget\nby title, case-insensitive"]
            T11["create_custom_dashboard\nconfirm + persist preview draft"]
        end
    end

    subgraph DB["Database — TimescaleDB"]
        DB1[("machines\nmachine_fields")]
        DB2[("telemetry_raw\ntelemetry_aggregates")]
        DB3[("dashboards\ndashboard_widgets")]
        DB4[("alerts\nalert_events")]
        DB5[("ai_conversations\nai_messages")]
    end

    subgraph RENDER["Frontend — Canvas Render"]
        R1["show_metric result\n→ FocusCard  (PreviewCanvasCard)"]
        R2["preview_dashboard result\n→ PreviewCanvasCard\nConfirm / Discard buttons"]
        R3["add_widget / create_dashboard result\n→ CreatedCanvasCard\nlink to live dashboard"]
        R4["text reply\n→ Chat bubble"]
    end

    POST --> B1

    DISP --> T1 & T2 & T3 & T4
    DISP --> T5 & T6
    DISP --> T7 & T8 & T9 & T10 & T11

    T1 & T5 --> DB1
    T1 & T2 & T3 --> DB2
    T6 & T7 & T8 & T9 & T10 & T11 --> DB3
    T4 --> DB4
    B1 & B2 & SAVE --> DB5

    RET --> RENDER
```

---

## Key Design Decisions

| Decision | Reason |
|----------|--------|
| Max 5 Groq iterations per request | Prevents infinite tool-call loops; forces summary after one chained round (i ≥ 1 → tools = nil) |
| History capped at 8 messages | Groq prompt-cache friendly; stable system+tools prefix stays cached |
| `show_metric` always required for live values | Context values are snapshot-in-time; calling the tool guarantees fresh data |
| Role-check at dispatch layer | Viewer token cannot trigger any write tool even if model hallucinates a write call |
| `buildDashboardContext()` sends current telemetry values | Model sees live sensor state so it can reason about thresholds without extra tool calls |
| Preview draft stored in DB (not frontend state) | Survives page refresh; AI page restores in-progress dashboard composition |
| Tool result reconstruction from `ai_messages` | Groq requires paired `assistant tool_calls` + `tool` messages in history; DB stores them as a single row |

---

## Tool Permission Matrix

| Tool | viewer | editor | admin |
|------|--------|--------|-------|
| get_machines, list_dashboards | ✓ | ✓ | ✓ |
| show_metric, get_telemetry_trend, get_daily_count | ✓ | ✓ | ✓ |
| get_active_alerts | ✓ | ✓ | ✓ |
| preview_* | — | ✓ | ✓ |
| add_widget_to_dashboard, remove_widget | — | ✓ | ✓ |
| create_custom_dashboard | — | ✓ | ✓ |
