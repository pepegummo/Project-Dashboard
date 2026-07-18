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
| 2 Ramp | 10 / 25 / 50 / 100 | 30s each step | Find latency knee point (4 steps) |
| 3 Load | 100 | 3 min | SLA baseline |
| 4 Spike | 300 | 30s | Test recovery after burst |
| 5 Cool | 50 | 30s | Confirm latency returns to baseline |

### Endpoints tested (round-robin, 15 targets)

**Dashboard load flow:**
- `GET /api/dashboards`
- `GET /api/dashboards/00000000-0000-0000-0000-000000000010`
- `GET /api/machines`

**Widget data — line-chart** (heaviest: `time_bucket()` queries):
- `GET /api/telemetry/<cwID>/series?field=weight&timeRange=1h`
- `GET /api/telemetry/<cbID>/series?field=speed&timeRange=1h`
- `GET /api/telemetry/<cwID>/series?field=weight&timeRange=24h`
- `GET /api/telemetry/<cwID>/series?field=weight&timeRange=1y` ← ~8760 points, 91× heavier than 24h
- `GET /api/telemetry/<tsID>/series?field=temp&timeRange=1y`

**Widget data — gauge + kpi-card:**
- `GET /api/telemetry/<cwID>/aggregate?field=weight&period=1h`
- `GET /api/telemetry/<tsID>/aggregate?field=temp&period=1h`
- `GET /api/telemetry/<cwID>/aggregate?field=throughput&period=1h`

**Widget data — alarm-panel** (public, no auth):
- `GET /api/alerts/events/active`

**Live snapshot:**
- `GET /api/telemetry/latest?ids=<all 4 machines>` × 2

**Daily-count bar chart:**
- `GET /api/telemetry/<cwID>/daily-count?days=7`

---

## WebSocket Load Test — k6

Two scripts with different purposes. Run them separately or together.

### Install k6
```powershell
winget install k6
# or
choco install k6
```

> Requires k6 v0.49+ for all features. Tested on k6 v2.0.0.

---

### `ws-flow.js` — Connection Capacity Test

Simulates **LED kiosk connections** at scale. Each VU connects without a token (public endpoint),
subscribes to all 4 machines, and stays connected — exactly like a real kiosk screen.
Goal: find how many concurrent connections the WS gateway can sustain before it starts
rejecting connections.

#### Run
```powershell
k6 run load-test/ws/ws-flow.js
```

#### Phases (~7 min total)
| Phase | VUs | Duration | Goal |
|---|---|---|---|
| 1 Smoke | 2 | 30s | Verify connect + subscribe + receive |
| 2 Ramp | 5→100 | 90s | Find broadcast degradation point |
| 3 Load | 100 | 3 min | SLA baseline for concurrent connections |
| 4 Spike | 150 | 30s | Push gateway fan-out to the limit (server capacity ceiling) |
| 5 Cool | 150→0 | 30s | Goroutine cleanup — no leak |

#### Thresholds
| Metric | Threshold | Meaning |
|---|---|---|
| `ws_connecting` | p(95) < 500ms | Handshake time (no JWT overhead) |
| `telemetry_msgs` | count > 0 | Must receive at least one broadcast |
| `broadcast_latency_ms` | p(95) < 3000ms, min > 0 | Server push latency; `min>0` catches Docker clock skew |
| `ws_sessions` | count < 500 | >500 sessions = VUs reconnect-looping = server rejecting connections |

#### Known capacity finding
Server WS connection limit is approximately **150 concurrent connections**.
Spike is capped at 150 VUs to stay within this limit and produce clean results.
Raising above 150 causes the server to close connections immediately — VUs reconnect-loop
and `ws_sessions count<500` threshold fails.

---

### `ws-test.js` — Real-Time Data & Performance Test

Verifies **data correctness + response time** under load. Each VU connects, subscribes,
measures ping-pong RTT, and validates every telemetry message for integrity and freshness.

#### Run
```powershell
k6 run load-test/ws/ws-test.js
```

#### Phases (~6 min total)
| Phase | VUs | Duration | Goal |
|---|---|---|---|
| 1 Smoke | 2 | 70s | Must capture ≥1 simulator tick (60s cycle) + verify all checks |
| 2 Ping baseline | 10 | 60s | RTT measurement under light load |
| 3 Load | 50 | 3 min | RTT + data integrity under sustained connections |
| 4 Spike | 150 | 30s | RTT degradation under burst (within server capacity) |

#### Thresholds
| Metric | Threshold | Meaning |
|---|---|---|
| `ws_connecting` | p(95) < 500ms | Handshake time |
| `ping_rtt_ms` | p(95) < 200ms | True request-response RTT (ping → pong) |
| `time_to_first_msg_ms` | p(95) < 70s | Time from subscribe → first telemetry tick |
| `broadcast_latency_ms` | p(95) < 3000ms, min > 0 | Server push latency; `min>0` catches Docker clock skew |
| `integrity_failures` | count == 0 | Zero tolerance — any bad payload fails the test |

#### Data integrity checks (per telemetry message)
| Check | Catches |
|---|---|
| `telemetry: has machineId` | Malformed/empty payload |
| `telemetry: has data object` | Missing data field |
| `telemetry: machineId is subscribed` | Server leaking data for unsubscribed machines |
| `telemetry: data values are numbers` | null / string garbage in live feed |
| `telemetry: data is fresh (<2 min)` | Frozen/stuck simulator |

#### VU lifecycle
1. Connect to `ws://localhost:4000/ws` — auth via `?token=<jwt>`
2. Record `subscribedAt` timestamp, send subscribe for all 4 machines
3. On first telemetry message → record `time_to_first_msg_ms`
4. On every telemetry message → measure broadcast latency + run 5 integrity checks
5. Send JSON ping `{ "type": "ping" }` every 10s → measure pong RTT
6. Stay connected until k6 ends the scenario

---

## Combined Pressure Test (recommended for final validation)

Run both simultaneously to see how REST degrades under WS broadcast load,
and whether data quality drops when connection count is high:

```powershell
# Terminal 1 — REST (dashboard users)
cd load-test/rest; go run .

# Terminal 2 — WebSocket capacity
k6 run load-test/ws/ws-flow.js

# Terminal 3 — WebSocket data quality
k6 run load-test/ws/ws-test.js
```

**Watch for:**
- REST p99 spiking during WS Phase 4 spike → pgx pool exhaustion from WS fan-out competing with DB queries
- `integrity_failures > 0` during combined load → data corruption under pressure
- `ping_rtt_ms` degrading as connection count rises → WS gateway becoming a bottleneck

---

## Output / Dashboard

### JSON export (built-in, no setup)
```powershell
k6 run -o json=load-test/ws/results.json --summary-export=load-test/ws/summary.json load-test/ws/ws-test.js
```

### Grafana + InfluxDB (real-time dashboard)

Add to `docker-compose.yml`:
```yaml
  influxdb:
    image: influxdb:1.8
    ports: ["8086:8086"]
    environment:
      INFLUXDB_DB: k6

  grafana:
    image: grafana/grafana:latest
    ports: ["3001:3000"]
    environment:
      GF_AUTH_ANONYMOUS_ENABLED: "true"
      GF_AUTH_ANONYMOUS_ORG_ROLE: Admin
```

Then run:
```powershell
docker compose up -d influxdb grafana
k6 run -o influxdb=http://localhost:8086/k6 load-test/ws/ws-test.js
```

Open **http://localhost:3001**, add InfluxDB data source (`http://influxdb:8086`, db=`k6`),
import Grafana dashboard ID **2587**.
