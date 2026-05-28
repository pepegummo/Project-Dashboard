package middleware

import (
	"iot-dashboard/internal/config"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

// JwtClaims mirrors the TypeScript JwtPayload
type JwtClaims struct {
	Sub   string `json:"sub"`
	OrgId string `json:"orgId"`
	Role  string `json:"role"`
	Email string `json:"email"`
	jwt.RegisteredClaims
}

// Authenticate validates Bearer JWT and sets "user" local.
func Authenticate(c *fiber.Ctx) error {
	authHeader := c.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return c.Status(401).JSON(fiber.Map{
			"success": false,
			"error":   fiber.Map{"code": "UNAUTHORIZED", "message": "Missing or invalid authorization header"},
		})
	}

	tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
	claims := &JwtClaims{}

	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fiber.ErrUnauthorized
		}
		return []byte(config.Env.JwtSecret), nil
	})

	if err != nil || !token.Valid {
		msg := "Invalid token"
		if strings.Contains(err.Error(), "expired") {
			msg = "Token expired"
		}
		return c.Status(401).JSON(fiber.Map{
			"success": false,
			"error":   fiber.Map{"code": "UNAUTHORIZED", "message": msg},
		})
	}

	c.Locals("user", claims)
	return c.Next()
}

// RequireRole returns middleware that checks user role.
func RequireRole(roles ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("user").(*JwtClaims)
		if !ok || claims == nil {
			return c.Status(403).JSON(fiber.Map{
				"success": false,
				"error":   fiber.Map{"code": "FORBIDDEN", "message": "Insufficient permissions"},
			})
		}
		for _, r := range roles {
			if claims.Role == r {
				return c.Next()
			}
		}
		return c.Status(403).JSON(fiber.Map{
			"success": false,
			"error":   fiber.Map{"code": "FORBIDDEN", "message": "Insufficient permissions"},
		})
	}
}

// GetUser is a helper to extract claims from context.
func GetUser(c *fiber.Ctx) *JwtClaims {
	if claims, ok := c.Locals("user").(*JwtClaims); ok {
		return claims
	}
	return nil
}
