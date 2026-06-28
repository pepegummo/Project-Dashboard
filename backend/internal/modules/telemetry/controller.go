package telemetry

import (
	"context"
	"iot-dashboard/internal/middleware"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

type Broadcaster interface {
	BroadcastOne(machineID, machineName, timestamp string, data map[string]interface{})
}

type AlertEvaluator interface {
	EvaluateAndBroadcast(ctx context.Context, machineID, machineName string, data map[string]interface{})
}

type Controller struct {
	svc         *Service
	broadcaster Broadcaster
	alertEval   AlertEvaluator
}

func NewController(b Broadcaster, ae AlertEvaluator) *Controller {
	return &Controller{svc: NewService(), broadcaster: b, alertEval: ae}
}

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
	field := c.Query("field")
	timeRange := c.Query("timeRange", "1h")
	startTime := c.Query("startTime", "")
	endTime := c.Query("endTime", "")
	result, err := ctrl.svc.GetSeries(c.Context(), c.Params("machineId"), field, timeRange, startTime, endTime, nil)
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
	days, _ := strconv.Atoi(c.Query("days", "7"))
	result, err := ctrl.svc.GetDailyCount(c.Context(), c.Params("machineId"), days, nil)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": result})
}

func (ctrl *Controller) GetHourlyCount(c *fiber.Ctx) error {
	hours, _ := strconv.Atoi(c.Query("hours", "8"))
	result, err := ctrl.svc.GetHourlyCount(c.Context(), c.Params("machineId"), hours, nil)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": result})
}

func (ctrl *Controller) GetTotalCount(c *fiber.Ctx) error {
	result, err := ctrl.svc.GetTotalCount(c.Context(), c.Params("machineId"), nil)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": result})
}

func (ctrl *Controller) GetCount(c *fiber.Ctx) error {
	sku := c.Query("sku")
	status := c.Query("status", "all")
	bucket := c.Query("bucket", "1h")
	points, _ := strconv.Atoi(c.Query("points", "48"))
	result, err := ctrl.svc.GetBucketCount(c.Context(), c.Params("machineId"), sku, status, bucket, points, nil)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": result})
}

func (ctrl *Controller) GetSkus(c *fiber.Ctx) error {
	result, err := ctrl.svc.GetMachineSkus(c.Context(), c.Params("machineId"), nil)
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
	machineID := c.Params("machineId")
	result, err := ctrl.svc.Ingest(c.Context(), machineID, body, user.OrgId)
	if err != nil {
		return err
	}
	// Broadcast immediately so connected clients see the new value without waiting for the next poll
	if ctrl.broadcaster != nil {
		ts, _ := result["timestamp"].(time.Time)
		ctrl.broadcaster.BroadcastOne(machineID, "", ts.UTC().Format(time.RFC3339), body)
	}
	// Evaluate alert rules against the ingested data
	if ctrl.alertEval != nil {
		ctrl.alertEval.EvaluateAndBroadcast(c.Context(), machineID, "", body)
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "data": result})
}
