package dashboards

import (
	"iot-dashboard/internal/middleware"

	"github.com/gofiber/fiber/v2"
)

func RegisterRoutes(router fiber.Router) {
	ctrl := NewController()
	router.Use(middleware.Authenticate)

	router.Get("/", ctrl.List)
	router.Get("/:id", ctrl.GetByID)
	router.Post("/", ctrl.Create)
	router.Patch("/:id", ctrl.Update)
	router.Delete("/:id", ctrl.Delete)

	router.Post("/:id/widgets", ctrl.AddWidget)
	router.Patch("/:id/layout", ctrl.BulkUpdateLayout)
	router.Patch("/:id/widgets/:widgetId", ctrl.UpdateWidget)
	router.Delete("/:id/widgets/:widgetId", ctrl.DeleteWidget)
}
