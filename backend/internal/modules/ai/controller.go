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
3. READ vs WRITE — this is critical. Only call a tool that creates, adds, removes, or changes something (create_custom_dashboard, add_widget_to_dashboard, remove_widget, create_alert, acknowledge_alert, resolve_alert) when the user EXPLICITLY asks for that action using words like "create", "build", "make", "add", "remove", "delete", or "set up an alert". Requests to "list", "show", "get", "display", "what is", "how many", "which", or to check status are READ-ONLY: answer them using only read tools (get_machines, get_latest_telemetry, get_telemetry_trend, get_daily_count, get_active_alerts, get_factory_overview) plus plain text. When in doubt, READ and answer — never create or modify anything.
4. "List all machines", "show me the machines", or "what's the status" means call get_machines and reply in text. It is NEVER a request to build a dashboard.
5. Always use a machine's human name (from get_machines / get_factory_overview) and a dashboard's exact name (from list_dashboards) in tool arguments. Never invent or guess machine names, metric keys, or dashboard names — if a name you were given does not exist in get_machines, tell the user it was not found instead of acting on a guess.
6. After any change, briefly confirm what you did in plain text.

Reading data:
- get_machines: list machines, their status and metric fields.
- get_latest_telemetry: the current value(s) of one machine.
- get_telemetry_trend: avg/min/max of a metric over a time range (e.g. 1h, 24h, 7d).
- get_daily_count: production counts per day.
- get_active_alerts: currently open alerts (each has an event id).
- get_factory_overview: a snapshot of every machine (status, latest values, open-alert count). Use this for "summarize the factory" / "what's wrong" questions, then reason over the result in text.

Making changes (these require permission; if a tool returns a permission error, tell the user their role cannot do it):
- create_custom_dashboard: build a NEW dashboard — ONLY when the user explicitly says to create/build/make a dashboard. First call get_machines for exact names and to confirm the machine exists. Listing, showing, or asking about machines is NOT a request to build one.
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
	Model    string           `json:"model"`
	Messages []groqMessage    `json:"messages"`
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

// ConfirmAction executes (or cancels) a pending write tool that the AI proposed.
// The pending action is stored on an ai_messages row; this flips it to executed
// or cancelled, so re-confirming the same message is rejected (no double action).
func (ctrl *Controller) ConfirmAction(c *fiber.Ctx) error {
	var body struct {
		MessageID string `json:"messageId"`
		Confirm   bool   `json:"confirm"`
	}
	if err := c.BodyParser(&body); err != nil || body.MessageID == "" {
		return middleware.NewAppError(400, "VALIDATION_ERROR", "messageId is required")
	}

	user := middleware.GetUser(c)
	msg, err := ctrl.repo.GetMessageByID(c.Context(), body.MessageID, user.Sub)
	if err != nil {
		return middleware.NewAppError(404, "NOT_FOUND", "Pending action not found")
	}

	var pend struct {
		Status   string          `json:"status"`
		ToolName string          `json:"toolName"`
		Params   json.RawMessage `json:"params"`
		Summary  string          `json:"summary"`
	}
	_ = json.Unmarshal(msg.ToolResult, &pend)
	if pend.Status != "pending_confirmation" {
		return middleware.NewAppError(409, "ALREADY_HANDLED", "This action was already handled")
	}

	// Cancelled by the user — record it, run nothing.
	if !body.Confirm {
		res, _ := json.Marshal(map[string]any{"status": "cancelled", "toolName": pend.ToolName, "summary": pend.Summary})
		updated, uerr := ctrl.repo.UpdateMessageResult(c.Context(), msg.ID, "Cancelled: "+pend.Summary, res)
		if uerr != nil {
			return uerr
		}
		return c.JSON(fiber.Map{"success": true, "data": []Message{*updated}})
	}

	// Confirmed — execute for real (RBAC + org-scoping enforced inside dispatch).
	result, dispatchErr := ctrl.dispatch(c, pend.ToolName, pend.Params)
	if dispatchErr != nil {
		return dispatchErr
	}
	resultJSON, _ := json.Marshal(result)
	updated, uerr := ctrl.repo.UpdateMessageResult(c.Context(), msg.ID, "Executed: "+pend.Summary, resultJSON)
	if uerr != nil {
		return uerr
	}
	return c.JSON(fiber.Map{"success": true, "data": []Message{*updated}})
}

