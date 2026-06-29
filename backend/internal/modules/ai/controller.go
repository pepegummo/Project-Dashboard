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

// gpt-oss-20b: chosen via the Phase 1 bake-off (see eval_test.go) — best Thai
// intent understanding (11/11, beat 120b and qwen), no language leaks, cheapest, and
// Groq prompt-caches it (the stable system+tools prefix is discounted and off the rate limit).
const groqModel = "openai/gpt-oss-20b"
const groqBaseURL = "https://api.groq.com/openai/v1/chat/completions"
const systemPrompt = `You are IotVision AI for an industrial IoT platform. Use tools to read data and make changes.

Rules:
1. Plain text for greetings/general questions — no tool call.
2. Use exact machine names and dashboard names. If the user mentions a name directly (e.g. "CW-01"), use it as-is — only call get_machines or list_dashboards when the name is ambiguous or unknown.
3. Do only what was asked; no extra chained actions.
4. After any change confirm briefly in plain text. After show_metric returns, reply with one short natural-language sentence (e.g. "นี่คือ speed ของ CW-01 ครับ") — NEVER output the raw JSON or the tool result object as your reply. NEVER output phrases like "Here is X", "นี่คือ X", "The X of CW-01 is Y" unless show_metric was actually called in this same turn — calling the tool is mandatory, not optional.
5. preview_add_widget, preview_remove_widget and preview_update_widget are ONLY for a new dashboard being composed this turn (after preview_dashboard was called and not yet confirmed). To edit a widget already in the preview (e.g. change its title or metric), use preview_update_widget with the widget's current title. For count/production widgets, always use type "daily-count" (NEVER use "count" or "counter"). For an existing dashboard the user NAMES use add_widget_to_dashboard / remove_widget directly; if no dashboard is named, prefer show_metric (rule 11) instead of asking which dashboard.
6. preview_dashboard: pick machine_overview for general/status, machine_production for output/count, machine_maintenance for health/alerts. If the user just says "create a dashboard" without a type, default to machine_overview and show the preview — the preview IS the confirmation step, so do NOT ask which template. The user confirms via button — do not ask them to type confirm.
7. If the answer is already in the provided dashboard context AND the user is asking a structural question (widget count, dashboard name, which widgets exist, layout), answer from it directly. For metric value questions ("what is X", "show me X", "speed of CW-01"), ALWAYS call show_metric — context values are never a substitute for a live widget.
8. Line/trend chart widgets support an absolute date range. To change it, call preview_update_widget with start_date/end_date as YYYY-MM-DD (convert any DD/MM/YYYY the user gives). Never claim only preset ranges (5m, 1h, 7d, …) are supported.
9. Reply entirely in the same language as the user's latest message. Never mix languages within a reply.
10. Ask a clarifying question only when you cannot act: no clear action (e.g. "fix it", "change it"), or a read/alert needs a machine and none is identifiable. When a machine is missing, ask which machine in ONE short question — never guess a name, and do not call get_machines just to list them back. When a sensible default exists (e.g. a dashboard preview per rule 6), use it instead of asking; never ask which dashboard template to use.
11. When the user asks to see, show, or ADD a widget for a specific metric (e.g. "add weight widget", "ขอ widget weight CW-01", "ความเร็ว CW-01 เท่าไหร่", "what is the speed of CW-01"), you MUST call show_metric — you have NO access to live sensor values without this tool, so you literally cannot answer without calling it. Do not output "Here is X" or "นี่คือ X" without a preceding show_metric call this turn. show_metric displays the widget in a preview card the user can add themselves, so NEVER ask which dashboard. Map the user's word in any language to the field key. Use viz:"trend" for history/trend questions, viz:"gauge" or "value" for a current reading. Use add_widget_to_dashboard instead ONLY when the user names a specific existing dashboard to add to (e.g. "add a weight widget to CW-01 Overview"); use preview_add_widget only while composing a preview dashboard this turn (rule 5). When the user asks to see ALL metrics of a machine, call get_machines first to list the fields, then call show_metric once for EACH field — never just list them as plain text.
12. When the user's message contains one or more @Widget Title tokens (at-mentions appended by the UI when they click a widget), those identify the exact widget(s) being referenced. For preview_update_widget use the @-mentioned title verbatim as widget_title — NEVER ask the user to name the widget if one is @-mentioned. For data/show queries, treat the @-mentioned widget's machine and metric as the subject.`

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
	case "get_machines":
		return getMachinesForOrg(ctx, user.OrgId)
	case "show_metric":
		return ctrl.tk.ShowMetric(ctx, user.OrgId, rawArgs)
	case "get_telemetry_trend":
		return ctrl.tk.GetTelemetryTrend(ctx, user.OrgId, rawArgs)
	case "get_active_alerts":
		return ctrl.tk.GetActiveAlerts(ctx, user.OrgId)
	case "get_daily_count":
		return ctrl.tk.GetDailyCount(ctx, user.OrgId, rawArgs)
	case "list_dashboards":
		return ctrl.tk.ListDashboards(ctx, user.OrgId, user.Sub)
	case "preview_dashboard":
		return ctrl.action.Preview(ctx, user.OrgId, rawArgs)
	case "preview_add_widget":
		return ctrl.action.PreviewAddWidget(ctx, user.OrgId, rawArgs)
	case "preview_remove_widget":
		var a struct {
			WidgetTitle string `json:"widget_title"`
		}
		json.Unmarshal(rawArgs, &a)
		return map[string]any{"removed": true, "widgetTitle": a.WidgetTitle}, nil
	case "preview_update_widget":
		return ctrl.action.PreviewUpdateWidget(ctx, user.OrgId, rawArgs)
	case "create_custom_dashboard":
		return ctrl.action.Handle(ctx, user.OrgId, user.Sub, rawArgs)
	case "add_widget_to_dashboard":
		return ctrl.tk.AddWidget(ctx, user.OrgId, rawArgs)
	case "remove_widget":
		return ctrl.tk.RemoveWidget(ctx, user.OrgId, rawArgs)
	case "create_alert":
		return ctrl.tk.CreateAlert(ctx, user.OrgId, rawArgs)
	case "manage_alert_event":
		var a struct {
			EventID string `json:"event_id"`
			Action  string `json:"action"`
		}
		json.Unmarshal(rawArgs, &a)
		if a.Action == "resolve" {
			return ctrl.tk.ResolveAlert(ctx, user.Sub, rawArgs)
		}
		return ctrl.tk.AckAlert(ctx, user.Sub, rawArgs)
	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}

