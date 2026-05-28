package machines

import (
	"iot-dashboard/internal/middleware"
	"strconv"

	"github.com/gofiber/fiber/v2"
)

type Controller struct {
	svc *Service
}

func NewController() *Controller { return &Controller{svc: NewService()} }

func (ctrl *Controller) List(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	filters := map[string]string{}
	if v := c.Query("productionLineId"); v != "" {
		filters["productionLineId"] = v
	}
	if v := c.Query("type"); v != "" {
		filters["type"] = v
	}
	if v := c.Query("status"); v != "" {
		filters["status"] = v
	}
	machines, err := ctrl.svc.GetMachines(c.Context(), user.OrgId, filters)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": machines})
}

func (ctrl *Controller) GetByID(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	m, err := ctrl.svc.GetMachineByID(c.Context(), c.Params("id"), user.OrgId)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": m})
}

func (ctrl *Controller) Create(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	var body map[string]interface{}
	if err := c.BodyParser(&body); err != nil {
		return middleware.NewAppError(400, "VALIDATION_ERROR", "Invalid request body")
	}
	m, err := ctrl.svc.CreateMachine(c.Context(), user.OrgId, body)
	if err != nil {
		return err
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "data": m})
}

func (ctrl *Controller) Update(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	var body map[string]interface{}
	if err := c.BodyParser(&body); err != nil {
		return middleware.NewAppError(400, "VALIDATION_ERROR", "Invalid request body")
	}
	m, err := ctrl.svc.UpdateMachine(c.Context(), c.Params("id"), user.OrgId, body)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": m})
}

func (ctrl *Controller) Delete(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	if err := ctrl.svc.DeleteMachine(c.Context(), c.Params("id"), user.OrgId); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": nil})
}

func (ctrl *Controller) GetFields(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	fields, err := ctrl.svc.GetMachineFields(c.Context(), c.Params("id"), user.OrgId)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": fields})
}

func (ctrl *Controller) UpsertField(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	var f MachineField
	if err := c.BodyParser(&f); err != nil {
		return middleware.NewAppError(400, "VALIDATION_ERROR", "Invalid request body")
	}
	result, err := ctrl.svc.UpsertMachineField(c.Context(), c.Params("id"), user.OrgId, f)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": result})
}

func (ctrl *Controller) GetFactories(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	factories, err := ctrl.svc.GetFactories(c.Context(), user.OrgId)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": factories})
}

func (ctrl *Controller) GetProductionLines(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	lines, err := ctrl.svc.GetProductionLines(c.Context(), user.OrgId)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": lines})
}

// helper — keep compiler happy
var _ = strconv.Itoa
