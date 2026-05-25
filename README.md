# IotVision — Industrial IoT AI Dashboard Platform

A production-style MVP for real-time industrial monitoring, built with Vue 3, Node.js, PostgreSQL/TimescaleDB, and WebSocket telemetry streaming.

---

## 📐 Architecture

```
iot-dashboard/
├── backend/                    # Node.js + Express + Prisma
│   ├── prisma/
│   │   ├── schema.prisma       # Full DB schema (15 tables)
│   │   └── seed.ts             # Demo data seeder
│   └── src/
│       ├── modules/
│       │   ├── auth/           # JWT authentication
│       │   ├── machines/       # Machine & field management
│       │   ├── telemetry/      # Ingestion + series queries
│       │   ├── dashboards/     # Dashboard CRUD + widget management
│       │   └── alerts/         # Alert rules + event evaluation
│       ├── websocket/          # ws.gateway.ts — real-time broadcast
│       ├── telemetry/          # simulator.ts — 4 machine data generators
│       └── ai-tools/           # Structured AI tool layer
│
└── frontend/                   # Vue 3 + Vite + TypeScript
    └── src/
        ├── stores/             # Pinia: auth, machines, telemetry, dashboards, alerts
        ├── services/           # api.service.ts, ws.service.ts
        ├── composables/        # useTelemetry, useWebSocket
        ├── pages/              # Login, DashboardList, DashboardEditor, Machines, Alerts, AI
        ├── components/
        │   ├── layout/         # Sidebar, TopBar
        │   ├── dashboard/      # GridStackCanvas, WidgetToolbox, WidgetConfigModal
        │   └── widgets/        # LineChart, Gauge, KpiCard, StatusCard, Table, AlarmPanel
        └── layouts/            # AppLayout (sidebar + topbar shell)
```

---

## 🚀 Quick Start (Docker — recommended)

```bash
cd iot-dashboard

# Copy env file
cp .env.example .env

# Start all services (db, redis, backend, frontend)
docker compose up -d

# Wait ~30 seconds for DB to initialize, then seed:
docker compose exec backend npm run db:migrate:deploy
docker compose exec backend npm run db:seed
```

**Access:**
- Frontend: http://localhost:5173
- Backend API: http://localhost:4000
- WebSocket: ws://localhost:4001
- Prisma Studio: `docker compose exec backend npm run db:studio`

**Default login:** `admin@acme-foods.com` / `Admin@1234`

---

## 💻 Local Development (without Docker)

### Prerequisites
- Node.js 20+
- PostgreSQL 16 + TimescaleDB extension
- Redis (optional — not strictly required for dev)

### Backend

```bash
cd backend
npm install

# Copy and edit .env
cp ../.env.example .env

# Run migrations
npm run db:migrate

# Seed demo data
npm run db:seed

# Start dev server (hot-reload)
npm run dev
```

### Frontend

```bash
cd frontend
npm install

# Start dev server
npm run dev
```

Frontend dev server: http://localhost:5173 (proxies /api to :4000)

---

## 🗄️ Database Schema

| Table | Description |
|---|---|
| `organizations` | Multi-tenant root entity |
| `factories` | Physical factory locations |
| `production_lines` | Lines within a factory |
| `machines` | Individual machines with type and metadata |
| `machine_fields` | Dynamic telemetry field schema per machine |
| `telemetry_raw` | TimescaleDB hypertable — 1s granularity JSONB |
| `telemetry_aggregates` | Pre-computed 1m/5m/1h/1d rollups |
| `users` | Users with role-based access |
| `dashboards` | User-owned dashboard configurations |
| `dashboard_widgets` | Widget layout + JSON config |
| `alerts` | Alert rules with threshold conditions |
| `alert_events` | Alert firing history with lifecycle |
| `ai_conversations` | AI conversation sessions |
| `ai_messages` | Messages + tool call records |
| `audit_logs` | Full audit trail |

### TimescaleDB setup

Hypertable creation runs automatically on startup:

```sql
SELECT create_hypertable('telemetry_raw', 'timestamp', if_not_exists => TRUE);
SELECT add_compression_policy('telemetry_raw', INTERVAL '7 days', if_not_exists => TRUE);
```

---

## 🔌 WebSocket Protocol

Connect: `ws://localhost:4001?token=<jwt>`

```jsonc
// Client → Server: subscribe to machine telemetry
{ "type": "subscribe", "payload": { "machineIds": ["uuid1", "uuid2"] }, "timestamp": 1719000000000 }

// Server → Client: telemetry broadcast (1/second)
{ "type": "telemetry", "payload": { "machineId": "...", "machineName": "CW-01", "timestamp": "...", "data": { "weight": 501.3, "speed": 62 } }, "timestamp": ... }

// Server → Client: alert event
{ "type": "alert", "payload": { "alertId": "...", "severity": "warning", "field": "weight", "value": 511.2, ... }, "timestamp": ... }
```

---

## 🤖 AI Tool Layer

The `/api/ai/tools` endpoint exposes structured tool definitions for LLM integration:

