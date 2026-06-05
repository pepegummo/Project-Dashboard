package alerts

import (
	"context"
	"iot-dashboard/internal/database"
	"time"
)

type Alert struct {
	ID          string   `json:"id"`
	MachineID   string   `json:"machineId"`
	Name        string   `json:"name"`
	Description *string  `json:"description"`
	Field       string   `json:"field"`
	Condition   string   `json:"condition"`
	Threshold   float64  `json:"threshold"`
	ThresholdHi *float64 `json:"thresholdHi"`
	Severity    string   `json:"severity"`
	CooldownSec int      `json:"cooldownSec"`
	IsActive    bool     `json:"isActive"`
	OrgID       string   `json:"organizationId,omitempty"`
}

type AlertEvent struct {
	ID          string     `json:"id"`
	AlertID     string     `json:"alertId"`
	AlertName   string     `json:"alertName,omitempty"`
	MachineID   string     `json:"machineId,omitempty"`
	MachineName string     `json:"machineName,omitempty"`
	Value       float64    `json:"value"`
	Message     string     `json:"message"`
	Status      string     `json:"status"`
	Severity    string     `json:"severity,omitempty"`
	Field       string     `json:"field,omitempty"`
	Threshold   float64    `json:"threshold,omitempty"`
	TriggeredAt time.Time  `json:"triggeredAt"`
	AckedAt     *time.Time `json:"acknowledgedAt"`
	ResolvedAt  *time.Time `json:"resolvedAt"`
}

type Repository struct{}

func (r *Repository) FindAll(ctx context.Context, orgID string, machineID *string) ([]Alert, error) {
	query := `
		SELECT a.id, a.machine_id, a.name, a.description, a.field, a.condition,
		       a.threshold, a.threshold_hi, a.severity, a.cooldown_sec, a.is_active,
		       f.organization_id
		FROM alerts a
		JOIN machines m ON m.id = a.machine_id
		JOIN production_lines pl ON pl.id = m.production_line_id
		JOIN factories f ON f.id = pl.factory_id
		WHERE f.organization_id = $1`
	args := []interface{}{orgID}

	if machineID != nil {
		query += " AND a.machine_id = $2"
		args = append(args, *machineID)
	}
	query += " ORDER BY a.created_at DESC"

	rows, err := database.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	alerts := make([]Alert, 0)
	for rows.Next() {
		var a Alert
		if err := rows.Scan(&a.ID, &a.MachineID, &a.Name, &a.Description, &a.Field, &a.Condition,
			&a.Threshold, &a.ThresholdHi, &a.Severity, &a.CooldownSec, &a.IsActive, &a.OrgID); err != nil {
			return nil, err
		}
		alerts = append(alerts, a)
	}
	return alerts, nil
}

func (r *Repository) FindByID(ctx context.Context, id string) (*Alert, error) {
	row := database.Pool.QueryRow(ctx, `
		SELECT a.id, a.machine_id, a.name, a.description, a.field, a.condition,
		       a.threshold, a.threshold_hi, a.severity, a.cooldown_sec, a.is_active,
		       f.organization_id
		FROM alerts a
		JOIN machines m ON m.id = a.machine_id
		JOIN production_lines pl ON pl.id = m.production_line_id
		JOIN factories f ON f.id = pl.factory_id
		WHERE a.id = $1
	`, id)

	var a Alert
	err := row.Scan(&a.ID, &a.MachineID, &a.Name, &a.Description, &a.Field, &a.Condition,
		&a.Threshold, &a.ThresholdHi, &a.Severity, &a.CooldownSec, &a.IsActive, &a.OrgID)
	return &a, err
}

func (r *Repository) Create(ctx context.Context, a Alert) (*Alert, error) {
	row := database.Pool.QueryRow(ctx, `
		INSERT INTO alerts (id, machine_id, name, description, field, condition, threshold, threshold_hi, severity, cooldown_sec, is_active, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9, true, NOW(), NOW())
		RETURNING id
	`, a.MachineID, a.Name, a.Description, a.Field, a.Condition, a.Threshold, a.ThresholdHi, a.Severity, a.CooldownSec)
	var id string
	if err := row.Scan(&id); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, id)
}

func (r *Repository) Update(ctx context.Context, id string, data map[string]interface{}) (*Alert, error) {
	_, err := database.Pool.Exec(ctx, `
		UPDATE alerts SET
			name         = COALESCE($1, name),
			description  = $2,
			field        = COALESCE($3, field),
			condition    = COALESCE($4, condition),
			threshold    = COALESCE($5, threshold),
			threshold_hi = $6,
			severity     = COALESCE($7, severity),
			cooldown_sec = COALESCE($8, cooldown_sec),
			is_active    = COALESCE($9, is_active),
			updated_at   = NOW()
		WHERE id = $10
	`,
		mapStr(data, "name"),
		mapStrPtr(data, "description"),
		mapStr(data, "field"),
		mapStr(data, "condition"),
		mapFloat64(data, "threshold"),
		mapFloat64(data, "thresholdHi"),
		mapStr(data, "severity"),
		mapInt(data, "cooldownSec"),
		mapBool(data, "isActive"),
		id,
	)
	if err != nil {
		return nil, err
	}
	return r.FindByID(ctx, id)
}

