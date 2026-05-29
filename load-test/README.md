# Load Test Suite — IoT Dashboard

## Prerequisites

Docker services must be running before any test:
```powershell
docker compose up -d
```

---

## REST Load Test — vegeta (Go)

Uses vegeta as a Go library. No separate CLI install needed.

### First time setup
```powershell
cd load-test/rest
go mod tidy   # downloads vegeta — already done if go.sum exists
```

### Run
```powershell
cd load-test/rest
go run .
```

### Phases
| Phase | RPS | Duration | Goal |
|---|---|---|---|
| 1 Smoke | 5 | 30s | Verify auth + endpoints work |
| 2 Ramp | 10→25→50→100 | 30s each | Find latency knee point |
| 3 Load | 100 | 3 min | SLA baseline (p95 target < 500ms) |
| 4 Spike | 300 | 30s | Test recovery after burst |
| 5 Cool | 50 | 30s | Confirm latency returns to baseline |

### Endpoints tested (round-robin)
- `GET /api/telemetry/latest?ids=<all 4 machines>` × 2 (highest real-world frequency)
- `GET /api/telemetry/<cwID>/series?field=weight&timeRange=1h` (heaviest — time_bucket query)
- `GET /api/telemetry/<tsID>/series?field=temp&timeRange=1h`
- `GET /api/telemetry/<cwID>/series?field=weight&timeRange=24h`
- `GET /api/telemetry/<cwID>/daily-count?days=7`
- `GET /api/machines`
- `GET /api/dashboards`

---

## WebSocket Load Test — k6

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
| 1 Smoke | 2 | 30s | Verify connect + subscribe + receive |
| 2 Ramp | 5→100 | 90s | Max subscribers before broadcast slows |
| 3 Load | 100 | 3 min | SLA baseline for concurrent subscribers |
| 4 Spike | 300 | 30s | ws_gateway fan-out under pressure |
| 5 Cool | 100→0 | 30s | Goroutine cleanup — no leak |

### VU lifecycle (30s per VU)
1. Connect `ws://localhost:4001?token=<jwt>`
2. Subscribe to all 4 machines
3. Receive telemetry/alert/machine_status messages
4. Ping every 10s (keep-alive)
5. Unsubscribe CB-01 + VC-01 at 15s (simulates widget removal)
6. Close at 30s

### Custom metrics
| Metric | Threshold | Meaning |
|---|---|---|
| `telemetry_msgs` | count > 0 | Must receive at least one broadcast |
| `broadcast_latency_ms` | p(95) < 3000ms | Server → client latency |
| `ws_connecting` | p(95) < 500ms | Handshake + JWT verify time |

---

## Combined Pressure Test (recommended)

Run both simultaneously to see how REST degrades under WS broadcast load.

```powershell
# Terminal 1 — REST
cd load-test/rest; go run .

# Terminal 2 — WebSocket
k6 run load-test/ws/ws-flow.js
```

**Watch for:** REST p99 spiking during WS Phase 4 spike → indicates pgx connection
pool exhaustion from concurrent WS subscribers triggering telemetry reads.
