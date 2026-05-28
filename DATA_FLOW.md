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
│   /api/*  →  backend:4000   |   /ws  →  backend:4001             │
└──────────────────┬──────────────────────┬────────────────────────┘
                   │                      │
   ┌───────────────▼──────────┐  ┌────────▼───────────────┐
   │   Go Fiber REST API      │  │  Gorilla WebSocket Hub  │
   │        :4000             │  │        :4001            │
   │                          │  │                         │
   │  Auth / Machines /       │  │  Telemetry broadcasts   │
   │  Dashboards / Alerts /   │  │  Alert events           │
   │  Telemetry / AI          │  │  Machine status changes │
   └───────────┬──────────────┘  └────────┬───────────────┘
               │                          │
               │    ┌─────────────────────┘
               │    │
   ┌───────────▼────▼──────────────────────┐
   │           Go Business Logic           │
   │  ┌──────────────┐  ┌───────────────┐  │
   │  │  Simulator   │  │  Alert Engine │  │
   │  │  (60s tick)  │  │  (on ingest)  │  │
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
                        ┌─── Every 60 seconds ───┐
                        │                        │
                  Simulator (Go)          New telemetry ingested
                  generates synthetic     via POST /api/telemetry/:id/ingest
                  readings for all        (external device or load test)
                  machines                │
                        │                │
                        └────────┬───────┘
                                 │
                    ┌────────────▼────────────────────┐
                    │  INSERT INTO telemetry_raw       │
                    │  (machine_id, timestamp, data)   │
                    │                                  │
                    │  Evaluate alert rules:           │
                    │    field > threshold? → fire     │
                    │    INSERT INTO alert_events      │
                    └───────────┬─────────────────────┘
                                │
                    ┌───────────▼─────────────────────┐
                    │  WebSocket Hub broadcasts to     │
                    │  ALL connected clients:          │
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

**Widget Polling vs Real-Time:**

| Widget Type        | Data Source              | Update Method    |
|--------------------|--------------------------|------------------|
| Line Chart         | `/telemetry/:id/series`  | Poll on load + WS push |
| Gauge              | `/telemetry/:id/latest`  | WS push (live)   |
| KPI Card           | `/telemetry/:id/latest`  | WS push (live)   |
| Status Card        | `/telemetry/:id/latest`  | WS push (live)   |
| Table              | `/telemetry/:id/series`  | Poll on load     |
| Alarm Panel        | `/alerts/events/active`  | WS push (live)   |
| Machine Daily Count| `/telemetry/:id/daily-count` | Poll on load |

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
                             Redirect to /dashboards/:id (editor)

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

1. On DashboardEditorPage, click "Export LED"
   (composable: useLedExport.ts)

2. Frontend serializes selected widgets:
   widgetIds[] → base64 encode → ?w=<base64string>

3. Share URL:  https://your-domain.com/led?w=eyJpZHMiOlsiMTIzIiwiNDU2Il19

─────────────────────────────────────────────────────────────

Kiosk Screen opens /led?w=<base64>:

┌───────────────────────────────────────────────┐
│   LedViewPage (NO auth required)              │
│                                               │
│   1. Parse ?w= → decode widgetIds             │
│                                               │
│   2. GET /api/telemetry/latest?ids=...        │
│      (public endpoint, no JWT needed)         │
│                                               │
│   3. GET /api/alerts/events/active            │
│      (public endpoint)                        │
│                                               │
│   4. Connect WebSocket (no token)             │
│      Subscribe to machine IDs                 │
│                                               │
│   5. Carousel auto-cycles through widgets     │
│      (MachineDailyCountWidget, GaugeWidget,   │
│       KpiCardWidget, AlarmPanelWidget, etc.)  │
│                                               │
│   6. Live WS updates refresh widget data      │
└───────────────────────────────────────────────┘

No login required — safe to display on factory floor screens.
```

---

### 2.6 AI Assistant Flow

```
User                  Frontend               Backend              External LLM
 │                       │                      │                      │
 │── Type question ──────►│                      │                      │
 │   "What is the avg     │                      │                      │
 │    weight today?"      │                      │                      │
 │                       │── POST /ai/conversations/:id/messages        │
 │                       │   { role: "user", content: "..." } ─────────►│
 │                       │                      │                      │
 │                       │                      │── Send to LLM with   │
 │                       │                      │   tool definitions   │
 │                       │                      │   (getMachines,      │
 │                       │                      │    getTelemetry,     │
 │                       │                      │    getAlerts, ...)   │
 │                       │                      │◄── LLM response:     │
 │                       │                      │   tool_call:         │
 │                       │                      │   getTelemetry(      │
 │                       │                      │     machineId, field,│
 │                       │                      │     from, to)        │
 │                       │                      │                      │
 │                       │                      │── POST /api/ai/tools/execute
 │                       │                      │   {toolName, params} │
 │                       │                      │── Query PostgreSQL   │
 │                       │                      │◄── {avg: 498.3, ...} │
 │                       │                      │                      │
 │                       │                      │── Send tool result   │
 │                       │                      │   back to LLM ──────►│
 │                       │                      │◄── Final answer ─────│
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
| Create a new dashboard | Click the **"+ New Dashboard"** button, enter a name |
| Delete a dashboard | Open the dashboard → click Delete (admin/editor only) |

Each card shows the dashboard name and the number of widgets it contains.

---

### 3.3 Dashboard Editor

The Dashboard Editor (`/dashboards/:id`) is a drag-and-drop canvas for building your monitoring view.

#### Adding a Widget
1. Click **"Add Widget"** in the toolbar
2. Select a widget type from the panel:

   | Widget | What it shows |
   |--------|---------------|
   | **Line Chart** | Historical time-series for a selected field |
   | **Gauge** | Live single-value radial gauge |
   | **KPI Card** | Live single numeric metric |
   | **Status Card** | Machine online/offline status |
   | **Table** | All fields in tabular view |
   | **Alarm Panel** | List of active alert events |
   | **Machine Daily Count** | Bar chart of daily reading counts |

3. Configure the widget: choose a **Machine**, **Field**, and any display options
4. Click **Save** — the widget appears on the canvas

#### Arranging Widgets
- **Drag** a widget by its header to reposition
- **Resize** by dragging the bottom-right corner handle
- The layout is saved automatically after you stop dragging

#### Editing a Widget
- Click the **gear icon** on the widget → modify config → Save

#### Deleting a Widget
- Click the **trash icon** on the widget → confirm

#### Exporting to LED Kiosk
- Click **"Export LED"** in the toolbar
- Select which widgets to include in the kiosk view
- Copy the generated URL and open it on any screen (no login required)

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
The AI has access to **tools** that query your live database:
- `getMachines` — lists machines and their current status
- `getTelemetry` — retrieves time-series sensor data
- `getAlerts` — fetches active alert events
- `getAggregates` — retrieves statistical summaries (avg, min, max)

The AI decides which tools to call based on your question, fetches real data, and returns a natural language answer grounded in your actual factory metrics.

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

*Last updated: 2026-05-28*
