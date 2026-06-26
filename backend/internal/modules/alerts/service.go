package alerts

import (
	"context"
	"fmt"
	"iot-dashboard/internal/middleware"
	"sync"
	"time"
)

type Service struct {
	repo        *Repository
	lastFiredMu sync.Mutex
	lastFiredAt map[string]time.Time // alertID → last fired time
}

func NewService() *Service {
	return &Service{repo: &Repository{}, lastFiredAt: make(map[string]time.Time)}
}

func (s *Service) GetAlerts(ctx context.Context, orgID string, machineID *string) ([]Alert, error) {
	return s.repo.FindAll(ctx, orgID, machineID)
}

func (s *Service) GetAlertByID(ctx context.Context, id, orgID string) (*Alert, error) {
	a, err := s.repo.FindByID(ctx, id)
	if err != nil || a.OrgID != orgID {
		return nil, middleware.NewAppError(404, "NOT_FOUND", "Alert not found")
	}
	return a, nil
}

func (s *Service) CreateAlert(ctx context.Context, orgID string, a Alert) (*Alert, error) {
	if a.CooldownSec == 0 {
		a.CooldownSec = 300
	}
	return s.repo.Create(ctx, a)
}

func (s *Service) UpdateAlert(ctx context.Context, id, orgID string, data map[string]interface{}) (*Alert, error) {
	if _, err := s.GetAlertByID(ctx, id, orgID); err != nil {
		return nil, err
	}
	return s.repo.Update(ctx, id, data)
}

func (s *Service) DeleteAlert(ctx context.Context, id, orgID string) error {
	if _, err := s.GetAlertByID(ctx, id, orgID); err != nil {
		return err
	}
	return s.repo.Delete(ctx, id)
}

func (s *Service) GetActiveEvents(ctx context.Context, orgID *string) ([]AlertEvent, error) {
	return s.repo.GetActiveAlerts(ctx, orgID)
}

func (s *Service) AcknowledgeEvent(ctx context.Context, eventID, userID string) error {
	return s.repo.AcknowledgeEvent(ctx, eventID, userID)
}

func (s *Service) ResolveEvent(ctx context.Context, eventID, userID string) error {
	return s.repo.ResolveEvent(ctx, eventID, userID)
}

type TriggeredAlert struct {
	AlertID   string  `json:"alertId"`
	AlertName string  `json:"alertName"`
	Field     string  `json:"field"`
	Value     float64 `json:"value"`
	Threshold float64 `json:"threshold"`
	Condition string  `json:"condition"`
	Severity  string  `json:"severity"`
	Message   string  `json:"message"`
}

// EvaluateTelemetry checks alert rules — called on telemetry ingest.
func (s *Service) EvaluateTelemetry(ctx context.Context, machineID string, data map[string]interface{}) ([]TriggeredAlert, error) {
	alertRules, err := s.repo.GetAlertsForMachines(ctx, []string{machineID})
	if err != nil {
		return nil, err
	}

	var triggered []TriggeredAlert
	now := time.Now()

	for _, rule := range alertRules {
		rawVal, ok := data[rule.Field]
		if !ok {
			continue
		}
		value, ok := toFloat64(rawVal)
		if !ok {
			continue
		}

		if !evaluateCondition(value, rule.Condition, rule.Threshold, rule.ThresholdHi) {
			continue
		}

		// Cooldown check (thread-safe).
		// Subtract 500ms grace to absorb Go ticker jitter — without this, a
		// 30s cooldown paired with a 30s broadcaster interval occasionally
		// measures 29.998s elapsed and blocks the alert unexpectedly.
		const timerGrace = 500 * time.Millisecond
		s.lastFiredMu.Lock()
		last := s.lastFiredAt[rule.ID]
		cooldown := time.Duration(rule.CooldownSec) * time.Second
		if now.Sub(last) < (cooldown - timerGrace) {
			s.lastFiredMu.Unlock()
			continue
		}
		s.lastFiredAt[rule.ID] = now
		s.lastFiredMu.Unlock()

		msg := fmt.Sprintf("%s: %s = %.2f (%s %.2f)", rule.Name, rule.Field, value, rule.Condition, rule.Threshold)
		_ = s.repo.CreateEvent(ctx, rule.ID, value, msg)

		triggered = append(triggered, TriggeredAlert{
			AlertID:   rule.ID,
			AlertName: rule.Name,
			Field:     rule.Field,
			Value:     value,
			Threshold: rule.Threshold,
			Condition: rule.Condition,
			Severity:  rule.Severity,
			Message:   msg,
		})
	}
	return triggered, nil
}

func evaluateCondition(value float64, condition string, threshold float64, thresholdHi *float64) bool {
	switch condition {
	case "gt":
		return value > threshold
	case "lt":
		return value < threshold
	case "gte":
		return value >= threshold
	case "lte":
		return value <= threshold
	case "eq":
		return value == threshold
	case "neq":
		return value != threshold
	case "between":
		return thresholdHi != nil && value >= threshold && value <= *thresholdHi
	case "outside":
		return thresholdHi != nil && (value < threshold || value > *thresholdHi)
	}
	return false
}

func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}
