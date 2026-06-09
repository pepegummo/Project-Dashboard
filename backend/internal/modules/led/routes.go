package led

import (
	"iot-dashboard/internal/middleware"

	"github.com/gofiber/fiber/v2"
)

func RegisterRoutes(r fiber.Router) {
	r.Use(middleware.Authenticate)
	r.Use(middleware.RequireRole("admin", "editor"))
	r.Post("/token", GenerateToken)
	r.Get("/token", GetToken)
	r.Delete("/token", RevokeToken)
}
