// Backfill — Random Telegraph Pulse telemetry at 1-minute resolution
//
// Ports the algorithm from backend/prisma/backfill.ts exactly.
//
// Shape  : Random pulse signal — NO fixed cycle.
//          Each field independently alternates between HIGH and LOW state
//          for a random duration (days). Smooth 5-day linear transitions.
// Noise  : ① Gaussian σ=22%  ② spike P=2.5% ±45%  ③ drift ±30% (30-day)
//          ④ vib ±8%  ⑤ micro ±3%  ⑥ wobble ±6%  ⑦ burst P=1.5% ±12%
//          Hard clamp: threshold ×0.90 – ×1.10  (±10% of threshold)
// Amplitude: ±25% of threshold
// Dates  : 2025-05-01 → 2026-06-10  (≈ 405 days)
// Total  : ~2.3 M rows  (4 machines × ~583 k points)
//
// Run:    docker compose exec backend ./backfill
//         DATABASE_URL=<url> ./backfill

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"iot-dashboard/internal/migrate"
)

// ─── Constants (identical to backend/prisma/backfill.ts) ──────────────────────
const (
	startDateStr = "2025-08-01T00:00:00Z"
	endDateStr   = "2026-08-15T23:59:00Z"
	batchSize    = 5_000
	transTicks   = 7_200  // 5-day smooth transition edge  (5 × 24 × 60)
	driftPeriod  = 43_200 // 30-day sinusoidal drift       (30 × 24 × 60)
	minsPerDay   = 1_440
)

// ─── RTP Config ───────────────────────────────────────────────────────────────
type rtpConfig struct {
	threshold   float64
	precision   int
	highMinDays float64
	highMaxDays float64
	lowMinDays  float64
	lowMaxDays  float64
}

// ─── Segment (pre-computed HIGH/LOW timeline) ─────────────────────────────────
type segment struct {
	start  int
	end    int
	isHigh bool
}

// ─── RTP Generator ────────────────────────────────────────────────────────────
// Mirrors makeRTPGen() in backfill.ts.
// Pre-computes the full random segment timeline once; subsequent next() calls
// just walk a pointer forward (amortised O(1)).
type rtpGen struct {
	segs      []segment
	threshold float64
	amplitude float64
	precision int
	idx       int // walking pointer
}

func newRTPGen(cfg rtpConfig, totalTicks int) *rtpGen {
	amp := cfg.threshold * 0.25
	var segs []segment
	t := 0
	isHigh := true // always start in HIGH state
	for t < totalTicks {
		var days float64
		if isHigh {
			days = cfg.highMinDays + rand.Float64()*(cfg.highMaxDays-cfg.highMinDays)
		} else {
			days = cfg.lowMinDays + rand.Float64()*(cfg.lowMaxDays-cfg.lowMinDays)
		}
		dur := int(math.Round(days * minsPerDay))
		end := t + dur
		if end > totalTicks {
			end = totalTicks
		}
		segs = append(segs, segment{start: t, end: end, isHigh: isHigh})
		t += dur
		isHigh = !isHigh
	}
	return &rtpGen{
		segs:      segs,
		threshold: cfg.threshold,
		amplitude: amp,
		precision: cfg.precision,
	}
}

