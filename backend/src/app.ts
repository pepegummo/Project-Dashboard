import express from 'express';
import cors from 'cors';
import helmet from 'helmet';
import compression from 'compression';
import morgan from 'morgan';
import rateLimit from 'express-rate-limit';

import { env } from './config/env';
import { authRoutes } from './modules/auth/auth.routes';
import { machineRoutes } from './modules/machines/machine.routes';
import { telemetryRoutes } from './modules/telemetry/telemetry.routes';
import { dashboardRoutes } from './modules/dashboards/dashboard.routes';
import { alertRoutes } from './modules/alerts/alert.routes';
import { aiRoutes } from './ai-tools/ai-tools.routes';
import { notFound, errorHandler } from './middleware/error';

export function createApp() {
  const app = express();

  // ─── Security ─────────────────────────────────────────────────────────────
  app.use(helmet({ crossOriginEmbedderPolicy: false }));
  app.use(cors({
    origin: env.CORS_ORIGIN,
    methods: ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS'],
    allowedHeaders: ['Content-Type', 'Authorization'],
    credentials: true,
  }));

  // ─── Rate limiting ────────────────────────────────────────────────────────
  app.use('/api/auth', rateLimit({
    windowMs: 15 * 60 * 1000,
    max: 20,
    standardHeaders: true,
    legacyHeaders: false,
    message: { success: false, error: { code: 'TOO_MANY_REQUESTS', message: 'Too many requests' } },
  }));

  app.use(rateLimit({
    windowMs: 1 * 60 * 1000,
    max: 500,
    standardHeaders: true,
    legacyHeaders: false,
  }));

  // ─── Parsing / Compression ─────────────────────────────────────────────────
  app.use(compression());
  app.use(express.json({ limit: '1mb' }));
  app.use(express.urlencoded({ extended: true }));

  // ─── Logging ──────────────────────────────────────────────────────────────
  if (env.isDev()) {
    app.use(morgan('dev'));
  } else {
    app.use(morgan('combined'));
  }

  // ─── Health check ─────────────────────────────────────────────────────────
  app.get('/health', (_req, res) => {
    res.json({
      status: 'ok',
      timestamp: new Date().toISOString(),
      version: process.env.npm_package_version ?? '1.0.0',
      env: env.NODE_ENV,
    });
  });

  // ─── API Routes ───────────────────────────────────────────────────────────
  app.use('/api/auth', authRoutes);
  app.use('/api/machines', machineRoutes);
  app.use('/api/telemetry', telemetryRoutes);
  app.use('/api/dashboards', dashboardRoutes);
  app.use('/api/alerts', alertRoutes);
  app.use('/api/ai', aiRoutes);

  // ─── Error handling ───────────────────────────────────────────────────────
  app.use(notFound);
  app.use(errorHandler);

  return app;
}