// ── Direct tool execute ───────────────────────────────────────────────────────

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

// ── Preview drafts ──────────────────────────────────────────────────────────────

func (ctrl *Controller) GetPreviewDraft(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	convID, dashID, data, found, err := ctrl.repo.GetDraft(c.Context(), user.Sub)
	if err != nil {
		return err
	}
	if !found {
		return c.JSON(fiber.Map{"success": true, "data": nil})
	}
	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{
		"conversationId": convID,
		"dashboardId":    dashID,
		"data":           data,
	}})
}

func (ctrl *Controller) PutSelectedDashboard(c *fiber.Ctx) error {
	var body struct {
		DashboardID string `json:"dashboardId"`
	}
	if err := c.BodyParser(&body); err != nil || body.DashboardID == "" {
		return middleware.NewAppError(400, "VALIDATION_ERROR", "dashboardId is required")
	}
	user := middleware.GetUser(c)
	if err := ctrl.repo.UpsertDashboard(c.Context(), user.Sub, body.DashboardID); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true})
}

func (ctrl *Controller) PutPreviewDraft(c *fiber.Ctx) error {
	var body struct {
		ConversationID string          `json:"conversationId"`
		Data           json.RawMessage `json:"data"`
	}
	if err := c.BodyParser(&body); err != nil || len(body.Data) == 0 {
		return middleware.NewAppError(400, "VALIDATION_ERROR", "data is required")
	}
	user := middleware.GetUser(c)
	if err := ctrl.repo.UpsertDraft(c.Context(), user.Sub, body.ConversationID, body.Data); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true})
}

func (ctrl *Controller) DeletePreviewDraft(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	if err := ctrl.repo.DeleteDraft(c.Context(), user.Sub); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"success": true})
}

// ── Chat ──────────────────────────────────────────────────────────────────────

