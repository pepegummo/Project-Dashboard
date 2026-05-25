#!/bin/sh
set -e

echo "🗄️  Applying database schema (prisma db push)..."
npx prisma db push --skip-generate

echo "⏱️  Setting up TimescaleDB hypertable..."
node -e "
const { PrismaClient } = require('@prisma/client');
const prisma = new PrismaClient();
(async () => {
  try {
    await prisma.\$executeRaw\`CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE\`;
    await prisma.\$executeRaw\`
      SELECT create_hypertable(
        'telemetry_raw'::regclass,
        by_range('timestamp', INTERVAL '1 day'),
        if_not_exists => TRUE
      )
    \`;
    console.log('   ✅ TimescaleDB hypertable ready');
  } catch(e) {
    console.warn('   ⚠️  TimescaleDB hypertable skipped:', e.message);
  } finally {
    await prisma.\$disconnect();
  }
})();
"

echo "🌱 Running seed if database is empty..."
node -e "
const { PrismaClient } = require('@prisma/client');
const { execSync } = require('child_process');
const prisma = new PrismaClient();
prisma.organization.count()
  .then(count => {
    if (count === 0) {
      console.log('   No data found — seeding...');
      execSync('npx tsx prisma/seed.ts', { stdio: 'inherit' });
    } else {
      console.log('   Already seeded (' + count + ' org). Skipping.');
    }
  })
  .catch(err => {
    console.error('Seed check failed:', err.message);
  })
  .finally(() => prisma.\$disconnect())
  .then(() => {
    console.log('🚀 Starting server...');
    require('child_process').execFileSync('node', ['dist/index.js'], { stdio: 'inherit' });
  });
"
