/**
 * Backfill Script — Random Telegraph Pulse telemetry at 1-minute resolution
 *
 * Shape  : Random pulse signal — NO fixed cycle.
 *          Each field independently alternates between HIGH and LOW state
 *          for a random duration (days). Smooth 5-day linear transitions.
 *          HIGH duration: 45–90 days  |  LOW duration: 14–45 days  (field-specific)
 * Noise  : 1) Gaussian σ=22% of amplitude  (base sensor noise)
 *          2) Random spikes P=2.5%, up to ±45% of amplitude
 *          3) Sinusoidal drift ±30% of amplitude (30-day period)
 *          4) Fast vibration ±8% of amplitude
 *          5) Micro-vibration ±3% of amplitude
 *          6) Correlated wobble ±6% of amplitude
 *          7) Burst oscillation P=1.5%, ±12% of amplitude
 *          Hard clamp: threshold ×0.90 – ×1.10  (±10% of threshold)
 * Amplitude: ±25% of threshold
 * Dates  : 2025-05-01 → 2026-06-10  (≈ 405 days)
 * Rate   : 1 data point per minute per machine
 * Total  : ~2.3 M rows  (4 machines × ~583 k points)
 *
 * Run:    npx tsx prisma/backfill.ts
 * Docker: docker compose exec backend npx tsx prisma/backfill.ts
 */

import { PrismaClient } from '@prisma/client';

const prisma = new PrismaClient();

// ─── Config ───────────────────────────────────────────────────────────────────
const START_DATE   = new Date('2025-05-01T00:00:00.000Z');
const END_DATE     = new Date('2026-06-10T23:59:00.000Z');
const INTERVAL_MS  = 60_000;    // 1 minute between each point
const BATCH_SIZE   = 5_000;
const TRANS_TICKS  = 7_200;     // smooth transition edge = 5 days (5 × 24 × 60)
const DRIFT_PERIOD = 43_200;    // secondary sinusoidal drift = 30 days
const MINS_PER_DAY = 1_440;     // 24 × 60

const ROWS_PER_MACHINE = Math.floor((END_DATE.getTime() - START_DATE.getTime()) / INTERVAL_MS);
const TOTAL_ROWS       = ROWS_PER_MACHINE * 4;

// ─── Random Telegraph Pulse Generator ────────────────────────────────────────
// Creates a stateful generator for one field. Pre-computes the full random
// segment timeline once; subsequent calls just walk a pointer forward (O(1)).
//
// Segment: { start, end, isHigh }
//   HIGH → value oscillates around threshold + amplitude (upper plateau)
//   LOW  → value oscillates around threshold - amplitude (lower plateau)
//
// Transition: linear blend across ±TRANS_TICKS around each segment boundary.

interface RTPConfig {
  threshold:   number;
  precision:   number;
  highMinDays: number;
  highMaxDays: number;
  lowMinDays:  number;
  lowMaxDays:  number;
}

