# IotVision — Technical Documentation & Onboarding Guide

> **Role:** Senior Software Engineer / Tech Lead  
> **Last Updated:** 2026-05-28  
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
- **AI assistant** — two backend-implemented surfaces: a conversational Chat Assistant (`POST /api/ai/chat`) with a live tool-execution layer, and an Ask-Data page (`POST /api/ai/ask`) that turns natural language into charts. See [`docs/ai-pages.md`](docs/ai-pages.md) for the full pipeline.

---

### Tech Stack

#### Frontend

| Technology | Version | Why It Was Chosen |
|---|---|---|
| **Vue 3** (Composition API) | `^3.4` | Reactive, component-based UI. Composition API enables clean, reusable logic extraction into composables — critical for complex telemetry wiring. |
| **TypeScript** | `^5.5` | Full type safety across stores, API responses, and component props. Catches data-shape mismatches early (especially important for dynamic telemetry payloads). |
| **Vite** | `^5.3` | Near-instant dev server HMR and tree-shaking build. |
| **Pinia** | `^2.1` | Official Vue state management. Setup-store style (`defineStore(() => {...})`) reads like a composable. |
| **Vue Router 4** | `^4.3` | SPA routing with navigation guards for auth. All page components are lazy-loaded. |
| **Axios** | `^1.7` | HTTP client wrapped in a singleton `ApiService` class with request/response interceptors for token injection and 401 redirect. |
| **ECharts + vue-echarts** | `^5.5 / ^6.7` | Industrial-grade charting library — handles large time-series datasets, supports canvas rendering. Only required chart types are registered to keep bundle size small. |
| **GridStack.js** | `^10.3` | Drag-and-drop, resize-enabled grid layout for the dashboard editor. GridStack controls the DOM directly, so each widget is mounted as its own `createApp()` instance injected into the grid cell. |
| **Tailwind CSS** | `^3.4` | Utility-first CSS with a custom dark-theme colour palette. All purged at build time. |
| **Lucide Vue Next** | `^0.395` | Consistent, tree-shakeable icon set. |
| **@vueuse/core** | `^10.11` | Browser utility composables. |

#### Backend

| Technology | Version | Why It Was Chosen |
|---|---|---|
| **Go** | `1.26` | Compiled, statically typed, low memory overhead — well-suited for long-running IoT telemetry services. |
| **Fiber v2** | `v2.52` | Fast HTTP framework built on fasthttp. Provides middleware (cors, helmet, compress, limiter, logger) out of the box with minimal boilerplate. |
| **pgx/v5** | `v5.9` | High-performance PostgreSQL driver. **No ORM** — all queries are raw SQL. The connection pool (`pgxpool`) is a package-level singleton in `internal/database`. |
| **gofiber/websocket** | `v2` | WebSocket hub mounted on Fiber itself at `GET /ws` — same `:4000` port as REST, no separate server/lifecycle to manage. |
| **golang-jwt/jwt v5** | `v5.3` | JWT signing and verification with HMAC-SHA256. |
| **golang.org/x/crypto** | `v0.52` | bcrypt for password hashing. |
| **godotenv** | `v1.5` | Loads `.env` on startup (ignored if not present — Docker uses real env vars). |

#### Database & Infrastructure

| Technology | Why It Was Chosen |
|---|---|
| **PostgreSQL 16 + TimescaleDB** | Relational DB for all entity data + the TimescaleDB extension converts `telemetry_raw` into a **hypertable**, enabling automatic time-based partitioning and compression of time-series data at scale. |
| **Redis 7** | Provisioned; currently unused by application code. Reserved for future horizontal scaling (e.g., sharing WebSocket subscriptions across backend instances). |
| **Docker + Docker Compose** | All five services (DB, Redis, Backend, Frontend, pgAdmin) are defined in a single `docker-compose.yml` for one-command local setup. |

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

