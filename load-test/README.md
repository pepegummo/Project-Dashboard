# Load Test Suite — IoT Dashboard

## Prerequisites

Docker services must be running before any test:
```powershell
docker compose up -d
```

---

## REST Load Test — vegeta (Go)

Simulates the **Dashboard Editor** page: login → load dashboard → poll all widget endpoints.

### First time setup
```powershell
cd load-test/rest
go mod tidy
```

### Run
```powershell
cd load-test/rest
go run .
```

### Phases
| Phase | RPS | Duration | Goal |
|---|---|---|---|
| 1 Smoke | 5 | 30s | Verify auth + all endpoints respond |
| 2 Ramp | 10→25→50→100 | 30s each | Find latency knee point |
| 3 Load | 100 | 3 min | SLA baseline (p95 target < 500ms) |
| 4 Spike | 300 | 30s | Test recovery after burst |
| 5 Cool | 50 | 30s | Confirm latency returns to baseline |

### Endpoints tested (round-robin)

**Dashboard load flow:**
- `GET /api/dashboards` — list
- `GET /api/dashboards/00000000-0000-0000-0000-000000000010` — seeded "Production Overview" (loads widgets)
- `GET /api/machines` — machine list

**Widget data — line-chart** (heaviest: `time_bucket()` queries):
- `GET /api/telemetry/<cwID>/series?field=weight&timeRange=1h`
- `GET /api/telemetry/<cbID>/series?field=speed&timeRange=1h`
- `GET /api/telemetry/<cwID>/series?field=weight&timeRange=24h`

**Widget data — gauge + kpi-card** (aggregate summary):
- `GET /api/telemetry/<cwID>/aggregate?field=weight&period=1h`
- `GET /api/telemetry/<tsID>/aggregate?field=temp&period=1h`
- `GET /api/telemetry/<cwID>/aggregate?field=throughput&period=1h`

**Widget data — alarm-panel** (public, no auth):
- `GET /api/alerts/events/active`

**Live snapshot** (polled every 2s by telemetry store):
- `GET /api/telemetry/latest?ids=<all 4 machines>` × 2

**Daily-count bar chart:**
- `GET /api/telemetry/<cwID>/daily-count?days=7`

---

## WebSocket Load Test — k6

Simulates **LED kiosk connections** (`/led?w=...` page).
Each VU connects **without a token** (public endpoint), subscribes to all 4 machines,
and stays connected for the full scenario duration — exactly like a real kiosk screen.

### Install k6
```powershell
winget install k6
# or
choco install k6
```

### Run
```powershell
k6 run load-test/ws/ws-flow.js
```

### Phases
| Phase | VUs | Duration | Goal |
|---|---|---|---|
| 1 Smoke | 2 | 30s | Verify no-token connect + subscribe + receive |
| 2 Ramp | 5→100 | 90s | Max kiosk connections before broadcast slows |
| 3 Load | 100 | 3 min | SLA baseline for concurrent kiosk connections |
| 4 Spike | 300 | 30s | ws_gateway fan-out under pressure |
| 5 Cool | 100→0 | 30s | Goroutine cleanup — no leak |

### VU lifecycle (permanent LED kiosk connection)
1. Connect to `ws://localhost:4001` — **no token** (public kiosk)
2. Subscribe to all 4 machines
3. Receive telemetry/alert/machine_status messages; measure broadcast latency
4. Send JSON ping `{ "type": "ping" }` every 10s to reset server read deadline
5. Stay connected until k6 ends the scenario (**no close, no unsubscribe**)

### Custom metrics
| Metric | Threshold | Meaning |
|---|---|---|
| `telemetry_msgs` | count > 0 | Must receive at least one broadcast |
| `broadcast_latency_ms` | p(95) < 3000ms | Server → client latency |
| `ws_connecting` | p(95) < 500ms | Handshake time (no JWT overhead) |

---

## Combined Pressure Test (recommended)

Run both simultaneously to see how REST degrades under WS broadcast load:

```powershell
# Terminal 1 — REST (dashboard users)
cd load-test/rest
go run .

# Terminal 2 — WebSocket (LED kiosks)
k6 run load-test/ws/ws-flow.js
```

**Watch for:** REST p99 spiking during WS Phase 4 spike → indicates pgx connection
pool exhaustion from concurrent WS fan-out competing with DB queries.