func (g *rtpGen) next(tick int) float64 {
	// Advance walking pointer (usually a no-op; a few steps at boundaries)
	for g.idx+1 < len(g.segs) && tick >= g.segs[g.idx].end {
		g.idx++
	}

	seg := g.segs[g.idx]
	var prev, next *segment
	if g.idx > 0 {
		prev = &g.segs[g.idx-1]
	}
	if g.idx+1 < len(g.segs) {
		next = &g.segs[g.idx+1]
	}

	hv := g.amplitude  // HIGH plateau offset
	lv := -g.amplitude // LOW  plateau offset
	valueOf := func(high bool) float64 {
		if high {
			return hv
		}
		return lv
	}

	// ── Base value with smooth linear transition at segment boundaries ─────
	var base float64
	boundary := seg.end

	if next != nil && tick >= boundary-transTicks {
		// Near end of current segment → blending into next
		t0 := clamp01(float64(tick-(boundary-transTicks)) / float64(2*transTicks))
		base = lerp(valueOf(seg.isHigh), valueOf(next.isHigh), t0)
	} else if prev != nil && tick < seg.start+transTicks {
		// Near start of current segment → still blending from previous
		t0 := clamp01(float64(tick-(seg.start-transTicks)) / float64(2*transTicks))
		base = lerp(valueOf(prev.isHigh), valueOf(seg.isHigh), t0)
	} else {
		base = valueOf(seg.isHigh) // flat plateau
	}

	// ── Layer 1: Gaussian noise (Box-Muller) — σ = 45% of amplitude ───────
	u1 := rand.Float64() + 1e-10
	u2 := rand.Float64()
	gaussNoise := math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2) * 0.45 * g.amplitude

	// ── Layer 2: Spike bursts  P=2.5%  ±45% of amplitude ──────────────────
	spikeNoise := 0.0
	if rand.Float64() < 0.025 {
		spikeNoise = (rand.Float64() - 0.5) * 0.90 * g.amplitude
	}

	// ── Layer 3: Long sinusoidal drift  ±30%  30-day period ───────────────
	drift := g.amplitude * 0.30 * math.Sin(2*math.Pi*float64(tick)/driftPeriod)

	// ── Layer 4: Fast vibration  ±20% ──────────────────────────────────────
	vibration := g.amplitude * 0.20 * math.Sin(float64(tick)*1.8)

	// ── Layer 5: Micro-vibration  ±12% ─────────────────────────────────────
	micro := g.amplitude * 0.12 * math.Sin(float64(tick)*7)

	// ── Layer 6: Correlated wobble  ±15% ───────────────────────────────────
	wobble := g.amplitude * 0.15 * math.Sin(float64(tick)*0.15+math.Sin(float64(tick)*0.03))

	// ── Layer 7: Burst oscillation  P=1.5%  ±12% ──────────────────────────
	burst := 0.0
	if rand.Float64() < 0.015 {
		burst = g.amplitude * 0.12 * math.Sin(float64(tick)*5)
	}

	val := g.threshold + base + gaussNoise + spikeNoise + drift + vibration + micro + wobble + burst

	// Hard clamp ±10% of threshold
	val = math.Max(g.threshold*0.90, math.Min(g.threshold*1.10, val))

	// Round to field precision
	factor := math.Pow(10, float64(g.precision))
	return math.Round(val*factor) / factor
}

// ─── Helpers ──────────────────────────────────────────────────────────────────
func clamp01(v float64) float64    { return math.Max(0, math.Min(1, v)) }
func lerp(a, b, t float64) float64 { return a*(1-t) + b*t }

// fmtNum formats an integer with comma separators (e.g. 2334720 → "2,334,720")
func fmtNum(n int) string {
	s := fmt.Sprintf("%d", n)
	out := ""
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out += ","
		}
		out += string(c)
	}
	return out
}

// ─── Batch row ────────────────────────────────────────────────────────────────
type batchRow struct {
	machineID string
	ts        time.Time
	data      map[string]interface{}
}

// ─── Alert rule (loaded from DB for historical evaluation) ────────────────────
type alertRuleDef struct {
	id          string
	machineID   string
	name        string
	field       string
	condition   string
	threshold   float64
	thresholdHi *float64
	severity    string
}

func checkAlertCondition(val float64, condition string, threshold float64, thresholdHi *float64) bool {
	switch condition {
	case "gt":
		return val > threshold
	case "lt":
		return val < threshold
	case "gte":
		return val >= threshold
	case "lte":
		return val <= threshold
	case "between":
		return thresholdHi != nil && val >= threshold && val <= *thresholdHi
	case "outside":
		return thresholdHi != nil && (val < threshold || val > *thresholdHi)
	}
	return false
}

