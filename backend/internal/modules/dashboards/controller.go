package dashboards

import (
	"encoding/json"
	"iot-dashboard/internal/middleware"

	"github.com/gofiber/fiber/v2"
)

type Controller struct{ svc *Service }

func NewController() *Controller { return &Controller{svc: NewService()} }

func (ctrl *Controller) List(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	data, err := ctrl.svc.GetDashboards(c.Context(), user.OrgId, user.Sub)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": data})
}

func (ctrl *Controller) GetByID(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	d, err := ctrl.svc.GetDashboardByID(c.Context(), c.Params("id"), user.OrgId)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": d})
}

func (ctrl *Controller) Create(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	var body struct {
		Name        string   `json:"name"`
		Description *string  `json:"description"`
		IsPublic    bool     `json:"isPublic"`
		Tags        []string `json:"tags"`
	}
	if err := c.BodyParser(&body); err != nil {
		return middleware.NewAppError(400, "VALIDATION_ERROR", "Invalid request body")
	}
	d, err := ctrl.svc.CreateDashboard(c.Context(), user.OrgId, user.Sub, body.Name, body.Description, body.IsPublic, body.Tags)
	if err != nil {
		return err
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "data": d})
}

func (ctrl *Controller) Update(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	var body map[string]interface{}
	if err := c.BodyParser(&body); err != nil {
		return middleware.NewAppError(400, "VALIDATION_ERROR", "Invalid request body")
	}
	d, err := ctrl.svc.UpdateDashboard(c.Context(), c.Params("id"), user.OrgId, body)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": d})
}

func (ctrl *Controller) Delete(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	if err := ctrl.svc.DeleteDashboard(c.Context(), c.Params("id"), user.OrgId, user.Sub, user.Role); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": nil})
}

func (ctrl *Controller) AddWidget(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	var w Widget
	if err := c.BodyParser(&w); err != nil {
		return middleware.NewAppError(400, "VALIDATION_ERROR", "Invalid request body")
	}
	if w.Layout == nil {
		w.Layout = json.RawMessage("{}")
	}
	if w.Config == nil {
		w.Config = json.RawMessage("{}")
	}
	result, err := ctrl.svc.AddWidget(c.Context(), c.Params("id"), user.OrgId, w)
	if err != nil {
		return err
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "data": result})
}

func (ctrl *Controller) UpdateWidget(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	var body map[string]interface{}
	if err := c.BodyParser(&body); err != nil {
		return middleware.NewAppError(400, "VALIDATION_ERROR", "Invalid request body")
	}
	if err := ctrl.svc.UpdateWidget(c.Context(), c.Params("widgetId"), user.OrgId, body); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": nil})
}

func (ctrl *Controller) BulkUpdateLayout(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	var widgets []map[string]interface{}
	if err := c.BodyParser(&widgets); err != nil {
		return middleware.NewAppError(400, "VALIDATION_ERROR", "Invalid request body")
	}
	if err := ctrl.svc.BulkUpdateLayout(c.Context(), c.Params("id"), user.OrgId, widgets); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": nil})
}

func (ctrl *Controller) DeleteWidget(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	if err := ctrl.svc.DeleteWidget(c.Context(), c.Params("widgetId"), user.OrgId); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": nil})
}
