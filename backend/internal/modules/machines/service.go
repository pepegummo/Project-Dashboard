package machines

import (
	"context"
	"encoding/json"
	"iot-dashboard/internal/middleware"
)

type Service struct {
	repo *Repository
}

func NewService() *Service { return &Service{repo: &Repository{}} }

func (s *Service) GetMachines(ctx context.Context, orgID string, filters map[string]string) ([]Machine, error) {
	return s.repo.FindAll(ctx, orgID, filters)
}

func (s *Service) GetMachineByID(ctx context.Context, id, orgID string) (*Machine, error) {
	m, err := s.repo.FindByID(ctx, id)
	if err != nil || m.ProductionLine.Factory.OrganizationID != orgID {
		return nil, middleware.NewAppError(404, "NOT_FOUND", "Machine not found")
	}
	return m, nil
}

func (s *Service) CreateMachine(ctx context.Context, orgID string, body map[string]interface{}) (*Machine, error) {
	productionLineID, _ := body["productionLineId"].(string)
	name, _ := body["name"].(string)
	machineType, _ := body["type"].(string)

	var serialNumber, model, manufacturer *string
	if v, ok := body["serialNumber"].(string); ok && v != "" {
		serialNumber = &v
	}
	if v, ok := body["model"].(string); ok && v != "" {
		model = &v
	}
	if v, ok := body["manufacturer"].(string); ok && v != "" {
		manufacturer = &v
	}

	var metadata json.RawMessage
	if v, ok := body["metadata"]; ok {
		b, _ := json.Marshal(v)
		metadata = b
	}

	return s.repo.Create(ctx, productionLineID, name, machineType, serialNumber, model, manufacturer, metadata)
}

func (s *Service) UpdateMachine(ctx context.Context, id, orgID string, data map[string]interface{}) (*Machine, error) {
	if _, err := s.GetMachineByID(ctx, id, orgID); err != nil {
		return nil, err
	}
	return s.repo.Update(ctx, id, data)
}

func (s *Service) DeleteMachine(ctx context.Context, id, orgID string) error {
	if _, err := s.GetMachineByID(ctx, id, orgID); err != nil {
		return err
	}
	return s.repo.Delete(ctx, id)
}

func (s *Service) GetMachineFields(ctx context.Context, machineID, orgID string) ([]MachineField, error) {
	if _, err := s.GetMachineByID(ctx, machineID, orgID); err != nil {
		return nil, err
	}
	return s.repo.GetFields(ctx, machineID)
}

func (s *Service) UpsertMachineField(ctx context.Context, machineID, orgID string, f MachineField) (*MachineField, error) {
	if _, err := s.GetMachineByID(ctx, machineID, orgID); err != nil {
		return nil, err
	}
	return s.repo.UpsertField(ctx, machineID, f)
}

func (s *Service) DeleteMachineField(ctx context.Context, machineID, orgID, fieldKey string) error {
	if _, err := s.GetMachineByID(ctx, machineID, orgID); err != nil {
		return err
	}
	return s.repo.DeleteField(ctx, machineID, fieldKey)
}

func (s *Service) GetProductionLines(ctx context.Context, orgID string) ([]ProductionLine, error) {
	return s.repo.GetProductionLines(ctx, orgID)
}

func (s *Service) GetFactories(ctx context.Context, orgID string) ([]Factory, error) {
	return s.repo.GetFactories(ctx, orgID)
}
