# IotVision — Technical Documentation & Onboarding Guide

> **Role:** Senior Software Engineer / Tech Lead  
> **Last Updated:** 2026-05-26  
> **Project Status:** Active MVP

---

## Table of Contents

1. [Project Overview & Tech Stack](#1-project-overview--tech-stack)
2. [Core Domain Knowledge & Know-How](#2-core-domain-knowledge--know-how)
3. [Architecture & Data Flow](#3-architecture--data-flow)
4. [File-by-File Breakdown](#4-file-by-file-breakdown)
5. [Future Improvements](#5-future-improvements)

---

## 1. Project Overview & Tech Stack

### What Is This Project?

**IotVision** is a production-grade, full-stack Industrial IoT monitoring dashboard. It is designed for factory floors that need real-time visibility into machine health, production metrics, and alerts — displayed across different screen types including a standard browser, a mobile device, and a small **640×320 industrial LED panel**.

The core objectives are:

- **Real-time telemetry streaming** — ingest and display live sensor data (weight, temperature, speed, defect rate, etc.) from industrial machines without page refreshes.
- **Configurable dashboards** — operators can build their own dashboards by dragging and dropping widget types (KPI cards, gauges, line charts, status cards, alarm panels) onto a grid.
- **Alert management** — define threshold-based rules per machine field; get notified in real-time when a rule is violated.
- **LED screen mode** — when the browser runs on a 640×320 display, automatically switch to a full-screen KPI carousel optimised for reading from a distance.
- **AI assistant** — an integrated chat interface backed by an LLM tool-execution layer.

---

### Tech Stack

#### Frontend

| Technology | Version | Why It Was Chosen |
|---|---|---|
| **Vue 3** (Composition API) | `^3.4` | Reactive, component-based UI. Composition API enables clean, reusable logic extraction into composables — critical for complex telemetry wiring. |
| **TypeScript** | `^5.5` | Full type safety across stores, API responses, and component props. Catches data-shape mismatches early (especially important for dynamic telemetry payloads). |
| **Vite** | `^5.3` | Near-instant dev server HMR and tree-shaking build — much faster than Webpack/CRA for a project with many lazy-loaded pages. |
| **Pinia** | `^2.1` | Official Vue state management. Setup-store style (`defineStore(() => {...})`) reads like a composable, making the stores easy to test and reason about. |
| **Vue Router 4** | `^4.3` | SPA routing with navigation guards for auth. All page components are lazy-loaded (`import()`). |
| **Axios** | `^1.7` | HTTP client wrapped in a singleton `ApiService` class with request/response interceptors for token injection and 401 redirect. |
| **ECharts + vue-echarts** | `^5.5 / ^6.7` | Industrial-grade charting library — handles large time-series datasets, supports canvas rendering, and provides gauge + line + bar charts needed for the dashboard widgets. Only required chart types are registered to keep bundle size small. |
| **GridStack.js** | `^10.3` | Drag-and-drop, resize-enabled grid layout used for the dashboard editor. The key challenge: GridStack controls the DOM directly, so each widget is mounted as its own `createApp()` instance and manually injected into the grid cell. |
| **Tailwind CSS** | `^3.4` | Utility-first CSS with a custom dark-theme colour palette (`surface-100`, `primary-500`, `accent-cyan`). No runtime CSS — all purged at build time. |
| **Lucide Vue Next** | `^0.395` | Consistent, tree-shakeable icon set. |
| **@vueuse/core** | `^10.11` | Used for browser utility composables. |

#### Backend

| Technology | Version | Why It Was Chosen |
|---|---|---|
| **Node.js + Express** | `^4.19` | Lightweight, well-understood HTTP server. The app is I/O bound (DB + WebSocket), so Node's event loop is a natural fit. |
| **TypeScript** | `^5.5` | Shared type definitions between backend modules. Prisma client is fully typed. |
| **Prisma ORM** | `^5.14` | Type-safe database client generated from the schema. Handles migrations, DB push, and seeding. The Prisma Studio UI is exposed on port 5555 for direct DB inspection. |
| **ws** | `^8.17` | Raw, low-overhead WebSocket server — chosen over Socket.IO to avoid the long-polling fallback overhead and keep the protocol simple for machine-subscription messages. |
| **jsonwebtoken + bcryptjs** | — | Standard JWT auth with bcrypt password hashing. |
| **Zod** | `^3.23` | Runtime schema validation for incoming request bodies — provides typed, safe parsing at API boundaries. |
| **helmet + express-rate-limit** | — | Security hardening: HTTP security headers and rate limiting on API routes. |
| **tsx** | `^4.16` | Runs TypeScript directly without a build step in development (`tsx watch`). |

#### Database & Infrastructure

| Technology | Why It Was Chosen |
|---|---|
| **PostgreSQL 16 + TimescaleDB** | Relational DB for all entity data (users, machines, dashboards, alerts) + the TimescaleDB extension converts the `telemetry_readings` table into a **hypertable**, enabling automatic time-based partitioning, and compression of time-series data at scale. |
| **Redis 7** | Provisioned for sessions, pub/sub, and caching. Currently reserved for future horizontal scaling (e.g., sharing WebSocket subscriptions across multiple backend instances). |
| **Docker + Docker Compose** | All five services (DB, Redis, Backend, Frontend, pgAdmin) are defined in a single `docker-compose.yml` for one-command local setup and consistent production deployments. |

---

## 2. Core Domain Knowledge & Know-How

### 2.1 Machine & Field Model

Everything in the system revolves around the `Machine` entity. Each machine has a `type`:

| Type | What It Measures |
|---|---|
| `checkweigher` | `weight`, `speed`, `throughput`, `rejects`, `status_code` |
| `temperature_sensor` | `temp`, `humidity`, `dew_point` |
| `conveyor` | `speed`, `load`, `rpm`, `vibration` |
| `vision_camera` | `defect_rate`, `confidence`, `inspected`, `passed`, `failed` |

Each field is described by a `MachineField` record which carries:
- `threshold` — the target/nominal value (used for OEE-style achievement % in LED mode).
- `upperLimit` / `lowerLimit` — ±10% bounds. Telemetry outside these bounds turns red/amber in the UI.
- `isKey` — marks the most important fields for a machine (used by LED mode to automatically pick what to display).
- `precision` — how many decimal places to round to in the UI.

### 2.2 Real-Time Telemetry Pipeline

The data flow for live sensor readings:

```
[Simulator / Real Sensor]
       │ (1 tick / second)
       ▼
[Backend: TelemetrySimulator.processMachine()]
       │  1. generates sine-wave value
       │  2. persists to TimescaleDB hypertable (every N ticks)
       │  3. evaluates alert rules
       ▼
[WsGateway.broadcastTelemetry()]
       │  sends JSON to all subscribed WebSocket clients
       ▼
[Frontend: wsService.onTelemetry('*', handler)]
       │  parsed in useWebSocket composable
       ▼
[telemetryStore.updateSnapshot(machineId, timestamp, data)]
       │  reactive — all computed properties referencing this update instantly
       ▼
[Widget / LEDCarousel reads telemetryStore.getFieldValue()]
       │  Vue reactivity re-renders the value
       ▼
[User sees the updated number on screen]
```

### 2.3 Alert Rule Evaluation

Alert rules are stored per-machine in the `alerts` table. Each rule defines a `field`, `condition` (gt/lt/eq/between/outside), and `threshold`. The `AlertService.evaluateTelemetry()` method runs on every simulator tick:

- It loads all active rules for the machine.
- Evaluates each rule's condition against the current field value.
- If triggered **and** not in cooldown (`cooldownSec`), it creates an `AlertEvent` in the DB and calls `WsGateway.broadcastAlert()` — which immediately pushes the alert to all connected clients.
- On the frontend, `useWebSocket` catches the alert message and calls `alertStore.addLiveAlert()`.

### 2.4 GridStack + Vue Integration (Key Technical Challenge)

The dashboard editor uses **GridStack.js** for drag-and-drop widget layout. GridStack manages its own DOM nodes — it is not a Vue component. This creates a fundamental conflict with Vue's virtual DOM.

The solution implemented in `GridStackCanvas.vue`:

1. GridStack creates a DOM cell for each widget position.
2. For each cell, a **separate Vue 3 app** is created with `createApp(WidgetWrapper, props)` and **mounted directly into the cell's DOM node**.
3. The parent Pinia instance is passed to each child app via `app.use(pinia)` — this ensures all widget mini-apps share the same stores (same telemetry data, same alert state).
4. A **fingerprint** (`JSON.stringify` of non-layout widget props) tracks whether a widget's config has changed. Only if the fingerprint changes is the widget app unmounted and remounted — preventing unnecessary re-renders on simple drag/resize operations.
5. On unmount, all child Vue apps are explicitly cleaned up with `app.unmount()` to prevent memory leaks.

### 2.5 Telemetry Data — Two-Track Strategy

A subtle but important design decision in `useTelemetry.ts`:

- **`useMachineTelemetry`** (for KPI cards, gauges): subscribes to WebSocket, writes every incoming data point into the Pinia store's rolling history (up to 300 points). Optimised for live values.
- **`useFieldSeries`** (for line charts): intentionally uses **only API bucket data** and refreshes every 60 seconds. It does **not** merge raw WebSocket points into the chart.

**Why?** KPI/gauge widgets poll at ~1 Hz and write raw 1-second points into the shared store. If a line chart tried to merge these raw points with 30-minute-bucketed API data, it would create hundreds of clustered raw points at the right edge of the chart, distorting its shape. The two-track approach keeps the chart clean.

### 2.6 LED Screen Mode

The `useScreenMode` composable detects a 640×320 LED panel by checking window dimensions on every `resize` event:

```
width ≤ 650 AND height ≤ 350  →  'led'
width < 768                    →  'mobile'
otherwise                      →  'desktop'
```

When `isLED` is true, `AppLayout.vue` replaces the entire layout with `<LEDCarousel>`, which:
- Reads `dashboardStore.widgets` (mirrors the current dashboard — 6 widgets = 6 slides).
- Displays one metric per slide with large glowing text, a status indicator, and a target achievement %.
- Supports keyboard/presenter remote controls (`ArrowLeft/Right`, `PageUp/Down`, `Space` to lock/unlock auto-advance).
- Pauses the progress bar CSS animation when locked (`animation-play-state: paused`).

---

## 3. Architecture & Data Flow

### 3.1 High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        FRONTEND  (Vue 3 SPA)                            │
│                                                                         │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌────────────┐  │
│  │  Dashboard   │  │  Machines    │  │  Alerts      │  │  AI Chat   │  │
│  │  Editor Page │  │  Mgmt Page   │  │  Page        │  │  Page      │  │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘  └─────┬──────┘  │
│         │                 │                  │                │          │
│  ┌──────▼─────────────────▼──────────────────▼────────────────▼──────┐  │
│  │                     Pinia Stores (Reactive State)                  │  │
│  │   auth ─ machine ─ telemetry ─ dashboard ─ alert                  │  │
│  └──────────────┬─────────────────────────────────────────────────────┘  │
│                 │                                                         │
│  ┌──────────────▼──────────────┐  ┌──────────────────────────────────┐  │
│  │   ApiService (axios)         │  │  WebSocketService (ws)           │  │
│  │   REST: :4000/api/*          │  │  WS:  :4001                      │  │
│  └──────────────┬──────────────┘  └──────────────┬───────────────────┘  │
└─────────────────┼───────────────────────────────────────────────────────┘
                  │ HTTP                             │ WebSocket
┌─────────────────▼─────────────────────────────────▼───────────────────┐
│                        BACKEND  (Express + ws)                         │
│                                                                        │
│  ┌───────────────────────────────────────────────────────────────┐    │
│  │  Express REST API  (:4000)                                    │    │
│  │  /api/auth  /api/machines  /api/telemetry                     │    │
│  │  /api/dashboards  /api/alerts  /api/ai                        │    │
│  └─────────────────────────────┬─────────────────────────────────┘    │
│                                 │                                      │
│  ┌──────────────────────────────▼──────────────────────────────────┐  │
│  │  Module Structure: controller → service → repository → Prisma   │  │
│  └──────────────────────────────┬──────────────────────────────────┘  │
│                                 │                                      │
│  ┌──────────────────────────────▼──────────────────────────────────┐  │
│  │  WsGateway  (:4001)                                             │  │
│  │  - Manages connected clients (Map<id, ExtendedWebSocket>)       │  │
│  │  - Handles subscribe/unsubscribe per machineId                  │  │
│  │  - Broadcasts telemetry/alert/machine_status messages           │  │
│  │  - Ping/pong heartbeat every 30s to detect dead connections     │  │
│  └──────────────────────────────┬──────────────────────────────────┘  │
│                                 │                                      │
│  ┌──────────────────────────────▼──────────────────────────────────┐  │
│  │  TelemetrySimulator                                             │  │
│  │  - Generates sine-wave data per machine type (1 tick/sec)       │  │
│  │  - Persists to TimescaleDB every N ticks                        │  │
│  │  - Evaluates alert rules → broadcasts triggered alerts          │  │
│  └──────────────────────────────┬──────────────────────────────────┘  │
└─────────────────────────────────┼──────────────────────────────────────┘
                                  │ Prisma ORM
┌─────────────────────────────────▼──────────────────────────────────────┐
│                  PostgreSQL 16 + TimescaleDB                            │
│  Tables: users, organizations, machines, machine_fields                 │
│          production_lines, factories, dashboards, dashboard_widgets     │
│          alerts, alert_events                                           │
│  Hypertable: telemetry_readings (time-partitioned, compressed)          │
└────────────────────────────────────────────────────────────────────────┘
```

### 3.2 State Management Pattern

All application state lives in five Pinia stores. Each follows the **Setup Store** pattern:

```
auth.store.ts      →  JWT token, user profile, login/logout, WS connection lifecycle
machine.store.ts   →  machines[], productionLines[], factories[], CRUD actions
telemetry.store.ts →  snapshots{}, history{} — updated by WebSocket in real-time
dashboard.store.ts →  dashboards[], currentDashboard, widget CRUD, layout persistence
alert.store.ts     →  alerts[], activeEvents[], liveAlerts[] (capped at 50)
```

**Key invariant:** `telemetryStore` is the only store that is **never fetched from REST** directly by components. It is always populated by:
1. `useMachineTelemetry` composable pre-loading 30 minutes of history from API on mount.
2. `useWebSocket` composable writing every incoming WebSocket message into the store.

This means any component that reads `telemetryStore.getFieldValue()` is automatically reactive to live data.

### 3.3 Authentication & Session Flow

```
1. User submits login form
2. auth.store → api.login() → POST /api/auth/login
3. Server returns { token, user }
4. auth.store stores token in localStorage AND calls wsService.connect(token)
5. WsGateway verifies JWT from query string on WebSocket connection
6. api.service.ts interceptor reads localStorage token on every request
7. On 401 response → localStorage cleared → redirect to /login
8. On app reload → auth.store reads token from localStorage → calls loadProfile()
```

### 3.4 Component Communication Patterns

| Pattern | Used For |
|---|---|
| **Props / Emits** | Parent ↔ child for layout state (sidebar open/close, widget edit events) |
| **Pinia Store** | Cross-component shared state (telemetry values, alert counts) |
| **Composables** | Reusable async logic with lifecycle management (`useMachineTelemetry`, `useFieldSeries`) |
| **createApp injection** | GridStack widget isolation — each widget is a mini Vue app sharing the parent Pinia |
| **WebSocket events** | Server → client push for telemetry, alerts, machine status changes |
| **window.addEventListener** | LED carousel keyboard/presenter remote navigation |

---

## 4. File-by-File Breakdown

### Directory Structure

```
Project-Dashboard-CPF/
├── docker-compose.yml          # All 5 services: DB, Redis, Backend, Frontend, pgAdmin
│
├── frontend/
│   ├── package.json
│   ├── src/
│   │   ├── main.ts                         # App bootstrap: Vue, Pinia, ECharts registration
│   │   ├── App.vue                         # Root component: router-view + AppLayout selector
│   │   │
│   │   ├── router/
│   │   │   └── index.ts                    # Route definitions + auth navigation guard
│   │   │
│   │   ├── layouts/
│   │   │   └── AppLayout.vue               # Shell: sidebar + topbar + LED mode gate
│   │   │
│   │   ├── pages/
│   │   │   ├── LoginPage.vue               # JWT login form
│   │   │   ├── DashboardListPage.vue       # Dashboard CRUD list
│   │   │   ├── DashboardEditorPage.vue     # Main grid editor with widget toolbox
│   │   │   ├── MachineManagementPage.vue   # Machine + field configuration
│   │   │   ├── AlertsPage.vue              # Alert rules + active events
│   │   │   └── AIAssistantPage.vue         # LLM chat with tool execution
│   │   │
│   │   ├── components/
│   │   │   ├── layout/
│   │   │   │   ├── Sidebar.vue             # Nav links, user info, sign out
│   │   │   │   └── TopBar.vue              # Sidebar toggle, clock, WS status, alert bell
│   │   │   │
│   │   │   ├── dashboard/
│   │   │   │   ├── GridStackCanvas.vue     # GridStack + Vue mini-app bridge
│   │   │   │   ├── WidgetToolbox.vue       # Drag-source palette of widget types
│   │   │   │   └── WidgetConfigModal.vue   # Edit widget: machine, field, time range, etc.
│   │   │   │
│   │   │   ├── widgets/
│   │   │   │   ├── WidgetWrapper.vue       # Container: title bar, edit/remove buttons
│   │   │   │   ├── KpiCardWidget.vue       # Single large metric with sparkline
│   │   │   │   ├── GaugeWidget.vue         # ECharts gauge with min/max/threshold
│   │   │   │   ├── LineChartWidget.vue     # ECharts line chart with time range selector
│   │   │   │   ├── StatusCardWidget.vue    # Machine online/offline/error status
│   │   │   │   ├── AlarmPanelWidget.vue    # Live alert event feed
│   │   │   │   ├── TableWidget.vue         # Tabular telemetry display
│   │   │   │   └── MachineDailyCountWidget.vue  # Bar chart of daily production count
│   │   │   │
│   │   │   └── led/
│   │   │       └── LEDCarousel.vue         # Full-screen KPI carousel for 640×320 displays
│   │   │
│   │   ├── stores/
│   │   │   ├── auth.store.ts               # Session, JWT, WS lifecycle
│   │   │   ├── machine.store.ts            # Machine list + status updates
│   │   │   ├── telemetry.store.ts          # Live snapshots + rolling history (300pts)
│   │   │   ├── dashboard.store.ts          # Dashboards + widget CRUD
│   │   │   └── alert.store.ts              # Alert rules, active events, live stream
│   │   │
│   │   ├── composables/
│   │   │   ├── useWebSocket.ts             # Global WS listener → writes to all stores
│   │   │   ├── useTelemetry.ts             # Three composables: machine telemetry, field series, aggregated value
│   │   │   └── useScreenMode.ts            # Detects led / mobile / desktop based on window size
│   │   │
│   │   ├── services/
│   │   │   ├── api.service.ts              # Axios singleton: all REST endpoints + interceptors
│   │   │   └── ws.service.ts               # WebSocket singleton: connect/subscribe/dispatch/reconnect
│   │   │
│   │   └── types/
│   │       └── index.ts                    # All TypeScript interfaces (shared frontend contracts)
│   │
└── backend/
    ├── package.json
    ├── prisma/
    │   ├── schema.prisma                   # DB schema: all models + relations
    │   ├── seed.ts                         # Seed: org, users, machines, fields, alerts, dashboard
    │   └── backfill.ts                     # Backfill historical telemetry for charts
    │
    └── src/
        ├── index.ts                        # Bootstrap: DB connect, HTTP server, WsGateway, Simulator
        ├── app.ts                          # Express app factory: middleware, routes
        │
        ├── config/
        │   ├── database.ts                 # Prisma client + ensureHypertable()
        │   └── env.ts                      # Zod-validated environment variables
        │
        ├── middleware/
        │   ├── auth.ts                     # JWT verify middleware (requireAuth)
        │   └── error.ts                    # Global error handler → { success: false, error: {...} }
        │
        ├── modules/
        │   ├── auth/                       # login, /me endpoint, bcrypt compare
        │   ├── machines/                   # CRUD machines, production lines, factories, fields
        │   ├── telemetry/                  # latest, series, aggregate, daily-count endpoints
        │   ├── dashboards/                 # Dashboard + widget CRUD, bulk layout update
        │   └── alerts/                     # Alert rules CRUD, event acknowledge/resolve
        │
        ├── ai-tools/
        │   ├── ai-tools.service.ts         # Registers tools, executes them against real DB data
        │   ├── ai-tools.controller.ts
        │   └── ai-tools.routes.ts
        │
        ├── websocket/
        │   ├── ws.gateway.ts               # WebSocket server: client registry, pub/sub, heartbeat
        │   └── ws.types.ts                 # ExtendedWebSocket interface (adds id, subscribedMachines)
        │
        └── telemetry/
            └── simulator.ts               # Sine-wave data generator per machine type
```

### Key File Responsibilities (Detail)

#### `frontend/src/services/ws.service.ts`
A singleton `WebSocketService` class. Responsibilities:
- Connects to the WS server with the JWT token in the query string.
- Implements **exponential backoff reconnection** (starts at 2s, max 30s) — critical for maintaining live data after network interruptions.
- Maintains separate `Map`/`Set` collections of handler callbacks for telemetry (per `machineId`), alerts, status changes, connect, and disconnect events.
- Supports wildcard `'*'` subscription: `onTelemetry('*', handler)` receives messages for all machines.
- Returns an `off()` unsubscribe function from each `on*` method for clean teardown.

#### `frontend/src/services/api.service.ts`
A singleton `ApiService` class wrapping Axios. Responsibilities:
- Injects `Authorization: Bearer <token>` on every request via a request interceptor.
- On 401 response, clears the token and hard-redirects to `/login` — no infinite loop risk.
- Unwraps the `ApiResponse<T>` envelope, returning `data.data` directly so callers receive typed objects.
- All 30+ API methods are defined here, grouped by domain (auth, machines, telemetry, dashboards, alerts, AI).

#### `frontend/src/components/dashboard/GridStackCanvas.vue`
The most technically complex component in the frontend. It bridges GridStack's imperative DOM API with Vue's declarative component model:
- On mount: initialises a 12-column GridStack grid and mounts a `WidgetWrapper` Vue app into each cell.
- On `props.widgets` change (deep watch): diffs old vs new widget arrays, removes stale apps, adds new ones, and fingerprint-checks existing ones for config-only changes.
- On unmount: calls `app.unmount()` for every child app and `grid.destroy()` to prevent leaks.

#### `backend/src/websocket/ws.gateway.ts`
The real-time broadcast hub. Key design details:
- Each connected client is stored as an `ExtendedWebSocket` with a UUID, a `subscribedMachines` Set, and an `isAlive` flag.
- `broadcastTelemetry()` filters by subscription — a client only receives data for machines it has subscribed to (or all machines if its set is empty).
- `broadcastAlert()` and `broadcastMachineStatus()` broadcast to **all** connected clients (no filtering).
- A 30-second ping/pong loop detects and terminates zombie connections.

#### `backend/src/telemetry/simulator.ts`
The development data engine. Key design details:
- Generates sine-wave values: `threshold ± 10% amplitude` with a small noise term.
- Each machine type has its own generator function. Vision camera also maintains cumulative counters (`vcState`) for `inspected/passed/failed`.
- Controlled by `SIMULATOR_ENABLED=true` env var — disabled in production.
- Runs `AlertService.evaluateTelemetry()` on every tick, so alert events are triggered by simulated out-of-range values.

---

## 5. Future Improvements

### 5.1 Redis is Provisioned but Unused

**Current state:** Redis is in `docker-compose.yml` and `backend` reads `REDIS_URL`, but no backend code actually connects to it.

**Technical debt:** If the backend is ever horizontally scaled (multiple instances), WebSocket subscriptions are stored in-process (`Map<string, ExtendedWebSocket>`). Two clients connecting to different instances would not receive each other's broadcasts.

**Recommendation:** Implement a Redis pub/sub adapter for `WsGateway`. When `broadcastTelemetry()` is called on instance A, it publishes to a Redis channel; all instances subscribe to that channel and forward to their local clients. This is the standard pattern for scaling stateful WebSocket servers.

---

### 5.2 Widget Config is a Loose `Record<string, unknown>`

**Current state:** `WidgetConfig` in `types/index.ts` has a typed subset of known fields (`field`, `timeRange`, `color`, etc.) but ends with `[key: string]: unknown`. This means unknown or misspelled config keys silently pass through to the DB.

**Technical debt:** There is no runtime validation when a widget is saved. A typo in `config.timeRange` (e.g., `"1Hour"` instead of `"1h"`) would be stored silently and only fail at render time.

**Recommendation:** Define a discriminated union type per widget type:
```ts
type WidgetConfig =
  | { widgetType: 'kpi-card'; field: string; aggregationPeriod: AggregationPeriod; unit?: string }
  | { widgetType: 'gauge';    field: string; min: number; max: number }
  | { widgetType: 'alarm-panel'; severities: AlertSeverity[] }
  // ...
```
Add a Zod schema on the backend `addWidget` / `updateWidget` endpoints to validate the config shape at the API boundary.

---

### 5.3 Telemetry Store Has No Eviction for Disconnected Machines

**Current state:** `telemetryStore.history` stores up to 300 data points per field per machine (`MAX_HISTORY = 300`). However, if a machine goes offline or is deleted, its history and snapshot entries are never removed from the store.

**Technical debt:** In a long-running session with many machines cycling online/offline, the store gradually accumulates stale entries. On a low-memory device (like the 640×320 LED panel), this could become a problem.

**Recommendation:** Subscribe to `machine_status` events in `useMachineStore`. When a machine transitions to `offline` and no widget is currently subscribed to it, call `telemetryStore.clearHistory(machineId)` after a grace period (e.g., 5 minutes). This keeps the store bounded to actively monitored machines.
