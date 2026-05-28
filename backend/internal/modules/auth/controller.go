package auth

import (
	"iot-dashboard/internal/middleware"

	"github.com/gofiber/fiber/v2"
)

type loginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=1"`
}

type Controller struct {
	svc *Service
}

func NewController() *Controller {
	return &Controller{svc: NewService()}
}

func (ctrl *Controller) Login(c *fiber.Ctx) error {
	var req loginRequest
	if err := c.BodyParser(&req); err != nil {
		return middleware.NewAppError(400, "VALIDATION_ERROR", "Invalid request body")
	}

	result, err := ctrl.svc.Login(
		c.Context(),
		req.Email,
		req.Password,
		c.IP(),
		c.Get("User-Agent"),
	)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": result})
}

func (ctrl *Controller) GetProfile(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	profile, err := ctrl.svc.GetProfile(c.Context(), user.Sub)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": profile})
}
