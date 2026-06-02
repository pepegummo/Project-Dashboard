package telemetry

import (
	"iot-dashboard/internal/middleware"

	"github.com/gofiber/fiber/v2"
)

func RegisterRoutes(router fiber.Router, broadcaster Broadcaster) {
	ctrl := NewController(broadcaster)

	// Public endpoints (for LED kiosk)
	router.Get("/latest", ctrl.GetLatestMulti)
	router.Get("/:machineId/latest", ctrl.GetLatest)
	router.Get("/:machineId/series", ctrl.GetSeries)
	router.Get("/:machineId/daily-count", ctrl.GetDailyCount)
	router.Get("/:machineId/total-count", ctrl.GetTotalCount)

	// Protected endpoints
	router.Get("/:machineId/aggregate", middleware.Authenticate, ctrl.GetAggregate)
	router.Post("/:machineId/ingest", middleware.Authenticate, ctrl.Ingest)
}
