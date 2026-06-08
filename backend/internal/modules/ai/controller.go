package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"iot-dashboard/internal/config"
	"iot-dashboard/internal/middleware"

	"github.com/gofiber/fiber/v2"
)

const groqModel = "llama-3.3-70b-versatile"
const groqBaseURL = "https://api.groq.com/openai/v1/chat/completions"
const systemPrompt = `You are IotVision AI, an industrial IoT assistant for a factory monitoring platform.

Rules for tool use:
1. For greetings or general questions — answer in plain text, do NOT call any tool.
2. When the user asks to list or describe machines — call getMachines, summarize the results in text, then STOP. Do NOT create a dashboard. Listing machines is NOT a request to build one.
3. Only call create_custom_dashboard when the user's latest message explicitly asks to create, build, set up, or generate a dashboard. In that case FIRST call getMachines to get exact machine names and their metric fields, THEN call create_custom_dashboard using the EXACT machine names from the getMachines result. Never guess or invent machine names.
4. Only call a tool when you are certain which tool to use and have all required inputs. Do exactly what the user asked — never chain extra tool calls beyond the request.

After creating a dashboard, briefly confirm what was built and offer to adjust it.`

// ── Groq / OpenAI-compatible API types ───────────────────────────────────────

type groqMessage struct {
	Role       string         `json:"role"`
	Content    *string        `json:"content"` // pointer so null stays null (tool-call turns)
	ToolCalls  []groqToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type groqToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type groqRequest struct {
	Model    string        `json:"model"`
	Messages []groqMessage `json:"messages"`
	Tools    []map[string]any `json:"tools,omitempty"`
}

type groqResponse struct {
	Choices []struct {
		Message      groqMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
}

// ── Controller ────────────────────────────────────────────────────────────────

type Controller struct {
	action *DashboardAction
	repo   *Repository
}

func NewController() *Controller {
	return &Controller{
		action: NewDashboardAction(),
		repo:   &Repository{},
	}
}

func (ctrl *Controller) dispatch(c *fiber.Ctx, toolName string, rawArgs json.RawMessage) (any, error) {
	user := middleware.GetUser(c)
	switch toolName {
	case "getMachines":
		return getMachinesForOrg(c.Context(), user.OrgId)
	case "create_custom_dashboard":
		return ctrl.action.Handle(c.Context(), user.OrgId, user.Sub, rawArgs)
	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}

// ── Direct tool execute (backward compat / manual testing) ────────────────────

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

func (ctrl *Controller) ListTools(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"success": true, "data": []any{GetMachinesTool, CreateDashboardTool}})
}

// ── Conversations ─────────────────────────────────────────────────────────────

func (ctrl *Controller) GetConversations(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	convs, err := ctrl.repo.ListConversations(c.Context(), user.Sub)
	if err != nil {
		return err
	}
	if convs == nil {
		convs = []Conversation{}
	}
	return c.JSON(fiber.Map{"success": true, "data": convs})
}

func (ctrl *Controller) CreateConversation(c *fiber.Ctx) error {
	var body struct {
		Title string `json:"title"`
	}
	_ = c.BodyParser(&body)
	if body.Title == "" {
		body.Title = "New Conversation"
	}
	user := middleware.GetUser(c)
	conv, err := ctrl.repo.CreateConversation(c.Context(), user.Sub, body.Title)
	if err != nil {
		return err
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "data": conv})
}

func (ctrl *Controller) GetMessages(c *fiber.Ctx) error {
	convID := c.Params("id")
	msgs, err := ctrl.repo.GetMessages(c.Context(), convID)
	if err != nil {
		return err
	}
	if msgs == nil {
		msgs = []Message{}
	}
	return c.JSON(fiber.Map{"success": true, "data": msgs})
}

func (ctrl *Controller) AddMessage(c *fiber.Ctx) error {
	convID := c.Params("id")
	var body struct {
		Role       string          `json:"role"`
		Content    string          `json:"content"`
		ToolName   *string         `json:"toolName"`
		ToolInput  json.RawMessage `json:"toolInput"`
		ToolResult json.RawMessage `json:"toolResult"`
	}
	if err := c.BodyParser(&body); err != nil {
		return middleware.NewAppError(400, "VALIDATION_ERROR", "Invalid request body")
	}
	msg, err := ctrl.repo.AddMessage(c.Context(), convID, body.Role, body.Content, body.ToolName, body.ToolInput, body.ToolResult)
	if err != nil {
		return err
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "data": msg})
}

// ── Chat ──────────────────────────────────────────────────────────────────────

