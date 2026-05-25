import { PrismaClient } from '@prisma/client';
import { env } from './env';

declare global {
  // eslint-disable-next-line no-var
  var __prisma: PrismaClient | undefined;
}

export const prisma =
  global.__prisma ??
  new PrismaClient({
    log: env.isDev() ? ['query', 'warn', 'error'] : ['warn', 'error'],
    errorFormat: 'pretty',
  });

if (env.isDev()) {
  global.__prisma = prisma;
}

// Ensure TimescaleDB hypertable for telemetry_raw.
// Uses the TimescaleDB 2.13+ by_range() API with explicit regclass cast.
// The telemetry_raw table must have a composite PK (id, timestamp) for this to work.
export async function ensureHypertable(): Promise<void> {
  try {
    // Install extension in case it wasn't created by the init script
    await prisma.$executeRaw`CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE`;

    // by_range() requires TimescaleDB ≥ 2.13 (2.27+ in our image)
    await prisma.$executeRaw`
      SELECT create_hypertable(
        'telemetry_raw'::regclass,
        by_range('timestamp', INTERVAL '1 day'),
        if_not_exists => TRUE
      )
    `;
    console.log('✅ TimescaleDB hypertable ready');
  } catch (err) {
    // TimescaleDB not available or already set up — log and continue
    console.warn('⚠️  TimescaleDB hypertable setup skipped:', (err as Error).message);
  }
}
