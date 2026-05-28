package machines

import (
	"iot-dashboard/internal/middleware"

	"github.com/gofiber/fiber/v2"
)

func RegisterRoutes(router fiber.Router) {
	ctrl := NewController()

	router.Use(middleware.Authenticate)

	router.Get("/factories", ctrl.GetFactories)
	router.Get("/production-lines", ctrl.GetProductionLines)

	router.Get("/", ctrl.List)
	router.Get("/:id", ctrl.GetByID)
	router.Post("/", middleware.RequireRole("admin", "editor"), ctrl.Create)
	router.Patch("/:id", middleware.RequireRole("admin", "editor"), ctrl.Update)
	router.Delete("/:id", middleware.RequireRole("admin"), ctrl.Delete)

	router.Get("/:id/fields", ctrl.GetFields)
	router.Put("/:id/fields", middleware.RequireRole("admin", "editor"), ctrl.UpsertField)
}
