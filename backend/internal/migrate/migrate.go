// Package migrate provides idempotent schema creation and seed data insertion.
// Safe to call on every startup — all statements use IF NOT EXISTS / ON CONFLICT DO NOTHING.
package migrate

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// RunAll runs EnsureSchema then EnsureSeed. Call this once after DB connect.
func RunAll(ctx context.Context, pool *pgxpool.Pool) error {
	if err := EnsureSchema(ctx, pool); err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	if err := EnsureSeed(ctx, pool); err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	return nil
}

// ─── Schema ───────────────────────────────────────────────────────────────────

// EnsureSchema creates all tables, the TimescaleDB hypertable, and indexes.
// Safe to run multiple times — uses IF NOT EXISTS throughout.
func EnsureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	ddl := []string{
		// Extensions
		`CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE`,
		`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`,

		// ── organizations ───────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS organizations (
			id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			name       TEXT        NOT NULL,
			slug       TEXT        UNIQUE NOT NULL,
			plan       TEXT        NOT NULL DEFAULT 'starter',
			settings   JSONB       NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,

		// ── users ────────────────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS users (
			id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			organization_id UUID        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			email           TEXT        UNIQUE NOT NULL,
			name            TEXT        NOT NULL,
			password_hash   TEXT        NOT NULL,
			role            TEXT        NOT NULL DEFAULT 'viewer',
			avatar_url      TEXT,
			preferences     JSONB       NOT NULL DEFAULT '{}'::jsonb,
			last_login_at   TIMESTAMPTZ,
			is_active       BOOLEAN     NOT NULL DEFAULT TRUE,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,

		// ── factories ────────────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS factories (
			id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			organization_id UUID        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			name            TEXT        NOT NULL,
			location        TEXT,
			timezone        TEXT        NOT NULL DEFAULT 'UTC',
			metadata        JSONB       NOT NULL DEFAULT '{}'::jsonb,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,

		// ── production_lines ─────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS production_lines (
			id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			factory_id UUID        NOT NULL REFERENCES factories(id) ON DELETE CASCADE,
			name       TEXT        NOT NULL,
			code       TEXT,
			status     TEXT        NOT NULL DEFAULT 'active',
			metadata   JSONB       NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,

		// ── machines ─────────────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS machines (
			id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			production_line_id UUID        NOT NULL REFERENCES production_lines(id) ON DELETE CASCADE,
			name               TEXT        NOT NULL,
			type               TEXT        NOT NULL,
			serial_number      TEXT,
			model              TEXT,
			manufacturer       TEXT,
			status             TEXT        NOT NULL DEFAULT 'online',
			last_seen_at       TIMESTAMPTZ,
			metadata           JSONB       NOT NULL DEFAULT '{}'::jsonb,
			created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		// ── machine_fields ───────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS machine_fields (
			id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			machine_id  UUID        NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
			key         TEXT        NOT NULL,
			label       TEXT        NOT NULL,
			unit        TEXT,
			data_type   TEXT        NOT NULL DEFAULT 'number',
			min         FLOAT,
			max         FLOAT,
			threshold   FLOAT,
			upper_limit FLOAT,
			lower_limit FLOAT,
			precision   INTEGER     NOT NULL DEFAULT 2,
			enum_values JSONB,
			is_key      BOOLEAN     NOT NULL DEFAULT FALSE,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(machine_id, key)
		)`,

		// ── telemetry_raw — composite PK required by TimescaleDB ─────────────
		`CREATE TABLE IF NOT EXISTS telemetry_raw (
			id         BIGINT      GENERATED ALWAYS AS IDENTITY,
			machine_id UUID        NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
			timestamp  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			data       JSONB       NOT NULL,
			quality    TEXT        NOT NULL DEFAULT 'good',
			PRIMARY KEY (id, timestamp)
		)`,

		// ── telemetry_aggregates ─────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS telemetry_aggregates (
			id         BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			machine_id UUID        NOT NULL REFERENCES machines(id),
			field      TEXT        NOT NULL,
			period     TEXT        NOT NULL,
			timestamp  TIMESTAMPTZ NOT NULL,
			avg        FLOAT,
			min        FLOAT,
			max        FLOAT,
			stddev     FLOAT,
			count      INTEGER
		)`,

		// ── dashboards ───────────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS dashboards (
			id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			organization_id UUID        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			user_id         UUID        NOT NULL REFERENCES users(id),
			name            TEXT        NOT NULL,
			description     TEXT,
			is_public       BOOLEAN     NOT NULL DEFAULT FALSE,
			is_default      BOOLEAN     NOT NULL DEFAULT FALSE,
			thumbnail       TEXT,
			tags            TEXT[]      NOT NULL DEFAULT '{}',
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,

		// ── dashboard_widgets ────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS dashboard_widgets (
			id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			dashboard_id UUID        NOT NULL REFERENCES dashboards(id) ON DELETE CASCADE,
			machine_id   UUID        REFERENCES machines(id) ON DELETE SET NULL,
			widget_type  TEXT        NOT NULL,
			title        TEXT,
			layout       JSONB       NOT NULL DEFAULT '{}'::jsonb,
			config       JSONB       NOT NULL DEFAULT '{}'::jsonb,
			"order"      INTEGER     NOT NULL DEFAULT 0,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,

		// ── alerts ───────────────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS alerts (
			id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			machine_id   UUID        NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
			name         TEXT        NOT NULL,
			description  TEXT,
			field        TEXT        NOT NULL,
			condition    TEXT        NOT NULL,
			threshold    FLOAT       NOT NULL,
			threshold_hi FLOAT,
			severity     TEXT        NOT NULL DEFAULT 'warning',
			is_active    BOOLEAN     NOT NULL DEFAULT TRUE,
			cooldown_sec INTEGER     NOT NULL DEFAULT 300,
			notify_email TEXT[]      NOT NULL DEFAULT '{}',
			created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,

		// ── alert_events ─────────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS alert_events (
			id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			alert_id    UUID        NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
			value       FLOAT       NOT NULL,
			message     TEXT,
			status      TEXT        NOT NULL DEFAULT 'open',
			resolved_at TIMESTAMPTZ,
			resolved_by TEXT,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,

		// ── ai_conversations ─────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS ai_conversations (
			id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			title      TEXT        NOT NULL DEFAULT 'New Conversation',
			context    JSONB       NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,

		// ── ai_messages ──────────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS ai_messages (
			id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			conversation_id UUID        NOT NULL REFERENCES ai_conversations(id) ON DELETE CASCADE,
			role            TEXT        NOT NULL,
			content         TEXT        NOT NULL,
			tool_name       TEXT,
			tool_input      JSONB,
			tool_result     JSONB,
			tokens          INTEGER,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,

		// ── audit_logs ───────────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id     UUID        REFERENCES users(id) ON DELETE SET NULL,
			action      TEXT        NOT NULL,
			resource    TEXT        NOT NULL,
			resource_id UUID,
			before      JSONB,
			after       JSONB,
			ip_address  TEXT,
			user_agent  TEXT,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
	}

	for _, stmt := range ddl {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("DDL failed: %w", err)
		}
	}

	// TimescaleDB hypertable (non-fatal if TimescaleDB is not available)
	_, err := pool.Exec(ctx, `
		SELECT create_hypertable(
			'telemetry_raw'::regclass,
			by_range('timestamp', INTERVAL '7 days'),
			if_not_exists => TRUE
		)
	`)
	if err != nil {
		fmt.Printf("⚠️  TimescaleDB hypertable skipped: %v\n", err)
	} else {
		fmt.Println("✅ TimescaleDB hypertable ready")
	}

	// Optional: compression policy (non-fatal)
	_, _ = pool.Exec(ctx, `
		ALTER TABLE telemetry_raw SET (
			timescaledb.compress,
			timescaledb.compress_segmentby = 'machine_id',
			timescaledb.compress_orderby   = 'timestamp DESC'
		)
	`)
	_, _ = pool.Exec(ctx, `
		SELECT add_compression_policy('telemetry_raw', INTERVAL '14 days', if_not_exists => TRUE)
	`)

	// Performance indexes
	indexes := []string{
		// telemetry_raw
		`CREATE INDEX IF NOT EXISTS idx_tr_machine_ts      ON telemetry_raw (machine_id, timestamp DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_tr_machine_ts_good ON telemetry_raw (machine_id, timestamp DESC) WHERE quality = 'good'`,
		`CREATE INDEX IF NOT EXISTS idx_tr_data_gin        ON telemetry_raw USING GIN (data)`,
		// telemetry_aggregates
		`CREATE INDEX IF NOT EXISTS idx_ta_lookup ON telemetry_aggregates (machine_id, field, period, timestamp DESC)`,
		// machines
		`CREATE INDEX IF NOT EXISTS idx_machines_status ON machines (status)`,
		`CREATE INDEX IF NOT EXISTS idx_machines_line   ON machines (production_line_id)`,
		// machine_fields
		`CREATE INDEX IF NOT EXISTS idx_mf_machine ON machine_fields (machine_id)`,
		// dashboards
		`CREATE INDEX IF NOT EXISTS idx_dashboards_org  ON dashboards (organization_id)`,
		`CREATE INDEX IF NOT EXISTS idx_dashboards_user ON dashboards (user_id)`,
		// dashboard_widgets
		`CREATE INDEX IF NOT EXISTS idx_dw_dashboard ON dashboard_widgets (dashboard_id)`,
		`CREATE INDEX IF NOT EXISTS idx_dw_machine   ON dashboard_widgets (machine_id)`,
		// alerts
		`CREATE INDEX IF NOT EXISTS idx_alerts_machine ON alerts (machine_id, is_active)`,
		// alert_events
		`CREATE INDEX IF NOT EXISTS idx_ae_alert_ts ON alert_events (alert_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_ae_open     ON alert_events (created_at DESC) WHERE status = 'open'`,
		// ai
		`CREATE INDEX IF NOT EXISTS idx_aiconv_user ON ai_conversations (user_id, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_aimsg_conv  ON ai_messages (conversation_id, created_at ASC)`,
		// audit
		`CREATE INDEX IF NOT EXISTS idx_audit_user     ON audit_logs (user_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_resource ON audit_logs (resource, resource_id)`,
	}
	for _, idx := range indexes {
		if _, err := pool.Exec(ctx, idx); err != nil {
			fmt.Printf("⚠️  Index skipped: %v\n", err)
		}
	}

	fmt.Println("✅ Schema ready")
	return nil
}