function makeRTPGen(config: RTPConfig, totalTicks: number): (tick: number) => number {
  const { threshold, precision, highMinDays, highMaxDays, lowMinDays, lowMaxDays } = config;
  const amplitude = threshold * 0.25;

  // ── Pre-generate segment list ──────────────────────────────────────────────
  const segments: Array<{ start: number; end: number; isHigh: boolean }> = [];
  let tick = 0;
  let isHigh = true; // always start in HIGH state

  while (tick < totalTicks) {
    const days = isHigh
      ? highMinDays + Math.random() * (highMaxDays - highMinDays)
      : lowMinDays  + Math.random() * (lowMaxDays  - lowMinDays);
    const duration = Math.round(days * MINS_PER_DAY);
    segments.push({ start: tick, end: Math.min(tick + duration, totalTicks), isHigh });
    tick += duration;
    isHigh = !isHigh;
  }

  // ── Walking pointer — ticks are called sequentially 0 → totalTicks-1 ──────
  let segIdx = 0;

  return (tick: number): number => {
    // Advance pointer (usually a no-op; max a few steps when crossing boundary)
    while (segIdx + 1 < segments.length && tick >= segments[segIdx].end) segIdx++;

    const seg     = segments[segIdx];
    const prevSeg = segIdx > 0 ? segments[segIdx - 1] : null;
    const nextSeg = segIdx + 1 < segments.length ? segments[segIdx + 1] : null;

    const highVal =  amplitude;
    const lowVal  = -amplitude;

    // ── Base value with smooth transition at segment boundaries ───────────
    let base: number;
    const boundary = seg.end; // boundary between seg and nextSeg

    if (nextSeg && tick >= boundary - TRANS_TICKS) {
      // Falling or rising INTO next segment (near end of current seg)
      const t  = (tick - (boundary - TRANS_TICKS)) / (2 * TRANS_TICKS);
      const t0 = Math.max(0, Math.min(1, t));
      base = (seg.isHigh ? highVal : lowVal) * (1 - t0) +
             (nextSeg.isHigh ? highVal : lowVal) * t0;
    } else if (prevSeg && tick < seg.start + TRANS_TICKS) {
      // Still in transition from previous segment (near start of current seg)
      const prevBoundary = seg.start;
      const t  = (tick - (prevBoundary - TRANS_TICKS)) / (2 * TRANS_TICKS);
      const t0 = Math.max(0, Math.min(1, t));
      base = (prevSeg.isHigh ? highVal : lowVal) * (1 - t0) +
             (seg.isHigh ? highVal : lowVal) * t0;
    } else {
      base = seg.isHigh ? highVal : lowVal; // flat plateau
    }

    // ── Noise layer 1: Strong Gaussian ────────────────────────────────
    const u1 = Math.random() + 1e-10;
    const u2 = Math.random();

    const gauss =
      Math.sqrt(-2 * Math.log(u1)) *
      Math.cos(2 * Math.PI * u2);

    const gaussNoise = gauss * 0.22 * amplitude;

    // ── Noise layer 2: Frequent spikes ────────────────────────────────
    const spikeNoise =
      Math.random() < 0.025
        ? (Math.random() - 0.5) * 0.90 * amplitude
        : 0;

    // ── Noise layer 3: Long drift ─────────────────────────────────────
    const drift =
      (amplitude * 0.30) *
      Math.sin((2 * Math.PI * tick) / DRIFT_PERIOD);

    // ── Noise layer 4: Fast vibration ─────────────────────────────────
    const vibration =
      amplitude * 0.08 *
      Math.sin(tick * 1.8);

    // ── Noise layer 5: Micro vibration ────────────────────────────────
    const microVibration =
      amplitude * 0.03 *
      Math.sin(tick * 7);

    // ── Noise layer 6: Correlated wobble ──────────────────────────────
    const wobble =
      amplitude * 0.06 *
      Math.sin(tick * 0.15 + Math.sin(tick * 0.03));

    // ── Noise layer 7: Burst oscillation ──────────────────────────────
    const burst =
      Math.random() < 0.015
        ? amplitude * 0.12 * Math.sin(tick * 5)
        : 0;

    // ── Final value ───────────────────────────────────────────────────
    const value =
      threshold +
      base +
      gaussNoise +
      spikeNoise +
      drift +
      vibration +
      microVibration +
      wobble +
      burst;

    // Keep hard clamp
    return +Math.max(
      threshold * 0.90,
      Math.min(threshold * 1.10, value)
    ).toFixed(precision);
  };
}

// ─── Per-field RTP generators ─────────────────────────────────────────────────
// Each field has its own independent random segment timeline.
// HIGH duration = 45–90 days, LOW duration = 14–45 days (field-specific ranges).

const cwWeightGen     = makeRTPGen({ threshold: 500,  precision: 2, highMinDays: 45, highMaxDays: 90, lowMinDays: 14, lowMaxDays: 45 }, ROWS_PER_MACHINE);
const cwSpeedGen      = makeRTPGen({ threshold: 60,   precision: 1, highMinDays: 40, highMaxDays: 85, lowMinDays: 14, lowMaxDays: 40 }, ROWS_PER_MACHINE);
const cwThroughputGen = makeRTPGen({ threshold: 60,   precision: 1, highMinDays: 40, highMaxDays: 85, lowMinDays: 14, lowMaxDays: 40 }, ROWS_PER_MACHINE);
const cwRejectsGen    = makeRTPGen({ threshold: 1.5,  precision: 0, highMinDays: 20, highMaxDays: 60, lowMinDays: 10, lowMaxDays: 30 }, ROWS_PER_MACHINE);

