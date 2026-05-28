package machines

import (
	"context"
	"encoding/json"
	"fmt"
	"iot-dashboard/internal/database"
	"time"
)

type Machine struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Type             string          `json:"type"`
	Status           string          `json:"status"`
	SerialNumber     *string         `json:"serialNumber"`
	Model            *string         `json:"model"`
	Manufacturer     *string         `json:"manufacturer"`
	Metadata         json.RawMessage `json:"metadata"`
	ProductionLineID string          `json:"productionLineId"`
	LastSeenAt       *time.Time      `json:"lastSeenAt"`
	CreatedAt        time.Time       `json:"createdAt"`
	ProductionLine   *ProductionLine `json:"productionLine,omitempty"`
	Fields           []MachineField  `json:"fields,omitempty"`
}

type MachineField struct {
	ID           string   `json:"id"`
	MachineID    string   `json:"machineId"`
	Key          string   `json:"key"`
	Label        string   `json:"label"`
	Unit         *string  `json:"unit"`
	DataType     string   `json:"dataType"`
	Min          *float64 `json:"min"`
	Max          *float64 `json:"max"`
	Threshold    *float64 `json:"threshold"`
	UpperLimit   *float64 `json:"upperLimit"`
	LowerLimit   *float64 `json:"lowerLimit"`
	Precision    int      `json:"precision"`
	IsKey        bool     `json:"isKey"`
}

type ProductionLine struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	FactoryID string   `json:"factoryId"`
	Factory   *Factory `json:"factory,omitempty"`
}

type Factory struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	OrganizationID string `json:"organizationId"`
}

type Repository struct{}

func (r *Repository) FindAll(ctx context.Context, orgID string, filters map[string]string) ([]Machine, error) {
	query := `
		SELECT m.id, m.name, m.type, m.status, m.serial_number, m.model, m.manufacturer,
		       m.metadata, m.production_line_id, m.last_seen_at, m.created_at,
		       pl.id, pl.name, pl.factory_id,
		       f.id, f.name, f.organization_id
		FROM machines m
		JOIN production_lines pl ON pl.id = m.production_line_id
		JOIN factories f ON f.id = pl.factory_id
		WHERE f.organization_id = $1
		ORDER BY m.name ASC`

	rows, err := database.Pool.Query(ctx, query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var machines []Machine
	for rows.Next() {
		var m Machine
		var pl ProductionLine
		var fac Factory
		err := rows.Scan(
			&m.ID, &m.Name, &m.Type, &m.Status, &m.SerialNumber, &m.Model, &m.Manufacturer,
			&m.Metadata, &m.ProductionLineID, &m.LastSeenAt, &m.CreatedAt,
			&pl.ID, &pl.Name, &pl.FactoryID,
			&fac.ID, &fac.Name, &fac.OrganizationID,
		)
		if err != nil {
			return nil, err
		}
		pl.Factory = &fac
		m.ProductionLine = &pl
		machines = append(machines, m)
	}

	// Attach fields for each machine
	for i := range machines {
		fields, err := r.GetFields(ctx, machines[i].ID)
		if err == nil {
			machines[i].Fields = fields
		}
	}

	return machines, nil
}

func (r *Repository) FindByID(ctx context.Context, id string) (*Machine, error) {
	row := database.Pool.QueryRow(ctx, `
		SELECT m.id, m.name, m.type, m.status, m.serial_number, m.model, m.manufacturer,
		       m.metadata, m.production_line_id, m.last_seen_at, m.created_at,
		       pl.id, pl.name, pl.factory_id,
		       f.id, f.name, f.organization_id
		FROM machines m
		JOIN production_lines pl ON pl.id = m.production_line_id
		JOIN factories f ON f.id = pl.factory_id
		WHERE m.id = $1
	`, id)

	var m Machine
	var pl ProductionLine
	var fac Factory
	err := row.Scan(
		&m.ID, &m.Name, &m.Type, &m.Status, &m.SerialNumber, &m.Model, &m.Manufacturer,
		&m.Metadata, &m.ProductionLineID, &m.LastSeenAt, &m.CreatedAt,
		&pl.ID, &pl.Name, &pl.FactoryID,
		&fac.ID, &fac.Name, &fac.OrganizationID,
	)
	if err != nil {
		return nil, err
	}
	pl.Factory = &fac
	m.ProductionLine = &pl

	fields, _ := r.GetFields(ctx, m.ID)
	m.Fields = fields

	return &m, nil
}

func (r *Repository) Create(ctx context.Context, productionLineID, name, machineType string,
	serialNumber, model, manufacturer *string, metadata json.RawMessage) (*Machine, error) {

	if metadata == nil {
		metadata = json.RawMessage("{}")
	}

	row := database.Pool.QueryRow(ctx, `
		INSERT INTO machines (id, production_line_id, name, type, status, serial_number, model, manufacturer, metadata, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2, $3, 'offline', $4, $5, $6, $7, NOW(), NOW())
		RETURNING id
	`, productionLineID, name, machineType, serialNumber, model, manufacturer, metadata)

	var id string
	if err := row.Scan(&id); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, id)
}

