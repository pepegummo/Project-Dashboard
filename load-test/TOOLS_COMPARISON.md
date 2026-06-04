# Load Testing Tools Comparison

Evaluated in the context of the IoT Dashboard project (Vue 3 + Go Fiber + TimescaleDB).  
Current stack: **vegeta** (REST) + **k6** (WebSocket).

---

## Tools Evaluated

| # | Tool | Language | Primary Use |
|---|---|---|---|
| 1 | **vegeta** | Go | HTTP rate-controlled hammering |
| 2 | **k6** | JS (Goja runtime) | Multi-protocol scripted scenarios |
| 3 | **Artillery** | Node.js / YAML+JS | Realistic user-flow scenarios |
| 4 | **Apache JMeter** | Java / GUI | Enterprise test plans |
| 5 | **Locust** | Python | Weighted task-mix simulation |

---

## Feature Matrix

| Feature | vegeta | k6 | Artillery | JMeter | Locust |
|---|---|---|---|---|---|
| HTTP/REST | ✅ Best | ✅ | ✅ | ✅ | ✅ |
| WebSocket | ❌ | ✅ Best | ✅ | ✅ (plugin) | ⚠️ manual |
| gRPC | ❌ | ✅ | ❌ | ✅ (plugin) | ❌ |
| Exact RPS guarantee | ✅ Token-bucket | ⚠️ Arrival-rate executor only | ⚠️ Approximate | ⚠️ Approximate | ❌ |
| Per-endpoint stats | ❌ Aggregate only | ✅ | ✅ | ✅ | ✅ |
| Think time / user flows | ❌ | ✅ | ✅ | ✅ | ✅ |
| Live web UI | ❌ | ❌ | ❌ | ❌ | ✅ localhost:8089 |
| HTML report | ❌ | ✅ (`--out html`) | ✅ | ✅ Built-in | ❌ CSV only |
| Scripting style | Go code | JS | YAML / JS hooks | GUI / XML | Python class |
| Max sustained RPS | Highest | High | Medium | Medium | Low |
| Runner overhead | Lowest | Low | Medium | High (JVM) | Medium (GIL) |
| Setup effort | Low | Low | Low | High | Medium |
| CI/CD friendly | ✅ | ✅ | ✅ | ⚠️ headless mode | ✅ |

---

## Per-Tool Detail

### 1. vegeta

**What it does well**

- Token-bucket rate limiter delivers the exact configured RPS independent of server response time — critical for finding the true latency-vs-load curve.
- Pure Go binary; near-zero runner overhead means the tool never becomes the bottleneck before the server does.
- Simple API: build a `[]Target` slice, call `attacker.Attack()`, read `Metrics`.

**Limitations**

- HTTP only — no WebSocket, gRPC, or TCP.
- No per-endpoint breakdown; all targets are aggregated into one `Metrics` struct per phase.
- No built-in HTML report; terminal output only.

**Best for:** High-RPS HTTP load tests where rate precision matters (finding the exact knee point on a latency curve).

**Used in this project:** `load-test/rest/main.go` — 5-phase REST test (smoke → ramp → sustained → spike → cool).

---

### 2. k6

**What it does well**

- Native WebSocket support (`k6/ws`) with the same scenario/threshold DSL as HTTP tests.
- Per-URL breakdown in the summary output.
- Rich custom metrics (`Counter`, `Trend`, `Rate`, `Gauge`) for domain-specific measurement (e.g. `broadcast_latency_ms`).
- `constant-arrival-rate` executor can match vegeta-style rate control for HTTP when needed.

**Limitations**

- JS runtime (Goja) adds overhead vs vegeta for pure HTTP hammering.
- `constant-vus` (default) does not guarantee RPS — VUs block on slow responses, so actual throughput drifts under load.
- No live UI; results appear only at test end unless streamed to Grafana/InfluxDB.

**Best for:** Multi-protocol scenarios (REST + WS in one run), custom latency metrics, scripted user flows.

**Used in this project:** `load-test/ws/ws-flow.js` — 5-phase WS test simulating permanent LED kiosk connections.

---

### 3. Artillery

**What it does well**

- YAML-first config with optional JS hooks — readable test plans for non-developers.
- Native HTTP + WebSocket support in one tool.
- `think` steps simulate realistic user pauses between requests.
- Plugin ecosystem: Playwright integration for browser-level testing.

**Limitations**

- Node.js single-thread limits raw RPS ceiling; struggles above ~500 RPS on a single machine.
- Less precise rate control than vegeta at high load.
- WS support is functional but less flexible than k6 for complex protocols.

**Best for:** User-journey scenarios (login → browse → interact → logout) with realistic pacing.

