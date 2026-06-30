# IotVision AI — Overview & Flow

> A conversational assistant built into the dashboard. Ask questions in plain language,
> get live data, and create dashboards — without touching any configuration UI.

---

## What the AI Can Do

### 📋 List Machines & Dashboards
Ask "what machines do we have?" or "list my dashboards." The AI fetches live data
and replies in plain text with names, types, statuses, and widget counts.

### 📊 View Live Sensor Data
Ask for any machine metric by name. The AI instantly renders a live widget card —
gauge, KPI value, or trend chart — directly in the chat canvas.

### 📈 Analyze Trends
Ask for averages, minimums, or maximums over any time period (5 min → 30 days).
The AI queries the historical sensor archive and replies in plain text.

### 🔔 Check Alerts
Ask what alerts are currently firing. The AI lists all open alert events with severity,
machine, metric, and value — in plain text.

### 🏭 Production Counts
View per-day production counts per machine, with configurable time buckets and SKU filters.

### 🗂️ Create Dashboards (Preview → Confirm)
Describe what you need ("a production dashboard for CW-01"). The AI picks a template
(`machine_overview`, `machine_production`, or `machine_maintenance`), generates a full
dashboard preview with widgets, and waits for your confirmation before saving.

### ✏️ Edit the Preview Before Saving
While a dashboard preview is open, tell the AI to add, remove, or change any widget —
all in-memory with no DB write. Confirm once when you're happy.

### 🔧 Modify Existing Dashboards
Add or remove widgets from any already-saved dashboard by name, without opening the editor.

---

## What the AI Cannot Do

| Limitation | Reason |
|------------|--------|
| Access another organization's data | All queries are org-scoped at the backend |
| Modify dashboards if you are a **Viewer** | Write tools are blocked at the API layer for viewer accounts |
| Create new machines or sensor fields | Outside the AI module scope — done through the Machines page |
| Answer metric questions without calling a tool | Live sensor values are never stored in the chat; the AI always fetches fresh data |
| Acknowledge or resolve alerts | Alert management is done through the Alerts page |

---

## How It Works — Sequence Diagram

The diagram below shows what happens from the moment you press Send to the moment a widget appears on screen.

```mermaid
sequenceDiagram
    actor User
    participant Chat as AI Chat (Browser)
    participant API as Backend API
    participant Groq as Groq LLM
    participant DB as Database

    User->>Chat: Types a message
    Note over Chat: Attaches live context:<br/>active dashboard · current sensor values

    Chat->>API: POST /api/ai/chat<br/>{ message, context, conversationId }
    API->>DB: Save message · load last 8 messages
    API->>Groq: System prompt + conversation history + tool definitions

    alt Needs live data or a dashboard action
        Groq-->>API: tool_call (e.g. show_metric, preview_dashboard, preview_add_widget)
        Note over API: Role check — viewer blocked from write tools
        API->>DB: Query machines / telemetry / dashboards / alerts
        DB-->>API: Result data
        API->>Groq: Tool result
        Note over Groq: Up to 5 tool-call rounds,<br/>then forced to summarize
        Groq-->>API: Final text reply
    else Simple question or greeting
        Groq-->>API: Text reply (no tool needed)
    end

    API->>DB: Save assistant reply
    API-->>Chat: Return new messages

    alt show_metric called
        Chat->>User: 📊 Live metric widget card (gauge / KPI / trend)
    else preview_dashboard called
        Chat->>User: 🗂️ Dashboard preview — Confirm or Discard
    else preview_add/remove/update_widget called
        Chat->>User: 🗂️ Updated preview (no DB write yet)
    else add_widget_to_dashboard or remove_widget called
        Chat->>User: ✅ Success card with link to dashboard
    else Text reply
        Chat->>User: 💬 Chat bubble
    end
```

---

## Example Interactions

| User says | AI calls | What you see |
|-----------|----------|--------------|
| "What machines do we have?" | `get_machines` | Plain-text list of machines, types, and fields |
| "List my dashboards" | `list_dashboards` | Plain-text list with widget counts |
| "Show me the speed of CW-01" | `show_metric` | Live gauge card for CW-01 speed |
| "Show me a trend chart for weight on CW-01" | `show_metric` (viz=trend) | Live line-chart card |
| "What was the average temperature last hour?" | `get_telemetry_trend` | "Average: 72.4 °C, Min: 68.1, Max: 76.0" |
| "Are there any active alerts right now?" | `get_active_alerts` | Plain-text list of open alert events |
| "What's the production count for CW-01 this week?" | `get_daily_count` | Plain-text per-day table |
| "Create a production dashboard for CW-01" | `preview_dashboard` | Full dashboard preview with Confirm button |
| "Add a weight gauge to the preview" | `preview_add_widget` | Preview updates in place |
| "Remove the temperature card from the preview" | `preview_remove_widget` | Preview updates in place |
| "Change the speed gauge max to 200" | `preview_update_widget` | Preview updates in place |
| "Add a weight widget to CW-01 Overview" | `add_widget_to_dashboard` | Confirmation card + link to updated dashboard |
| "Remove the pressure widget from CW-01 Overview" | `remove_widget` | Confirmation that widget was removed |

> **Note:** The AI works in both **English and Thai**. You can switch languages mid-conversation.

---

## Role & Permission Summary

```mermaid
graph LR
    subgraph Viewer
        V1[View live metrics]
        V2[Check active alerts]
        V3[List dashboards & machines]
        V4[Ask trend questions]
    end

    subgraph Editor["Editor / Admin"]
        E1[Everything in Viewer]
        E2[Create & modify dashboards]
        E3[Add / remove widgets]
    end

    style Viewer fill:#e8f4fd,stroke:#3b82f6
    style Editor fill:#f0fdf4,stroke:#22c55e
```

---

## Technical Summary (for developers)

| Component | Detail |
|-----------|--------|
| LLM | Groq — `openai/gpt-oss-20b` (best Thai intent, prompt-cached) |
| Max tool rounds | 5 per request, then forced to plain-text summary |
| Conversation history | Last 8 messages sent to LLM per request |
| Context injection | Frontend sends live widget values + active dashboard state |
| Role enforcement | Backend blocks write tools for `viewer` role at dispatch layer |
| Persistence | Conversations + messages stored in `ai_conversations` / `ai_messages` |
| Dashboard drafts | Preview state saved in DB — survives page refresh |

### Tool Reference

| Tool | Description | Role required |
|------|-------------|---------------|
| `get_machines` | List all machines, types, statuses, and fields | Viewer |
| `show_metric` | Render a live widget card (gauge / KPI / trend) | Viewer |
| `get_telemetry_trend` | avg/min/max over a time window (5m – 30d) | Viewer |
| `get_active_alerts` | List all open alert events | Viewer |
| `get_daily_count` | Per-day production count for one machine | Viewer |
| `list_dashboards` | List dashboards with widget counts | Viewer |
| `preview_dashboard` | Generate a template dashboard preview (no DB write) | Viewer |
| `preview_add_widget` | Add a widget to the open preview plan (no DB write) | Viewer |
| `preview_remove_widget` | Remove a widget from the open preview plan (no DB write) | Viewer |
| `preview_update_widget` | Edit a widget in the open preview plan (no DB write) | Viewer |
| `create_custom_dashboard` | Save the confirmed preview as a real dashboard | Editor / Admin |
| `add_widget_to_dashboard` | Add a widget to an existing saved dashboard | Editor / Admin |
| `remove_widget` | Remove a widget from an existing saved dashboard | Editor / Admin |
