package dashboards

import (
	"context"
	"iot-dashboard/internal/middleware"
)

type Service struct{ repo *Repository }

func NewService() *Service { return &Service{repo: &Repository{}} }

func (s *Service) GetDashboards(ctx context.Context, orgID, userID string) ([]Dashboard, error) {
	return s.repo.FindAll(ctx, orgID, userID)
}

func (s *Service) GetDashboardByID(ctx context.Context, id, orgID string) (*Dashboard, error) {
	d, err := s.repo.FindByID(ctx, id)
	if err != nil || d.OrganizationID != orgID {
		return nil, middleware.NewAppError(404, "NOT_FOUND", "Dashboard not found")
	}
	return d, nil
}

func (s *Service) CreateDashboard(ctx context.Context, orgID, userID, name string, description *string, isPublic bool, tags []string) (*Dashboard, error) {
	return s.repo.Create(ctx, orgID, userID, name, description, isPublic, tags)
}

func (s *Service) UpdateDashboard(ctx context.Context, id, orgID string, data map[string]interface{}) (*Dashboard, error) {
	if _, err := s.GetDashboardByID(ctx, id, orgID); err != nil {
		return nil, err
	}
	return s.repo.Update(ctx, id, data)
}

func (s *Service) DeleteDashboard(ctx context.Context, id, orgID, userID, userRole string) error {
	d, err := s.GetDashboardByID(ctx, id, orgID)
	if err != nil {
		return err
	}
	if d.UserID != userID && userRole != "admin" {
		return middleware.NewAppError(403, "FORBIDDEN", "You can only delete your own dashboards")
	}
	return s.repo.Delete(ctx, id)
}

func (s *Service) AddWidget(ctx context.Context, dashboardID, orgID string, w Widget) (*Widget, error) {
	if _, err := s.GetDashboardByID(ctx, dashboardID, orgID); err != nil {
		return nil, err
	}
	return s.repo.AddWidget(ctx, dashboardID, w)
}

func (s *Service) UpdateWidget(ctx context.Context, widgetID, orgID string, data map[string]interface{}) error {
	_, wOrgID, err := s.repo.FindWidget(ctx, widgetID)
	if err != nil || wOrgID != orgID {
		return middleware.NewAppError(404, "NOT_FOUND", "Widget not found")
	}
	return s.repo.UpdateWidget(ctx, widgetID, data)
}

func (s *Service) BulkUpdateLayout(ctx context.Context, dashboardID, orgID string, widgets []map[string]interface{}) error {
	if _, err := s.GetDashboardByID(ctx, dashboardID, orgID); err != nil {
		return err
	}
	return s.repo.BulkUpdateLayout(ctx, widgets)
}

func (s *Service) DeleteWidget(ctx context.Context, widgetID, orgID string) error {
	_, wOrgID, err := s.repo.FindWidget(ctx, widgetID)
	if err != nil || wOrgID != orgID {
		return middleware.NewAppError(404, "NOT_FOUND", "Widget not found")
	}
	return s.repo.DeleteWidget(ctx, widgetID)
}
