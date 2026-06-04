package database

import (
	"context"
	"fmt"
	"iot-dashboard/internal/config"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var Pool *pgxpool.Pool

func Connect(ctx context.Context) error {
	const maxAttempts = 15
	const retryDelay = 3 * time.Second

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		poolConfig, parseErr := pgxpool.ParseConfig(config.Env.DatabaseURL)
		if parseErr != nil {
			lastErr = fmt.Errorf("failed to parse pool config: %w", parseErr)
			fmt.Printf("⏳ DB not ready (attempt %d/%d): %v — retrying in %v\n", attempt, maxAttempts, lastErr, retryDelay)
			time.Sleep(retryDelay)
			continue
		}
		poolConfig.MaxConns = 50
		poolConfig.MinConns = 5
		poolConfig.MaxConnLifetime = 30 * time.Minute
		poolConfig.MaxConnIdleTime = 5 * time.Minute

		pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
		if err == nil {
			if pingErr := pool.Ping(ctx); pingErr == nil {
				Pool = pool
				fmt.Println("✅ Database connected")
				return nil
			} else {
				pool.Close()
				lastErr = fmt.Errorf("failed to ping database: %w", pingErr)
			}
		} else {
			lastErr = fmt.Errorf("failed to create connection pool: %w", err)
		}
		fmt.Printf("⏳ DB not ready (attempt %d/%d): %v — retrying in %v\n", attempt, maxAttempts, lastErr, retryDelay)
		time.Sleep(retryDelay)
	}
	return lastErr
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
