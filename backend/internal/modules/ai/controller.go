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

// gpt-oss-20b: confirmed via bake-off (see eval_test.go) — 12/13, beats 120b (11/13) and qwen.
// 120b fails preview-edit intent (calls preview_dashboard instead of preview_update_widget).
// 20b is also cheaper and Groq prompt-caches the stable base prefix.
const groqModel = "openai/gpt-oss-120b"
const groqBaseURL = "https://api.groq.com/openai/v1/chat/completions"

// systemPromptMinimal is sent on the no-tool path (greetings, chit-chat, "what
// can you do?"). It carries only identity + the language rule — the full
// TOOL SELECTION / SLOT FILLING / WIDGET rules are useless without tools, so a
// bare greeting no longer pays for them (~300 tokens saved vs systemPromptBase).
const systemPromptMinimal = `You are IotVision AI, assistant for an industrial IoT platform. Language: match the user's latest message exactly — Thai or English, never mix. Reply in one short, natural sentence.`

// systemPromptContextAnswer replaces systemPromptContextExt for focused-widget
// READS the on-screen context already answers — analytical questions (the full
// series is injected) and plain current-value / window / config questions. The
// model answers in one no-tool call, avoiding the redundant fetch+summarize round
// that double-billed tokens and tripped the 8k/min rate limit.
const systemPromptContextAnswer = `You are IotVision AI, assistant for an industrial IoT platform. Language: match the user's latest message exactly — Thai or English, never mix.
Answer from the focused widget's on-screen context (its current value, shown time window, bucket/sku/config, and any data series present). For a trend/analysis question, state direction (up/down/stable) and min/max, and extrapolate simply if asked to predict. Do NOT call any tool. If the requested number is not in the context, say you can fetch it — never fabricate. Reply in 1-4 natural sentences, never raw JSON or a bare list of numbers.`
// systemPromptBase is always sent. It covers the no-preview path (pure reads, dashboard
// creation, existing-dashboard edits, greetings). Kept stable so Groq can cache it.
const systemPromptBase = `You are IotVision AI, assistant for an industrial IoT platform. Language: match the user's latest message exactly — Thai or English, never mix.

TOOL SELECTION:
- Greeting / general question → plain text only, no tool.
- "What is X?" / "Show me X" / "ดู X" / any metric read → ALWAYS call show_metric. You have no live sensor data without it. Never fabricate a value. After the tool returns, reply with one short natural sentence — never print raw JSON.
- User asks to see ALL metrics of a machine → get_machines first, then show_metric once per field.
- "Create a dashboard" / "สร้าง dashboard" → call preview_dashboard (default template: machine_overview). Never ask which template. Never call create_custom_dashboard — the user confirms via a button, not by typing.
- User names an existing dashboard to modify → add_widget_to_dashboard / remove_widget.
- "Show / add a widget for X" without naming a dashboard → show_metric (renders a card the user can add themselves). Never ask which dashboard.
- "List SKUs" / which SKUs are available for a machine or count widget → call get_skus(machine).
- Active alerts → get_active_alerts.
- Alert rule management (create / resolve / acknowledge) → plain text: "Alert rules are managed on the Alerts page." Offer get_active_alerts instead.

SLOT FILLING:
- Machine unknown → ask which machine in ONE question. Never guess. Never call get_machines just to list them back.
- Dashboard unknown → ask which dashboard in ONE question.
- Ambiguous action ("fix it", "change it") → ask what to change in ONE question.

WIDGET TYPES: daily-count (production/piece counts) · kpi-card (single value) · line-chart (trend) · gauge (dial)
Line charts support absolute date ranges — convert any DD/MM/YYYY the user gives to YYYY-MM-DD for start_date/end_date.`