// mapStr returns a *string for COALESCE — nil means "leave column unchanged".
func mapStr(data map[string]interface{}, key string) *string {
	if v, ok := data[key].(string); ok && v != "" {
		return &v
	}
	return nil
}

// mapStrPtr returns a *string or nil; allows clearing a nullable column.
func mapStrPtr(data map[string]interface{}, key string) *string {
	v, ok := data[key]
	if !ok {
		return nil
	}
	if s, ok := v.(string); ok {
		return &s
	}
	return nil
}

func mapFloat64(data map[string]interface{}, key string) *float64 {
	v, ok := data[key]
	if !ok {
		return nil
	}
	switch n := v.(type) {
	case float64:
		return &n
	case float32:
		f := float64(n)
		return &f
	}
	return nil
}

func mapInt(data map[string]interface{}, key string) *int {
	v, ok := data[key]
	if !ok {
		return nil
	}
	switch n := v.(type) {
	case float64:
		i := int(n)
		return &i
	case int:
		return &n
	}
	return nil
}

func mapBool(data map[string]interface{}, key string) *bool {
	if v, ok := data[key].(bool); ok {
		return &v
	}
	return nil
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	_, err := database.Pool.Exec(ctx, `DELETE FROM alerts WHERE id=$1`, id)
	return err
}

func (r *Repository) GetActiveAlerts(ctx context.Context, orgID *string) ([]AlertEvent, error) {
	query := `
		SELECT ae.id, ae.alert_id, a.name, m.id, m.name, ae.value, COALESCE(ae.message, ''),
		       ae.status, a.severity, a.field, a.threshold, ae.triggered_at, ae.acknowledged_at, ae.resolved_at
		FROM alert_events ae
		JOIN alerts a ON a.id = ae.alert_id
		JOIN machines m ON m.id = a.machine_id
		JOIN production_lines pl ON pl.id = m.production_line_id
		JOIN factories f ON f.id = pl.factory_id
		WHERE ae.status = 'open'`
	args := []interface{}{}

	if orgID != nil {
		query += " AND f.organization_id = $1"
		args = append(args, *orgID)
	}
	query += " ORDER BY ae.triggered_at DESC LIMIT 100"

	rows, err := database.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]AlertEvent, 0)
	for rows.Next() {
		var e AlertEvent
		if err := rows.Scan(&e.ID, &e.AlertID, &e.AlertName, &e.MachineID, &e.MachineName,
			&e.Value, &e.Message, &e.Status, &e.Severity, &e.Field, &e.Threshold, &e.TriggeredAt, &e.AckedAt, &e.ResolvedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func (r *Repository) CreateEvent(ctx context.Context, alertID string, value float64, message string) error {
	_, err := database.Pool.Exec(ctx, `
		INSERT INTO alert_events (id, alert_id, value, message, status, triggered_at)
		VALUES (gen_random_uuid(), $1, $2, $3, 'open', NOW())
	`, alertID, value, message)
	return err
}

func (r *Repository) AcknowledgeEvent(ctx context.Context, eventID, userID string) error {
	_, err := database.Pool.Exec(ctx, `
		UPDATE alert_events SET status='acknowledged', acknowledged_at=NOW(), acknowledged_by=$1 WHERE id=$2
	`, userID, eventID)
	return err
}

func (r *Repository) ResolveEvent(ctx context.Context, eventID, userID string) error {
	_, err := database.Pool.Exec(ctx, `
		UPDATE alert_events SET status='resolved', resolved_at=NOW(), resolved_by=$1 WHERE id=$2
	`, userID, eventID)
	return err
}

func (r *Repository) GetAlertsForMachines(ctx context.Context, machineIDs []string) ([]Alert, error) {
	rows, err := database.Pool.Query(ctx, `
		SELECT id, machine_id, name, field, condition, threshold, threshold_hi, severity, cooldown_sec
		FROM alerts WHERE machine_id = ANY($1::uuid[]) AND is_active = true
	`, machineIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	alerts := make([]Alert, 0)
	for rows.Next() {
		var a Alert
		if err := rows.Scan(&a.ID, &a.MachineID, &a.Name, &a.Field, &a.Condition,
			&a.Threshold, &a.ThresholdHi, &a.Severity, &a.CooldownSec); err != nil {
			return nil, err
		}
		alerts = append(alerts, a)
	}
	return alerts, nil
}
