package database

import (
	"context"
	"fmt"
	"iot-dashboard/internal/config"

	"github.com/jackc/pgx/v5/pgxpool"
)

var Pool *pgxpool.Pool

func Connect(ctx context.Context) error {
	pool, err := pgxpool.New(ctx, config.Env.DatabaseURL)
	if err != nil {
		return fmt.Errorf("failed to create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	Pool = pool
	fmt.Println("✅ Database connected")
	return nil
}

func Close() {
	if Pool != nil {
		Pool.Close()
	}
}

// EnsureHypertable creates the TimescaleDB hypertable for telemetry_raw if not exists.
func EnsureHypertable(ctx context.Context) error {
	_, err := Pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE`)
	if err != nil {
		fmt.Printf("⚠️  TimescaleDB extension skipped: %v\n", err)
		return nil // non-fatal
	}

	_, err = Pool.Exec(ctx, `
		SELECT create_hypertable(
			'telemetry_raw'::regclass,
			by_range('timestamp', INTERVAL '1 day'),
			if_not_exists => TRUE
		)
	`)
	if err != nil {
		fmt.Printf("⚠️  TimescaleDB hypertable setup skipped: %v\n", err)
		return nil // non-fatal
	}

	fmt.Println("✅ TimescaleDB hypertable ready")
	return nil
}