// systemPromptContextExt is appended only when a dashboard/preview context is present.
// Keeps preview-specific rules and the CONTEXT section out of no-preview requests (~100 tokens saved).
const systemPromptContextExt = `

Compound message (read + write intent) → serve the read first, then ask about the write.
Editing the current preview canvas → use preview_add_widget / preview_update_widget / preview_remove_widget. For count/production widgets always use type "daily-count".

CONTEXT: An authoritative dashboard state may be injected.
- Structural questions (widget count, names, layout) → answer from context.
- Live value questions with NO widget mentioned ("what is X", "speed of CW-01") → call show_metric.
@Widget Title tokens identify the exact widget the user is referring to — this OVERRIDES the generic
live-value rule above. Any question about a mentioned widget (status, "how's it doing now",
"what is this", vague follow-ups) must route by that widget's ACTUAL type from context, never by
guessing a metric from conversation history:
- Edit/remove intent → use the title verbatim as widget_title in preview_update_widget / preview_remove_widget.
- Simple current-value question ("what is it now", "ตอนนี้เท่าไหร่") → read the widget's type/machine/metric from context, then:
  • daily-count / count-style widget → call get_production_count(machine_id, bucket, sku, status — all from context); never call get_daily_count or show_metric for a mentioned count widget, they don't honor its bucket/sku/status filters. If context lists a "bucket" for this widget, copy it verbatim — never substitute a different one. If context has no bucket line at all, default to "1h", never "1d", unless the user explicitly asks for a daily/monthly view.
  • gauge / kpi-card / line-chart / status-card / table → call show_metric(machine and metric from context).
  • alarm-panel → call get_active_alerts; describe what alerts it monitors.
- Analytical question about a mentioned widget (trend, "how's it doing", vague follow-ups like "ข้อมูลตอนนี้เป็นอย่างไร", แนวโน้ม, วิเคราะห์, ทำนาย, predict, analyze, forecast) → NEVER answer from a single current value — fetch the full series instead:
  • daily-count / count-style widget → call get_production_count(machine_id, bucket, sku, status from context).
  • gauge / kpi-card / line-chart / status-card / table → call get_telemetry_series(machine_id, metric from context, time_range "24h" unless the user implies a different window).
  • alarm-panel → call get_active_alerts.
  Then summarize the trend across ALL returned points — direction (up/down/stable), min/max — and if asked to predict, give a simple extrapolation from the pattern. This overrides the "one short sentence" rule above: use 2-4 natural sentences, never a raw list of numbers or JSON.
  Never fabricate machine or metric — always read them from the context entry's "metric" field for that widget. The widget's "title" (e.g. "Trend", "Speed Gauge") is a display label, NEVER a metric value — never pass it as the metric argument.
  If multiple widgets are mentioned, answer each from its own context entry — do not reuse one widget's metric for another.`

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
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
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
	case "get_telemetry_series":
		return ctrl.tk.GetTelemetrySeries(ctx, user.OrgId, rawArgs)
	case "get_production_count":
		return ctrl.tk.GetProductionCount(ctx, user.OrgId, rawArgs)
	case "get_skus":
		return ctrl.tk.GetSkus(ctx, user.OrgId, rawArgs)
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
	// create_custom_dashboard is intentionally excluded from AllTools() — the LLM
	// cannot trigger it. Only the frontend calls it (via POST /ai/tools/execute) after
	// the user clicks Confirm on a preview. This enforces the preview-then-confirm flow.
	case "create_custom_dashboard":
		return ctrl.action.Handle(ctx, user.OrgId, user.Sub, rawArgs)
	case "add_widget_to_dashboard":
		return ctrl.tk.AddWidget(ctx, user.OrgId, rawArgs)
	case "remove_widget":
		return ctrl.tk.RemoveWidget(ctx, user.OrgId, rawArgs)
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

	// Build Groq messages: system prompt + capped history.
	hasContext := body.Context != ""
	// A message only pays for the context extension / dashboard-state block / tool list when
	// it actually looks like it needs one — a bare greeting with a preview open shouldn't cost
	// any more than a greeting with nothing open. needsTools already matches @WidgetTitle
	// mentions, so the "click a widget then ask something vague" flow is unaffected.
	needsToolsFlag := needsTools(body.Message)

	// The frontend injects the focused widget's full series (marked "on-screen data")
	// only for analytical questions. When present, answer from it in one no-tool call
	// instead of re-fetching via a tool — that redundant round doubled tokens and hit
	// the 8k/min rate limit.
	inlineData := hasContext && strings.Contains(body.Context, "on-screen data")

	// A focused-widget read the injected context already answers (current value,
	// shown window, bucket/sku/config) needs no tool round either. Guardrail: edits
	// and aggregate/range questions still take the tool path.
	focused := hasContext && strings.Contains(body.Message, "@")
	contextRead := focused && !editRe.MatchString(body.Message) && !rangeRe.MatchString(body.Message) && !skuRe.MatchString(body.Message)
	// answerFromContext = one no-tool call from the on-screen context.
	answerFromContext := inlineData || contextRead

	// No-tool path (greetings, chit-chat) gets the minimal prompt — the full
	// rule set is dead weight without tools to act on.
	sp := systemPromptBase
	if !needsToolsFlag {
		sp = systemPromptMinimal
	} else if answerFromContext {
		sp = systemPromptContextAnswer
	} else if hasContext {
		sp += systemPromptContextExt
	}
	msgs := []groqMessage{{Role: "system", Content: &sp}}
	msgs = append(msgs, buildGroqMessages(history)...)
	// Inject the on-screen dashboard preview AFTER history so the current state is
	// the last thing the model sees — recency makes it win over any stale earlier
	// turn that named the old config (e.g. a metric the user has since changed).
	if hasContext && needsToolsFlag {
		ctxContent := "Authoritative current dashboard state (overrides anything said earlier):\n" + body.Context
		msgs = append(msgs, groqMessage{Role: "system", Content: &ctxContent})
	}

	tools := buildGroqTools(middleware.GetUser(c).Role, hasContext)

	// Pure conversational messages (greetings, "what can you do?") get no tools —
	// the model answers in plain text and tools are sent on the next actionable message.
	// answerFromContext likewise needs no tools: the on-screen context already has the answer.
	if !needsToolsFlag || answerFromContext {
		tools = nil
	}

	// Force a tool call only when an @WidgetTitle mention is present — the one signal that
	// guarantees the model has a concrete widget (and its context block) to act on. Forcing
	// on a broader keyword guess risks the model emitting an invalid function call when the
	// message is too vague, which Groq rejects (400) and triggers a costly full retry.
	// Never force when we're answering from context — we want a plain-text answer.
	firstToolChoice := ""
	if focused && !answerFromContext {
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

		// Cap chained tool rounds, then force a text summary. A focused-widget
		// message gets 1 round (2 calls) — each round re-sends the full ~3k context,
		// so more rounds blow the 8k/min limit. Non-focused keeps 2 rounds for the
		// get_machines → show_metric ×N fan-out. Outer loop cap is the hard stop.
		roundCap := 1
		if focused {
			roundCap = 0
		}
		if i >= roundCap {
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

// toGroqToolSlim sends only name + description (no parameters/input_schema).
// The description embeds arg hints so the model knows what JSON to produce.
// Saves ~50–80 tokens per simple tool vs the full schema form.
var slimToolDescriptions = map[string]string{
	"show_metric":         "Show a live metric widget. Args: machine (name, e.g. CW-01), metric (a REAL sensor field key like speed/weight/rejects/throughput — NEVER a display style word), viz (OPTIONAL display style only: value|gauge|trend — never put these words in metric).",
	"get_telemetry_trend": "Get avg/min/max of one metric. Args: machine_id (name), metric (field key), time_range (5m|15m|30m|1h|6h|24h|7d|15d|30d).",
	"get_daily_count":     "Per-day production count. Args: machine_id (name), days (integer, default 7).",
	"get_skus":            "List the SKU values available for a machine. Args: machine_id (name).",
	"preview_dashboard":   "Preview a template dashboard. Args: machine (name), template (machine_overview|machine_production|machine_maintenance).",
	"remove_widget":       "Remove a widget from a named dashboard. Args: dashboard_name, widget_title.",
}

func toGroqToolSlim(t map[string]any) map[string]any {
	name := t["name"].(string)
	desc, _ := t["description"].(string)
	if slim, ok := slimToolDescriptions[name]; ok {
		desc = slim
	}
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        name,
			"description": desc,
			// additionalProperties:true lets the model pass args derived from the
			// description hints without Groq's schema validator rejecting them.
			"parameters": map[string]any{
				"type":                 "object",
				"additionalProperties": true,
			},
		},
	}
}

