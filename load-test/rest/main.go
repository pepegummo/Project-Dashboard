package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

const (
	baseURL  = "http://localhost:4000"
	email    = "admin@acme-foods.com"
	password = "Admin@1234"

	cwID        = "00000000-0000-0000-0000-000000000005" // CW-01 Checkweigher
	tsID        = "00000000-0000-0000-0000-000000000006" // TS-01 Temp Sensor
	cbID        = "00000000-0000-0000-0000-000000000007" // CB-01 Conveyor
	vcID        = "00000000-0000-0000-0000-000000000008" // VC-01 Vision Camera
	dashboardID = "00000000-0000-0000-0000-000000000010" // seeded "Production Overview"
)

func login() string {
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	resp, err := http.Post(baseURL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		log.Fatalf("login request failed: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Fatalf("login decode failed: %v", err)
	}
	if result.Data.Token == "" {
		log.Fatalf("no token in login response")
	}
	return result.Data.Token
}

func buildTargets(token string) []vegeta.Target {
	auth := http.Header{"Authorization": []string{"Bearer " + token}}
	allIDs := strings.Join([]string{cwID, tsID, cbID, vcID}, ",")

	return []vegeta.Target{
		// ── Dashboard load flow ────────────────────────────────────────────────
		// 1. List dashboards (DashboardListPage)
		{Method: "GET", URL: baseURL + "/api/dashboards", Header: auth},
		// 2. Load specific dashboard + all widgets (DashboardEditorPage)
		{Method: "GET", URL: baseURL + "/api/dashboards/" + dashboardID, Header: auth},
		// 3. Machine list (sidebar + widget rendering)
		{Method: "GET", URL: baseURL + "/api/machines", Header: auth},

		// ── Widget data — line-chart (time_bucket queries, heaviest) ──────────
		// Widget 14: CW-01 weight (default 1h)
		{Method: "GET", URL: baseURL + "/api/telemetry/" + cwID + "/series?field=weight&timeRange=1h", Header: auth},
		// Widget 18: CB-01 belt speed
		{Method: "GET", URL: baseURL + "/api/telemetry/" + cbID + "/series?field=speed&timeRange=1h", Header: auth},
		// Longer range — stress TimescaleDB chunk scans
		{Method: "GET", URL: baseURL + "/api/telemetry/" + cwID + "/series?field=weight&timeRange=24h", Header: auth},
		// 1-year range — max chunk scan (1h bucket, ~8760 pts, ~52 chunks)
		{Method: "GET", URL: baseURL + "/api/telemetry/" + cwID + "/series?field=weight&timeRange=1y", Header: auth},
		{Method: "GET", URL: baseURL + "/api/telemetry/" + tsID + "/series?field=temp&timeRange=1y", Header: auth},

		// ── Widget data — gauge + kpi-card (aggregate summary) ────────────────
		// Widget 15: CW-01 weight gauge
		{Method: "GET", URL: baseURL + "/api/telemetry/" + cwID + "/aggregate?field=weight&period=1h", Header: auth},
		// Widget 16: TS-01 temperature kpi-card
		{Method: "GET", URL: baseURL + "/api/telemetry/" + tsID + "/aggregate?field=temp&period=1h", Header: auth},
		// Widget 17: CW-01 throughput kpi-card
		{Method: "GET", URL: baseURL + "/api/telemetry/" + cwID + "/aggregate?field=throughput&period=1h", Header: auth},

		// ── Widget data — alarm-panel (public endpoint, no auth required) ─────
		// Widget 19: active alert events
		{Method: "GET", URL: baseURL + "/api/alerts/events/active", Header: auth},

		// ── Live snapshot — polled every 2s by telemetry store ────────────────
		{Method: "GET", URL: baseURL + "/api/telemetry/latest?ids=" + allIDs, Header: auth},
		{Method: "GET", URL: baseURL + "/api/telemetry/latest?ids=" + allIDs, Header: auth},

		// ── Daily-count bar chart widget ──────────────────────────────────────
		{Method: "GET", URL: baseURL + "/api/telemetry/" + cwID + "/daily-count?days=7", Header: auth},
	}
}

type phaseResult struct {
	Name        string
	RateRPS     int
	Requests    uint64
	Throughput  float64
	SuccessPct  float64
	P50         time.Duration
	P95         time.Duration
	P99         time.Duration
	Max         time.Duration
	StatusCodes map[string]int
}

func runPhase(name string, rateRPS int, duration time.Duration, targets []vegeta.Target) phaseResult {
	fmt.Printf("\n┌─────────────────────────────────────────────┐\n")
	fmt.Printf("│  %-43s│\n", fmt.Sprintf("%s  —  %d RPS / %s", name, rateRPS, duration))
	fmt.Printf("└─────────────────────────────────────────────┘\n")

	rate := vegeta.Rate{Freq: rateRPS, Per: time.Second}
	targeter := vegeta.NewStaticTargeter(targets...)
	attacker := vegeta.NewAttacker(
		vegeta.KeepAlive(true),
		vegeta.Connections(500),
		vegeta.Timeout(60*time.Second),
	)

	var metrics vegeta.Metrics
	for res := range attacker.Attack(targeter, rate, duration, name) {
		metrics.Add(res)
	}
	metrics.Close()

	successPct := metrics.Success * 100
	fmt.Printf("  Requests    %d\n", metrics.Requests)
	fmt.Printf("  Throughput  %.1f req/s\n", metrics.Throughput)
	fmt.Printf("  Success     %.2f%%\n", successPct)
	fmt.Printf("  p50         %s\n", metrics.Latencies.P50.Round(time.Millisecond))
	fmt.Printf("  p95         %s\n", metrics.Latencies.P95.Round(time.Millisecond))
	fmt.Printf("  p99         %s\n", metrics.Latencies.P99.Round(time.Millisecond))
	fmt.Printf("  max         %s\n", metrics.Latencies.Max.Round(time.Millisecond))
	fmt.Printf("  StatusCodes %v\n", metrics.StatusCodes)

	if successPct < 95 {
		fmt.Printf("  ⚠  WARNING: success rate below 95%% — check DB pool / backend logs\n")
	}
	if metrics.Latencies.P99 > 2*time.Second {
		fmt.Printf("  ⚠  WARNING: p99 above 2s — TimescaleDB may be saturated\n")
	}

	return phaseResult{
		Name:        name,
		RateRPS:     rateRPS,
		Requests:    metrics.Requests,
		Throughput:  metrics.Throughput,
		SuccessPct:  successPct,
		P50:         metrics.Latencies.P50,
		P95:         metrics.Latencies.P95,
		P99:         metrics.Latencies.P99,
		Max:         metrics.Latencies.Max,
		StatusCodes: metrics.StatusCodes,
	}
}

func runDiagnostic(targets []vegeta.Target) {
	fmt.Printf("\n── Diagnostic: one request per endpoint ──\n")
	client := &http.Client{Timeout: 10 * time.Second}
	for _, t := range targets {
		req, err := http.NewRequest(t.Method, t.URL, nil)
		if err != nil {
			fmt.Printf("  [ERR]  %s  %s  — build request: %v\n", t.Method, t.URL, err)
			continue
		}
		for k, vals := range t.Header {
			for _, v := range vals {
				req.Header.Set(k, v)
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("  [ERR]  %s  %s  — %v\n", t.Method, t.URL, err)
			continue
		}
		resp.Body.Close()
		marker := " "
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			marker = " ← BROKEN"
		}
		fmt.Printf("  [%d]  %s  %s%s\n", resp.StatusCode, t.Method, t.URL, marker)
	}
	fmt.Println()
}

func writeReport(phases []phaseResult, startTime time.Time) {
	type chartPhase struct {
		Name       string         `json:"name"`
		RateRPS    int            `json:"rateRPS"`
		Requests   uint64         `json:"requests"`
		Throughput float64        `json:"throughput"`
		SuccessPct float64        `json:"successPct"`
		P50Ms      float64        `json:"p50Ms"`
		P95Ms      float64        `json:"p95Ms"`
		P99Ms      float64        `json:"p99Ms"`
		MaxMs      float64        `json:"maxMs"`
		Codes      map[string]int `json:"codes"`
	}

	cp := make([]chartPhase, len(phases))
	for i, p := range phases {
		cp[i] = chartPhase{
			Name:       p.Name,
			RateRPS:    p.RateRPS,
			Requests:   p.Requests,
			Throughput: p.Throughput,
			SuccessPct: p.SuccessPct,
			P50Ms:      float64(p.P50.Milliseconds()),
			P95Ms:      float64(p.P95.Milliseconds()),
			P99Ms:      float64(p.P99.Milliseconds()),
			MaxMs:      float64(p.Max.Milliseconds()),
			Codes:      p.StatusCodes,
		}
	}

	dataJSON, _ := json.Marshal(cp)
	duration := time.Since(startTime).Round(time.Second)

	f, err := os.Create("report.html")
	if err != nil {
		fmt.Printf("⚠  Could not write report.html: %v\n", err)
		return
	}
	defer f.Close()

	fmt.Fprintf(f, `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>IoT Dashboard — REST Load Test Report</title>
<script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.0/dist/chart.umd.min.js"></script>
<style>
  *{box-sizing:border-box;margin:0;padding:0}
  body{font-family:system-ui,sans-serif;background:#0f172a;color:#e2e8f0;padding:24px}
  h1{font-size:1.5rem;font-weight:700;color:#f8fafc;margin-bottom:4px}
  .meta{font-size:.85rem;color:#94a3b8;margin-bottom:28px}
  .meta span{margin-right:20px}
  .grid{display:grid;grid-template-columns:1fr 1fr;gap:20px;margin-bottom:20px}
  .card{background:#1e293b;border-radius:12px;padding:20px}
  .card.full{grid-column:1/-1}
  .card h2{font-size:.95rem;font-weight:600;color:#cbd5e1;margin-bottom:16px;text-transform:uppercase;letter-spacing:.05em}
  canvas{max-height:260px}
  table{width:100%%;border-collapse:collapse;font-size:.82rem}
  th{background:#0f172a;color:#94a3b8;font-weight:600;text-align:left;padding:8px 12px;border-bottom:1px solid #334155}
  td{padding:7px 12px;border-bottom:1px solid #1e293b;color:#cbd5e1}
  tr:hover td{background:#1e293b}
  .ok{color:#4ade80} .warn{color:#fb923c} .bad{color:#f87171}
  .tag{display:inline-block;font-size:.72rem;padding:1px 6px;border-radius:4px;margin:1px;background:#0f172a}
</style>
</head>
<body>
<h1>IoT Dashboard — REST Load Test Report</h1>
<div class="meta">
  <span>🕐 %s</span>
  <span>⏱ Duration: %s</span>
  <span>🎯 Target: %s</span>
</div>

<div class="grid">
  <div class="card">
    <h2>Success Rate (%%) per Phase</h2>
    <canvas id="successChart"></canvas>
  </div>
  <div class="card">
    <h2>Throughput (req/s) per Phase</h2>
    <canvas id="throughputChart"></canvas>
  </div>
  <div class="card full">
    <h2>Latency per Phase (ms)</h2>
    <canvas id="latencyChart"></canvas>
  </div>
  <div class="card full">
    <h2>Full Results Table</h2>
    <table>
      <thead><tr>
        <th>Phase</th><th>Rate RPS</th><th>Requests</th>
        <th>Throughput</th><th>Success</th>
        <th>p50</th><th>p95</th><th>p99</th><th>Max</th><th>Status Codes</th>
      </tr></thead>
      <tbody id="tableBody"></tbody>
    </table>
  </div>
</div>

<script>
const phases = %s;

const labels = phases.map(p => p.name.replace(/PHASE \d+\s+/,''));
const successColors = phases.map(p =>
  p.successPct >= 99 ? '#4ade80' : p.successPct >= 95 ? '#fb923c' : '#f87171'
);

new Chart(document.getElementById('successChart'), {
  type: 'bar',
  data: {
    labels,
    datasets: [{
      label: 'Success %%',
      data: phases.map(p => p.successPct.toFixed(2)),
      backgroundColor: successColors,
      borderRadius: 4,
    }]
  },
  options: {
    plugins: { legend: { display: false } },
    scales: {
      x: { ticks: { color: '#94a3b8', maxRotation: 35 }, grid: { color: '#1e293b' } },
      y: { min: 0, max: 100, ticks: { color: '#94a3b8' }, grid: { color: '#334155' } }
    }
  }
});

new Chart(document.getElementById('throughputChart'), {
  type: 'line',
  data: {
    labels,
    datasets: [{
      label: 'req/s',
      data: phases.map(p => p.throughput.toFixed(1)),
      borderColor: '#38bdf8',
      backgroundColor: 'rgba(56,189,248,.15)',
      fill: true,
      tension: 0.3,
      pointRadius: 5,
    }]
  },
  options: {
    plugins: { legend: { display: false } },
    scales: {
      x: { ticks: { color: '#94a3b8', maxRotation: 35 }, grid: { color: '#1e293b' } },
      y: { ticks: { color: '#94a3b8' }, grid: { color: '#334155' } }
    }
  }
});

new Chart(document.getElementById('latencyChart'), {
  type: 'bar',
  data: {
    labels,
    datasets: [
      { label: 'p50', data: phases.map(p => p.p50Ms), backgroundColor: '#4ade80', borderRadius: 3 },
      { label: 'p95', data: phases.map(p => p.p95Ms), backgroundColor: '#fb923c', borderRadius: 3 },
      { label: 'p99', data: phases.map(p => p.p99Ms), backgroundColor: '#f87171', borderRadius: 3 },
    ]
  },
  options: {
    plugins: { legend: { labels: { color: '#94a3b8' } } },
    scales: {
      x: { ticks: { color: '#94a3b8', maxRotation: 35 }, grid: { color: '#1e293b' } },
      y: { ticks: { color: '#94a3b8', callback: v => v+'ms' }, grid: { color: '#334155' } }
    }
  }
});

const tbody = document.getElementById('tableBody');
phases.forEach(p => {
  const cls = p.successPct >= 99 ? 'ok' : p.successPct >= 95 ? 'warn' : 'bad';
  const codes = Object.entries(p.codes || {})
    .map(([k,v]) => '<span class="tag">' + k + ': ' + v + '</span>').join('');
  const rps = p.rateRPS === 0 ? 'unlimited' : p.rateRPS;
  tbody.innerHTML +=
    '<tr>' +
    '<td>' + p.name + '</td>' +
    '<td>' + rps + '</td>' +
    '<td>' + p.requests.toLocaleString() + '</td>' +
    '<td>' + p.throughput.toFixed(1) + '</td>' +
    '<td class="' + cls + '">' + p.successPct.toFixed(2) + '%%</td>' +
    '<td>' + p.p50Ms + 'ms</td>' +
    '<td>' + p.p95Ms + 'ms</td>' +
    '<td class="' + cls + '">' + p.p99Ms + 'ms</td>' +
    '<td>' + p.maxMs + 'ms</td>' +
    '<td>' + codes + '</td>' +
    '</tr>';
});
</script>
</body>
</html>`,
		startTime.Format("2006-01-02 15:04:05"),
		duration,
		baseURL,
		string(dataJSON),
	)

	fmt.Println("\n📊 Report saved → report.html")
	exec.Command("cmd", "/c", "start", "report.html").Start()
}

func main() {
	fmt.Println("══════════════════════════════════════════════")
	fmt.Println("  IoT Dashboard — REST Load Test (vegeta)")
	fmt.Println("  Target:", baseURL)
	fmt.Println("══════════════════════════════════════════════")

	fmt.Print("\nLogging in... ")
	token := login()
	fmt.Printf("OK (%.20s…)\n", token)

	targets := buildTargets(token)
	fmt.Printf("Endpoints: %d targets (round-robin)\n", len(targets))

	runDiagnostic(targets)

	startTime := time.Now()
	var phases []phaseResult

	// Phase 1: Smoke — verify no errors at minimal load
	phases = append(phases, runPhase("PHASE 1  SMOKE", 5, 30*time.Second, targets))

	// Phase 2: Ramp — find the latency knee point
	for _, rps := range []int{10, 25, 50, 100} {
		phases = append(phases, runPhase(fmt.Sprintf("PHASE 2  RAMP %d RPS", rps), rps, 30*time.Second, targets))
	}

	// Phase 3: Sustained load — SLA baseline
	phases = append(phases, runPhase("PHASE 3  SUSTAINED LOAD", 100, 3*time.Minute, targets))

	// Phase 4: Spike — test recovery under burst
	phases = append(phases, runPhase("PHASE 4  SPIKE", 300, 30*time.Second, targets))

	// Phase 5: Cool down — confirm latency returns to baseline (no leak)
	phases = append(phases, runPhase("PHASE 5  COOL DOWN", 50, 30*time.Second, targets))

	fmt.Println("\n══════════════════════════════════════════════")
	fmt.Println("  Load test complete")
	fmt.Println("══════════════════════════════════════════════")

	writeReport(phases, startTime)
}
