/**
 * Backfill Script — Pulse-wave telemetry at 1-minute resolution
 *
 * Shape  : pulse wave — 1 complete cycle = 120 min (2 hours)
 *          High plateau for dutyCycle%, smooth linear edges (TRANS_TICKS wide)
 * Noise  : 1) Gaussian σ=5% of amplitude  (base sensor noise)
 *          2) Random spikes P=3%, up to ±35% of amplitude
 *          3) Slow sinusoidal drift ±30% of amplitude (8-cycle period)
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
const CYCLE_TICKS  = 120;       // 1 pulse cycle = 120 minutes = 2 hours
const TRANS_TICKS  = 5;         // smooth edge over ±5 ticks

const ROWS_PER_MACHINE = Math.floor((END_DATE.getTime() - START_DATE.getTime()) / INTERVAL_MS);
const TOTAL_ROWS       = ROWS_PER_MACHINE * 4; // 4 machines

// ─── Pulse wave + layered noise ───────────────────────────────────────────────
function pulse(
  threshold:   number,
  tick:        number,
  phaseOffset: number,
  precision:   number,
  dutyCycle  = 0.45,
): number {
  const amplitude = threshold * 0.1;
  const period    = CYCLE_TICKS;
  const phase     = ((tick + phaseOffset) % period + period) % period;
  const highEnd   = dutyCycle * period;

  // ── Pulse shape ──────────────────────────────────────────────────────────
  let base: number;
  if (phase < highEnd - TRANS_TICKS) {
    base = amplitude;                                                     // high plateau
  } else if (phase < highEnd + TRANS_TICKS) {
    const t = (phase - (highEnd - TRANS_TICKS)) / (2 * TRANS_TICKS);
    base = amplitude * (1 - t) + (-amplitude) * t;                       // falling edge
  } else if (phase < period - TRANS_TICKS) {
    base = -amplitude;                                                    // low plateau
  } else {
    const t = (phase - (period - TRANS_TICKS)) / TRANS_TICKS;
    base = -amplitude * (1 - t) + amplitude * t;                         // rising edge
  }

  // ── Noise layer 1: Gaussian (σ = 5% of amplitude) ────────────────────────
  const u1    = Math.random() + 1e-10;
  const u2    = Math.random();
  const gauss = Math.sqrt(-2 * Math.log(u1)) * Math.cos(2 * Math.PI * u2);
  const gaussNoise = gauss * 0.05 * amplitude;

  // ── Noise layer 2: Random spike (P=3%, magnitude ±35% of amplitude) ──────
  const spikeNoise = Math.random() < 0.03
    ? (Math.random() - 0.5) * 0.70 * amplitude
    : 0;

  // ── Noise layer 3: Slow sinusoidal drift (period = 8 cycles = 16 hours) ──
  const drift = (amplitude * 0.30) * Math.sin((2 * Math.PI * tick) / (8 * CYCLE_TICKS));

  const value = threshold + base + gaussNoise + spikeNoise + drift;
  // Extended clamp ±18% to allow spikes through
  return +Math.max(threshold * 0.82, Math.min(threshold * 1.18, value)).toFixed(precision);
}

// ─── Machine + field definitions ─────────────────────────────────────────────
const MACHINES = [
  {
    id:   '00000000-0000-0000-0000-000000000005',
    name: 'Checkweigher CW-01',
    generate: (tick: number) => {
      const rejects = Math.max(0, Math.round(pulse(1.5, tick, 10, 0, 0.30)));
      return {
        weight:      pulse(500,  tick,  0, 2, 0.45),
        speed:       pulse(60,   tick,  5, 1, 0.50),
        throughput:  pulse(60,   tick,  5, 1, 0.50),
        rejects,
        status_code: rejects > 0 ? 1 : 0,
      };
    },
  },
  {
    id:   '00000000-0000-0000-0000-000000000006',
    name: 'Temp Sensor TS-01',
    generate: (tick: number) => ({
      temp:      pulse(22,  tick,  0, 2, 0.55),
      humidity:  pulse(55,  tick, 20, 1, 0.48),
      dew_point: pulse(11,  tick, 10, 2, 0.52),
    }),
  },
  {
    id:   '00000000-0000-0000-0000-000000000007',
    name: 'Conveyor Belt CB-01',
    generate: (tick: number) => ({
      speed:     pulse(1000, tick,  0, 1, 0.45),
      load:      pulse(45,   tick, 30, 1, 0.50),
      rpm:       pulse(750,  tick, 15, 0, 0.45),
      vibration: pulse(5,    tick,  8, 2, 0.40),
    }),
  },
  {
    id:   '00000000-0000-0000-0000-000000000008',
    name: 'Vision Camera VC-01',
    inspected: 0, passed: 0, failed: 0,
    generate(tick: number) {
      const defect_rate = pulse(1,  tick,  0, 3, 0.35);
      const confidence  = pulse(97, tick, 25, 1, 0.60);
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

  console.log('⚡ Backfill — Pulse Wave (1 min resolution, 2-hr cycle, layered noise)');
  console.log(`   Date range : ${START_DATE.toISOString().slice(0,10)} → ${END_DATE.toISOString().slice(0,10)}  (${spanDays.toFixed(1)} days)`);
  console.log(`   Interval   : 1 minute per point`);
  console.log(`   Cycle      : ${CYCLE_TICKS} min = 2 hours per pulse`);
  console.log(`   Noise      : Gaussian σ=5%  +  spike P=3%±35%  +  drift ±30% (16-hr period)`);
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