// complexSchemaTools keep their full input_schema because they have nested objects
// (widgetItemSchema) or many optional patch fields that the model needs to see.
var complexSchemaTools = map[string]bool{
	"add_widget_to_dashboard": true,
	"preview_add_widget":      true,
	"preview_remove_widget":   true,
	"preview_update_widget":   true,
}

// previewOnlyTools are only useful when a dashboard/preview context is present.
// Hiding them on no-context requests saves tokens from the tools list.
var previewOnlyTools = map[string]bool{
	"preview_add_widget":    true,
	"preview_remove_widget": true,
	"preview_update_widget": true,
}

// buildGroqTools returns the tool list filtered by role and context.
// Simple tools use slim (description-only) form; complex tools keep full schemas.
func buildGroqTools(role string, hasContext bool) []map[string]any {
	canWrite := role == "admin" || role == "editor"
	out := make([]map[string]any, 0, len(AllTools()))
	for _, t := range AllTools() {
		name := t["name"].(string)
		if isWriteTool(name) && !canWrite {
			continue
		}
		if previewOnlyTools[name] && !hasContext {
			continue
		}
		if complexSchemaTools[name] {
			out = append(out, toGroqTool(t))
		} else {
			out = append(out, toGroqToolSlim(t))
		}
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
			// Groq's function-call parser failed (malformed generation, e.g. gpt-oss
			// leaking a "<|channel|>commentary" token into the tool name). Retry once
			// without tools so the user gets a plain-text reply instead of an error.
			if (strings.Contains(result.Error.Message, "Failed to call a function") ||
				strings.Contains(result.Error.Message, "Tool call validation failed")) && len(tools) > 0 {
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

// needsTools returns true when the message likely needs a tool — a metric read, action request,
// or reference to a machine/dashboard. Used only to zero-out tools for pure greetings on
// no-context requests, so they don't pay for the full tool list.
var needsToolsRe = regexp.MustCompile(`(?i)@|\b(` +
	`speed|temp(erature)?|weight|pressure|humidity|voltage|current|power|flow|level|count|status|` +
	`ดู|แสดง|เร็ว|อุณห|น้ำหนัก|ความดัน|กระแส|กำลัง|` +
	`show|create|dashboard|add|widget|remove|alert|machine|trend|gauge|kpi|production|sku|` +
	`สร้าง|เพิ่ม|ลบ|แก้|เปลี่ยน|เครื่อง|แจ้งเตือน|ผลิต` +
	`)\b`)

func needsTools(msg string) bool { return needsToolsRe.MatchString(msg) }

// editRe / rangeRe classify a focused-widget message so we know whether the
// on-screen context can answer it without a tool:
//   - editIntent  → the user wants to mutate the widget (needs preview_update_widget etc.).
//   - rangeIntent → the answer needs an aggregate/time-range fetch not guaranteed in context.
// A focused read that is neither can be answered straight from the injected context.
var editRe = regexp.MustCompile(`(?i)\b(change|set|update|rename|move|remove|delete|add)\b|เปลี่ยน|แก้|ตั้ง|ลบ|เพิ่ม|ย้าย`)
var rangeRe = regexp.MustCompile(`(?i)\b(avg|average|mean|sum|total|min|max|peak|trend|forecast|predict|analy\w*|over|last|past|hour|day|week|month|yesterday|today)\b|เฉลี่ย|รวม|สูงสุด|ต่ำสุด|ย้อนหลัง|เมื่อวาน|วันนี้|แนวโน้ม|วิเคราะห์|ทำนาย`)

// skuRe: a SKU question needs get_skus — the available-SKU list is never in the
// injected context, so it must NOT take the no-tool contextRead path.
var skuRe = regexp.MustCompile(`(?i)sku`)

// buildGroqMessages converts the last few DB messages to Groq/OpenAI format.
// GetMessages now returns DESC order (newest first) to avoid a full table scan;
// reverse here so Groq receives oldest-first conversation order.
//
// Capped to the last 3 rows: after a long chat the transcript was the single
// biggest input-token cost per call, and inflating every request pushed focused
// follow-ups over the 8k/min rate limit. 3 keeps the immediate back-and-forth.
//
// Past tool calls/results are deliberately NOT reconstructed here — only the
// user/assistant text rows are. The assistant's final reply already summarizes
// whatever the tool returned, so replaying the raw tool JSON on every later turn
// just re-pays its token cost for no benefit; a follow-up that genuinely needs
// fresh data makes its own tool call in the current turn anyway.
func buildGroqMessages(msgs []Message) []groqMessage {
	// reverse DESC→ASC
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	if len(msgs) > 3 {
		msgs = msgs[len(msgs)-3:]
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