| Tool | Description |
|---|---|
| `getMachines` | List all machines with status |
| `getMachineFields` | Get field schema for a machine |
| `getTelemetry` | Time-series data for a field |
| `getLatestTelemetry` | Current snapshot |
| `getDashboards` | List user dashboards |
| `createWidget` | Add widget to a dashboard |
| `updateWidget` | Update widget config |
| `moveWidget` | Reposition widget on grid |
| `createAlert` | Define alert rule |
| `getActiveAlerts` | Current open alert events |

### Connecting Claude/OpenAI

```typescript
// Fetch tool definitions
const tools = await fetch('/api/ai/tools').then(r => r.json());

// Execute via API (for LLM tool-use callbacks)
await fetch('/api/ai/tools/execute', {
  method: 'POST',
  body: JSON.stringify({ toolName: 'getMachines', params: { status: 'online' } })
});
```

---

## 🎛️ REST API Reference

```
POST   /api/auth/login
GET    /api/auth/me

GET    /api/machines
POST   /api/machines
GET    /api/machines/:id
PATCH  /api/machines/:id
DELETE /api/machines/:id
GET    /api/machines/:id/fields
PUT    /api/machines/:id/fields

GET    /api/telemetry/:machineId/latest
GET    /api/telemetry/:machineId/series?field=weight&timeRange=1h
POST   /api/telemetry/:machineId/ingest
GET    /api/telemetry/latest?ids=id1,id2

GET    /api/dashboards
POST   /api/dashboards
GET    /api/dashboards/:id
PATCH  /api/dashboards/:id
DELETE /api/dashboards/:id
POST   /api/dashboards/:id/widgets
PATCH  /api/dashboards/:id/layout
PATCH  /api/dashboards/:id/widgets/:widgetId
DELETE /api/dashboards/:id/widgets/:widgetId

GET    /api/alerts
POST   /api/alerts
PATCH  /api/alerts/:id
DELETE /api/alerts/:id
GET    /api/alerts/events/active
PATCH  /api/alerts/events/:eventId/acknowledge
PATCH  /api/alerts/events/:eventId/resolve

GET    /api/ai/tools
POST   /api/ai/tools/execute
GET    /api/ai/conversations
POST   /api/ai/conversations
GET    /api/ai/conversations/:id/messages
POST   /api/ai/conversations/:id/messages
```

---

## 📡 Simulated Machines

| Machine | Type | Key Fields |
|---|---|---|
| Checkweigher CW-01 | `checkweigher` | weight, speed, rejects, throughput |
| Temp Sensor TS-01 | `temperature_sensor` | temp, humidity, dew_point |
| Conveyor Belt CB-01 | `conveyor` | speed, load, rpm, vibration |
| Vision AI Camera VC-01 | `vision_camera` | defect_rate, inspected, passed, failed, confidence |

Telemetry is generated every **1 second** with realistic random walks and occasional anomaly spikes to trigger alerts.

---

## 🧱 Widget Types

| Widget | Config Keys |
|---|---|
| `line-chart` | `field`, `timeRange`, `color` |
| `gauge` | `field`, `min`, `max`, `unit`, `thresholds` |
| `kpi-card` | `field`, `unit`, `precision` |
| `status-card` | _(machine-level)_ |
| `table` | _(all fields)_ |
| `alarm-panel` | `maxItems`, `severities` |

---

## 🔐 Authentication & Roles

| Role | Permissions |
|---|---|
| `admin` | Full access including delete |
| `editor` | Create/update machines, dashboards, alerts |
| `viewer` | Read-only |

JWT tokens expire after 24h (configurable via `JWT_EXPIRES_IN`).

---

## 🏗️ Production Checklist

- [ ] Change `JWT_SECRET` to a 256-bit random value
- [ ] Set strong `POSTGRES_PASSWORD`
- [ ] Configure `CORS_ORIGIN` to your actual frontend domain
- [ ] Enable SSL/TLS termination at the load balancer
- [ ] Set up TimescaleDB retention policy for `telemetry_raw`
- [ ] Configure Redis for session storage (optional)
- [ ] Set up log aggregation (structured JSON logs via morgan)
- [ ] Configure monitoring / alerting for the backend service

---

## 📦 Tech Stack

| Layer | Technology |
|---|---|
| Frontend framework | Vue 3 + TypeScript |
| Build tool | Vite 5 |
| State management | Pinia |
| Router | Vue Router 4 |
| Dashboard grid | GridStack.js |
| Charts | Apache ECharts + vue-echarts |
| Styling | TailwindCSS 3 |
| Icons | lucide-vue-next |
| HTTP client | Axios |
| Backend runtime | Node.js 20 + TypeScript |
| Web framework | Express 4 |
| ORM | Prisma 5 |
| Database | PostgreSQL 16 + TimescaleDB |
| Real-time | ws (WebSocket) |
| Auth | JWT (jsonwebtoken + bcryptjs) |
| Validation | Zod |
| Container | Docker + Docker Compose |
