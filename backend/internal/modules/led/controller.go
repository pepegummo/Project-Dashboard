package led

import (
	"iot-dashboard/internal/config"
	"iot-dashboard/internal/database"
	"iot-dashboard/internal/middleware"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

// GenerateToken creates a permanent LED-viewer JWT for the org and stores it.
func GenerateToken(c *fiber.Ctx) error {
	user := middleware.GetUser(c)

	claims := middleware.JwtClaims{
		Sub:   user.OrgId,
		OrgId: user.OrgId,
		Role:  "led-viewer",
		// No ExpiresAt — this token never expires
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(config.Env.JwtSecret))
	if err != nil {
		return middleware.NewAppError(500, "TOKEN_ERROR", "Failed to generate LED token")
	}

	_, err = database.Pool.Exec(c.Context(),
		`UPDATE organizations SET led_token = $1 WHERE id = $2`,
		tokenStr, user.OrgId,
	)
	if err != nil {
		return middleware.NewAppError(500, "DB_ERROR", "Failed to store LED token")
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    fiber.Map{"token": tokenStr},
	})
}

// GetToken returns the current LED token for the org (nil if not yet generated).
func GetToken(c *fiber.Ctx) error {
	user := middleware.GetUser(c)

	var token *string
	err := database.Pool.QueryRow(c.Context(),
		`SELECT led_token FROM organizations WHERE id = $1`,
		user.OrgId,
	).Scan(&token)
	if err != nil || token == nil {
		return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"token": nil}})
	}

	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"token": *token}})
}

// RevokeToken clears the LED token — any existing kiosk URLs will stop working.
func RevokeToken(c *fiber.Ctx) error {
	user := middleware.GetUser(c)

	_, err := database.Pool.Exec(c.Context(),
		`UPDATE organizations SET led_token = NULL WHERE id = $1`,
		user.OrgId,
	)
	if err != nil {
		return middleware.NewAppError(500, "DB_ERROR", "Failed to revoke LED token")
	}

	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"revoked": true}})
}