const tsTempGen       = makeRTPGen({ threshold: 22,   precision: 2, highMinDays: 45, highMaxDays: 90, lowMinDays: 14, lowMaxDays: 45 }, ROWS_PER_MACHINE);
const tsHumidityGen   = makeRTPGen({ threshold: 55,   precision: 1, highMinDays: 40, highMaxDays: 80, lowMinDays: 14, lowMaxDays: 40 }, ROWS_PER_MACHINE);
const tsDewGen        = makeRTPGen({ threshold: 11,   precision: 2, highMinDays: 40, highMaxDays: 80, lowMinDays: 14, lowMaxDays: 40 }, ROWS_PER_MACHINE);

const cbSpeedGen      = makeRTPGen({ threshold: 1000, precision: 1, highMinDays: 45, highMaxDays: 90, lowMinDays: 14, lowMaxDays: 45 }, ROWS_PER_MACHINE);
const cbLoadGen       = makeRTPGen({ threshold: 45,   precision: 1, highMinDays: 40, highMaxDays: 85, lowMinDays: 14, lowMaxDays: 40 }, ROWS_PER_MACHINE);
const cbRpmGen        = makeRTPGen({ threshold: 750,  precision: 0, highMinDays: 45, highMaxDays: 90, lowMinDays: 14, lowMaxDays: 45 }, ROWS_PER_MACHINE);
const cbVibrationGen  = makeRTPGen({ threshold: 5,    precision: 2, highMinDays: 30, highMaxDays: 75, lowMinDays: 14, lowMaxDays: 45 }, ROWS_PER_MACHINE);

const vcDefectGen     = makeRTPGen({ threshold: 1,    precision: 3, highMinDays: 20, highMaxDays: 60, lowMinDays: 14, lowMaxDays: 45 }, ROWS_PER_MACHINE);
const vcConfidenceGen = makeRTPGen({ threshold: 97,   precision: 1, highMinDays: 45, highMaxDays: 90, lowMinDays: 14, lowMaxDays: 40 }, ROWS_PER_MACHINE);

// ─── Machine + field definitions ─────────────────────────────────────────────
const MACHINES = [
  {
    id:   '00000000-0000-0000-0000-000000000005',
    name: 'Checkweigher CW-01',
    generate: (tick: number) => {
      const rejects = Math.max(0, cwRejectsGen(tick));
      return {
        weight:      cwWeightGen(tick),
        speed:       cwSpeedGen(tick),
        throughput:  cwThroughputGen(tick),
        rejects,
        status_code: rejects > 0 ? 1 : 0,
      };
    },
  },
  {
    id:   '00000000-0000-0000-0000-000000000006',
    name: 'Temp Sensor TS-01',
    generate: (tick: number) => ({
      temp:      tsTempGen(tick),
      humidity:  tsHumidityGen(tick),
      dew_point: tsDewGen(tick),
    }),
  },
  {
    id:   '00000000-0000-0000-0000-000000000007',
    name: 'Conveyor Belt CB-01',
    generate: (tick: number) => ({
      speed:     cbSpeedGen(tick),
      load:      cbLoadGen(tick),
      rpm:       cbRpmGen(tick),
      vibration: cbVibrationGen(tick),
    }),
  },
  {
    id:   '00000000-0000-0000-0000-000000000008',
    name: 'Vision Camera VC-01',
    inspected: 0, passed: 0, failed: 0,
    generate(tick: number) {
      const defect_rate  = vcDefectGen(tick);
      const confidence   = vcConfidenceGen(tick);
      const newInspected = Math.floor(Math.random() * 3) + 1;
      this.inspected += newInspected;
      const newFailed    = Math.random() < defect_rate / 100 ? 1 : 0;
      this.failed    += newFailed;
      this.passed    += newInspected - newFailed;
      return { defect_rate, confidence, inspected: this.inspected, passed: this.passed, failed: this.failed };
    },
  },
] as const;

