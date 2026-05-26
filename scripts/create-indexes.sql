-- ─────────────────────────────────────────────────────────────────────────────
-- TimescaleDB + Performance Indexes
-- Run once after prisma db push / migrate:
--   docker compose exec db psql -U iot_user -d iot_dashboard -f /scripts/create-indexes.sql
-- Or via psql locally:
--   psql $DATABASE_URL -f scripts/create-indexes.sql
-- ─────────────────────────────────────────────────────────────────────────────

-- ─── 1. telemetry_raw — convert to TimescaleDB hypertable ────────────────────
-- Prisma creates the table; TimescaleDB must be told to partition it.
-- Safe to run multiple times (IF NOT EXISTS guard on each step).

SELECT create_hypertable(
  'telemetry_raw',
  'timestamp',
  migrate_data    => true,
  if_not_exists   => true,
  chunk_time_interval => INTERVAL '7 days'   -- 1 week per chunk (1-min data ≈ 40 k rows/machine/week)
);

-- GIN index: fast JSONB field extraction for any field key (weight, temp, rpm…)
CREATE INDEX IF NOT EXISTS idx_tr_data_gin
  ON telemetry_raw USING GIN (data);

-- Composite index: most queries filter by machine_id + timestamp range
-- (TimescaleDB adds its own chunk exclusion, but an explicit index helps Prisma's ORM queries)
CREATE INDEX IF NOT EXISTS idx_tr_machine_ts
  ON telemetry_raw (machine_id, timestamp DESC);

-- Partial index for quality='good' — the overwhelming majority of rows
CREATE INDEX IF NOT EXISTS idx_tr_machine_ts_good
  ON telemetry_raw (machine_id, timestamp DESC)
  WHERE quality = 'good';

-- ─── 2. TimescaleDB compression (data older than 14 days) ────────────────────
-- Compresses ~90-95% for 1-min numeric JSONB data
ALTER TABLE telemetry_raw SET (
  timescaledb.compress,
  timescaledb.compress_segmentby = 'machine_id',
  timescaledb.compress_orderby   = 'timestamp DESC'
);

SELECT add_compression_policy(
  'telemetry_raw',
  INTERVAL '14 days',
  if_not_exists => true
);

-- ─── 3. telemetry_aggregates ─────────────────────────────────────────────────
-- Used by the KPI/gauge widgets for period summaries
CREATE INDEX IF NOT EXISTS idx_ta_machine_field_period_ts
  ON telemetry_aggregates (machine_id, field, period, timestamp DESC);

-- ─── 4. machines ─────────────────────────────────────────────────────────────
CREATE INDEX IF NOT EXISTS idx_machines_status
  ON machines (status);

CREATE INDEX IF NOT EXISTS idx_machines_production_line
  ON machines (production_line_id);

-- ─── 5. machine_fields ───────────────────────────────────────────────────────
CREATE INDEX IF NOT EXISTS idx_mf_machine_id
  ON machine_fields (machine_id);

-- ─── 6. dashboard_widgets ────────────────────────────────────────────────────
CREATE INDEX IF NOT EXISTS idx_dw_dashboard_id
  ON dashboard_widgets (dashboard_id);

CREATE INDEX IF NOT EXISTS idx_dw_machine_id
  ON dashboard_widgets (machine_id);

-- ─── 7. dashboards ───────────────────────────────────────────────────────────
CREATE INDEX IF NOT EXISTS idx_dashboards_org
  ON dashboards (organization_id);

CREATE INDEX IF NOT EXISTS idx_dashboards_user
  ON dashboards (user_id);

-- ─── 8. alerts ───────────────────────────────────────────────────────────────
-- Most queries filter: WHERE machine_id = X AND is_active = true
CREATE INDEX IF NOT EXISTS idx_alerts_machine_active
  ON alerts (machine_id, is_active);

-- ─── 9. alert_events ─────────────────────────────────────────────────────────
-- Alarm panel widget: open events sorted by most recent
CREATE INDEX IF NOT EXISTS idx_ae_open_recent
  ON alert_events (created_at DESC)
  WHERE status = 'open';

CREATE INDEX IF NOT EXISTS idx_ae_alert_id_ts
  ON alert_events (alert_id, created_at DESC);

-- ─── 10. ai_conversations + ai_messages ──────────────────────────────────────
CREATE INDEX IF NOT EXISTS idx_aiconv_user
  ON ai_conversations (user_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_aimsg_conv
  ON ai_messages (conversation_id, created_at ASC);

-- ─── 11. audit_logs ──────────────────────────────────────────────────────────
CREATE INDEX IF NOT EXISTS idx_auditlog_user_ts
  ON audit_logs (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_auditlog_resource
  ON audit_logs (resource, resource_id);

-- ─────────────────────────────────────────────────────────────────────────────
-- Verify
-- ─────────────────────────────────────────────────────────────────────────────
SELECT
  schemaname,
  tablename,
  indexname,
  indexdef
FROM pg_indexes
WHERE schemaname = 'public'
ORDER BY tablename, indexname;
