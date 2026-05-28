package telemetry

import (
	"iot-dashboard/internal/middleware"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

type Controller struct {
	svc *Service
}

func NewController() *Controller { return &Controller{svc: NewService()} }

func (ctrl *Controller) GetLatestMulti(c *fiber.Ctx) error {
	idsParam := c.Query("ids")
	var ids []string
	if idsParam != "" {
		ids = strings.Split(idsParam, ",")
	}
	// Public endpoint — no org check
	result, err := ctrl.svc.GetMultiMachineLatest(c.Context(), ids, nil)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": result})
}

func (ctrl *Controller) GetLatest(c *fiber.Ctx) error {
	machineID := c.Params("machineId")
	result, err := ctrl.svc.GetLatest(c.Context(), machineID, nil) // public endpoint
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": result})
}

func (ctrl *Controller) GetSeries(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	field := c.Query("field")
	timeRange := c.Query("timeRange", "1h")
	startTime := c.Query("startTime", "")
	endTime := c.Query("endTime", "")
	result, err := ctrl.svc.GetSeries(c.Context(), c.Params("machineId"), field, timeRange, startTime, endTime, user.OrgId)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": result})
}

func (ctrl *Controller) GetAggregate(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	field := c.Query("field")
	period := c.Query("period", "1h")
	result, err := ctrl.svc.GetAggregate(c.Context(), c.Params("machineId"), field, period, user.OrgId)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": result})
}

func (ctrl *Controller) GetDailyCount(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	days, _ := strconv.Atoi(c.Query("days", "7"))
	result, err := ctrl.svc.GetDailyCount(c.Context(), c.Params("machineId"), days, user.OrgId)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": result})
}

func (ctrl *Controller) Ingest(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	var body map[string]interface{}
	if err := c.BodyParser(&body); err != nil {
		return middleware.NewAppError(400, "VALIDATION_ERROR", "Invalid request body")
	}
	result, err := ctrl.svc.Ingest(c.Context(), c.Params("machineId"), body, user.OrgId)
	if err != nil {
		return err
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "data": result})
}
