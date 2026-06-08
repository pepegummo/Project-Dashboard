package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"iot-dashboard/internal/config"
	"iot-dashboard/internal/middleware"

	"github.com/gofiber/fiber/v2"
)

const groqModel = "llama-3.3-70b-versatile"
const groqBaseURL = "https://api.groq.com/openai/v1/chat/completions"
const systemPrompt = `You are IotVision AI, an assistant for an industrial IoT factory-monitoring platform. You can read live data and make changes on the user's behalf using tools.

General rules:
1. For greetings or general questions, answer in plain text — do NOT call a tool.
2. Call a tool only when the request clearly needs it, and only with inputs you are sure of. Do exactly what was asked; never chain extra actions beyond the request.
3. Always use a machine's human name (from get_machines / get_factory_overview) and a dashboard's exact name (from list_dashboards) in tool arguments. Never invent or guess machine names, metric keys, or dashboard names — if unsure, call get_machines or list_dashboards first.
4. After any change, briefly confirm what you did in plain text.

Reading data:
- get_machines: list machines, their status and metric fields.
- get_latest_telemetry: the current value(s) of one machine.
- get_telemetry_trend: avg/min/max of a metric over a time range (e.g. 1h, 24h, 7d).
- get_daily_count: production counts per day.
- get_active_alerts: currently open alerts (each has an event id).
- get_factory_overview: a snapshot of every machine (status, latest values, open-alert count). Use this for "summarize the factory" / "what's wrong" questions, then reason over the result in text.

Making changes (these require permission; if a tool returns a permission error, tell the user their role cannot do it):
- create_custom_dashboard: build a NEW dashboard. First call get_machines for exact names. Listing machines is NOT a request to build one.
- add_widget_to_dashboard / remove_widget: modify an EXISTING dashboard (call list_dashboards first if unsure of the name).
- create_alert: define a threshold alert rule. condition is one of gt, lt, gte, lte, eq, neq, between, outside; for between/outside also provide threshold_hi.
- acknowledge_alert / resolve_alert: act on an open alert — first call get_active_alerts to get its event_id.`

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
	tk     *ToolKit
	repo   *Repository
}

func NewController() *Controller {
	return &Controller{
		action: NewDashboardAction(),
		tk:     NewToolKit(),
		repo:   &Repository{},
	}
}

func (ctrl *Controller) dispatch(c *fiber.Ctx, toolName string, rawArgs json.RawMessage) (any, error) {
	user := middleware.GetUser(c)
	ctx := c.Context()

	// Mutating tools require admin or editor — viewers can only read.
	if isWriteTool(toolName) && user.Role != "admin" && user.Role != "editor" {
		return nil, fmt.Errorf("permission denied: role %q cannot perform %q (requires admin or editor)", user.Role, toolName)
	}

	switch toolName {
	// ── Category A: read ──────────────────────────────────────────────
	case "get_machines":
		return getMachinesForOrg(ctx, user.OrgId)
	case "get_latest_telemetry":
		return ctrl.tk.GetLatestTelemetry(ctx, user.OrgId, rawArgs)
	case "get_telemetry_trend":
		return ctrl.tk.GetTelemetryTrend(ctx, user.OrgId, rawArgs)
	case "get_active_alerts":
		return ctrl.tk.GetActiveAlerts(ctx, user.OrgId)
	case "get_daily_count":
		return ctrl.tk.GetDailyCount(ctx, user.OrgId, rawArgs)
	// ── Category D: analysis ──────────────────────────────────────────
	case "get_factory_overview":
		return ctrl.tk.GetFactoryOverview(ctx, user.OrgId)
	// ── Category B: dashboards ────────────────────────────────────────
	case "list_dashboards":
		return ctrl.tk.ListDashboards(ctx, user.OrgId, user.Sub)
	case "create_custom_dashboard":
		return ctrl.action.Handle(ctx, user.OrgId, user.Sub, rawArgs)
	case "add_widget_to_dashboard":
		return ctrl.tk.AddWidget(ctx, user.OrgId, rawArgs)
	case "remove_widget":
		return ctrl.tk.RemoveWidget(ctx, user.OrgId, rawArgs)
	// ── Category C: alerts ────────────────────────────────────────────
	case "create_alert":
		return ctrl.tk.CreateAlert(ctx, user.OrgId, rawArgs)
	case "acknowledge_alert":
		return ctrl.tk.AckAlert(ctx, user.Sub, rawArgs)
	case "resolve_alert":
		return ctrl.tk.ResolveAlert(ctx, user.Sub, rawArgs)
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
	return c.JSON(fiber.Map{"success": true, "data": AllTools()})
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

	tools := make([]map[string]any, 0, len(AllTools()))
	for _, t := range AllTools() {
		tools = append(tools, toGroqTool(t))
	}

	reqBody := groqRequest{
		Model:    groqModel,
		Messages: messages,
		Tools:    tools,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	httpClient := &http.Client{Timeout: 90 * time.Second}

	// Retry on rate limit (HTTP 429). Groq's free tier has a low tokens-per-minute
	// budget and we send the full tool catalog each call, so transient 429s happen;
	// we honor the "try again in Ns" hint, capped so the request stays responsive.
	const maxAttempts = 3
	lastErr := fmt.Errorf("the AI service is busy (rate limit). Please wait a few seconds and try again")

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequest("POST", groqBaseURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+config.Env.GroqApiKey)

		httpResp, err := httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		respBytes, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()

		if httpResp.StatusCode == http.StatusTooManyRequests {
			if attempt < maxAttempts {
				time.Sleep(parseRetryAfter(httpResp.Header.Get("Retry-After"), respBytes))
			}
			continue
		}

		var result groqResponse
		if err := json.Unmarshal(respBytes, &result); err != nil {
			return nil, fmt.Errorf("failed to parse Groq response: %w", err)
		}
		if result.Error != nil {
			return nil, fmt.Errorf("Groq API: %s", result.Error.Message)
		}
		return &result, nil
	}

	return nil, lastErr
}

var retryHintRe = regexp.MustCompile(`try again in ([0-9.]+)s`)

// parseRetryAfter derives how long to wait before retrying, from the Retry-After
// header or the "try again in 7.6s" hint in Groq's error body. Capped at 8s.
func parseRetryAfter(header string, body []byte) time.Duration {
	const maxWait = 8 * time.Second

	if header != "" {
		if secs, err := strconv.ParseFloat(strings.TrimSpace(header), 64); err == nil && secs > 0 {
			if d := time.Duration(secs * float64(time.Second)); d <= maxWait {
				return d
			}
			return maxWait
		}
	}
	if m := retryHintRe.FindSubmatch(body); m != nil {
		if secs, err := strconv.ParseFloat(string(m[1]), 64); err == nil && secs > 0 {
			if d := time.Duration((secs + 0.3) * float64(time.Second)); d <= maxWait {
				return d
			}
			return maxWait
		}
	}
	return 2 * time.Second
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
