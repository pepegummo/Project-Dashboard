# CPF Industrial IoT Dashboard — Data Flow & User Guide

---

## Table of Contents
1. [System Architecture Overview](#1-system-architecture-overview)
2. [Data Flow Diagrams](#2-data-flow-diagrams)
   - [Authentication Flow](#21-authentication-flow)
   - [Real-Time Telemetry Flow](#22-real-time-telemetry-flow)
   - [Dashboard & Widget Flow](#23-dashboard--widget-flow)
   - [Alert Flow](#24-alert-flow)
   - [LED Kiosk Flow](#25-led-kiosk-flow)
   - [AI Assistant Flow](#26-ai-assistant-flow)
3. [How to Use the Web Application](#3-how-to-use-the-web-application)
   - [Getting Started / Login](#31-getting-started--login)
   - [Dashboard List](#32-dashboard-list)
   - [Dashboard Editor](#33-dashboard-editor)
   - [Machine Management](#34-machine-management)
   - [Alerts](#35-alerts)
   - [AI Assistant](#36-ai-assistant)
   - [LED Kiosk Mode](#37-led-kiosk-mode)
4. [Role Permissions](#4-role-permissions)
5. [API Quick Reference](#5-api-quick-reference)

---

## 1. System Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                        Browser / Client                         │
│                                                                 │
│   Vue 3 + TypeScript SPA                                        │
│   ┌─────────────┐  ┌─────────────┐  ┌──────────────────────┐   │
│   │  Pinia Store│  │  Vue Router │  │  ECharts / GridStack │   │
│   └──────┬──────┘  └──────┬──────┘  └──────────────────────┘   │
│          │                │                                     │
│   ┌──────▼────────────────▼──────────────────────────────┐      │
│   │           Axios (REST)      WebSocket Client          │      │
│   └──────────────┬──────────────────────┬────────────────┘      │
└──────────────────│──────────────────────│────────────────────────┘
                   │ HTTP /api/*          │ ws://.../ws
┌──────────────────▼──────────────────────▼────────────────────────┐
│                     Nginx Reverse Proxy (:5173)                   │
│   /api/*  →  backend:4000   |   /ws  →  backend:4000 (upgrade)   │
└──────────────────┬──────────────────────┬────────────────────────┘
                   │                      │
   ┌───────────────▼──────────────────────▼───────────┐
   │            Go Fiber :4000 — REST + WS             │
   │                                                    │
   │  Auth / Machines / Dashboards / Alerts /           │
   │  Telemetry / AI            Fiber WS Hub (/ws)      │
   │                             Telemetry broadcasts    │
   │                             Alert events            │
   │                             Machine status changes  │
   └───────────┬────────────────────────────────────────┘
               │                          │
               │    ┌─────────────────────┘
               │    │
   ┌───────────▼────▼──────────────────────┐
   │           Go Business Logic           │
   │  ┌──────────────┐  ┌───────────────┐  │
   │  │ DB Broadcaster│  │  Alert Engine │  │
   │  │  (30s poll +  │  │  (on ingest)  │  │
   │  │ immediate on  │  │               │  │
   │  │    ingest)    │  │               │  │
   │  └──────┬───────┘  └───────┬───────┘  │
   └─────────│──────────────────│──────────┘
             │                  │
   ┌─────────▼──────────────────▼──────────┐
   │         PostgreSQL 16 + TimescaleDB   │
   │  telemetry_raw (hypertable)           │
   │  telemetry_aggregates                 │
   │  machines / dashboards / alerts / ... │
   └───────────────────────────────────────┘
             │
   ┌─────────▼──────────┐
   │   Redis 7           │
   │  Rate limiting      │
   │  Session cache      │
   └────────────────────┘
```

---

## 2. Data Flow Diagrams

### 2.1 Authentication Flow

```
User                  Frontend (Vue)           Backend (Go)              PostgreSQL
 │                         │                       │                          │
 │── Enter email/password ─►│                       │                          │
 │                         │── POST /api/auth/login ►│                          │
 │                         │                       │── SELECT user WHERE email ►│
 │                         │                       │◄── user row ───────────────│
 │                         │                       │                          │
 │                         │                       │  bcrypt.compare(password)  │
 │                         │                       │  jwt.sign({id,role}, 24h)  │
 │                         │◄── {token, user} ──────│                          │
 │                         │                       │                          │
 │                         │  Store token in        │                          │
 │                         │  localStorage          │                          │
 │                         │  Set Axios header      │                          │
 │◄── Redirect to /dashboards│                       │                          │
 │                         │                       │                          │

Every subsequent request:
Frontend Axios  ──  Authorization: Bearer <token>  ──►  Go Middleware validates JWT
                                                         extracts {userId, orgId, role}
                                                         injects into request context
```

**Token Lifecycle:**
- Token stored in `localStorage` under key `auth_token`
- Expires after **24 hours**
- On 401 response → Axios interceptor clears token and redirects to `/login`
- Exception: `/led` route never redirects (public kiosk page)

---

### 2.2 Real-Time Telemetry Flow

```
New telemetry ingested                    DB Broadcaster (every 30s)
via POST /api/telemetry/:id/ingest        DISTINCT ON (machine_id)
(external device or load test)            SELECT latest telemetry_raw
        │                                 → fallback heartbeat for ALL
        ▼                                   connected clients
┌───────────────────────────────┐                   │
│  INSERT INTO telemetry_raw     │                   │
│  (machine_id, timestamp, data) │                   │
│                                 │                   │
│  Evaluate alert rules:         │                   │
│    field > threshold? → fire   │                   │
│    INSERT INTO alert_events    │                   │
│                                 │                   │
│  Broadcast immediately         │                   │
│  (does not wait for the 30s)   │                   │
└───────────────┬─────────────────┘                  │
                │                                     │
                └──────────────────┬──────────────────┘
                                   ▼
                    ┌─────────────────────────────────┐
                    │  WebSocket Hub broadcasts to     │
                    │  ALL / subscribed clients:       │
                    │                                  │
                    │  { type: "telemetry",            │
                    │    payload: {                    │
                    │      machineId, machineName,     │
                    │      timestamp,                  │
                    │      data: { weight, speed, ... }│
                    │    }                             │
                    │  }                               │
                    │                                  │
                    │  If alert fired:                 │
                    │  { type: "alert", payload: {...} }│
                    └───────────┬─────────────────────┘
                                │
              ┌─────────────────▼──────────────────────────┐
              │          Browser WebSocket Client           │
              │                                             │
              │  wsService.on("telemetry") →               │
              │    telemetryStore.updateLatest(data)        │
              │    → ECharts line/gauge widgets re-render   │
              │                                             │
              │  wsService.on("alert") →                   │
              │    alertStore.addEvent(event)               │
              │    → AlarmPanel widget updates              │
              └─────────────────────────────────────────────┘
```

There is no simulator — telemetry only enters the system via ingest. The 30s DB broadcaster is a fallback heartbeat, not a data generator.

**Widget Data Sources (current):**

| Widget | REST (initial seed) | WebSocket (live updates) | Notes |
|---|---|---|---|
| Line Chart | `GET /telemetry/:id/series` — 60s refresh | ✅ Live Mode: REST seed → WS append | Normal mode = REST only; Live Mode = rolling 30-min window via WS |
| Gauge | `GET /telemetry/:id/latest` — once on mount | ✅ global `onTelemetry` handler | No longer polls REST every 2s |
| KPI Card | `GET /telemetry/:id/latest` — once on mount | ✅ global `onTelemetry` handler | No longer polls REST every 2s |
| Status Card | — (store only) | ✅ global `onTelemetry` handler | Reads `telemetryStore` reactively |
| Table | `GET /telemetry/:id/latest` — once on mount | ✅ per-widget `onTelemetry` handler | Was store-only; now independently fetches |
| Alarm Panel | `GET /alerts/events/active` — once on mount | ✅ `addLiveAlert` handler | Can filter by machine via widget config |
| Daily Count | `GET /telemetry/:id/daily-count` — on mount | ❌ REST only | Daily granularity; WS not needed |

**Live data flow (gauge, KPI, table, status, alarm):**
```
Mount → REST (seed store immediately)
         ↓
WS message arrives → global useWebSocket handler
  → telemetryStore.updateSnapshot()
  → widget computed re-renders automatically
```

**Line Chart Live Mode (30-min rolling window):**
```
Mount → REST seed: GET /telemetry/:id/series?timeRange=30m
         ↓
WS message → append point to liveData[]
           → trim points older than 30 min
           → chart re-renders
Every 5 min → REST re-anchor to keep bucket shapes accurate
```

---

### 2.3 Dashboard & Widget Flow

```
User Action                  Frontend                    Backend / DB
─────────────────────────────────────────────────────────────────────

[Open Dashboard List]
User visits /dashboards ───► GET /api/dashboards ─────────────────────►
                             ◄── Dashboard[] (id, name, widget count) ──
                             Render grid of dashboard cards

[Create New Dashboard]
Click "+ New" ─────────────► POST /api/dashboards {name} ──────────────►
                                                            INSERT dashboards
                             ◄── {id, name, widgets: []} ───────────────
                             Redirect to /dashboards/:id (empty editor)

[Import Dashboard from JSON]
Click "Import" ────────────► File picker → user selects .json
                             Parse {name, description, tags, widgets[]}
                             POST /api/dashboards {name, ...} ──────────►
                             ◄── {id, ...} ───────────────────────────────
                             For each widget in JSON:
                               POST /api/dashboards/:id/widgets ──────────►
                             Redirect to /dashboards/:id

[Add a Widget]
Click "Add Widget" ────────► Select widget type + configure fields
                             POST /api/dashboards/:id/widgets
                             { type, config: {machineId, field, ...} } ──►
                                                            INSERT dashboard_widgets
                             ◄── {widgetId, type, config, layout} ───────
                             Render new widget on GridStack canvas

[Drag/Resize Widget]
Drag widget on grid ───────► (GridStack emits layout-changed)
                             Collect all widgets' {id, x, y, w, h}
                             PATCH /api/dashboards/:id/layout
                             { widgets: [{id, x, y, w, h}, ...] } ───────►
                                                            UPDATE each widget position
                             ◄── 200 OK ──────────────────────────────────

[Edit Widget Config]
Click widget settings ─────► Open config modal
                             User changes machine / field / time range
                             PATCH /api/dashboards/:id/widgets/:widgetId
                             { config: {...} } ─────────────────────────►
                                                            UPDATE dashboard_widgets
                             ◄── updated widget ──────────────────────────
                             Widget re-fetches data with new config

[Delete Widget]
Click delete button ───────► DELETE /api/dashboards/:id/widgets/:widgetId ►
                                                            DELETE dashboard_widgets row
                             ◄── 200 OK ──────────────────────────────────
                             Remove widget from GridStack canvas
```

---

### 2.4 Alert Flow

```
                        ┌─── Admin/Editor creates alert rule ───┐
                        │                                       │
                        │  POST /api/alerts {                   │
                        │    machineId,                         │
                        │    field: "weight",                   │
                        │    condition: "gt",   (gt/lt/eq/gte/lte)
                        │    threshold: 500,                    │
                        │    severity: "warning",               │
                        │    cooldown: 300       (seconds)      │
                        │  }                                    │
                        └────────────────┬──────────────────────┘
                                         │ Stored in alerts table
                                         ▼
                            ┌────────────────────────┐
                            │  Telemetry ingested     │
                            │  weight = 511.2         │
                            └──────────┬─────────────┘
                                       │
                            ┌──────────▼─────────────────────────┐
                            │  Alert Engine evaluates rule:       │
                            │  511.2 > 500 → TRUE                 │
                            │                                     │
                            │  Check cooldown: last fired?        │
                            │  If within cooldown window → skip   │
                            │  Else:                              │
                            │    INSERT alert_events (status=open)│x
                            └──────────┬──────────────────────────┘
                                       │
                        ┌──────────────▼──────────────────────────┐
                        │  WS broadcast { type: "alert", payload: │
                        │    { alertName, machineId, field,        │
                        │      value: 511.2, threshold: 500,       │
                        │      severity: "warning" }               │
                        │  }                                       │
                        └──────────────┬──────────────────────────┘
                                       │
             ┌─────────────────────────▼────────────────────────────┐
             │  Frontend receives WS message                         │
             │  alertStore.addEvent(event)                           │
             │                                                       │
             │  AlarmPanelWidget re-renders with new event           │
             │  (optional: browser notification toast)               │
             └───────────────────────────────────────────────────────┘

Lifecycle of an Alert Event:
  open ──► acknowledged ──► resolved
         (user clicks ACK)  (user clicks Resolve)
         PATCH /api/alerts/events/:id/acknowledge
         PATCH /api/alerts/events/:id/resolve
```

---

### 2.5 LED Kiosk Flow

```
User/Operator generates a shareable kiosk URL:

1. On DashboardEditorPage, click "Export" (copy LED link or open preview)
   (composable: useLedExport.ts)

2. Frontend maps each DashboardWidget → LedWidget type:
   line-chart  → sparkline   (30-min rolling mini chart)
   gauge       → gauge       (semicircle arc)
   kpi-card    → metric      (large number readout)
   status-card → status      (RUNNING / OFFLINE badge)
   alarm-panel → alarm       (critical / warning counts)

3. Serialize: LedWidget[] → JSON → encodeURIComponent → btoa → ?w=<base64>

4. Share URL:  https://your-domain.com/led?w=<base64>

─────────────────────────────────────────────────────────────

Kiosk Screen opens /led?w=<base64>  (LedView.vue):

Mount sequence:
  1. Decode ?w= → LedWidget[]
  2. Connect WebSocket (no token), subscribe to all machineIds
  3. GET /api/telemetry/:id/latest  (every 2s poll as WS fallback)
     → telemetryStore.updateSnapshot() → metric/gauge/status cells re-render
  4. GET /api/alerts/events/active  (once, for alarm cell counts)
  5. GET /api/telemetry/:id/daily-count  (for daily-count cells, refresh 60s)

  For each sparkline widget:
  6. GET /api/telemetry/:id/series?timeRange=30m  (seed 30-min history)
     → liveSparkData[widgetId] seeded with 1-min REST buckets
  7. WS onTelemetry('*') → append new points to liveSparkData
                         → trim points older than 30 min
     → SVG sparkline re-renders with live rolling history

Widget types and their live data:
  metric / gauge / status  →  telemetryStore (WS + 2s REST poll)
  alarm                    →  alertStore (REST on mount + WS push)
  sparkline                →  liveSparkData (REST seed + WS append)
  daily-count              →  REST only, refresh 60s

No login required — safe to display on factory floor screens.
```

---

### 2.6 AI Assistant Flow

There are two independent AI surfaces — the Chat Assistant (`POST /ai/chat`) and Ask-Data (`POST /ai/ask`). Both proxy to an OpenAI-compatible provider (production: KKU GenAI, generation model `claude-sonnet-5`, router/verifier model `gpt-5.4-mini`). Full pipeline detail: [`docs/ai-pages.md`](docs/ai-pages.md).

```
User                  Frontend               Backend (Go)          AI Provider (KKU)
 │                       │                      │                      │
 │── Type question ──────►│                      │                      │
 │   "What is the avg     │                      │                      │
 │    weight today?"      │                      │                      │
 │                       │── POST /api/ai/chat   │                      │
 │                       │   { message: "..." } ─►│                      │
 │                       │                      │── ClassifyIntent      │
 │                       │                      │   (router model) ────►│
 │                       │                      │◄── intent ─────────────│
 │                       │                      │                      │
 │                       │                      │── dispatchIntent (Go) │
 │                       │                      │   forces tool_choice  │
 │                       │                      │   by function name    │
 │                       │                      │   e.g. get_telemetry_ │
 │                       │                      │   trend(machineId,    │
 │                       │                      │   field, from, to)    │
 │                       │                      │                      │
 │                       │                      │── execute tool        │
 │                       │                      │   (tool_actions.go)   │
 │                       │                      │── Query PostgreSQL    │
 │                       │                      │◄── {avg: 498.3, ...}  │
 │                       │                      │                      │
 │                       │                      │── generation model    │
 │                       │                      │   composes answer ───►│
 │                       │                      │◄── draft answer ───────│
 │                       │                      │── verify-then-repair  │
 │                       │                      │   (verify.go) ───────►│
 │                       │◄── {role:"assistant",│                      │
 │                       │    content: "The avg │                      │
 │                       │    weight today is   │                      │
 │                       │    498.3 kg"}        │                      │
 │◄── Render in chat UI ─│                      │                      │
```

---

## 3. How to Use the Web Application

### 3.1 Getting Started / Login

1. Open your browser and navigate to the dashboard URL (e.g., `http://localhost:5173`)
2. Enter your credentials:
   - **Default admin:** `admin@acme-foods.com` / `Admin@1234`
3. Click **Sign In**

You will be redirected to the Dashboard List page on successful login.

> Your session lasts 24 hours. You will be automatically logged out after that.

---

### 3.2 Dashboard List

The Dashboard List (`/dashboards`) is your home screen after login.

| Action | How |
|--------|-----|
| View a dashboard | Click the dashboard card |
| Create a new dashboard | Click **"+ New Dashboard"**, enter a name — opens an empty canvas |
| Import a dashboard | Click **"Import"**, select a `.json` file exported from any dashboard |
| Delete a dashboard | Hover the card → click the trash icon (admin/editor only) |

Each card shows the dashboard name and the number of widgets it contains.

---

### 3.3 Dashboard Editor

The Dashboard Editor (`/dashboards/:id`) is a drag-and-drop canvas for building your monitoring view.

#### Adding a Widget
1. Click **"Add Widget"** in the toolbar
2. Select a widget type from the panel:

   | Widget | What it shows | Config fields |
   |--------|---------------|---------------|
   | **Line Chart** | Historical time-series + optional Live Mode | Machine, Field, Color |
   | **Gauge** | Live single-value radial gauge | Machine, Field, Min/Max |
   | **KPI Card** | Live single numeric metric | Machine, Field |
   | **Status Card** | Machine online/offline status | Machine |
   | **Table** | All fields in tabular view, live via WS | Machine |
   | **Alarm Panel** | Active alert events, optionally filtered | Machine (optional) |
   | **Machine Daily Count** | Bar chart of daily reading counts | Machine |

3. Configure the widget: choose a **Machine** and **Field** where required
4. Click **Save** — the widget appears on the canvas

> **Note:** Time Range and Aggregation Period are no longer configurable in the widget modal. Gauge and KPI always show live real-time values. Use Line Chart's built-in date picker or Live Mode for time-range control.

#### Line Chart — Live Mode
- Click the **"⊙ Live"** button in the chart toolbar to activate Live Mode
- Live Mode shows the last **30 minutes** of data as a rolling window
- New WebSocket points are appended to the chart in real-time
- Click **"Exit Live"** to return to the datetime picker

#### Arranging Widgets
- **Drag** a widget by its header to reposition
- **Resize** by dragging the bottom-right corner handle
- Click **Save** in the toolbar to persist the layout

#### Editing a Widget
- Click the **gear icon** on the widget → modify config → Save

#### Deleting a Widget
- Click the **trash icon** on the widget → confirm

#### Export Dashboard as JSON
- Click **"Export"** in the toolbar — downloads `<dashboard-name>.json`
- The file contains the dashboard name, tags, and all widget configs (type, machine, field, layout)
- Use it as a template to recreate the dashboard on any environment

#### Import Dashboard from JSON
- On the Dashboard List page, click **"Import"**
- Select a previously exported `.json` file
- A new dashboard is created with all widgets pre-configured

#### LED Kiosk Link
- Click the **monitor icon** button group in the toolbar
- **Copy link** — copies the kiosk URL to clipboard
- **Open preview** — opens the LED view in a new tab (no login required)

---

### 3.4 Machine Management

The Machine Management page (`/machines`) lets admins and editors manage the IoT devices being monitored.

#### Viewing Machines
- The list shows all machines with their type, status, factory, and production line
- Click a machine row to expand its details and field schema

#### Adding a Machine *(admin/editor)*
1. Click **"+ Add Machine"**
2. Fill in: Name, Type, Factory, Production Line
3. Click **Create**

**Machine Types:**
- `checkweigher` — weight/speed sensors
- `temperature_sensor` — temperature/humidity readings
- `conveyor` — belt speed and motor current
- `vision_camera` — defect detection rate

#### Managing Fields *(admin/editor)*
Each machine has a dynamic field schema. To add/edit fields:
1. Open the machine details
2. Click **"Manage Fields"**
3. Add a field: Key (e.g., `weight`), Unit (e.g., `kg`), Min/Max limits, Thresholds
4. Click **Save Fields**

Fields defined here appear in widget configuration dropdowns.

---

### 3.5 Alerts

The Alerts page (`/alerts`) manages automatic notifications when machine readings cross thresholds.

#### Creating an Alert Rule *(admin/editor)*
1. Click **"+ New Alert"**
2. Configure:
   - **Machine** — which device to watch
   - **Field** — which sensor reading (e.g., `weight`)
   - **Condition** — `gt` (greater than), `lt` (less than), `gte`, `lte`, `eq`
   - **Threshold** — the trigger value (e.g., `500`)
   - **Severity** — `info`, `warning`, `critical`
   - **Cooldown** — minimum seconds between repeated firings (prevents alert spam)
3. Click **Save**

#### Viewing Active Alerts
- The **"Active Events"** tab lists all currently open alert firings
- Columns: Machine, Field, Value, Threshold, Severity, Time

#### Managing Alert Events

| Action | How |
|--------|-----|
| Acknowledge | Click **ACK** — marks that someone is aware of the issue |
| Resolve | Click **Resolve** — marks the issue as fixed |

> Resolved events are moved to history and no longer appear in the Alarm Panel widget.

---

### 3.6 AI Assistant

The AI Assistant page (`/ai`) provides a natural language interface to query your factory data.

#### Starting a Conversation
1. Click **"New Conversation"** or select an existing one from the sidebar
2. Type your question in the chat input

#### Example Questions
```
"What machines are currently online?"
"Show me the average weight for CW-01 over the last 24 hours"
"Are there any active alerts right now?"
"What was the total production count yesterday?"
"Which machine had the most anomalies this week?"
```

#### How it Works
The AI has access to **tools** that query your live database (snake_case, `schema.go` `AllTools()`):
- `get_machines` — lists machines and their current status
- `get_telemetry_series` / `get_telemetry_trend` — retrieves time-series sensor data
- `get_active_alerts` — fetches active alert events
- `get_production_count`, `get_skus`, `show_metric`, `list_dashboards`, plus `preview_*` dashboard-edit tools

An intent router (`gpt-5.4-mini`) forces the correct tool by function name, then the generation model (`claude-sonnet-5`) composes the answer, which is passed through a verify-then-repair pass before returning. See [`docs/ai-pages.md`](docs/ai-pages.md) for the full pipeline. The **Ask-Data** page (`/ai/ask`) is a separate surface for natural-language-to-SQL chart queries.

---

### 3.7 LED Kiosk Mode

The LED Kiosk page (`/led`) is a **public, authentication-free** display designed for large screens on the factory floor.

#### Opening a Kiosk URL
A kiosk URL looks like:
```
http://your-domain.com/led?w=eyJpZHMiOlsiMTIzIiwiNDU2Il19
```

Just open it in a browser — no login is required. The page will:
- Display the configured widgets in a carousel
- Automatically cycle through each widget
- Update live via WebSocket as new telemetry arrives

#### Generating a Kiosk URL
1. Go to the **Dashboard Editor** for any dashboard
2. Click **"Export LED"** in the toolbar
3. Select the widgets you want displayed on the kiosk
4. Copy the generated URL

The URL encodes your widget selection as a base64 string in the `?w=` parameter. Share or bookmark it freely.

> **Tip:** The LED view works well in fullscreen mode (`F11` in most browsers). No keyboard/mouse interaction is needed once the URL is open.

---

## 4. Role Permissions

| Action | Viewer | Editor | Admin |
|--------|--------|--------|-------|
| View dashboards | ✅ | ✅ | ✅ |
| Create/edit/delete dashboards | ❌ | ✅ | ✅ |
| View machines | ✅ | ✅ | ✅ |
| Add/edit machines & fields | ❌ | ✅ | ✅ |
| Delete machines | ❌ | ❌ | ✅ |
| View alerts | ✅ | ✅ | ✅ |
| Create/edit alert rules | ❌ | ✅ | ✅ |
| Delete alert rules | ❌ | ❌ | ✅ |
| Acknowledge/resolve alert events | ✅ | ✅ | ✅ |
| Use AI Assistant | ✅ | ✅ | ✅ |
| View LED kiosk | ✅ (no auth) | ✅ | ✅ |
| Manage users | ❌ | ❌ | ✅ |

---

## 5. API Quick Reference

All REST endpoints are prefixed with `/api`. Requests require `Authorization: Bearer <token>` except where noted.

### Authentication
```
POST /api/auth/login       Body: {email, password}  →  {token, user}
GET  /api/auth/me          →  current user
```

### Machines
```
GET    /api/machines
POST   /api/machines                    (admin/editor)
GET    /api/machines/:id
PATCH  /api/machines/:id                (admin/editor)
DELETE /api/machines/:id                (admin)
GET    /api/machines/:id/fields
PUT    /api/machines/:id/fields         (admin/editor)
```

### Telemetry
```
GET  /api/telemetry/latest?ids=id1,id2          (no auth)
GET  /api/telemetry/:machineId/latest           (no auth)
GET  /api/telemetry/:machineId/series?field=weight&from=ISO&to=ISO
GET  /api/telemetry/:machineId/aggregate?field=weight&interval=1h
GET  /api/telemetry/:machineId/daily-count
POST /api/telemetry/:machineId/ingest           Body: {timestamp, data: {...}}
```

### Dashboards & Widgets
```
GET    /api/dashboards
POST   /api/dashboards                  Body: {name}
GET    /api/dashboards/:id
PATCH  /api/dashboards/:id
DELETE /api/dashboards/:id
POST   /api/dashboards/:id/widgets      Body: {type, config}
PATCH  /api/dashboards/:id/layout       Body: {widgets: [{id,x,y,w,h}]}
PATCH  /api/dashboards/:id/widgets/:wid Body: {config}
DELETE /api/dashboards/:id/widgets/:wid
```

### Alerts
```
GET    /api/alerts
GET    /api/alerts/events/active                (no auth — for kiosk)
POST   /api/alerts                      (admin/editor)
PATCH  /api/alerts/:id                  (admin/editor)
DELETE /api/alerts/:id                  (admin)
PATCH  /api/alerts/events/:id/acknowledge
PATCH  /api/alerts/events/:id/resolve
```

### WebSocket
```
ws://your-domain.com/ws?token=<jwt>

Subscribe:    { "type": "subscribe",   "payload": { "machineIds": ["id1"] } }
Unsubscribe:  { "type": "unsubscribe", "payload": { "machineIds": ["id1"] } }
Receive:      { "type": "telemetry" | "alert" | "machine_status", "payload": {...} }
```

---

---

## 6. Backfill & Mock Data

The backfill script (`backend/cmd/backfill/main.go`) generates historical data for all widget types:

| Widget | Data generated |
|---|---|
| Line Chart / Gauge / KPI / Table / Status | ~2.3M `telemetry_raw` rows — 4 machines × 405 days × 1 min/point |
| Daily Count | Same `telemetry_raw` rows (aggregated by day on query) |
| Alarm Panel | 16 `alert_events` — 13 resolved (historical) + 3 open (recent) |

**Machines seeded:**
| ID suffix | Name | Fields |
|---|---|---|
| `...0005` | CW-01 Checkweigher | weight, speed, throughput, rejects, status_code |
| `...0006` | TS-01 Temp Sensor | temp, humidity, dew_point |
| `...0007` | CB-01 Conveyor Belt | speed, load, rpm, vibration |
| `...0008` | VC-01 Vision Camera | defect_rate, confidence, inspected, passed, failed |

**Alert rules seeded (hardcoded IDs):**
| ID suffix | Machine | Condition | Severity |
|---|---|---|---|
| `...0011` | CW-01 | weight > 510 g | warning |
| `...0012` | CW-01 | weight < 490 g | critical |
| `...0013` | TS-01 | temp > 35 °C | critical |

Run backfill:
```bash
docker compose exec backend ./backfill
```

---

*Last updated: 2026-05-29*