// ─── Bulk insert via pgx SendBatch ────────────────────────────────────────────
// SendBatch pipelines all statements in one network round-trip; equivalent
// to the multi-row INSERT used by the TypeScript backfill.
func insertBatch(ctx context.Context, pool *pgxpool.Pool, rows []batchRow) error {
	b := &pgx.Batch{}
	for _, r := range rows {
		dataJSON, err := json.Marshal(r.data)
		if err != nil {
			return err
		}
		b.Queue(
			`INSERT INTO telemetry_raw (machine_id, timestamp, data, quality)
			 VALUES ($1, $2, $3::jsonb, $4)`,
			r.machineID, r.ts, string(dataJSON), "good",
		)
	}
	br := pool.SendBatch(ctx, b)
	defer br.Close()
	for range rows {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

// ─── Progress bar ─────────────────────────────────────────────────────────────
func showProgress(done, total int, label string) {
	pct := int(float64(done) / float64(total) * 100)
	filled := pct / 2
	if filled > 50 {
		filled = 50
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", 50-filled)
	fmt.Printf("\r  [%s] %3d%%  %s / %s  %-22s",
		bar, pct, fmtNum(done), fmtNum(total), label)
}

// ─── Main ─────────────────────────────────────────────────────────────────────
func main() {
	_ = godotenv.Load() // load .env if present (ignored in Docker where env is injected)

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Fprintln(os.Stderr, "❌  DATABASE_URL is not set")
		os.Exit(1)
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌  DB connect failed: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()
	fmt.Println("✅  Database connected")

	// Ensure schema + seed exist (idempotent — skipped if already set up)
	fmt.Println("🔧  Ensuring schema and seed data...")
	if err := migrate.RunAll(ctx, pool); err != nil {
		fmt.Fprintf(os.Stderr, "❌  Schema setup failed: %v\n", err)
		os.Exit(1)
	}

	// Load active alert rules for historical evaluation
	ruleRows, err := pool.Query(ctx, `
		SELECT id, machine_id, name, field, condition, threshold, threshold_hi, severity
		FROM alerts WHERE is_active = true
	`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌  Failed to load alert rules: %v\n", err)
		os.Exit(1)
	}
	rulesByMachine := map[string][]alertRuleDef{}
	for ruleRows.Next() {
		var r alertRuleDef
		if err := ruleRows.Scan(&r.id, &r.machineID, &r.name, &r.field, &r.condition, &r.threshold, &r.thresholdHi, &r.severity); err == nil {
			rulesByMachine[r.machineID] = append(rulesByMachine[r.machineID], r)
		}
	}
	ruleRows.Close()
	fmt.Printf("✅  Loaded %d alert rule(s)\n", func() int {
		n := 0
		for _, rs := range rulesByMachine {
			n += len(rs)
		}
		return n
	}())

	// Parse date range
	startDate, _ := time.Parse(time.RFC3339, startDateStr)
	endDate, _ := time.Parse(time.RFC3339, endDateStr)
	rowsPerMachine := int(endDate.Sub(startDate).Minutes())
	totalRows := rowsPerMachine * 4
	spanDays := endDate.Sub(startDate).Hours() / 24

	fmt.Printf("\n⚡  Backfill — Random Telegraph Pulse (no fixed cycle, 1 min resolution)\n")
	fmt.Printf("   Date range  : %s → %s  (%.1f days)\n", startDateStr[:10], endDateStr[:10], spanDays)
	fmt.Printf("   Interval    : 1 minute per point\n")
	fmt.Printf("   Amplitude   : ±25%% of threshold, hard clamp ±10%%\n")
	fmt.Printf("   Transition  : %s min = 5-day smooth edge\n", fmtNum(transTicks))
	fmt.Printf("   Noise layers: ① Gauss σ=22%%  ② spike P=2.5%% ±45%%  ③ drift ±30%% (30d)\n")
	fmt.Printf("                 ④ vib ±8%%  ⑤ micro ±3%%  ⑥ wobble ±6%%  ⑦ burst P=1.5%% ±12%%\n")
	fmt.Printf("   Total rows  : ~%s  (%s per machine × 4)\n", fmtNum(totalRows), fmtNum(rowsPerMachine))
	fmt.Printf("   Batch size  : %s rows per flush\n\n", fmtNum(batchSize))

	// Clear existing telemetry + alert event data
	fmt.Print("🗑️   Clearing existing telemetry data...")
	ct, err := pool.Exec(ctx, "DELETE FROM telemetry_raw")
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n❌  Delete failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf(" deleted %s rows\n", fmtNum(int(ct.RowsAffected())))
	fmt.Print("🗑️   Clearing existing alert events...")
	ae, _ := pool.Exec(ctx, "DELETE FROM alert_events")
	fmt.Printf(" deleted %s rows\n\n", fmtNum(int(ae.RowsAffected())))

	// Cutoff: events triggered before this date are auto-resolved
	openCutoff := endDate.Add(-7 * 24 * time.Hour)
	// Minimum ticks between alert events for the same rule (4 hours)
	const histCooldownTicks = 240
	totalAlertEvents := 0

	// ── CW-01: Checkweigher ───────────────────────────────────────────────────
	cwWeight := newRTPGen(rtpConfig{
		threshold: 500, precision: 2,
		highMinDays: 45, highMaxDays: 90, lowMinDays: 14, lowMaxDays: 45,
	}, rowsPerMachine)
	cwSpeed := newRTPGen(rtpConfig{
		threshold: 60, precision: 1,
		highMinDays: 40, highMaxDays: 85, lowMinDays: 14, lowMaxDays: 40,
	}, rowsPerMachine)
	cwThroughput := newRTPGen(rtpConfig{
		threshold: 60, precision: 1,
		highMinDays: 40, highMaxDays: 85, lowMinDays: 14, lowMaxDays: 40,
	}, rowsPerMachine)
	cwRejects := newRTPGen(rtpConfig{
		threshold: 1.5, precision: 0,
		highMinDays: 20, highMaxDays: 60, lowMinDays: 10, lowMaxDays: 30,
	}, rowsPerMachine)

	// ── TS-01: Temp Sensor ────────────────────────────────────────────────────
	tsTemp := newRTPGen(rtpConfig{
		threshold: 22, precision: 2,
		highMinDays: 45, highMaxDays: 90, lowMinDays: 14, lowMaxDays: 45,
	}, rowsPerMachine)
	tsHumidity := newRTPGen(rtpConfig{
		threshold: 55, precision: 1,
		highMinDays: 40, highMaxDays: 80, lowMinDays: 14, lowMaxDays: 40,
	}, rowsPerMachine)
	tsDewPoint := newRTPGen(rtpConfig{
		threshold: 11, precision: 2,
		highMinDays: 40, highMaxDays: 80, lowMinDays: 14, lowMaxDays: 40,
	}, rowsPerMachine)

	// ── CB-01: Conveyor Belt ──────────────────────────────────────────────────
	cbSpeed := newRTPGen(rtpConfig{
		threshold: 1000, precision: 1,
		highMinDays: 45, highMaxDays: 90, lowMinDays: 14, lowMaxDays: 45,
	}, rowsPerMachine)
	cbLoad := newRTPGen(rtpConfig{
		threshold: 45, precision: 1,
		highMinDays: 40, highMaxDays: 85, lowMinDays: 14, lowMaxDays: 40,
	}, rowsPerMachine)
	cbRPM := newRTPGen(rtpConfig{
		threshold: 750, precision: 0,
		highMinDays: 45, highMaxDays: 90, lowMinDays: 14, lowMaxDays: 45,
	}, rowsPerMachine)
	cbVibration := newRTPGen(rtpConfig{
		threshold: 5, precision: 2,
		highMinDays: 30, highMaxDays: 75, lowMinDays: 14, lowMaxDays: 45,
	}, rowsPerMachine)

	// ── VC-01: Vision Camera (cumulative counters) ────────────────────────────
	vcDefect := newRTPGen(rtpConfig{
		threshold: 1, precision: 3,
		highMinDays: 20, highMaxDays: 60, lowMinDays: 14, lowMaxDays: 45,
	}, rowsPerMachine)
	vcConfidence := newRTPGen(rtpConfig{
		threshold: 97, precision: 1,
		highMinDays: 45, highMaxDays: 90, lowMinDays: 14, lowMaxDays: 40,
	}, rowsPerMachine)
	var vcInspected, vcPassed, vcFailed int

	// ── Machine definitions ───────────────────────────────────────────────────
	type machineDef struct {
		id       string
		name     string
		generate func(tick int) map[string]interface{}
	}

	machines := []machineDef{
		{
			id:   "00000000-0000-0000-0000-000000000005",
			name: "Checkweigher CW-01",
			generate: func(tick int) map[string]interface{} {
				rejects := math.Max(0, cwRejects.next(tick))
				statusCode := 0
				if rejects > 0 {
					statusCode = 1
				}
				return map[string]interface{}{
					"weight":      cwWeight.next(tick),
					"speed":       cwSpeed.next(tick),
					"throughput":  cwThroughput.next(tick),
					"rejects":     rejects,
					"status_code": statusCode,
				}
			},
		},
		{
			id:   "00000000-0000-0000-0000-000000000006",
			name: "Temp Sensor TS-01",
			generate: func(tick int) map[string]interface{} {
				return map[string]interface{}{
					"temp":      tsTemp.next(tick),
					"humidity":  tsHumidity.next(tick),
					"dew_point": tsDewPoint.next(tick),
				}
			},
		},
		{
			id:   "00000000-0000-0000-0000-000000000007",
			name: "Conveyor Belt CB-01",
			generate: func(tick int) map[string]interface{} {
				return map[string]interface{}{
					"speed":     cbSpeed.next(tick),
					"load":      cbLoad.next(tick),
					"rpm":       cbRPM.next(tick),
					"vibration": cbVibration.next(tick),
				}
			},
		},
		{
			id:   "00000000-0000-0000-0000-000000000008",
			name: "Vision Camera VC-01",
			generate: func(tick int) map[string]interface{} {
				defectRate := vcDefect.next(tick)
				confidence := vcConfidence.next(tick)
				newInspected := rand.Intn(3) + 1 // 1–3 items per minute
				vcInspected += newInspected
				newFailed := 0
				if rand.Float64() < defectRate/100 {
					newFailed = 1
				}
				vcFailed += newFailed
				vcPassed += newInspected - newFailed
				return map[string]interface{}{
					"defect_rate": defectRate,
					"confidence":  confidence,
					"inspected":   vcInspected,
					"passed":      vcPassed,
					"failed":      vcFailed,
				}
			},
		},
	}

	// ── Main backfill loop ────────────────────────────────────────────────────
	overallStart := time.Now()
	totalInserted := 0

	for _, m := range machines {
		fmt.Printf("📦  %s  (%s rows)\n", m.name, fmtNum(rowsPerMachine))
		machineStart := time.Now()
		machineInserted := 0
		batch := make([]batchRow, 0, batchSize)
		lastFiredTick := map[string]int{} // rule id → last tick when event was created

		for tick := 0; tick < rowsPerMachine; tick++ {
			ts := startDate.Add(time.Duration(tick) * time.Minute)
			data := m.generate(tick)

			// Evaluate alert rules against this data point
			for _, rule := range rulesByMachine[m.id] {
				raw, ok := data[rule.field]
				if !ok {
					continue
				}
				val, ok := raw.(float64)
				if !ok {
					continue
				}
				if !checkAlertCondition(val, rule.condition, rule.threshold, rule.thresholdHi) {
					continue
				}
				if last, fired := lastFiredTick[rule.id]; fired && tick-last < histCooldownTicks {
					continue
				}
				lastFiredTick[rule.id] = tick

				status := "open"
				var resolvedAt *time.Time
				if ts.Before(openCutoff) {
					status = "resolved"
					t := ts.Add(4 * time.Hour)
					resolvedAt = &t
				}
				msg := fmt.Sprintf("%s: %s = %.2f (%s %.2f)", rule.name, rule.field, val, rule.condition, rule.threshold)
				if _, insertErr := pool.Exec(ctx, `
					INSERT INTO alert_events (id, alert_id, value, message, status, triggered_at, resolved_at)
					VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6)
				`, rule.id, val, msg, status, ts, resolvedAt); insertErr == nil {
					totalAlertEvents++
				}
			}

			batch = append(batch, batchRow{
				machineID: m.id,
				ts:        ts,
				data:      data,
			})

			if len(batch) >= batchSize {
				if err := insertBatch(ctx, pool, batch); err != nil {
					fmt.Fprintf(os.Stderr, "\n❌  Insert failed at tick %d: %v\n", tick, err)
					os.Exit(1)
				}
				machineInserted += len(batch)
				totalInserted += len(batch)
				batch = batch[:0]
				showProgress(machineInserted, rowsPerMachine, m.name)
			}
		}

		// Flush remaining rows
		if len(batch) > 0 {
			if err := insertBatch(ctx, pool, batch); err != nil {
				fmt.Fprintf(os.Stderr, "\n❌  Insert failed (final flush): %v\n", err)
				os.Exit(1)
			}
			machineInserted += len(batch)
			totalInserted += len(batch)
		}

		showProgress(machineInserted, rowsPerMachine, m.name)
		elapsed := time.Since(machineStart).Seconds()
		rate := 0
		if elapsed > 0 {
			rate = int(float64(machineInserted) / elapsed)
		}
		fmt.Printf("\n  ✅  %s rows in %.1fs  (%s rows/sec)\n\n",
			fmtNum(machineInserted), elapsed, fmtNum(rate))
	}

	totalElapsed := time.Since(overallStart).Seconds()
	fmt.Printf("🎉  Backfill complete!\n")
	fmt.Printf("   Range        : %s → %s\n", startDateStr[:10], endDateStr[:10])
	fmt.Printf("   Inserted     : %s telemetry rows\n", fmtNum(totalInserted))
	fmt.Printf("   Alert events : %s (real, from data evaluation)\n", fmtNum(totalAlertEvents))
	fmt.Printf("   Time         : %.1fs\n", totalElapsed)
	if totalElapsed > 0 {
		fmt.Printf("   Speed        : ~%s rows/sec\n", fmtNum(int(float64(totalInserted)/totalElapsed)))
	}
}
