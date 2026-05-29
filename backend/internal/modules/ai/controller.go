package ai

import (
	"encoding/json"

	"iot-dashboard/internal/middleware"

	"github.com/gofiber/fiber/v2"
)

type Controller struct {
	action *DashboardAction
}

func NewController() *Controller {
	return &Controller{action: NewDashboardAction()}
}

// dispatch routes a single tool call to its action. The LLM loop (Step 3) will
// call this for each tool_use block; for now it backs the direct execute endpoint.
func (ctrl *Controller) dispatch(c *fiber.Ctx, toolName string, rawArgs json.RawMessage) (any, error) {
	user := middleware.GetUser(c)
	switch toolName {
	case "create_custom_dashboard":
		return ctrl.action.Handle(c.Context(), user.OrgId, user.Sub, rawArgs)
	default:
		return nil, middleware.NewAppError(400, "UNKNOWN_TOOL", "Unknown tool: "+toolName)
	}
}

// ExecuteTool — POST /api/ai/tools/execute  { toolName, params }
// Matches the existing api.service.ts signature and lets us test the full
// create → render flow before the LLM is wired up.
func (ctrl *Controller) ExecuteTool(c *fiber.Ctx) error {
	var body struct {
		ToolName string          `json:"toolName"`
		Params   json.RawMessage `json:"params"`
	}
	if err := c.BodyParser(&body); err != nil {
		return middleware.NewAppError(400, "VALIDATION_ERROR", "Invalid request body")
	}
	if len(body.Params) == 0 {
		body.Params = json.RawMessage("{}")
	}
	result, err := ctrl.dispatch(c, body.ToolName, body.Params)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true, "data": result})
}

// ListTools — GET /api/ai/tools  exposes the schema for the LLM client / UI.
func (ctrl *Controller) ListTools(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"success": true, "data": []any{CreateDashboardTool}})
}
