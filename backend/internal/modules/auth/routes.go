package auth

import (
	"iot-dashboard/internal/middleware"

	"github.com/gofiber/fiber/v2"
)

func RegisterRoutes(router fiber.Router) {
	ctrl := NewController()

	router.Post("/login", ctrl.Login)
	router.Post("/switch-org", middleware.Authenticate, ctrl.SwitchOrg)
	router.Get("/me", middleware.Authenticate, ctrl.GetProfile)
}
