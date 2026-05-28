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

func (ctrl *Controller) GetActiveEvents(c *fiber.Ctx) error {
	// Public — no auth required (LED kiosk)
	events, err := ctrl.svc.GetActiveEvents(c.Context(), nil)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": events})
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
