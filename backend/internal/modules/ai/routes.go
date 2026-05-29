package ai

import (
	"iot-dashboard/internal/middleware"

	"github.com/gofiber/fiber/v2"
)

func RegisterRoutes(router fiber.Router) {
	ctrl := NewController()
	router.Use(middleware.Authenticate)

	router.Get("/tools", ctrl.ListTools)
	router.Post("/tools/execute", ctrl.ExecuteTool)
	// router.Post("/chat", ctrl.Chat)  // ← LLM loop endpoint (Step 3)
}
