// Package broadcaster polls the database for the latest telemetry per machine
// and broadcasts it to WebSocket clients every pollInterval seconds.
// This replaces the simulator when SIMULATOR_ENABLED=false, ensuring the
// frontend always receives real DB data instead of synthetic values.
package broadcaster

import (
	"context"
	"fmt"
	"iot-dashboard/internal/database"
	ws "iot-dashboard/internal/websocket"
	"time"
)

type machineRow struct {
	id   string
	name string
}

// AlertEvaluator evaluates alert rules against telemetry and broadcasts triggered events.
type AlertEvaluator interface {
	EvaluateAndBroadcast(ctx context.Context, machineID, machineName string, data map[string]interface{})
}

type Broadcaster struct {
	gateway      *ws.Gateway
	alertEval    AlertEvaluator
	pollInterval time.Duration
	stop         chan struct{}
}

func New(gateway *ws.Gateway, pollInterval time.Duration, alertEval AlertEvaluator) *Broadcaster {
	return &Broadcaster{
		gateway:      gateway,
		alertEval:    alertEval,
		pollInterval: pollInterval,
		stop:         make(chan struct{}),
	}
}

func (b *Broadcaster) Start() {
	go b.run()
}

func (b *Broadcaster) Stop() {
	close(b.stop)
}

func (b *Broadcaster) run() {
	fmt.Printf("📡  DB Broadcaster started — polling every %v\n", b.pollInterval)

	ticker := time.NewTicker(b.pollInterval)
	defer ticker.Stop()

	// Broadcast immediately on start, then every pollInterval
	b.broadcast()

	for {
		select {
		case <-b.stop:
			return
		case <-ticker.C:
			b.broadcast()
		}
	}
}

func (b *Broadcaster) broadcast() {
	ctx := context.Background()

	// Load all machines
	machines, err := loadMachines(ctx)
	if err != nil || len(machines) == 0 {
		return
	}

	// Collect IDs for batch query
	ids := make([]string, len(machines))
	nameByID := make(map[string]string, len(machines))
	for i, m := range machines {
		ids[i] = m.id
		nameByID[m.id] = m.name
	}

	// Fetch latest telemetry row per machine in one round-trip
	rows, err := database.Pool.Query(ctx, `
		SELECT DISTINCT ON (machine_id)
			machine_id,
			timestamp,
			data
		FROM telemetry_raw
		WHERE machine_id = ANY($1::uuid[])
		  AND timestamp <= NOW()
		ORDER BY machine_id, timestamp DESC
	`, ids)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var machineID string
		var ts time.Time
		var data map[string]interface{}
		if err := rows.Scan(&machineID, &ts, &data); err != nil {
			continue
		}
		b.gateway.BroadcastTelemetry(ws.TelemetryPayload{
			MachineID:   machineID,
			MachineName: nameByID[machineID],
			Timestamp:   ts.UTC().Format(time.RFC3339),
			Data:        data,
		})
		if b.alertEval != nil {
			b.alertEval.EvaluateAndBroadcast(ctx, machineID, nameByID[machineID], data)
		}
	}
}

// BroadcastOne pushes a single machine's telemetry immediately (called from ingest endpoint).
func (b *Broadcaster) BroadcastOne(machineID, machineName, timestamp string, data map[string]interface{}) {
	b.gateway.BroadcastTelemetry(ws.TelemetryPayload{
		MachineID:   machineID,
		MachineName: machineName,
		Timestamp:   timestamp,
		Data:        data,
	})
}

func loadMachines(ctx context.Context) ([]machineRow, error) {
	rows, err := database.Pool.Query(ctx, `SELECT id, name FROM machines`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var machines []machineRow
	for rows.Next() {
		var m machineRow
		if err := rows.Scan(&m.id, &m.name); err != nil {
			continue
		}
		machines = append(machines, m)
	}
	return machines, nil
}
