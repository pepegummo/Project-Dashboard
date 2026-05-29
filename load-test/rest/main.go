package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

const (
	baseURL  = "http://localhost:4000"
	email    = "admin@acme-foods.com"
	password = "Admin@1234"

	cwID = "00000000-0000-0000-0000-000000000005" // CW-01 Checkweigher
	tsID = "00000000-0000-0000-0000-000000000006" // TS-01 Temp Sensor
	cbID = "00000000-0000-0000-0000-000000000007" // CB-01 Conveyor
	vcID = "00000000-0000-0000-0000-000000000008" // VC-01 Vision Camera
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
		// latest snapshot — highest frequency in real dashboards (2x weight)
		{Method: "GET", URL: baseURL + "/api/telemetry/latest?ids=" + allIDs, Header: auth},
		{Method: "GET", URL: baseURL + "/api/telemetry/latest?ids=" + allIDs, Header: auth},
		// heavy: TimescaleDB time_bucket() queries
		{Method: "GET", URL: baseURL + "/api/telemetry/" + cwID + "/series?field=weight&timeRange=1h", Header: auth},
		{Method: "GET", URL: baseURL + "/api/telemetry/" + tsID + "/series?field=temp&timeRange=1h", Header: auth},
		{Method: "GET", URL: baseURL + "/api/telemetry/" + cwID + "/series?field=weight&timeRange=24h", Header: auth},
		// 7-day daily aggregation
		{Method: "GET", URL: baseURL + "/api/telemetry/" + cwID + "/daily-count?days=7", Header: auth},
		// lightweight — baseline reference
		{Method: "GET", URL: baseURL + "/api/machines", Header: auth},
		{Method: "GET", URL: baseURL + "/api/dashboards", Header: auth},
	}
}

func runPhase(name string, rateRPS int, duration time.Duration, targets []vegeta.Target) {
	fmt.Printf("\n┌─────────────────────────────────────────────┐\n")
	fmt.Printf("│  %-43s│\n", fmt.Sprintf("%s  —  %d RPS / %s", name, rateRPS, duration))
	fmt.Printf("└─────────────────────────────────────────────┘\n")

	rate := vegeta.Rate{Freq: rateRPS, Per: time.Second}
	targeter := vegeta.NewStaticTargeter(targets...)
	attacker := vegeta.NewAttacker(vegeta.KeepAlive(true), vegeta.Connections(500))

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

	if successPct < 95 {
		fmt.Printf("  ⚠  WARNING: success rate below 95%% — check DB pool / backend logs\n")
	}
	if metrics.Latencies.P99 > 2*time.Second {
		fmt.Printf("  ⚠  WARNING: p99 above 2s — TimescaleDB may be saturated\n")
	}
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

	// Phase 1: Smoke — verify no errors at minimal load
	runPhase("PHASE 1  SMOKE", 5, 30*time.Second, targets)

	// Phase 2: Ramp — find the latency knee point
	for _, rps := range []int{10, 25, 50, 100} {
		runPhase(fmt.Sprintf("PHASE 2  RAMP %d RPS", rps), rps, 30*time.Second, targets)
	}

	// Phase 3: Sustained load — SLA baseline
	runPhase("PHASE 3  SUSTAINED LOAD", 100, 3*time.Minute, targets)

	// Phase 4: Spike — test recovery under burst
	runPhase("PHASE 4  SPIKE", 300, 30*time.Second, targets)

	// Phase 5: Cool down — confirm latency returns to baseline (no leak)
	runPhase("PHASE 5  COOL DOWN", 50, 30*time.Second, targets)

	fmt.Println("\n══════════════════════════════════════════════")
	fmt.Println("  Load test complete")
	fmt.Println("══════════════════════════════════════════════")
}
