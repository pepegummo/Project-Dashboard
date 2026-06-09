# IotVision — Architecture Diagram

## System Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Docker Network (cpf_net)                      │
│                                                                      │
│  ┌──────────────┐    ┌──────────────────┐    ┌──────────────────┐   │
│  │   Frontend   │    │     Backend      │    │   TimescaleDB    │   │
│  │  Nginx + Vue │───▶│   Go / Fiber     │───▶│  PostgreSQL 16   │   │
│  │   :80→5173   │    │     :4000        │    │     :5432        │   │
│  └──────────────┘    │  REST + WS /ws   │    └──────────────────┘   │
│                      └──────────────────┘                           │
│                               │                    ┌─────────────┐  │
│                               └───────────────────▶│    Redis    │  │
│                                                    │   :6379     │  │
│                                                    └─────────────┘  │
└─────────────────────────────────────────────────────────────────────┘

Browser/Kiosk → :5173 (Nginx)
  /api/*  → proxy → backend:4000  (REST)
  /ws     → proxy → backend:4000  (WebSocket upgrade — same port)
```

---

## Request Flow

```
                    ┌─────────────────────────────────────────┐
                    │              Browser / Kiosk             │
                    │                                          │
                    │  Vue 3 SPA        ws.service.ts          │
                    │  Pinia stores     ?token=<jwt>           │
                    │  api.service.ts                          │
                    └──────────┬──────────────────┬───────────┘
                               │ HTTPS/WSS         │
                    ┌──────────▼──────────────────▼───────────┐
                    │              Nginx :5173                  │
                    │  /api/* → backend:4000                    │
                    │  /ws    → backend:4000 (upgrade)          │
                    └──────────┬──────────────────┬───────────┘
                               │                  │
               ┌───────────────▼──┐  ┌────────────▼───────────┐
               │   REST  :4000    │  │  WebSocket  :4000/ws    │
               │                  │  │                         │
               │  Fiber Middleware│  │  IsWebSocketUpgrade()   │
               │  helmet          │  │  AuthenticateWS()       │
               │  cors            │  │  fiberws.New(HandleFiber│
               │  compress        │  │                         │
               │  logger          │  │  Hub (map[*client])     │
               │  limiter         │  │  readPump / writePump   │
               │  Authenticate    │  │  Subscription filter    │
               └──────────────────┘  └─────────────────────────┘
```

---

## Backend Module Structure

```
backend/
├── cmd/server/main.go          Entry point — wires everything
│
├── internal/
│   ├── config/env.go           Env vars → Config struct
│   ├── database/db.go          pgxpool singleton
│   ├── migrate/migrate.go      Idempotent DDL + seed (runs on startup)
│   ├── middleware/
│   │   ├── auth.go             Authenticate (Bearer JWT)
│   │   │                       AuthenticateWS (?token= query param)
│   │   │                       RequireRole(roles...)
│   │   └── error.go            Global error handler → JSON
│   │
│   ├── websocket/ws_gateway.go Hub, HandleFiber, Broadcast*
│   ├── broadcaster/            Polls DB every 30s → BroadcastTelemetry
│   │
│   └── modules/
│       ├── auth/               POST /login · GET /me
│       ├── machines/           CRUD + /fields
│       ├── telemetry/          latest · series · aggregate · daily-count · ingest
│       ├── dashboards/         CRUD + widgets + layout
│       ├── alerts/             Rules CRUD + events + ack/resolve
│       ├── ai/                 Chat · tools · conversations (Groq llama-3.3-70b)
│       └── led/                GET/POST/DELETE /token (permanent kiosk JWT)
```

---

## API Routes

```
POST   /api/auth/login
GET    /api/auth/me                        🔒 auth

GET    /api/machines                       🔒 auth
POST   /api/machines                       🔒 admin|editor
PATCH  /api/machines/:id                   🔒 admin|editor
DELETE /api/machines/:id                   🔒 admin

GET    /api/telemetry/:id/latest           ✅ public
GET    /api/telemetry/:id/series           ✅ public
GET    /api/telemetry/:id/daily-count      ✅ public
GET    /api/telemetry/:id/hourly-count     ✅ public
GET    /api/telemetry/latest               ✅ public (batch)
GET    /api/telemetry/:id/aggregate        🔒 auth
POST   /api/telemetry/:id/ingest           🔒 auth

GET    /api/alerts/events/active           ✅ public
GET    /api/alerts                         🔒 auth
POST   /api/alerts                         🔒 admin|editor
PATCH  /api/alerts/events/:id/acknowledge  🔒 auth
PATCH  /api/alerts/events/:id/resolve      🔒 auth

GET    /api/dashboards                     🔒 auth
POST   /api/dashboards                     🔒 auth
PATCH  /api/dashboards/:id/layout          🔒 auth

GET    /api/ai/conversations               🔒 auth
POST   /api/ai/chat                        🔒 auth

GET    /api/led/token                      🔒 admin|editor
POST   /api/led/token                      🔒 admin|editor  (generate/replace)
DELETE /api/led/token                      🔒 admin|editor  (revoke)

GET    /ws?token=<jwt>                     🔒 AuthenticateWS
```

---

## Real-Time Data Flow

```
IoT Device
    │
    │  POST /api/telemetry/:id/ingest
    ▼
┌─────────────────────────────────────────┐
│  Ingest Handler                          │
│  1. Write row → telemetry_raw            │
│  2. alertEval.EvaluateAndBroadcast()     │──── BroadcastAlert → WS clients
│  3. broadcaster.BroadcastOne()           │──── BroadcastTelemetry → subscribed WS clients
└─────────────────────────────────────────┘

                    ┌──────────────────────────────┐
                    │  DB Broadcaster (every 30s)   │
                    │  DISTINCT ON (machine_id)     │
                    │  → BroadcastTelemetry         │──── WS heartbeat fallback
                    │  → EvaluateAndBroadcast       │
                    └──────────────────────────────┘

WS clients receive:
  { type: "telemetry", payload: { machineId, data, timestamp } }
  { type: "alert",     payload: { alertId, severity, value, ... } }
  { type: "machine_status", payload: { machineId, status } }
```

---

## WebSocket Hub

```
Gateway
  clients: map[*client]struct{}   ← all connected clients
  mu: sync.RWMutex

client
  conn:          *fiberws.Conn    ← Fiber WS (wraps fasthttp/ws → gorilla)
  send:          chan []byte [256] ← buffered outbox
  subscriptions: map[machineID]   ← filter: empty = receive all

                 ┌──────────────────────────────────────────┐
                 │              readPump (goroutine)         │
                 │  deadline: 60s + pong reset               │
                 │  handles: subscribe / unsubscribe / ping  │
                 └──────────────────────────────────────────┘
                 ┌──────────────────────────────────────────┐
                 │              writePump (goroutine)        │
                 │  ticker: ping every 30s                   │
                 │  drain send channel → WriteMessage        │
                 │  channel closed → send Close frame        │
                 └──────────────────────────────────────────┘
```

---

## Authentication & Authorization

```
JWT (HS256, signed with JWT_SECRET)

Roles:
  admin       → full access
  editor      → read + write (no delete machines/org)
  viewer      → read only
  led-viewer  → read only, no expiry (LED kiosk token)

Middleware chain (REST):
  Authenticate     → parse Bearer token → c.Locals("user", claims)
  RequireRole(...) → check claims.Role

Middleware chain (WebSocket):
  IsWebSocketUpgrade → reject non-WS requests
  AuthenticateWS     → parse ?token= query param → c.Locals("user", claims)
  fiberws.New(...)   → upgrade + HandleFiber
```

---

## LED Kiosk Flow

```
Admin Dashboard
    │
    │  POST /api/led/token  (generate once per org)
    │  ← { token: "<permanent-jwt, role=led-viewer, no expiry>" }
    │    stored in organizations.led_token
    │
    │  Export LED Link button
    │  ← /led?w=<base64-widgets>&token=<led-jwt>
    │
    ▼
Factory Floor Kiosk (no login, any machine)
    │
    │  LedViewPage mounts
    │  → api.setOverrideToken(token)   REST calls use LED JWT
    │  → wsService.connect(token)      WS connects with ?token=
    │
    │  LedView.vue
    │  → REST seed: getLatestTelemetry, getTelemetrySeries, getActiveAlerts
    │  → WS subscribe machineIds
    │  → live updates forever (token never expires)
    │
    │  Revoke: DELETE /api/led/token → existing URLs stop working
    │          POST   /api/led/token → generate new token → new URLs
    ▼
  640×320 LED display, auto-scaled to any viewport
```

---

## Database Schema (key tables)

```
organizations ──────────────────────────────────────────────────────
  id · name · slug · plan · settings (JSONB) · led_token

users ──────────────────────────────────────────────────────────────
  id · organization_id · email · role · password_hash

machines ───────────────────────────────────────────────────────────
  id · production_line_id · name · type · serial_number · status

machine_fields ─────────────────────────────────────────────────────
  id · machine_id · key · label · unit · data_type
  min · max · threshold · upper_limit · lower_limit

telemetry_raw (TimescaleDB hypertable, 7-day chunks) ───────────────
  machine_id · timestamp · data (JSONB) · quality
  compress: older than 14 days, segmentby machine_id

telemetry_aggregates ───────────────────────────────────────────────
  machine_id · field · period · timestamp · avg/min/max/stddev/count

dashboards ─────────────────────────────────────────────────────────
  id · org_id · user_id · name · is_public · is_default

dashboard_widgets ──────────────────────────────────────────────────
  id · dashboard_id · machine_id · widget_type · layout (JSONB) · config (JSONB)

alerts ─────────────────────────────────────────────────────────────
  id · machine_id · field · condition · threshold · severity
  cooldown_sec · is_active

alert_events ───────────────────────────────────────────────────────
  id · alert_id · value · status (open/acknowledged/resolved)
  triggered_at · acknowledged_at · resolved_at

ai_conversations ────────────────────────────────────────────────────
  id · user_id · title · context (JSONB)

ai_messages ─────────────────────────────────────────────────────────
  id · conversation_id · role · content
  tool_name · tool_input (JSONB) · tool_result (JSONB)
```

---

## AI Module (Groq llama-3.3-70b)

```
POST /api/ai/chat
    │
    ├── Load conversation history from DB
    ├── Build request: system prompt + history + 13 tools
    │
    └── Agentic Loop (max 5 iterations)
            │
            ├── Call Groq API
            │     retry on 429: parse Retry-After header, max 3 attempts
            │
            ├── finish_reason == "tool_calls"?
            │     YES → execute tools → save to DB → loop
            │     NO  → extract text → save → return
            │
            └── Tools by category:
                  Read:   getMachines · getLatestTelemetry · getTelemetryTrend
                          getActiveAlerts · getDailyCount · getFactoryOverview
                  Write:  createAlert · acknowledgeAlert · resolveAlert
                          createCustomDashboard · addWidgetToDashboard · removeWidget
                  Auth:   admin|editor for write tools (checked inside tool_actions.go)
```
