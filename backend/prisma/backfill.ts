/**
 * Backfill Script — Sine-wave telemetry at 1-second resolution
 *
 * Shape  : sine wave, 1 complete cycle = 10 data points
 * Range  : threshold ± 10%  (lower = threshold×0.9, upper = threshold×1.1)
 * Noise  : ±0.5% of amplitude for realism
 * Dates  : 2026-04-20 00:00:00 UTC  →  2026-06-30 23:59:59 UTC
 * Rate   : 1 data point per second per machine
 *
 * Run:    npx tsx prisma/backfill.ts
 * Docker: docker compose exec backend npx tsx prisma/backfill.ts
 */

import { PrismaClient } from '@prisma/client';

const prisma = new PrismaClient();

// ─── Config ───────────────────────────────────────────────────────────────────
const START_DATE      = new Date('2026-04-20T00:00:00.000Z');
const END_DATE        = new Date('2026-05-31T23:59:59.000Z');
const INTERVAL_MS      = 1_000;    // 1 second between each point
const BATCH_SIZE       = 5_000;
const POINTS_PER_CYCLE = 3_600;   // 1 sine wave = 3600 seconds = 1 hour

const ROWS_PER_MACHINE = Math.floor((END_DATE.getTime() - START_DATE.getTime()) / INTERVAL_MS);
const TOTAL_ROWS       = ROWS_PER_MACHINE * 4; // 4 machines

// ─── Sine wave (period = 10 points) ──────────────────────────────────────────
function sine(threshold: number, tick: number, phaseOffset: number, precision: number): number {
  const amplitude = threshold * 0.1;
  const noise     = (Math.random() - 0.5) * 0.01 * amplitude;
  const value     = threshold
    + amplitude * Math.sin((2 * Math.PI * (tick + phaseOffset)) / POINTS_PER_CYCLE)
    + noise;
  return +Math.max(threshold * 0.9, Math.min(threshold * 1.1, value)).toFixed(precision);
}

// ─── Machine + field definitions ─────────────────────────────────────────────
const MACHINES = [
  {
    id:   '00000000-0000-0000-0000-000000000005',
    name: 'Checkweigher CW-01',
    generate: (tick: number) => {
      const rejects = Math.max(0, Math.round(sine(1.5, tick, 5, 0)));
      return {
        weight:      sine(500,  tick, 0, 2),
        speed:       sine(60,   tick, 2, 1),
        throughput:  sine(60,   tick, 2, 1),
        rejects,
        status_code: rejects > 0 ? 1 : 0,
      };
    },
  },
  {
    id:   '00000000-0000-0000-0000-000000000006',
    name: 'Temp Sensor TS-01',
    generate: (tick: number) => ({
      temp:      sine(22,  tick, 0, 2),
      humidity:  sine(55,  tick, 3, 1),
      dew_point: sine(11,  tick, 1, 2),
    }),
  },
  {
    id:   '00000000-0000-0000-0000-000000000007',
    name: 'Conveyor Belt CB-01',
    generate: (tick: number) => ({
      speed:     sine(1000, tick, 0, 1),
      load:      sine(45,   tick, 3, 1),
      rpm:       sine(750,  tick, 3, 0),
      vibration: sine(5,    tick, 5, 2),
    }),
  },
  {
    id:   '00000000-0000-0000-0000-000000000008',
    name: 'Vision Camera VC-01',
    inspected: 0, passed: 0, failed: 0,
    generate(tick: number) {
      const defect_rate = sine(1,  tick, 0, 3);
      const confidence  = sine(97, tick, 4, 1);
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

  console.log('🌊 Backfill — Sine Wave (1 second resolution, ±10% of threshold)');
  console.log(`   Date range : ${START_DATE.toISOString().slice(0,10)} → ${END_DATE.toISOString().slice(0,10)}  (${spanDays.toFixed(1)} days)`);
  console.log(`   Interval   : ${INTERVAL_MS / 1000}s between points`);
  console.log(`   Total rows : ${TOTAL_ROWS.toLocaleString()}  (${ROWS_PER_MACHINE.toLocaleString()} per machine × 4)`);
  console.log(`   Batch size : ${BATCH_SIZE.toLocaleString()} rows per INSERT`);
  console.log(`   Est. time  : ~${Math.ceil(ROWS_PER_MACHINE / 19_000 * 4 / 60)} minutes\n`);

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
    console.log(`\n  ✅ ${machineInserted.toLocaleString()} rows in ${elapsed}s`);
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
