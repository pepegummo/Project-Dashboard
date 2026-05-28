package telemetry

import (
	"iot-dashboard/internal/middleware"

	"github.com/gofiber/fiber/v2"
)

func RegisterRoutes(router fiber.Router) {
	ctrl := NewController()

	// Public endpoints (for LED kiosk)
	router.Get("/latest", ctrl.GetLatestMulti)
	router.Get("/:machineId/latest", ctrl.GetLatest)

	// Protected endpoints
	router.Get("/:machineId/series", middleware.Authenticate, ctrl.GetSeries)
	router.Get("/:machineId/aggregate", middleware.Authenticate, ctrl.GetAggregate)
	router.Get("/:machineId/daily-count", middleware.Authenticate, ctrl.GetDailyCount)
	router.Post("/:machineId/ingest", middleware.Authenticate, ctrl.Ingest)
}
