package alerts

import (
	"iot-dashboard/internal/middleware"

	"github.com/gofiber/fiber/v2"
)

func RegisterRoutes(router fiber.Router) {
	ctrl := NewController()

	// Public — LED kiosk
	router.Get("/events/active", ctrl.GetActiveEvents)

	// Protected
	router.Get("/", middleware.Authenticate, ctrl.List)
	router.Get("/:id", middleware.Authenticate, ctrl.GetByID)
	router.Post("/", middleware.Authenticate, middleware.RequireRole("admin", "editor"), ctrl.Create)
	router.Patch("/:id", middleware.Authenticate, middleware.RequireRole("admin", "editor"), ctrl.Update)
	router.Delete("/:id", middleware.Authenticate, middleware.RequireRole("admin"), ctrl.Delete)

	router.Patch("/events/:eventId/acknowledge", middleware.Authenticate, ctrl.AcknowledgeEvent)
	router.Patch("/events/:eventId/resolve", middleware.Authenticate, ctrl.ResolveEvent)
}