func (ctrl *Controller) Chat(c *fiber.Ctx) error {
	if config.Env.GroqApiKey == "" {
		return middleware.NewAppError(503, "AI_UNAVAILABLE", "GROQ_API_KEY is not configured")
	}

	var body struct {
		ConversationID string `json:"conversationId"`
		Message        string `json:"message"`
		Context        string `json:"context"`
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

	// Load conversation history, capped to last 8 messages (Fix 3)
	history, err := ctrl.repo.GetMessages(ctx, body.ConversationID)
	if err != nil {
		return err
	}

	// Build Groq messages: system prompt + capped history
	sp := systemPrompt
	msgs := []groqMessage{{Role: "system", Content: &sp}}
	msgs = append(msgs, buildGroqMessages(history)...)
	// Inject the on-screen dashboard preview AFTER history so the current state is
	// the last thing the model sees — recency makes it win over any stale earlier
	// turn that named the old config (e.g. a metric the user has since changed).
	if body.Context != "" {
		ctxContent := "Authoritative current dashboard state (overrides anything said earlier):\n" + body.Context
		msgs = append(msgs, groqMessage{Role: "system", Content: &ctxContent})
	}

	tools := buildGroqTools(middleware.GetUser(c).Role)

	// When the user has a dashboard/metric card visible, force a tool call on the first
	// iteration so the model cannot skip show_metric and answer from text alone.
	firstToolChoice := ""
	if body.Context != "" {
		firstToolChoice = "required"
	}

	callTools := tools
	for i := 0; i < 5; i++ {
		tc := ""
		if i == 0 {
			tc = firstToolChoice
		}
		resp, err := callGroq(msgs, callTools, tc)
		if err != nil {
			// qwen3 reasoning models try to chain tools even on no-tool summary calls.
			// Groq surfaces this as "Tool choice is none" — retry with the full toolset.
			if strings.Contains(err.Error(), "Tool choice is none") {
				resp, err = callGroq(msgs, tools, "")
			}
			// We forced tool_choice:"required" on turn 0 (dashboard context present),
			// but the model chose to answer in plain text. That's a valid answer —
			// retry with auto so it can.
			// ponytail: required is an optimization, not a hard constraint.
			if err != nil && strings.Contains(err.Error(), "Tool choice is required") {
				resp, err = callGroq(msgs, callTools, "")
			}
		}
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

		msgs = append(msgs, choice.Message)

		for _, tc := range choice.Message.ToolCalls {
			toolInputRaw := json.RawMessage(tc.Function.Arguments)

			result, dispatchErr := ctrl.dispatch(c, tc.Function.Name, toolInputRaw)
			resultJSON, _ := json.Marshal(result)
			if dispatchErr != nil {
				resultJSON, _ = json.Marshal(map[string]any{"error": dispatchErr.Error()})
			}

			tn := tc.Function.Name
			toolMsg, _ := ctrl.repo.AddMessage(ctx, body.ConversationID, "tool",
				"Tool executed: "+tc.Function.Name, &tn,
				toolInputRaw, json.RawMessage(resultJSON))
			if toolMsg != nil {
				newMessages = append(newMessages, *toolMsg)
			}

			resultStr := string(resultJSON)
			msgs = append(msgs, groqMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    &resultStr,
			})
		}

		// Allow one chained tool round (e.g. get_machines → show_metric × N), then force summary.
		if i >= 1 {
			callTools = nil
		}
	}

	return c.JSON(fiber.Map{"success": true, "data": newMessages})
}

// ── Groq HTTP helpers ─────────────────────────────────────────────────────────

func toGroqTool(t map[string]any) map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        t["name"],
			"description": t["description"],
			"parameters":  t["input_schema"],
		},
	}
}

// buildGroqTools returns all tools the LLM may call, filtered only by role.
// Mutating tools require admin/editor — viewers can only read.
func buildGroqTools(role string) []map[string]any {
	canWrite := role == "admin" || role == "editor"
	out := make([]map[string]any, 0, len(AllTools()))
	for _, t := range AllTools() {
		name := t["name"].(string)
		if isWriteTool(name) && !canWrite {
			continue
		}
		out = append(out, toGroqTool(t))
	}
	return out
}

// callGroq sends messages to Groq with the default model. Pass nil tools for a
// plain (no-function-call) request. toolChoice: "" = auto, "required" = force a tool call.
func callGroq(messages []groqMessage, tools []map[string]any, toolChoice string) (*groqResponse, error) {
	return callGroqModel(groqModel, messages, tools, toolChoice)
}

// callGroqModel is callGroq with an explicit model — used by the bake-off harness
// to compare candidates.
func callGroqModel(model string, messages []groqMessage, tools []map[string]any, toolChoice string) (*groqResponse, error) {
	reqBody := map[string]any{
		"model":            model,
		"messages":         messages,
		"reasoning_format": "hidden",
	}
	if len(tools) > 0 {
		reqBody["tools"] = tools
		if toolChoice != "" {
			reqBody["tool_choice"] = toolChoice
		}
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	httpClient := &http.Client{Timeout: 90 * time.Second}

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
			// Groq's function-call parser failed (malformed generation). Retry once
			// without tools so the user gets a plain-text reply instead of an error.
			if strings.Contains(result.Error.Message, "Failed to call a function") && len(tools) > 0 {
				return callGroqModel(model, messages, nil, "")
			}
			return nil, fmt.Errorf("Groq API: %s", result.Error.Message)
		}
		return &result, nil
	}

	return nil, lastErr
}

var retryHintRe = regexp.MustCompile(`try again in ([0-9.]+)s`)

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

// buildGroqMessages converts the last 8 DB messages to Groq/OpenAI format.
// GetMessages now returns DESC order (newest first) to avoid a full table scan;
// reverse here so Groq receives oldest-first conversation order.
func buildGroqMessages(msgs []Message) []groqMessage {
	// reverse DESC→ASC
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	if len(msgs) > 8 {
		msgs = msgs[len(msgs)-8:]
	}
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