// ─── Bulk INSERT ──────────────────────────────────────────────────────────────
async function insertBatch(rows: Array<{ machineId: string; timestamp: Date; data: object }>) {
  const values = rows
    .map(r => {
      const json = JSON.stringify(r.data).replace(/'/g, "''");
      const ts   = r.timestamp.toISOString();
      return `('${r.machineId}','${ts}'::timestamptz,'${json}'::jsonb,'good')`;
    })
    .join(',');
  await prisma.$executeRawUnsafe(
    `INSERT INTO telemetry_raw (machine_id, timestamp, data, quality) VALUES ${values}`,
  );
}

// ─── Progress bar ─────────────────────────────────────────────────────────────
function progress(done: number, total: number, label: string) {
  const pct    = Math.floor((done / total) * 100);
  const filled = Math.floor(pct / 2);
  const bar    = '█'.repeat(filled) + '░'.repeat(50 - filled);
  process.stdout.write(`\r  [${bar}] ${pct}%  ${done.toLocaleString()} / ${total.toLocaleString()}  ${label}   `);
}

// ─── Main ─────────────────────────────────────────────────────────────────────
async function main() {
  const spanDays = (END_DATE.getTime() - START_DATE.getTime()) / (1000 * 60 * 60 * 24);

  console.log('⚡ Backfill — Random Telegraph Pulse (no fixed cycle, 1 min resolution)');
  console.log(`   Date range : ${START_DATE.toISOString().slice(0,10)} → ${END_DATE.toISOString().slice(0,10)}  (${spanDays.toFixed(1)} days)`);
  console.log(`   Interval   : 1 minute per point`);
  console.log(`   Shape      : Random HIGH/LOW state machine, no repeating period`);
  console.log(`   HIGH state : 45–90 days  |  LOW state: 14–45 days  (per field)`);
  console.log(`   Transition : ${TRANS_TICKS.toLocaleString()} min = 5-day smooth edge`);
  console.log(`   Amplitude  : ±25% of threshold`);
  console.log(`   Noise      : ① Gauss σ=22%  ② spike P=2.5% ±45%  ③ drift ±30% (30 days)`);
  console.log(`                ④ vib ±8%  ⑤ micro ±3%  ⑥ wobble ±6%  ⑦ burst P=1.5% ±12%`);
  console.log(`   Clamp      : threshold ×0.90 – ×1.10  (±10% of threshold)`);
  console.log(`   Total rows : ${TOTAL_ROWS.toLocaleString()}  (${ROWS_PER_MACHINE.toLocaleString()} per machine × 4)`);
  console.log(`   Batch size : ${BATCH_SIZE.toLocaleString()} rows per INSERT\n`);

  // Clear existing data first
  console.log('🗑️  Clearing existing telemetry data...');
  const deleted = await prisma.telemetryRaw.deleteMany({});
  console.log(`   Deleted ${deleted.count.toLocaleString()} existing rows\n`);

  const overallStart = Date.now();
  let totalInserted  = 0;

  for (const machine of MACHINES) {
    console.log(`\n📦 ${machine.name}  (${ROWS_PER_MACHINE.toLocaleString()} rows)`);
    const machineStart = Date.now();
    let batch: Array<{ machineId: string; timestamp: Date; data: object }> = [];
    let machineInserted = 0;

    for (let tick = 0; tick < ROWS_PER_MACHINE; tick++) {
      const timestamp = new Date(START_DATE.getTime() + tick * INTERVAL_MS);
      const data      = machine.generate(tick);
      batch.push({ machineId: machine.id, timestamp, data });

      if (batch.length >= BATCH_SIZE) {
        await insertBatch(batch);
        machineInserted += batch.length;
        totalInserted   += batch.length;
        batch = [];
        progress(machineInserted, ROWS_PER_MACHINE, machine.name);
      }
    }

    if (batch.length > 0) {
      await insertBatch(batch);
      machineInserted += batch.length;
      totalInserted   += batch.length;
    }

    progress(machineInserted, ROWS_PER_MACHINE, machine.name);
    const elapsed = ((Date.now() - machineStart) / 1000).toFixed(1);
    const rate    = Math.floor(machineInserted / +elapsed).toLocaleString();
    console.log(`\n  ✅ ${machineInserted.toLocaleString()} rows in ${elapsed}s  (${rate} rows/sec)`);
  }

  const totalSec = ((Date.now() - overallStart) / 1000).toFixed(1);
  console.log(`\n🎉 Backfill complete!`);
  console.log(`   Range    : ${START_DATE.toISOString().slice(0,10)} → ${END_DATE.toISOString().slice(0,10)}`);
  console.log(`   Inserted : ${totalInserted.toLocaleString()} rows`);
  console.log(`   Time     : ${totalSec}s`);
  console.log(`   Speed    : ~${Math.floor(totalInserted / +totalSec).toLocaleString()} rows/sec`);
}

main()
  .catch(e => { console.error('\n❌ Error:', e.message); process.exit(1); })
  .finally(() => prisma.$disconnect());
