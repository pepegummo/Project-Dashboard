import 'dotenv/config';
import http from 'http';
import { createApp } from './app';
import { env } from './config/env';
import { prisma, ensureHypertable } from './config/database';
import { WsGateway } from './websocket/ws.gateway';
import { TelemetrySimulator } from './telemetry/simulator';

async function bootstrap() {
  console.log(`\n🏭 Industrial IoT AI Dashboard — Backend`);
  console.log(`   Environment: ${env.NODE_ENV}`);

  // ─── Database ─────────────────────────────────────────────────────────────
  await prisma.$connect();
  console.log('✅ Database connected');
  await ensureHypertable();

  // ─── HTTP Server ──────────────────────────────────────────────────────────
  const app = createApp();
  const httpServer = http.createServer(app);

  // ─── WebSocket (same port upgrade or separate port) ───────────────────────
  const gateway = new WsGateway(); // standalone WS server on WS_PORT

  // ─── Telemetry Simulator (set SIMULATOR_ENABLED=true to enable) ──────────
  const simulatorEnabled = process.env.SIMULATOR_ENABLED === 'true';
  const simulator = new TelemetrySimulator(gateway, 1000, 1);

  if (simulatorEnabled) {
    try {
      const machines = await prisma.machine.findMany({
        select: { id: true, name: true, type: true },
      });

      if (machines.length > 0) {
        simulator.configureMachines(machines);
        simulator.start();
      } else {
        console.warn('⚠️  No machines found. Run db:seed first.');
      }
    } catch (err) {
      console.warn('⚠️  Could not load machines for simulator:', (err as Error).message);
    }
  } else {
    console.log('ℹ️  Simulator disabled (SIMULATOR_ENABLED=false) — using static backfill data only');
  }

  // ─── Start HTTP ───────────────────────────────────────────────────────────
  httpServer.listen(env.PORT, () => {
    console.log(`✅ REST API listening on http://localhost:${env.PORT}`);
    console.log(`✅ WebSocket listening on ws://localhost:${env.WS_PORT}`);
    console.log(`\n📋 API Endpoints:`);
    console.log(`   POST /api/auth/login`);
    console.log(`   GET  /api/machines`);
    console.log(`   GET  /api/telemetry/:id/latest`);
    console.log(`   GET  /api/dashboards`);
    console.log(`   GET  /api/alerts`);
    console.log(`   GET  /api/ai/tools`);
    console.log(`\n`);
  });

  // ─── Graceful shutdown ─────────────────────────────────────────────────────
  const shutdown = async (signal: string) => {
    console.log(`\n${signal} received — shutting down gracefully`);
    simulator.stop();
    gateway.close();
    httpServer.close(async () => {
      await prisma.$disconnect();
      console.log('👋 Shutdown complete');
      process.exit(0);
    });
    setTimeout(() => process.exit(1), 10_000);
  };

  process.on('SIGTERM', () => shutdown('SIGTERM'));
  process.on('SIGINT', () => shutdown('SIGINT'));
  process.on('uncaughtException', (err) => {
    console.error('Uncaught exception:', err);
    process.exit(1);
  });
  process.on('unhandledRejection', (reason) => {
    console.error('Unhandled rejection:', reason);
    process.exit(1);
  });
}

bootstrap().catch((err) => {
  console.error('❌ Bootstrap failed:', err);
  process.exit(1);
});