func (ctrl *Controller) Chat(c *fiber.Ctx) error {
	if config.Env.GroqApiKey == "" {
		return middleware.NewAppError(503, "AI_UNAVAILABLE", "GROQ_API_KEY is not configured")
	}

	var body struct {
		ConversationID string `json:"conversationId"`
		Message        string `json:"message"`
	}
	if err := c.BodyParser(&body); err != nil {
		return middleware.NewAppError(400, "VALIDATION_ERROR", "Invalid request body")
	}
	if body.ConversationID == "" || body.Message == "" {
		return middleware.NewAppError(400, "VALIDATION_ERROR", "conversationId and message are required")
	}

	ctx := c.Context()

	// Save user message
	userMsg, err := ctrl.repo.AddMessage(ctx, body.ConversationID, "user", body.Message, nil, nil, nil)
	if err != nil {
		return err
	}
	newMessages := []Message{*userMsg}

	// Load full conversation history
	history, err := ctrl.repo.GetMessages(ctx, body.ConversationID)
	if err != nil {
		return err
	}

	// Build Groq messages: system prompt + conversation history
	sp := systemPrompt
	msgs := []groqMessage{{Role: "system", Content: &sp}}
	msgs = append(msgs, buildGroqMessages(history)...)

	// Agentic tool-use loop (max 5 iterations)
	for i := 0; i < 5; i++ {
		resp, err := callGroq(msgs)
		if err != nil {
			return middleware.NewAppError(502, "AI_ERROR", fmt.Sprintf("Groq API error: %v", err))
		}
		if len(resp.Choices) == 0 {
			return middleware.NewAppError(502, "AI_ERROR", "Groq returned no choices")
		}

		choice := resp.Choices[0]

			if choice.FinishReason != "tool_calls" || len(choice.Message.ToolCalls) == 0 {
			text := ""
			if choice.Message.Content != nil {
				text = *choice.Message.Content
			}
			assistantMsg, err := ctrl.repo.AddMessage(ctx, body.ConversationID, "assistant", text, nil, nil, nil)
			if err != nil {
				return err
			}
			newMessages = append(newMessages, *assistantMsg)
			break
		}

		// Append assistant's tool_calls message to in-memory history
		msgs = append(msgs, choice.Message)

		// Execute each tool call
		for _, tc := range choice.Message.ToolCalls {
			toolInputRaw := json.RawMessage(tc.Function.Arguments)

			result, dispatchErr := ctrl.dispatch(c, tc.Function.Name, toolInputRaw)
			resultJSON, _ := json.Marshal(result)
			if dispatchErr != nil {
				resultJSON, _ = json.Marshal(map[string]any{"error": dispatchErr.Error()})
			}

			// Persist tool message to DB
			tn := tc.Function.Name
			toolMsg, _ := ctrl.repo.AddMessage(ctx, body.ConversationID, "tool",
				"Tool executed: "+tc.Function.Name, &tn,
				toolInputRaw, json.RawMessage(resultJSON))
			if toolMsg != nil {
				newMessages = append(newMessages, *toolMsg)
			}

			// Feed tool result back — OpenAI format: role=tool, tool_call_id, content only
			resultStr := string(resultJSON)
			msgs = append(msgs, groqMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    &resultStr,
			})
		}
	}

	return c.JSON(fiber.Map{"success": true, "data": newMessages})
}

// ── Groq HTTP helpers ─────────────────────────────────────────────────────────

func callGroq(messages []groqMessage) (*groqResponse, error) {
	// Convert Anthropic-format tool schemas → OpenAI function tool format
	toGroqTool := func(t map[string]any) map[string]any {
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t["name"],
				"description": t["description"],
				"parameters":  t["input_schema"],
			},
		}
	}

	reqBody := groqRequest{
		Model:    groqModel,
		Messages: messages,
		Tools: []map[string]any{
			toGroqTool(GetMachinesTool),
			toGroqTool(CreateDashboardTool),
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", groqBaseURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+config.Env.GroqApiKey)

	httpClient := &http.Client{Timeout: 90 * time.Second}
	httpResp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	respBytes, _ := io.ReadAll(httpResp.Body)

	var result groqResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse Groq response: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("Groq API: %s", result.Error.Message)
	}

	return &result, nil
}

func strPtr(s string) *string { return &s }

// buildGroqMessages converts DB messages to Groq/OpenAI format.
func buildGroqMessages(msgs []Message) []groqMessage {
	var result []groqMessage
	for _, m := range msgs {
		switch m.Role {
		case "user":
			result = append(result, groqMessage{Role: "user", Content: strPtr(m.Content)})
		case "assistant":
			result = append(result, groqMessage{Role: "assistant", Content: strPtr(m.Content)})
		}
	}
	return result
}