// ─── Seed ─────────────────────────────────────────────────────────────────────

// EnsureSeed inserts the default org / factory / machines / users / dashboard
// if the database is empty. Safe to call repeatedly — skipped if data exists.
func EnsureSeed(ctx context.Context, pool *pgxpool.Pool) error {
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM organizations`).Scan(&count); err != nil {
		return fmt.Errorf("check org count: %w", err)
	}
	if count > 0 {
		return nil // already seeded
	}

	fmt.Print("🌱  Seeding database...")

	// Hash admin password (bcrypt cost 12, matching seed.ts)
	hash, err := bcrypt.GenerateFromPassword([]byte("Admin@1234"), 12)
	if err != nil {
		return fmt.Errorf("bcrypt: %w", err)
	}

	// Helper pointers for nullable floats
	f := func(v float64) *float64 { return &v }

	stmts := []struct {
		sql  string
		args []interface{}
	}{
		// Organization
		{
			`INSERT INTO organizations (id, name, slug, plan, settings, created_at, updated_at)
			 VALUES ('00000000-0000-0000-0000-000000000001','ACME Foods Co.','acme-foods','pro','{}',NOW(),NOW())
			 ON CONFLICT (id) DO NOTHING`,
			nil,
		},
		// Factory
		{
			`INSERT INTO factories (id, organization_id, name, location, timezone, metadata, created_at, updated_at)
			 VALUES ('00000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001',
			         'Bangkok Plant 1','Bangkok, Thailand','Asia/Bangkok','{}',NOW(),NOW())
			 ON CONFLICT (id) DO NOTHING`,
			nil,
		},
		// Production Lines
		{
			`INSERT INTO production_lines (id, factory_id, name, code, status, metadata, created_at, updated_at)
			 VALUES ('00000000-0000-0000-0000-000000000003','00000000-0000-0000-0000-000000000002',
			         'Line A — Packaging','LINE-A','active','{}',NOW(),NOW())
			 ON CONFLICT (id) DO NOTHING`,
			nil,
		},
		{
			`INSERT INTO production_lines (id, factory_id, name, code, status, metadata, created_at, updated_at)
			 VALUES ('00000000-0000-0000-0000-000000000004','00000000-0000-0000-0000-000000000002',
			         'Line B — Filling','LINE-B','active','{}',NOW(),NOW())
			 ON CONFLICT (id) DO NOTHING`,
			nil,
		},
		// Machines
		{
			`INSERT INTO machines (id, production_line_id, name, type, serial_number, model, manufacturer, status, metadata, created_at, updated_at)
			 VALUES ('00000000-0000-0000-0000-000000000005','00000000-0000-0000-0000-000000000003',
			         'Checkweigher CW-01','checkweigher','CW-2024-001','ProCheck X200','MettlerToledo','online',
			         '{"targetWeight":500,"tolerance":5}',NOW(),NOW())
			 ON CONFLICT (id) DO NOTHING`,
			nil,
		},
		{
			`INSERT INTO machines (id, production_line_id, name, type, serial_number, model, manufacturer, status, metadata, created_at, updated_at)
			 VALUES ('00000000-0000-0000-0000-000000000006','00000000-0000-0000-0000-000000000003',
			         'Temp Sensor TS-01','temperature_sensor','TS-2024-001','ThermoGuard Pro','Omega','online',
			         '{"location":"cold_storage_a"}',NOW(),NOW())
			 ON CONFLICT (id) DO NOTHING`,
			nil,
		},
		{
			`INSERT INTO machines (id, production_line_id, name, type, serial_number, model, manufacturer, status, metadata, created_at, updated_at)
			 VALUES ('00000000-0000-0000-0000-000000000007','00000000-0000-0000-0000-000000000003',
			         'Conveyor Belt CB-01','conveyor','CB-2024-001','FlexLine 500','Interroll','online',
			         '{"maxSpeed":2000}',NOW(),NOW())
			 ON CONFLICT (id) DO NOTHING`,
			nil,
		},
		{
			`INSERT INTO machines (id, production_line_id, name, type, serial_number, model, manufacturer, status, metadata, created_at, updated_at)
			 VALUES ('00000000-0000-0000-0000-000000000008','00000000-0000-0000-0000-000000000004',
			         'Vision AI Camera VC-01','vision_camera','VC-2024-001','SmartEye AI-Pro','Cognex','online',
			         '{"resolution":"4K","model":"defect-detection-v2"}',NOW(),NOW())
			 ON CONFLICT (id) DO NOTHING`,
			nil,
		},
		// Admin User
		{
			`INSERT INTO users (id, organization_id, email, name, password_hash, role, is_active, created_at, updated_at)
			 VALUES ('00000000-0000-0000-0000-000000000009','00000000-0000-0000-0000-000000000001',
			         'admin@acme-foods.com','Admin User',$1,'admin',TRUE,NOW(),NOW())
			 ON CONFLICT (id) DO NOTHING`,
			[]interface{}{string(hash)},
		},
		// Dashboard
		{
			`INSERT INTO dashboards (id, organization_id, user_id, name, description, is_public, is_default, tags, created_at, updated_at)
			 VALUES ('00000000-0000-0000-0000-000000000010','00000000-0000-0000-0000-000000000001',
			         '00000000-0000-0000-0000-000000000009','Production Overview',
			         'Main production monitoring dashboard',FALSE,TRUE,
			         ARRAY['production','overview'],NOW(),NOW())
			 ON CONFLICT (id) DO NOTHING`,
			nil,
		},
	}

	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s.sql, s.args...); err != nil {
			return fmt.Errorf("seed stmt: %w", err)
		}
	}

	// ── Machine Fields ────────────────────────────────────────────────────────
	type mf struct {
		machineID string
		key, label, unit string
		min, max, thr, upper, lower *float64
		prec  int
		isKey bool
	}

	fields := []mf{
		// CW-01 Checkweigher
		{"00000000-0000-0000-0000-000000000005", "weight",      "Weight",       "g",       f(0),   f(2000), f(500),  f(550),  f(450),  2, true},
		{"00000000-0000-0000-0000-000000000005", "speed",       "Belt Speed",   "ppm",     f(0),   f(120),  f(60),   f(66),   f(54),   2, false},
		{"00000000-0000-0000-0000-000000000005", "rejects",     "Reject Count", "pcs",     f(0),   f(9999), f(0),    f(3),    f(0),    2, false},
		{"00000000-0000-0000-0000-000000000005", "throughput",  "Throughput",   "pcs/min", f(0),   f(120),  f(60),   f(66),   f(54),   2, false},
		{"00000000-0000-0000-0000-000000000005", "status_code", "Status Code",  "",        nil,    nil,     nil,     nil,     nil,     2, false},
		// TS-01 Temp Sensor
		{"00000000-0000-0000-0000-000000000006", "temp",      "Temperature", "°C",  f(-20), f(80),  f(22), f(24.2), f(19.8), 2, true},
		{"00000000-0000-0000-0000-000000000006", "humidity",  "Humidity",    "%RH", f(0),   f(100), f(55), f(60.5), f(49.5), 2, false},
		{"00000000-0000-0000-0000-000000000006", "dew_point", "Dew Point",   "°C",  f(-30), f(60),  f(11), f(12.1), f(9.9),  2, false},
		// CB-01 Conveyor
		{"00000000-0000-0000-0000-000000000007", "speed",     "Belt Speed", "mm/s",  f(0), f(2000), f(1000), f(1100), f(900), 2, true},
		{"00000000-0000-0000-0000-000000000007", "load",      "Motor Load", "%",     f(0), f(100),  f(45),   f(49.5), f(40.5),2, false},
		{"00000000-0000-0000-0000-000000000007", "rpm",       "Motor RPM",  "rpm",   f(0), f(1500), f(750),  f(825),  f(675), 2, false},
		{"00000000-0000-0000-0000-000000000007", "vibration", "Vibration",  "mm/s²", f(0), f(50),   f(5),    f(5.5),  f(4.5), 2, false},
		// VC-01 Vision Camera
		{"00000000-0000-0000-0000-000000000008", "defect_rate", "Defect Rate",     "%",   f(0), f(100),    f(1),  f(1.1),  f(0.9),  2, true},
		{"00000000-0000-0000-0000-000000000008", "inspected",   "Items Inspected", "pcs", f(0), f(999999), nil,   nil,     nil,     2, false},
		{"00000000-0000-0000-0000-000000000008", "passed",      "Items Passed",    "pcs", f(0), f(999999), nil,   nil,     nil,     2, false},
		{"00000000-0000-0000-0000-000000000008", "failed",      "Items Failed",    "pcs", f(0), f(999999), nil,   nil,     nil,     2, false},
		{"00000000-0000-0000-0000-000000000008", "confidence",  "AI Confidence",   "%",   f(0), f(100),    f(97), f(99.9), f(87.3), 2, false},
	}

	for _, field := range fields {
		unit := (*string)(nil)
		if field.unit != "" {
			u := field.unit
			unit = &u
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO machine_fields
				(id, machine_id, key, label, unit, data_type, min, max, threshold, upper_limit, lower_limit, precision, is_key, created_at)
			VALUES
				(gen_random_uuid(), $1, $2, $3, $4, 'number', $5, $6, $7, $8, $9, $10, $11, NOW())
			ON CONFLICT (machine_id, key) DO NOTHING`,
			field.machineID, field.key, field.label, unit,
			field.min, field.max, field.thr, field.upper, field.lower,
			field.prec, field.isKey,
		)
		if err != nil {
			return fmt.Errorf("field %s.%s: %w", field.machineID[len(field.machineID)-4:], field.key, err)
		}
	}

	// ── Dashboard Widgets ─────────────────────────────────────────────────────
	type widget struct {
		id, machineID, wType, title, layout, config string
	}
	cw := "00000000-0000-0000-0000-000000000005"
	ts := "00000000-0000-0000-0000-000000000006"
	cb := "00000000-0000-0000-0000-000000000007"

	widgets := []widget{
		{"00000000-0000-0000-0000-000000000014", cw, "line-chart", "Weight Over Time",
			`{"x":0,"y":0,"w":6,"h":4}`, `{"field":"weight","timeRange":"1h","color":"#3b82f6"}`},
		{"00000000-0000-0000-0000-000000000015", cw, "gauge", "Current Weight",
			`{"x":6,"y":0,"w":3,"h":4}`, `{"field":"weight","min":400,"max":600,"unit":"g"}`},
		{"00000000-0000-0000-0000-000000000016", ts, "kpi-card", "Temperature",
			`{"x":9,"y":0,"w":3,"h":2}`, `{"field":"temp","unit":"°C","precision":1}`},
		{"00000000-0000-0000-0000-000000000017", cw, "kpi-card", "Throughput",
			`{"x":9,"y":2,"w":3,"h":2}`, `{"field":"throughput","unit":"pcs/min","precision":0}`},
		{"00000000-0000-0000-0000-000000000018", cb, "line-chart", "Conveyor Speed",
			`{"x":0,"y":4,"w":6,"h":4}`, `{"field":"speed","timeRange":"30m","color":"#10b981"}`},
		{"00000000-0000-0000-0000-000000000019", "", "alarm-panel", "Active Alerts",
			`{"x":6,"y":4,"w":6,"h":4}`, `{"maxItems":10,"severities":["warning","critical"]}`},
	}

	for _, w := range widgets {
		var machineID interface{} = w.machineID
		if w.machineID == "" {
			machineID = nil
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO dashboard_widgets (id, dashboard_id, machine_id, widget_type, title, layout, config, created_at, updated_at)
			VALUES ($1,'00000000-0000-0000-0000-000000000010',$2,$3,$4,$5::jsonb,$6::jsonb,NOW(),NOW())
			ON CONFLICT (id) DO NOTHING`,
			w.id, machineID, w.wType, w.title, w.layout, w.config,
		)
		if err != nil {
			return fmt.Errorf("widget %s: %w", w.id[len(w.id)-4:], err)
		}
	}

	// ── Alert Rules ───────────────────────────────────────────────────────────
	type alertRule struct {
		id, machineID, name, field, condition, severity string
		threshold                                        float64
	}
	alertRules := []alertRule{
		{"00000000-0000-0000-0000-000000000011", cw, "Weight Over Tolerance",  "weight", "gt", "warning",  510},
		{"00000000-0000-0000-0000-000000000012", cw, "Weight Under Tolerance", "weight", "lt", "critical", 490},
		{"00000000-0000-0000-0000-000000000013", ts, "High Temperature",       "temp",   "gt", "critical",  35},
	}
	for _, a := range alertRules {
		_, err := pool.Exec(ctx, `
			INSERT INTO alerts (id, machine_id, name, field, condition, threshold, severity, is_active, cooldown_sec, notify_email, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,TRUE,300,'{}',NOW(),NOW())
			ON CONFLICT (id) DO NOTHING`,
			a.id, a.machineID, a.name, a.field, a.condition, a.threshold, a.severity,
		)
		if err != nil {
			return fmt.Errorf("alert %s: %w", a.name, err)
		}
	}

	fmt.Println(" done ✅")
	return nil
}