func (r *Repository) Update(ctx context.Context, id string, fields map[string]interface{}) (*Machine, error) {
	if len(fields) == 0 {
		return r.FindByID(ctx, id)
	}
	// Build dynamic update (simple approach for known fields)
	if name, ok := fields["name"].(string); ok {
		_, err := database.Pool.Exec(ctx, `UPDATE machines SET name=$1, updated_at=NOW() WHERE id=$2`, name, id)
		if err != nil {
			return nil, err
		}
	}
	if status, ok := fields["status"].(string); ok {
		_, err := database.Pool.Exec(ctx, `UPDATE machines SET status=$1, updated_at=NOW() WHERE id=$2`, status, id)
		if err != nil {
			return nil, err
		}
	}
	return r.FindByID(ctx, id)
}

func (r *Repository) UpdateStatus(ctx context.Context, id, status string) error {
	_, err := database.Pool.Exec(ctx, `
		UPDATE machines SET status=$1, last_seen_at=NOW(), updated_at=NOW() WHERE id=$2
	`, status, id)
	return err
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	_, err := database.Pool.Exec(ctx, `DELETE FROM machines WHERE id=$1`, id)
	return err
}

func (r *Repository) GetFields(ctx context.Context, machineID string) ([]MachineField, error) {
	rows, err := database.Pool.Query(ctx, `
		SELECT id, machine_id, key, label, unit, data_type, min, max,
		       threshold, upper_limit, lower_limit, precision, is_key
		FROM machine_fields
		WHERE machine_id = $1
		ORDER BY is_key DESC, key ASC
	`, machineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fields []MachineField
	for rows.Next() {
		var f MachineField
		err := rows.Scan(
			&f.ID, &f.MachineID, &f.Key, &f.Label, &f.Unit, &f.DataType,
			&f.Min, &f.Max, &f.Threshold, &f.UpperLimit, &f.LowerLimit,
			&f.Precision, &f.IsKey,
		)
		if err != nil {
			return nil, err
		}
		fields = append(fields, f)
	}
	return fields, nil
}

func (r *Repository) UpsertField(ctx context.Context, machineID string, f MachineField) (*MachineField, error) {
	_, err := database.Pool.Exec(ctx, `
		INSERT INTO machine_fields (id, machine_id, key, label, unit, data_type, min, max,
		                            threshold, upper_limit, lower_limit, precision, is_key)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (machine_id, key) DO UPDATE SET
		  label=$3, unit=$4, data_type=$5, min=$6, max=$7,
		  threshold=$8, upper_limit=$9, lower_limit=$10, precision=$11, is_key=$12
	`, machineID, f.Key, f.Label, f.Unit, f.DataType, f.Min, f.Max,
		f.Threshold, f.UpperLimit, f.LowerLimit, f.Precision, f.IsKey)
	if err != nil {
		return nil, err
	}
	// Return the upserted field
	row := database.Pool.QueryRow(ctx, `
		SELECT id, machine_id, key, label, unit, data_type, min, max,
		       threshold, upper_limit, lower_limit, precision, is_key
		FROM machine_fields WHERE machine_id=$1 AND key=$2
	`, machineID, f.Key)
	var out MachineField
	err = row.Scan(
		&out.ID, &out.MachineID, &out.Key, &out.Label, &out.Unit, &out.DataType,
		&out.Min, &out.Max, &out.Threshold, &out.UpperLimit, &out.LowerLimit,
		&out.Precision, &out.IsKey,
	)
	return &out, err
}

func (r *Repository) GetProductionLines(ctx context.Context, orgID string) ([]ProductionLine, error) {
	rows, err := database.Pool.Query(ctx, `
		SELECT pl.id, pl.name, pl.factory_id, f.id, f.name, f.organization_id
		FROM production_lines pl
		JOIN factories f ON f.id = pl.factory_id
		WHERE f.organization_id = $1
		ORDER BY pl.name ASC
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lines []ProductionLine
	for rows.Next() {
		var pl ProductionLine
		var fac Factory
		if err := rows.Scan(&pl.ID, &pl.Name, &pl.FactoryID, &fac.ID, &fac.Name, &fac.OrganizationID); err != nil {
			return nil, err
		}
		pl.Factory = &fac
		lines = append(lines, pl)
	}
	return lines, nil
}

func (r *Repository) GetFactories(ctx context.Context, orgID string) ([]Factory, error) {
	rows, err := database.Pool.Query(ctx, `
		SELECT id, name, organization_id FROM factories WHERE organization_id=$1 ORDER BY name ASC
	`, orgID)
	if err != nil {
		return nil, fmt.Errorf("get factories: %w", err)
	}
	defer rows.Close()

	var factories []Factory
	for rows.Next() {
		var f Factory
		if err := rows.Scan(&f.ID, &f.Name, &f.OrganizationID); err != nil {
			return nil, err
		}
		factories = append(factories, f)
	}
	return factories, nil
}