There is no simulator. Telemetry enters the system only via ingest; the DB broadcaster is a fallback heartbeat. The data flow for live sensor readings:

```
[POST /api/telemetry/:id/ingest]
       │  external device or load test writes a reading
       ▼
[Ingest handler]
       │  1. INSERT INTO telemetry_raw
       │  2. alertEval.EvaluateAndBroadcast() — evaluates alert rules
       │  3. broadcaster.BroadcastOne() — broadcasts immediately, no wait
       ▼                                              ▲
[WsGateway.BroadcastTelemetry()]          [Broadcaster: polls DB every 30s]
       │  sends JSON to all subscribed         DISTINCT ON (machine_id) —
       │  WebSocket clients                    fallback heartbeat for ALL
       ▼                                       connected clients
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

> **Note:** There is no simulator generating synthetic data. Live updates come from ingest (immediate broadcast) plus a 30-second DB-polling broadcaster as a fallback heartbeat. The `telemetry_raw` table stores every ingested reading. Historical chart data is loaded from the DB via REST, not synthesised from WebSocket traffic.

### 2.3 Alert Rule Evaluation

Alert rules are stored per-machine in the `alerts` table. Each rule defines a `field`, `condition` (gt/lt/eq/between/outside), and `threshold`. `AlertService.EvaluateTelemetry()` runs on every telemetry ingest (`EvaluateAndBroadcast`, called from the ingest handler):

- It loads all active rules for the machine from the DB.
- Evaluates each rule's condition against the current field value.
- If triggered **and** not in cooldown (`cooldownSec`, default 300s), it creates an `AlertEvent` in the DB and calls `WsGateway.BroadcastAlert()` — which immediately pushes the alert to all connected clients.
- On the frontend, `useWebSocket` catches the alert message and calls `alertStore.addLiveAlert()`.
- Cooldown state is kept **in-process** in `Service.lastFiredAt` — a map keyed by alert ID. This resets on backend restart.

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

**Why?** If a line chart tried to merge raw WebSocket points with 30-minute-bucketed API data, it would create clustered raw points at the right edge of the chart, distorting its shape. The two-track approach keeps the chart clean.

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

The `/led` route is also accessible as a public shareable kiosk URL (`/led?w=<base64-encoded LedWidget[]>`), generated by `useLedExport → exportLedLink()`. This route bypasses JWT auth entirely — the 401 interceptor in `api.service.ts` skips the redirect for `/led` paths.

#### LED CSS Grid Layout

`LedView.vue` renders its widget grid with an auto-sizing CSS Grid. Column count (`colsForCount`) scales with the number of active widgets:

| Widget count | Columns |
|---|---|
| 1–2 | 2 |
| 3 | 3 |
| 4 | 2 |
| 5–6 | 3 |
| 7+ | 4 |

Each `LedWidget` carries an optional `colSpan` field (default 1). `daily-count` widgets default to `colSpan: 2` because their 8-hour bar chart needs the extra width. `effectiveSpan()` clamps the value to the current column count so a wide widget never overflows.

The grid uses **`grid-auto-flow: dense`** so that 1-span widgets automatically backfill any hole left when a wide widget wraps to the next row (e.g., with 3 columns, if a span-2 widget finds only 1 remaining slot in the current row it moves to the next row — dense packing fills that orphaned slot with the next 1-span widget instead of leaving it empty).

`emptySlotCount` pads the last row with black `<div>` cells so the 1px gap background colour does not bleed through as a visible grey strip.

### 2.7 Schema Management (No Migration Files)

The backend has **no migration files**. All DDL lives in `backend/internal/migrate/migrate.go`. On every startup, `migrate.RunAll()` runs before the HTTP server starts:

1. `EnsureSchema()` — `CREATE TABLE IF NOT EXISTS` for all 14 tables, creates the TimescaleDB hypertable, sets compression policy, and creates all indexes.
2. `EnsureSeed()` — checks `COUNT(*) FROM organizations`; if zero, inserts the default org, factory, production lines, machines, fields, users, dashboard, widgets, and alert rules.

Both functions are fully idempotent — safe to run on every restart.

### 2.8 Dashboard Widget Inheritance

When a new dashboard is created (`POST /api/dashboards`), the backend automatically copies all widgets from the organisation's `is_default = TRUE` dashboard into the new dashboard. This is done via a single `INSERT ... SELECT` in `repository.CopyWidgetsFromDefault()`. Each widget gets a fresh `gen_random_uuid()` — they are independent copies, not references.

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
│  │   ApiService (axios)         │  │  WebSocketService                │  │
│  │   REST: :4000/api/*          │  │  WS:  :4000/ws                   │  │
│  └──────────────┬──────────────┘  └──────────────┬───────────────────┘  │
└─────────────────┼─────────────────────────────────┼─────────────────────┘
                  │ HTTP                             │ WebSocket
┌─────────────────▼─────────────────────────────────▼───────────────────┐
│                     BACKEND  (Go / Fiber v2 + gofiber/websocket)       │
│                                                                        │
│  ┌────────────────────────────────────────────────────────────────┐   │
│  │  Fiber REST + WS  (:4000)                                      │   │
│  │  /health  /api/auth  /api/machines  /api/telemetry             │   │
│  │  /api/dashboards  /api/alerts  /api/ai  /ws                    │   │
│  └──────────────────────────┬─────────────────────────────────────┘   │
│                              │                                         │
│  ┌───────────────────────────▼─────────────────────────────────────┐  │
│  │  Module structure: controller → service → repository → pgxpool  │  │
│  └───────────────────────────┬─────────────────────────────────────┘  │
│                              │                                         │
│  ┌───────────────────────────▼─────────────────────────────────────┐  │
│  │  WsGateway  (:4000/ws, gofiber/websocket)                       │  │
│  │  - Manages connected clients (map[*client]struct{})             │  │
│  │  - Handles subscribe/unsubscribe per machineId                  │  │
│  │  - Broadcasts telemetry/alert/machine_status messages           │  │
│  │  - Ping/pong heartbeat every 30s to detect dead connections     │  │
│  └───────────────────────────┬─────────────────────────────────────┘  │
│                              │                                         │
│  ┌───────────────────────────▼─────────────────────────────────────┐  │
│  │  Broadcaster (in-process goroutine) — no simulator               │  │
│  │  - Polls DB every 30s: DISTINCT ON (machine_id) latest reading  │  │
│  │  - Broadcasts via WsGateway (fallback heartbeat)                │  │
│  │  - Ingest also broadcasts + evaluates alert rules immediately   │  │
│  └───────────────────────────┬─────────────────────────────────────┘  │
└─────────────────────────────┼──────────────────────────────────────────┘
                              │ pgx/v5 (raw SQL)
┌─────────────────────────────▼──────────────────────────────────────────┐
│                  PostgreSQL 16 + TimescaleDB                            │
│  Tables: users, organizations, machines, machine_fields                 │
│          production_lines, factories, dashboards, dashboard_widgets     │
│          alerts, alert_events, ai_conversations, ai_messages            │
│          audit_logs, telemetry_aggregates                               │
│  Hypertable: telemetry_raw (time-partitioned, 7-day chunks, compressed) │
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

**Key invariant:** `telemetryStore` is never fetched from REST directly by components. It is always populated by:
1. `useMachineTelemetry` composable pre-loading recent history from API on mount.
2. `useWebSocket` composable writing every incoming WebSocket message into the store.

### 3.3 Authentication & Session Flow

```
1. User submits login form
2. auth.store → api.login() → POST /api/auth/login
3. Server returns { token, user }
4. auth.store stores token in localStorage AND calls wsService.connect(token)
5. WsGateway accepts connection (token validation is optional — unauthenticated WS is allowed)
6. api.service.ts interceptor reads localStorage token on every request
7. On 401 response → localStorage cleared → redirect to /login (except /led paths)
8. On app reload → auth.store reads token from localStorage → calls loadProfile()
```

### 3.4 Public vs Protected Endpoints

| Endpoint | Auth Required |
|---|---|
| `GET /health` | No |
| `GET /api/telemetry/latest` | No (for LED kiosk) |
| `GET /api/telemetry/:id/latest` | No (for LED kiosk) |
| All other `/api/*` | Yes — JWT Bearer |
| `PATCH/DELETE /api/machines` | Role: `admin` or `editor` |
| `DELETE /api/machines/:id` | Role: `admin` only |

### 3.5 Component Communication Patterns

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
├── docker-compose.yml              # 5 services: TimescaleDB, Redis, Backend, Frontend, pgAdmin
├── .env / .env.example             # Environment variables
├── scripts/
│   ├── init-timescale.sql          # Enables timescaledb + uuid-ossp on first DB boot
│   └── create-indexes.sql          # Supplementary index definitions
│
├── frontend/
│   ├── package.json
│   ├── vite.config.ts              # Dev server: proxies /api and /ws → :4000 (same port)
│   ├── nginx.conf                  # Prod: /api/* → backend:4000, /ws → backend:4000 (upgrade)
│   └── src/
│       ├── main.ts                 # App bootstrap: Vue, Pinia, ECharts registration
│       ├── App.vue                 # Root: router-view + AppLayout selector
│       │
│       ├── router/index.ts         # Route definitions + auth navigation guard
│       ├── layouts/AppLayout.vue   # Shell: sidebar + topbar + LED mode gate
│       │
│       ├── pages/
│       │   ├── LoginPage.vue
│       │   ├── DashboardListPage.vue
│       │   ├── DashboardEditorPage.vue     # Main grid editor with widget toolbox
│       │   ├── MachineManagementPage.vue
│       │   ├── AlertsPage.vue
│       │   ├── AIAssistantPage.vue         # Chat UI for POST /api/ai/chat (see docs/ai-pages.md)
│       │   └── LedViewPage.vue             # Public kiosk — no auth, no layout shell
│       │
│       ├── components/
│       │   ├── layout/
│       │   │   ├── Sidebar.vue             # Nav links, user info, sign out
│       │   │   └── TopBar.vue              # Clock, WS status indicator, alert bell
│       │   ├── dashboard/
│       │   │   ├── GridStackCanvas.vue     # GridStack ↔ Vue mini-app bridge (most complex)
│       │   │   ├── WidgetToolbox.vue       # Drag-source palette of widget types
│       │   │   └── WidgetConfigModal.vue   # Edit widget: machine, field, time range, etc.
│       │   ├── widgets/
│       │   │   ├── WidgetWrapper.vue       # Container: title bar, edit/remove buttons
│       │   │   ├── KpiCardWidget.vue       # Single large metric with sparkline
│       │   │   ├── GaugeWidget.vue         # ECharts gauge with min/max/threshold
│       │   │   ├── LineChartWidget.vue     # ECharts line chart with time range selector
│       │   │   ├── StatusCardWidget.vue    # Machine online/offline/error status
│       │   │   ├── AlarmPanelWidget.vue    # Live alert event feed
│       │   │   ├── TableWidget.vue         # Tabular telemetry display
│       │   │   └── MachineDailyCountWidget.vue  # Cumulative area chart of daily production; ResizeObserver drives ECharts resize
│       │   └── led/
│       │       ├── LEDCarousel.vue         # Full-screen KPI carousel for 640×320 displays
│       │       └── LedView.vue             # LED kiosk grid; colSpan + grid-auto-flow:dense; ledMode prop renders SVG bars
│       │
│       ├── stores/
│       │   ├── auth.store.ts
│       │   ├── machine.store.ts
│       │   ├── telemetry.store.ts          # Live snapshots + rolling history (300 pts/field)
│       │   ├── dashboard.store.ts
│       │   └── alert.store.ts              # liveAlerts[] capped at 50 entries
│       │
│       ├── composables/
│       │   ├── useWebSocket.ts             # Global WS listener → writes to all stores
│       │   ├── useTelemetry.ts             # useMachineTelemetry + useFieldSeries + useAggregatedValue
│       │   ├── useScreenMode.ts            # Detects led / mobile / desktop
│       │   └── useLedExport.ts             # Generates shareable /led?w=<base64> URL
│       │
│       ├── services/
│       │   ├── api.service.ts              # Axios singleton: all REST endpoints + interceptors
│       │   └── ws.service.ts               # WS singleton: connect/subscribe/dispatch/reconnect
│       │
│       └── types/index.ts                  # All TypeScript interfaces (shared contracts)
│
├── backend/
│   ├── go.mod / go.sum
│   └── cmd/
│   │   ├── server/main.go          # Entry point: config → DB (with retry) → migrate → WS → broadcaster → Fiber
│   │   └── backfill/main.go        # Standalone utility to backfill historical telemetry
│   └── internal/
│       ├── config/env.go           # Reads env vars; DATABASE_URL is required (panics if missing)
│       ├── database/db.go          # pgxpool singleton; Connect() retries up to 15× with 3s delay
│       ├── migrate/migrate.go      # ALL DDL + seed data — runs on every startup, idempotent
│       ├── middleware/
│       │   ├── auth.go             # Authenticate (JWT verify), RequireRole, GetUser helpers
│       │   └── error.go            # AppError type + global Fiber ErrorHandler
│       ├── modules/
│       │   ├── auth/               # login endpoint, bcrypt compare, JWT sign
│       │   ├── machines/           # Machine CRUD, production lines, factories, field upsert
│       │   ├── telemetry/          # latest (public), series, aggregate, daily-count, ingest
│       │   ├── dashboards/         # Dashboard + widget CRUD; Create auto-copies default widgets
│       │   └── alerts/             # Alert rules CRUD, event acknowledge/resolve, EvaluateTelemetry
│       ├── broadcaster/            # Polls DB every 30s → BroadcastTelemetry (fallback heartbeat; no simulator)
│       └── websocket/ws_gateway.go # gofiber/websocket hub (:4000/ws): client map, subscription filter, ping/pong
│
└── loadtest/                       # Vegeta-based load test tool (separate Go module)
    ├── main.go                     # Attack runner + SSE dashboard server
    ├── dashboard.html              # Live browser dashboard for test metrics
    ├── setup.ps1                   # Logs in, creates test dashboards, writes targets.json
    └── targets.json                # Endpoint definitions with weights (auto-generated by setup.ps1)
```

### Page-by-Page Breakdown

Each page lives in `frontend/src/pages/` and maps to a Vue Router route. All routes except `/login` and `/led` require a valid JWT (enforced by the navigation guard in `router/index.ts`).

---

#### `LoginPage.vue` — `/login`

Full-screen centered card. Demo credentials (`admin@acme-foods.com` / `Admin@1234`) are pre-filled in the reactive form so a first-time visitor can sign in immediately.

**Flow:**
1. User submits → `auth.store.login()` → `POST /api/auth/login`.
2. On success: reads the `?redirect=` query param and pushes that route, otherwise goes to `/dashboards`.
3. On failure: `auth.store.error` is displayed inline beneath the form.

**Notable:** password visibility toggle (Eye/EyeOff icon), no layout shell (public route).

---

#### `DashboardListPage.vue` — `/dashboards`

Gallery of all dashboards belonging to the organisation. Cards are arranged in a responsive 1→2→3 column grid.

**What each card shows:** name, optional description, widget count, last-updated time-ago, tags, "Default" and "Public" badges.

**Actions:**
| Button | Behaviour |
|---|---|
| **New Dashboard** | Opens an inline modal to set name + description; on create, navigates directly to the editor. |
| **Import** | Hidden `<input type="file">` accepts `.json`. Parses `{name, description, tags, widgets[]}`, creates the dashboard via API, then sequentially calls `dashboardStore.addWidget()` for each entry. |
| Card **Delete** (hover) | `confirm()` dialog → `dashboardStore.deleteDashboard()`. |
| Card **click** | Navigates to `/dashboards/:id`. |

**Key detail:** Dashboard creation on the backend automatically copies every widget from the org's default dashboard (`is_default = TRUE`) into the new one — so a new dashboard is never truly empty.

---

#### `DashboardEditorPage.vue` — `/dashboards/:id`

The primary work surface. Renders `GridStackCanvas` (drag-and-drop widget grid) with a toolbar above it.

**Toolbar actions:**

| Button | What it does |
|---|---|
| **Add Widget** | Toggles `WidgetToolbox` panel → user picks a type → `WidgetConfigModal` opens for initial config → `dashboardStore.addWidget()`. |
| **Export** | Serialises `{name, description, tags, widgets[]}` to a `.json` blob and triggers a browser download. Used as a portable backup / import source for `DashboardListPage`. |
| **Export LED Link** | Calls `useLedExport.exportLedLink()` which base64-encodes the current widget list into a `/led?w=<payload>` URL and copies it to the clipboard. The adjacent `ExternalLink` button opens the URL in a new tab for immediate preview. |
| **Save** | (1) Reads current GridStack positions via `gridCanvasRef.getCurrentLayouts()` and persists them with `dashboardStore.saveLayout()`. (2) Reads datetime ranges from `widgetViewStateStore` and persists them as widget config updates — so line-chart date ranges survive a page reload. |

**Widget lifecycle:**
- **Edit** (gear icon on widget hover) → reopens `WidgetConfigModal` with existing config → `dashboardStore.updateWidget()`.
- **Remove** (X icon) → `confirm()` → `dashboardStore.removeWidget()`.

**Important:** `Save` does not auto-run. Layout changes, widget edits, and datetime range changes are only persisted when the user clicks Save.

---

#### `MachineManagementPage.vue` — `/machines`

Admin view for the physical machine fleet. Calls `useWebSocket()` on mount so status dot colours update in real-time as the backend broadcasts `machine_status` events.

**Summary row:** 4 stat cards (Total / Online / Maintenance / Offline) computed from `machineStore`.

**Filter bar:** free-text search against name and serial number; type dropdown (Checkweigher / Temp Sensor / Conveyor / Vision AI).

**Table columns:** Machine (name + serial), Type, Status (live dot + badge), Production Line, Field count, Alert rule count, Actions.

**Per-row actions:**
- **Edit (pencil)** → `EditMachineModal` — updates name, serial, production line assignment, and per-field thresholds/limits.
- **Maintenance (wrench)** → `confirm()` → `PATCH /api/machines/:id { status: "maintenance" }`.
- **Online (power)** — shown instead of wrench when machine is already in maintenance → sets status back to `"online"`.

**Note:** Machine deletion is not exposed in the UI (requires `admin` role on the API) — intentional, to protect widget data integrity.

---

#### `AlertsPage.vue` — `/alerts`

Three-panel view of the alert subsystem.

**Summary row:** Critical count, Warning count, Total rules defined — read from `alertStore`.

**Tabs:**

| Tab | Content |
|---|---|
| **Active Events** | List of open / acknowledged alert events fetched from `GET /api/alerts/events`. Each row shows: rule name, severity badge, status badge, machine + field + value vs threshold, timestamp. Actions: **Ack** (open → acknowledged) and **Resolve** (any → resolved). Empty state shows a green checkmark. |
| **Alert Rules** | Table of all configured alert rules: name, machine, field, condition + threshold, severity, active/disabled toggle dot, lifetime event count. Rules are created via the Go backend seed or future CRUD endpoints — there is no "create rule" UI yet. |
| **Live Stream** | Real-time feed of WebSocket alert pushes. Entries appear instantly without polling. Capped at 50 entries in `alertStore.liveAlerts`. **Clear** button empties the list. Each entry shows rule name, machine, field value, and time. |

---

#### `AIAssistantPage.vue` — `/ai-assistant`

Chat UI for the Chat Assistant surface, backed by a fully implemented Go module. Layout is a two-column split: conversation list on the left, chat area on the right.

**Conversation management:** New Conversation button creates a DB record via `POST /api/ai/conversations`. Clicking an existing conversation loads its messages.

**Message flow:**
1. User types → optimistic push to `messages[]` (UI updates instantly).
2. `api.addMessage()` persists the user turn.
3. The message is sent to `POST /api/ai/chat`, which runs the server-side pipeline: intent router (`gpt-5.4-mini`) → forced tool_choice → bounded tool loop against the live database → verify-then-repair before returning the assistant's reply.
4. The assistant's reply (and any dashboard preview it staged) is appended to `messages[]`.

**Tool inspector:** Collapsible panel lists available tools returned by `GET /api/ai/tools`. Useful for debugging what actions the AI layer can take.

> **Status:** `/api/ai/*` is fully implemented — see [`docs/ai-pages.md`](docs/ai-pages.md) and §5.4 (updated below) for the pipeline detail. A separate Ask-Data page (`AskDataPage.vue`, `POST /api/ai/ask`) covers natural-language-to-chart queries.

---

#### `LedViewPage.vue` — `/led?w=<base64>`

Public kiosk wrapper — **no auth guard, no sidebar, no topbar**. Designed to be pasted into a Viplex Express browser or any kiosk browser pointed at a 640×320 LED panel.

**Scaling strategy:** `LedView` is always rendered at exactly 640×320 px internally. `LedViewPage` wraps it in a `transform: scale()` layer — computed as `Math.min(vw / 640, vh / 320)` — so the design fills any viewport (dev monitor, projector, kiosk screen) without ever reflowing the inner layout. The holder `<div>` is sized to the post-scale visual dimensions so the outer flexbox can centre it correctly.

**`?w=` payload:** `decodeLedPayload()` base64-decodes and JSON-parses the query parameter into a `LedWidget[]` array, which is passed to `LedView` as `activeWidgets`. If the param is absent, `LedView` falls back to its built-in mock data.

**Dev vs kiosk mode:** When `scale < 0.98` (running in a full-size browser), a `led-dev-bar` caption appears below the panel showing scale %, widget count, and payload status. On a real 640×320 kiosk (`scale ≈ 1`) the bar is hidden and the panel border shadow is removed.

---

### Key File Responsibilities

#### `backend/internal/migrate/migrate.go`
The single source of truth for DB schema. Contains two functions:
- `EnsureSchema()` — creates all tables with `IF NOT EXISTS`, sets up the TimescaleDB hypertable with 7-day chunks, enables compression after 14 days, and creates all performance indexes.
- `EnsureSeed()` — inserts the default org → factory → production lines → machines → fields → admin user → dashboard → widgets → alert rules. Uses fixed UUIDs (`00000000-0000-0000-0000-00000000000X`) so it is safe to re-run.

**To add a new table:** add it to `EnsureSchema()`. **To add seed data:** add it to `EnsureSeed()`.

#### `backend/cmd/server/main.go`
Wires the entire application in order:
1. `config.Load()` — reads env vars
2. `database.Connect(ctx)` — connects with retry loop; exits on failure
3. `migrate.RunAll(ctx, pool)` — schema + seed (non-fatal warning on error)
4. `websocket.NewGateway()` — creates the WS hub, mounted on Fiber at `GET /ws`
5. `broadcaster.New(gateway, pool, 30*time.Second).Start()` — starts the DB-polling fallback heartbeat (no simulator)
6. Fiber app with middleware stack → route registration → `app.Listen(":4000")`

#### `backend/internal/database/db.go`
Holds the package-level `Pool *pgxpool.Pool`. `Connect()` retries up to 15 times with 3-second delays — this prevents crashes during Docker startup when TimescaleDB is still loading its shared library even after reporting healthy via `pg_isready`.

#### `frontend/src/components/dashboard/GridStackCanvas.vue`
Most technically complex frontend component. Bridges GridStack's imperative DOM API with Vue's declarative model. See Section 2.4 for the full explanation.

#### `frontend/src/services/ws.service.ts`
Singleton `WebSocketService`. Key behaviours:
- Exponential backoff reconnection (2s → max 30s).
- Per-`machineId` handler sets plus `'*'` wildcard for all-machine listeners.
- Returns an unsubscribe `() => void` from every `on*` method for clean teardown in `onUnmounted`.

---

## 5. Future Improvements

### 5.1 Redis is Provisioned but Unused

**Current state:** Redis is in `docker-compose.yml` and `REDIS_URL` is wired through, but the Go backend never connects to it. The Fiber rate limiter uses **in-process memory storage** by default.

**Technical debt (two separate concerns):**

1. **Rate limiter state is not shared** across backend restarts or multiple instances. A restart resets all counters.
2. **WebSocket subscriptions are in-process** (`map[*client]struct{}`). Horizontally scaling to multiple backend instances would break fan-out — a client on instance A would not receive telemetry broadcast by instance B.

**Recommendation:** Implement a Redis pub/sub adapter for `WsGateway`. When `BroadcastTelemetry()` is called on instance A, publish to a Redis channel; all instances subscribe and forward to local clients.

---

### 5.2 Widget Config is a Loose `Record<string, unknown>`

**Current state:** `WidgetConfig` in `types/index.ts` has a typed subset of known fields but ends with `[key: string]: unknown`. Unknown or misspelled config keys silently pass through to the DB.

**Recommendation:** Define a discriminated union type per widget type:
```ts
type WidgetConfig =
  | { widgetType: 'kpi-card'; field: string; aggregationPeriod: AggregationPeriod; unit?: string }
  | { widgetType: 'gauge';    field: string; min: number; max: number }
  | { widgetType: 'alarm-panel'; severities: AlertSeverity[] }
  // ...
```
Validate at the Go `AddWidget` / `UpdateWidget` endpoints by checking known keys before persisting.

---

### 5.3 Telemetry Store Has No Eviction for Disconnected Machines

**Current state:** `telemetryStore.history` stores up to 300 data points per field per machine. If a machine goes offline or is deleted, its entries are never removed from the store.

**Recommendation:** Subscribe to `machine_status` events in `machineStore`. When a machine transitions to `offline` and no widget is currently subscribed to it, call `telemetryStore.clearHistory(machineId)` after a grace period (e.g., 5 minutes).

---

### 5.4 AI Assistant — Implemented, Not a Stub

**Current state:** `internal/modules/ai/` is fully implemented — conversation CRUD, tool listing/execution, and two production pipelines: the Chat Assistant (`POST /api/ai/chat`, `controller.go` + `router.go` — intent router → forced tool_choice → bounded tool loop → verify-then-repair) and Ask-Data (`POST /api/ai/ask`, `nl2sql.go` — natural language → hardened SQL → LLM-authored ECharts chart, plus saved boards in `boards.go`). Production runs on KKU GenAI (`claude-sonnet-5` generation, `gpt-5.4-mini` router/verifier); Groq/gpt-oss are only the fallback defaults in code. See [`docs/ai-pages.md`](docs/ai-pages.md) for the full end-to-end breakdown.