// writeIntentRe matches a user message that is actually asking to change
// something (English + Thai). When it does NOT match, write tools are withheld
// from the model entirely, so it cannot propose a mutation on a read request.
var writeIntentRe = regexp.MustCompile(`(?i)\b(create|build|make|add|set ?up|configure|new dashboard|remove|delete|drop|alert me|notify me|acknowledge|resolve|rename|change|update|edit)\b|สร้าง|เพิ่ม|ทำ|ลบ|เอาออก|ตั้งค่า|ตั้งเตือน|แจ้งเตือน|เปลี่ยน|แก้`)

// selectTools returns the tool catalog handed to the LLM for this turn. Read
// tools are always available; write (mutating) tools only when the user's
// message shows write intent.
func selectTools(userMsg string) []map[string]any {
	all := AllTools()
	if writeIntentRe.MatchString(userMsg) {
		return all
	}
	out := make([]map[string]any, 0, len(all))
	for _, t := range all {
		if name, _ := t["name"].(string); !writeTools[name] {
			out = append(out, t)
		}
	}
	return out
}

// confirmSummary builds a short human-readable description of a pending write
// action, shown to the user on the Confirm/Cancel card.
func confirmSummary(toolName string, raw json.RawMessage) string {
	var a map[string]any
	_ = json.Unmarshal(raw, &a)
	s := func(k string) string { v, _ := a[k].(string); return v }
	switch toolName {
	case "create_custom_dashboard":
		n := 0
		if ws, ok := a["widgets"].([]any); ok {
			n = len(ws)
		}
		return fmt.Sprintf("Create dashboard %q with %d widget(s)", s("dashboard_name"), n)
	case "add_widget_to_dashboard":
		wt := ""
		if w, ok := a["widget"].(map[string]any); ok {
			wt, _ = w["type"].(string)
		}
		return fmt.Sprintf("Add a %s widget to %q", wt, s("dashboard_name"))
	case "remove_widget":
		return fmt.Sprintf("Remove widget %q from %q", s("widget_title"), s("dashboard_name"))
	case "create_alert":
		return fmt.Sprintf("Create an alert on %s: %s %s %v", s("machine_id"), s("metric"), s("condition"), a["threshold"])
	case "acknowledge_alert":
		return fmt.Sprintf("Acknowledge alert %s", s("event_id"))
	case "resolve_alert":
		return fmt.Sprintf("Resolve alert %s", s("event_id"))
	}
	return "Perform " + toolName
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

	// Build Groq messages: system prompt + conversation history.
	// Window the history to the most recent turns so token usage stays bounded
	// on long conversations — Groq's free-tier TPM budget is small and we resend
	// the full system prompt + tool catalog on every call.
	sp := systemPrompt
	msgs := []groqMessage{{Role: "system", Content: &sp}}
	conv := buildGroqMessages(history)
	const maxHistoryTurns = 10
	if len(conv) > maxHistoryTurns {
		conv = conv[len(conv)-maxHistoryTurns:]
	}
	msgs = append(msgs, conv...)

	// Expose write tools only when the user's message actually asks to change
	// something. If create/add/remove tools are not in the catalog we hand the
	// model, it physically cannot propose a mutation on a read-only request —
	// far more reliable than asking it not to via the system prompt.
	toolDefs := selectTools(body.Message)

	// Agentic tool-use loop (max 4 iterations — real requests rarely need
	// more than 2 sequential tool calls before a final answer).
	for i := 0; i < 4; i++ {
		resp, err := callGroq(msgs, toolDefs)
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

			// Mutating tools require explicit user confirmation. Instead of running
			// them, persist a pending action the frontend renders with Confirm/Cancel
			// buttons, then stop — the user drives execution via POST /api/ai/confirm.
			if isWriteTool(tc.Function.Name) {
				summary := confirmSummary(tc.Function.Name, toolInputRaw)
				pending, _ := json.Marshal(map[string]any{
					"status":   "pending_confirmation",
					"toolName": tc.Function.Name,
					"params":   toolInputRaw,
					"summary":  summary,
				})
				tn := tc.Function.Name
				pendingMsg, _ := ctrl.repo.AddMessage(ctx, body.ConversationID, "tool",
					"Awaiting confirmation: "+summary, &tn, toolInputRaw, json.RawMessage(pending))
				if pendingMsg != nil {
					newMessages = append(newMessages, *pendingMsg)
				}
				return c.JSON(fiber.Map{"success": true, "data": newMessages})
			}

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

func callGroq(messages []groqMessage, toolDefs []map[string]any) (*groqResponse, error) {
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

	tools := make([]map[string]any, 0, len(toolDefs))
	for _, t := range toolDefs {
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
