package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"iot-dashboard/internal/database"
	"time"
)

type TelemetryPoint struct {
	Bucket    *time.Time `json:"bucket,omitempty"`
	Ts        *time.Time `json:"ts,omitempty"`
	Avg       *float64   `json:"avg,omitempty"`
	Min       *float64   `json:"min,omitempty"`
	Max       *float64   `json:"max,omitempty"`
	Value     *float64   `json:"value,omitempty"`
	Count     int        `json:"count,omitempty"`
}

type LatestSnapshot struct {
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

type AggregateSummary struct {
	Avg   float64 `json:"avg"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	Count int     `json:"count"`
}

type DailyCount struct {
	Date  time.Time `json:"date"`
	Count int       `json:"count"`
}

type TotalCount struct {
	Total int        `json:"total"`
	Since *time.Time `json:"since"`
}

type Repository struct{}

func (r *Repository) Ingest(ctx context.Context, machineID string, data map[string]interface{}, timestamp time.Time) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = database.Pool.Exec(ctx, `
		INSERT INTO telemetry_raw (id, machine_id, timestamp, data)
		VALUES (gen_random_uuid(), $1, $2, $3)
	`, machineID, timestamp, string(b))
	return err
}

func (r *Repository) GetLatest(ctx context.Context, machineID string) (*LatestSnapshot, error) {
	row := database.Pool.QueryRow(ctx, `
		SELECT timestamp, data
		FROM telemetry_raw
		WHERE machine_id = $1 AND timestamp <= NOW()
		ORDER BY timestamp DESC
		LIMIT 1
	`, machineID)

	var ts time.Time
	var rawData json.RawMessage
	if err := row.Scan(&ts, &rawData); err != nil {
		return nil, err
	}

	var data map[string]interface{}
	_ = json.Unmarshal(rawData, &data)
	return &LatestSnapshot{Timestamp: ts, Data: data}, nil
}

// GetTimescaleAggregate runs a time_bucket query — exact port of TypeScript version.
func (r *Repository) GetTimescaleAggregate(ctx context.Context, machineID, field string, from, to time.Time, bucket string) ([]TelemetryPoint, error) {
	query := fmt.Sprintf(`
		SELECT
			time_bucket('%s'::interval, timestamp) AS bucket,
			AVG((data->>'%s')::float)              AS avg,
			MIN((data->>'%s')::float)              AS min,
			MAX((data->>'%s')::float)              AS max,
			COUNT(*)                                AS count
		FROM telemetry_raw
		WHERE machine_id = $1
		  AND timestamp >= $2
		  AND timestamp <= $3
		  AND data ? '%s'
		GROUP BY bucket
		ORDER BY bucket ASC
	`, bucket, field, field, field, field)

	rows, err := database.Pool.Query(ctx, query, machineID, from, to)
	if err != nil {
		// Fallback to raw series if TimescaleDB not available
		return r.GetFieldSeries(ctx, machineID, field, from, to, 500)
	}
	defer rows.Close()

	var points []TelemetryPoint
	for rows.Next() {
		var p TelemetryPoint
		var count int64
		if err := rows.Scan(&p.Bucket, &p.Avg, &p.Min, &p.Max, &count); err != nil {
			return nil, err
		}
		p.Count = int(count)
		points = append(points, p)
	}
	return points, nil
}

func (r *Repository) GetFieldSeries(ctx context.Context, machineID, field string, from, to time.Time, limit int) ([]TelemetryPoint, error) {
	query := fmt.Sprintf(`
		SELECT
			timestamp AS ts,
			(data->>'%s')::float AS value
		FROM telemetry_raw
		WHERE machine_id = $1
		  AND timestamp >= $2
		  AND timestamp <= $3
		  AND data ? '%s'
		ORDER BY timestamp ASC
		LIMIT %d
	`, field, field, limit)

	rows, err := database.Pool.Query(ctx, query, machineID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []TelemetryPoint
	for rows.Next() {
		var p TelemetryPoint
		if err := rows.Scan(&p.Ts, &p.Value); err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	return points, nil
}

func (r *Repository) GetAggregateSummary(ctx context.Context, machineID, field string, from, to time.Time) (*AggregateSummary, error) {
	query := fmt.Sprintf(`
		SELECT
			AVG((data->>'%s')::float) AS avg,
			MIN((data->>'%s')::float) AS min,
			MAX((data->>'%s')::float) AS max,
			COUNT(*)                   AS count
		FROM telemetry_raw
		WHERE machine_id = $1
		  AND timestamp >= $2
		  AND timestamp <= $3
		  AND data ? '%s'
	`, field, field, field, field)

	row := database.Pool.QueryRow(ctx, query, machineID, from, to)

	var avg, min, max *float64
	var count int64
	if err := row.Scan(&avg, &min, &max, &count); err != nil {
		return nil, err
	}
	if count == 0 || avg == nil {
		return nil, nil
	}
	return &AggregateSummary{
		Avg: *avg, Min: *min, Max: *max, Count: int(count),
	}, nil
}

func (r *Repository) GetDailyCount(ctx context.Context, machineID string, days int) ([]DailyCount, error) {
	from := time.Now().AddDate(0, 0, -days)
	to := time.Now()

	rows, err := database.Pool.Query(ctx, `
		SELECT
			time_bucket('1 day', timestamp) AS date,
			COUNT(*) AS count
		FROM telemetry_raw
		WHERE machine_id = $1
		  AND timestamp >= $2
		  AND timestamp <= $3
		GROUP BY date
		ORDER BY date ASC
	`, machineID, from, to)
	if err != nil {
		return []DailyCount{}, nil
	}
	defer rows.Close()

	var result []DailyCount
	for rows.Next() {
		var d DailyCount
		var count int64
		if err := rows.Scan(&d.Date, &count); err != nil {
			continue
		}
		d.Count = int(count)
		result = append(result, d)
	}
	return result, nil
}

func (r *Repository) GetHourlyCount(ctx context.Context, machineID string, hours int) ([]TelemetryPoint, error) {
	from := time.Now().Add(-time.Duration(hours) * time.Hour)

	rows, err := database.Pool.Query(ctx, `
		SELECT
			time_bucket('1 hour', timestamp) AS bucket,
			COUNT(*) AS count
		FROM telemetry_raw
		WHERE machine_id = $1
		  AND timestamp >= $2
		  AND timestamp <= NOW()
		GROUP BY bucket
		ORDER BY bucket ASC
	`, machineID, from)
	if err != nil {
		return []TelemetryPoint{}, nil
	}
	defer rows.Close()

	var result []TelemetryPoint
	for rows.Next() {
		var p TelemetryPoint
		var count int64
		if err := rows.Scan(&p.Bucket, &count); err != nil {
			continue
		}
		p.Count = int(count)
		result = append(result, p)
	}
	return result, nil
}

func (r *Repository) GetTotalCount(ctx context.Context, machineID string) (*TotalCount, error) {
	row := database.Pool.QueryRow(ctx, `
		SELECT COUNT(*) AS total, MIN(timestamp) AS since
		FROM telemetry_raw
		WHERE machine_id = $1
	`, machineID)
	var total int64
	var since *time.Time
	if err := row.Scan(&total, &since); err != nil {
		return nil, err
	}
	return &TotalCount{Total: int(total), Since: since}, nil
}

func (r *Repository) GetLatestForMachines(ctx context.Context, machineIDs []string) (map[string]*LatestSnapshot, error) {
	if len(machineIDs) == 0 {
		return map[string]*LatestSnapshot{}, nil
	}

	rows, err := database.Pool.Query(ctx, `
		SELECT DISTINCT ON (machine_id)
			machine_id,
			timestamp,
			data
		FROM telemetry_raw
		WHERE machine_id = ANY($1::uuid[])
		  AND timestamp <= NOW()
		ORDER BY machine_id, timestamp DESC
	`, machineIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]*LatestSnapshot)
	for rows.Next() {
		var machineID string
		var ts time.Time
		var rawData json.RawMessage
		if err := rows.Scan(&machineID, &ts, &rawData); err != nil {
			continue
		}
		var data map[string]interface{}
		_ = json.Unmarshal(rawData, &data)
		result[machineID] = &LatestSnapshot{Timestamp: ts, Data: data}
	}
	return result, nil
}
