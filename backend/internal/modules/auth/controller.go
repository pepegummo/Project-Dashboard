package auth

import (
	"strings"

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

	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" || req.Password == "" {
		return middleware.NewAppError(400, "VALIDATION_ERROR", "Email and password are required")
	}
	if !strings.Contains(req.Email, "@") {
		return middleware.NewAppError(400, "VALIDATION_ERROR", "Invalid email format")
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

func (ctrl *Controller) SwitchOrg(c *fiber.Ctx) error {
	var req struct {
		OrganizationID string `json:"organizationId"`
	}
	if err := c.BodyParser(&req); err != nil || req.OrganizationID == "" {
		return middleware.NewAppError(400, "VALIDATION_ERROR", "organizationId is required")
	}
	user := middleware.GetUser(c)
	token, err := ctrl.svc.SwitchOrg(c.Context(), user.Sub, user.Role, user.Email, req.OrganizationID)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"token": token}})
}

func (ctrl *Controller) GetProfile(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	profile, err := ctrl.svc.GetProfile(c.Context(), user.Sub)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": profile})
}
