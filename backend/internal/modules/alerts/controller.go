package alerts

import (
	"iot-dashboard/internal/middleware"

	"github.com/gofiber/fiber/v2"
)

type Controller struct{ svc *Service }

func NewController() *Controller { return &Controller{svc: NewService()} }

func (ctrl *Controller) List(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	var machineID *string
	if v := c.Query("machineId"); v != "" {
		machineID = &v
	}
	data, err := ctrl.svc.GetAlerts(c.Context(), user.OrgId, machineID)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": data})
}

func (ctrl *Controller) GetByID(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	a, err := ctrl.svc.GetAlertByID(c.Context(), c.Params("id"), user.OrgId)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": a})
}

func (ctrl *Controller) Create(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	var a Alert
	if err := c.BodyParser(&a); err != nil {
		return middleware.NewAppError(400, "VALIDATION_ERROR", "Invalid request body")
	}
	result, err := ctrl.svc.CreateAlert(c.Context(), user.OrgId, a)
	if err != nil {
		return err
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "data": result})
}

func (ctrl *Controller) Update(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	var body map[string]interface{}
	if err := c.BodyParser(&body); err != nil {
		return middleware.NewAppError(400, "VALIDATION_ERROR", "Invalid request body")
	}
	result, err := ctrl.svc.UpdateAlert(c.Context(), c.Params("id"), user.OrgId, body)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": result})
}

func (ctrl *Controller) Delete(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	if err := ctrl.svc.DeleteAlert(c.Context(), c.Params("id"), user.OrgId); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": nil})
}

type alertMachineResp struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type alertInfoResp struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Field     string           `json:"field"`
	Severity  string           `json:"severity"`
	Threshold float64          `json:"threshold"`
	Machine   alertMachineResp `json:"machine"`
}

type alertEventResp struct {
	ID        string        `json:"id"`
	AlertID   string        `json:"alertId"`
	Value     float64       `json:"value"`
	Message   string        `json:"message,omitempty"`
	Status    string        `json:"status"`
	CreatedAt string        `json:"createdAt"`
	Alert     alertInfoResp `json:"alert"`
}

func (ctrl *Controller) GetActiveEvents(c *fiber.Ctx) error {
	// Public — no auth required (LED kiosk)
	events, err := ctrl.svc.GetActiveEvents(c.Context(), nil)
	if err != nil {
		return err
	}
	resp := make([]alertEventResp, 0, len(events))
	for _, e := range events {
		resp = append(resp, alertEventResp{
			ID:        e.ID,
			AlertID:   e.AlertID,
			Value:     e.Value,
			Message:   e.Message,
			Status:    e.Status,
			CreatedAt: e.TriggeredAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			Alert: alertInfoResp{
				ID:        e.AlertID,
				Name:      e.AlertName,
				Field:     e.Field,
				Severity:  e.Severity,
				Threshold: e.Threshold,
				Machine: alertMachineResp{
					ID:   e.MachineID,
					Name: e.MachineName,
				},
			},
		})
	}
	return c.JSON(fiber.Map{"success": true, "data": resp})
}

func (ctrl *Controller) AcknowledgeEvent(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	if err := ctrl.svc.AcknowledgeEvent(c.Context(), c.Params("eventId"), user.Sub); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": nil})
}

func (ctrl *Controller) ResolveEvent(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	if err := ctrl.svc.ResolveEvent(c.Context(), c.Params("eventId"), user.Sub); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": nil})
}