**Example (partial):**
```yaml
config:
  target: "http://localhost:4000"
  phases:
    - duration: 30
      arrivalRate: 100
scenarios:
  - flow:
      - post:
          url: /api/auth/login
          json: { email: "admin@acme-foods.com", password: "Admin@1234" }
          capture:
            - json: "$.data.token"
              as: token
      - get:
          url: "/api/telemetry/{{ machineId }}/series?field=weight&timeRange=1h"
          headers:
            Authorization: "Bearer {{ token }}"
      - think: 2
```

---

### 4. Apache JMeter

**What it does well**

- Widest protocol coverage: HTTP, HTTPS, JDBC (direct DB queries), MQTT, WebSocket (plugin), gRPC (plugin), FTP.
- GUI test plan builder — accessible to QA teams without coding skills.
- Built-in HTML dashboard with percentile graphs, error rates, throughput timeline.
- Mature ecosystem; most enterprises already have JMeter expertise.

**Limitations**

- JVM startup and heap management add significant runner overhead; requires tuning (`-Xms`, `-Xmx`) for high RPS.
- XML-based `.jmx` files are verbose and hard to diff/review in git.
- GUI is resource-heavy; headless CLI mode is less discoverable.
- WebSocket support requires a third-party plugin (not bundled).

**Best for:** Enterprise environments, test plans shared with non-developers, scenarios that also test DB connections directly.

**Run headless:**
```bash
jmeter -n -t test-plan.jmx -l results.jtl -e -o report/
```

---

### 5. Locust

**What it does well**

- Live web UI at `localhost:8089` — spawn/despawn VUs interactively during the test.
- `@task(weight)` decorator makes it natural to express realistic user behaviour ratios (e.g. 3× read for every 1× write).
- Pure Python — easy to add custom logic, authenticate, chain requests.
- `locust-plugins` adds WebSocket and MQTT support.

**Limitations**

- Python GIL limits a single process to ~300–500 RPS; requires distributed mode (`--master`/`--worker`) for high load.
- No exact rate guarantee — VU count drives load, not RPS.
- WS support is unofficial; not as clean as k6.

**Best for:** Exploratory load testing with live tuning, or teams more comfortable with Python than Go/JS.

**Example:**
```python
from locust import HttpUser, task, between

class DashboardUser(HttpUser):
    wait_time = between(1, 3)

    def on_start(self):
        res = self.client.post("/api/auth/login",
            json={"email": "admin@acme-foods.com", "password": "Admin@1234"})
        self.token = res.json()["data"]["token"]
        self.client.headers["Authorization"] = f"Bearer {self.token}"

    @task(3)
    def poll_series(self):
        self.client.get(f"/api/telemetry/00000000-0000-0000-0000-000000000005"
                        f"/series?field=weight&timeRange=1h")

    @task(1)
    def load_dashboard(self):
        self.client.get("/api/dashboards/00000000-0000-0000-0000-000000000010")
```

---

## Decision Summary

### Why vegeta stays for REST

| Criterion | Winner | Reason |
|---|---|---|
| Rate precision | **vegeta** | Token-bucket guarantees exact RPS; k6 `constant-vus` drifts under slow responses |
| Runner overhead | **vegeta** | Go binary vs JS runtime; vegeta never bottlenecks before the server |
| Simplicity | **vegeta** | Already in repo, no extra install, single `go run .` command |

Switching REST to k6 would gain per-endpoint breakdown but lose rate precision and increase runner overhead — not a worthwhile trade for this project.

### Why k6 stays for WebSocket

| Criterion | Winner | Reason |
|---|---|---|
| WS support | **k6** | Native `k6/ws`; custom metrics (`broadcast_latency_ms`) are clean and idiomatic |
| Same tool as REST alt | k6 | If REST ever needs per-URL stats, k6 can handle both in one script |
| Threshold DSL | k6 | `p(95)<3000` thresholds integrate naturally with CI pass/fail |

### When to consider switching

| Scenario | Recommended tool |
|---|---|
| Need per-URL REST breakdown without changing tools | Add per-phase single-target runs in `main.go` |
| Team prefers no-code test plans | **JMeter** (GUI) or **Artillery** (YAML) |
| Need live interactive VU control | **Locust** |
| Need to test DB layer directly | **JMeter** (JDBC sampler) |
| Combined REST+WS in one script | **k6** (replace both) |

---

## Running the Current Stack

```powershell
# Prerequisites
docker compose up -d

# REST load test (vegeta)
cd load-test/rest
go mod tidy        # first time only
go run .

# WebSocket load test (k6)
winget install k6  # first time only
k6 run load-test/ws/ws-flow.js

# Combined pressure test (recommended — run simultaneously)
# Terminal 1
cd load-test/rest && go run .
# Terminal 2
k6 run load-test/ws/ws-flow.js
```
