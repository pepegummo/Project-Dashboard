package middleware

import (
	"fmt"
	"iot-dashboard/internal/config"

	"github.com/gofiber/fiber/v2"
)

// AppError mirrors the TypeScript AppError class.
type AppError struct {
	StatusCode int
	Code       string
	Message    string
	Details    interface{}
}

func (e *AppError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func NewAppError(statusCode int, code, message string) *AppError {
	return &AppError{StatusCode: statusCode, Code: code, Message: message}
}

// ErrorHandler is the Fiber global error handler.
func ErrorHandler(c *fiber.Ctx, err error) error {
	fmt.Printf("[ERROR] %s %s: %v\n", c.Method(), c.Path(), err)

	if appErr, ok := err.(*AppError); ok {
		return c.Status(appErr.StatusCode).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    appErr.Code,
				"message": appErr.Message,
				"details": appErr.Details,
			},
		})
	}

	// Fiber's built-in errors (404, etc.)
	if fiberErr, ok := err.(*fiber.Error); ok {
		return c.Status(fiberErr.Code).JSON(fiber.Map{
			"success": false,
			"error":   fiber.Map{"code": "ERROR", "message": fiberErr.Message},
		})
	}

	// Generic 500
	resp := fiber.Map{
		"success": false,
		"error": fiber.Map{
			"code":    "INTERNAL_ERROR",
			"message": "Internal server error",
		},
	}
	if config.Env.IsDev() {
		resp["error"] = fiber.Map{
			"code":    "INTERNAL_ERROR",
			"message": "Internal server error",
			"details": err.Error(),
		}
	}
	return c.Status(500).JSON(resp)
}
