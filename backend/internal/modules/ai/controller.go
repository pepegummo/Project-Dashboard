package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"iot-dashboard/internal/config"
	"iot-dashboard/internal/middleware"

	"github.com/gofiber/fiber/v2"
)

// gpt-oss-20b: confirmed via bake-off (see eval_test.go) — 2026-07-06 run: 23/23, zero
// rate-limits, smallest prompts (~2.7k tok), fastest (~0.83s median); 120b scored 21/22
// (one nondeterministic preview_* slip) and buys no accuracy edge. 20b is also cheaper and
// Groq prompt-caches the stable base prefix. See docs/AI_ARCHITECTURE.md §3.
// Overridable via AI_MODEL / AI_BASE_URL env vars (config.Load); defaults below.
func aiModel() string { return envOr(config.Env.AIModel, "openai/gpt-oss-120b") }
func aiBaseURL() string {
	u := envOr(config.Env.AIBaseURL, "https://api.groq.com/openai/v1/chat/completions")
	// Accept either the provider base (".../v1") or the full completions URL.
	if !strings.HasSuffix(u, "/chat/completions") {
		u = strings.TrimRight(u, "/") + "/chat/completions"
	}
	return u
}

// aiMaxTokens caps completion length via AI_MAX_TOKENS (config.Load). Hidden reasoning
// counts against this cap — don't set below ~1024 or tool-call JSON may truncate.
func aiMaxTokens() int {
	if n, err := strconv.Atoi(config.Env.AIMaxTokens); err == nil && n > 0 {
		return n
	}
	return 2048
}

// envOr falls back when the config field is empty (tests build config.Env by hand).
func envOr(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

// systemPromptUnified is the single, byte-stable system prompt sent on all requests
// with the full role-filtered tool set. Merged from:
//   - systemPromptBase (no-preview path: pure reads, dashboard creation, edits, greetings)
//   - systemPromptContextExt (dashboard/preview context rules)
//   - systemPromptContextAnswer's distinct rule (ON-SCREEN DATA section below) — made
//     conditional on the "on-screen data" context line instead of being a prompt swap
//   - dateEditRule static text (relative date resolution + EDIT rules — dynamic date appended separately)
//   - bucketEditRule (bar interval change EDIT rules)
//   - fieldsEditRule (metric-overlay change EDIT rules)
//
// Groq caches the static prefix; tools are ordered static-first for cache re-use.
const systemPromptUnified = `You are IotVision AI, assistant for an industrial IoT platform. Language: match the user's latest message exactly — Thai or English, never mix. Plain text only — no markdown, no asterisks (**) or bold.

TOOL SELECTION:
- Greeting / general question → plain text, no tool.
- "What is X?" / "Show me X" / "ดู X" / any metric read → ALWAYS call show_metric. You have no live sensor data without it — never fabricate a value. After the tool returns, reply in one short natural sentence, never raw JSON.
- All metrics of a machine → get_machines first, then show_metric once per field.
- "Create a dashboard" / "สร้าง dashboard" → preview_dashboard (default template: machine_overview). Never ask which template. Never call create_custom_dashboard — the user confirms via a button, not by typing.
- Modify an existing dashboard → it must be OPEN on screen (an "Active dashboard" context). Stage the change with preview_add_widget / preview_update_widget / preview_remove_widget — NOTHING is saved until the user clicks Save. If a preview or Active dashboard is on screen, stage the edit on THAT — never ask the user to open anything. Only when NO preview/Active dashboard is on screen, or they name a different dashboard than the open one, ask them to open it in the AI page; never write without an open context.
- "Show / add a widget for X" without naming a dashboard → show_metric (renders a card the user can add). Never ask which dashboard.
- SKUs available for a machine or count widget → get_skus(machine).
- Active alerts → get_active_alerts. Alert rule management (create/resolve/ack) → plain text: "Alert rules are managed on the Alerts page." Offer get_active_alerts instead.

SLOT FILLING: machine unknown, dashboard unknown, or ambiguous action ("fix it", "change it") → ask ONE question. Never guess. Never call get_machines just to list them back.

WIDGET TYPES: daily-count (production/piece counts) · kpi-card (single value) · line-chart (trend) · gauge (dial) · chart (multi-metric overlay — set fields[] not metric, plus chartType and scaling). Line charts support absolute date ranges — convert any DD/MM/YYYY the user gives to YYYY-MM-DD for start_date/end_date.

Compound message (read + write intent) → serve the read first, then ask about the write.
Preview/Active-dashboard changes are staged via preview_* (no DB write); count/production widgets always use type "daily-count"; after staging tell the user to click Save (Active dashboard) or Confirm (new preview) — never claim it is saved.

CONTEXT: An authoritative dashboard state may be injected.
- A context line marked [FOCUSED] is the widget the user clicked — route the answer to THAT widget (its machine/type/metric/bucket/sku), ignoring other widgets unless the user names one; never let another widget's title (e.g. "Trend") pull you off it.
- A line beginning "user clicked ..." names the exact element the user pointed at inside that widget — answer about that element specifically (the axis, unit, threshold, or data point), not the widget as a whole.
- Structural questions (widget count, names, layout) → answer from context.
- Live value questions with NO widget mentioned ("what is X", "speed of CW-01") → show_metric.
@Widget Title tokens identify the exact widget the user means — this OVERRIDES the live-value rule above. Any question about a mentioned widget must route by that widget's ACTUAL type from context, never by guessing a metric from conversation history:
- Edit/remove intent → use the title verbatim as widget_title in preview_update_widget / preview_remove_widget.
- Data questions about a mentioned widget — both simple current-value ("what is it now", "ตอนนี้เท่าไหร่") and analytical (trend, "how's it doing", vague follow-ups like "ข้อมูลตอนนี้เป็นอย่างไร", แนวโน้ม, วิเคราะห์, ทำนาย, predict, analyze, forecast) — pick the tool from the widget's type in context:
  • daily-count / count-style → get_production_count(machine_id, bucket, sku, status — all from context); never show_metric for a count widget, it ignores the widget's bucket/sku/status filters. Copy the context's bucket verbatim; if context has no bucket line, default to "1h", never "1d", unless the user explicitly asks for a daily/monthly view.
  • gauge / kpi-card / line-chart / status-card / table → show_metric(machine and metric from context) for a current value; get_telemetry_series(machine_id, metric from context, time_range "24h" unless the user implies a different window) for an analytical question — NEVER answer an analytical question from a single current value.
  • alarm-panel → get_active_alerts; describe what alerts it monitors.
  Analytical answers: summarize the trend across ALL returned points — describe the SHAPE (call out a rise-then-fall or fall-then-rise, not one net direction) plus min/max; if asked to predict, give a simple extrapolation from the pattern. Quote times exactly as given (plant-local). This overrides the one-short-sentence rule: use 2-4 natural sentences, never a raw list of numbers or JSON.
  Machine and metric always come from the context entry's "metric" field — never fabricate them. A widget's "title" (e.g. "Trend", "Speed Gauge") is a display label, NEVER a metric value — never pass it as the metric argument. Multiple mentioned widgets → answer each from its own context entry.

ON-SCREEN DATA: when the context includes an "on-screen data" line, that widget's full series is already injected — answer directly from it and do NOT call any tool to re-fetch it. If the requested number is not present in the on-screen data, say you can fetch it — never fabricate.

EDITS of a focused widget — the following are preview_update_widget calls, NOT data reads. Read tools (show_metric / get_telemetry_series / get_telemetry_trend / get_production_count) only return data and will NOT change what the widget shows — never call them for these, never refuse, and never answer by summarizing the trend instead:
- RELATIVE DATES: resolve from today's date (appended separately per request): yesterday/เมื่อวาน = the day before today, today/วันนี้ = today, last week/สัปดาห์ที่แล้ว = the 7 days ending today. Changing what a focused line-chart displays for a time period — including a plain VIEW/SEE request ("ดูของเมื่อวาน", "show yesterday", "เปลี่ยนเป็นเมื่อวาน", "ดูของสัปดาห์ที่แล้ว") — is such an EDIT: pass start_date and end_date as YYYY-MM-DD. The chart's currently-shown window is NOT a date reference — compute yesterday/last week only from today's date, never from the on-screen window.
- BUCKET: changing a focused count/chart widget's bar interval — a bare "<N> minutes/hours", "22 นาที", "ทุก 15 นาที", "รายชั่วโมง", or "every 15 min" — is such an EDIT: pass bucket as <number><m|h|d> (22 นาที → "22m", 1 ชั่วโมง → "1h"). Any bucket like "22m" is valid — never say the widget only supports its current interval.
- METRIC OVERLAYS: comparing or overlaying metrics — "เปรียบเทียบ weight, speed", "compare speed and throughput", "overlay X vs Y" — ALWAYS resolves to a custom chart widget (type "chart"), NEVER a line-chart (a line-chart shows one metric and cannot compare). Match the user's metric words to the machine's real field keys, e.g. fields:["weight","speed"].
  • A custom chart (type "chart") already on the dashboard → EDIT it: preview_update_widget with fields as the new metric keys — never refuse because it currently shows other metrics.
  • NO custom chart yet → ADD one: preview_add_widget with type:"chart", fields:[the metric keys], and for exactly two metrics scaling:"dual" (they usually have different units; dual axis keeps both readable).`

// dateLineForRequest returns "Today is YYYY-MM-DD (plant-local)." to append to
// the dynamic context (dashboard state message) so the model can resolve relative dates.
// This moves the date OUT of the system prompt (which is Groq-cached) and INTO the
// per-request context, so the cache remains byte-identical across all requests and days.
// ponytail: server-local date. In Docker that's UTC — near midnight it can be a day
// off plant-local. If the plant isn't near UTC, have the frontend pass its local date.
func dateLineForRequest() string {
	today := time.Now().Format("2006-01-02")
	return "Today is " + today + " (plant-local)."
}

// ── Groq / OpenAI-compatible API types ───────────────────────────────────────

type aiMessage struct {
	Role       string       `json:"role"`
	Content    *string      `json:"content"` // pointer so null stays null (tool-call turns)
	ToolCalls  []aiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
}

type aiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type aiResponse struct {
	Choices []struct {
		Message      aiMessage `json:"message"`
		FinishReason string    `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *aiError `json:"error"`
}

// aiError tolerates both OpenAI-style {"error":{"message":...,"code":...}} and
// bare-string errors ({"error":"This model reached daily limit."} — KKU proxy).
type aiError struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

func (e *aiError) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		e.Message = s
		return nil
	}
	type plain aiError
	return json.Unmarshal(b, (*plain)(e))
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

	// Mutating tools require admin or editor — viewers can only read. Preview tools
	// stage dashboard edits (no DB write) but are gated the same way as an actual
	// write — mirrors the exclusion in buildAITools in case a client forges the call.
	if (isWriteTool(toolName) || previewTools[toolName]) && user.Role != "admin" && user.Role != "editor" {
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
	if config.Env.AIApiKey == "" {
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

	// Build Groq messages: unified system prompt + capped history.
	hasContext := body.Context != ""

	// The frontend injects the focused widget's full series (marked "on-screen data")
	// only for analytical questions. The prompt's ON-SCREEN DATA rule tells the model
	// to answer from it directly instead of re-fetching via a tool; tools stay attached
	// (Groq prompt-prefix cache survives) but tool_choice is forced to "none" below for
	// this turn — that redundant fetch+summarize round doubled tokens and hit the
	// 8k/min rate limit.
	inlineData := hasContext && strings.Contains(body.Context, "on-screen data")

	// focused = the user @-mentioned a widget with a dashboard/preview on screen.
	// Feeds both dispatchIntent's tool-choice rules and the chained-round cap below.
	focused := hasContext && strings.Contains(body.Message, "@")

	// chartExists = a custom chart (type "chart") is already on the dashboard.
	// Lets dispatchIntent route a `compare` to update-the-chart vs add-a-chart.
	chartExists := customChartRe.MatchString(body.Context)

	// Always use the unified prompt; append today's date to the dynamic context.
	sp := systemPromptUnified
	msgs := []aiMessage{{Role: "system", Content: &sp}}
	msgs = append(msgs, buildAIMessages(history)...)
	// Inject the on-screen dashboard preview AFTER history so the current state is
	// the last thing the model sees — recency makes it win over any stale earlier
	// turn that named the old config (e.g. a metric the user has since changed).
	// Append today's date so the model can resolve relative dates (it was in dateEditRule).
	// The date line is always present, with or without a context block, so relative-date
	// resolution works even on a plain (no dashboard on screen) message.
	if hasContext {
		ctxContent := "Authoritative current dashboard state (overrides anything said earlier):\n" +
			body.Context + "\n" + dateLineForRequest()
		msgs = append(msgs, aiMessage{Role: "system", Content: &ctxContent})
	} else {
		dateContent := dateLineForRequest()
		msgs = append(msgs, aiMessage{Role: "system", Content: &dateContent})
	}

	user := middleware.GetUser(c)
	role := user.Role
	// Always build the full role-filtered tool set (previews are always available now,
	// not just when context is present).
	tools := buildAITools(role)

	// Intent router (Phase 3): classify the message into a small enum, then let
	// dispatchIntent decide deterministically. Design law: the model classifies, Go
	// decides. A router miss/decline (ok == false) falls back to plain auto — see
	// dispatchIntent's !ok branch — Chat must never fail because the router failed.
	routerStart := time.Now()
	intentRes, routerOK := ClassifyIntent(ctx, body.Message, focusedContextSummary(body.Context))
	if routerOK {
		log.Printf("[ai router] intent=%s confidence=%.2f duration=%s", intentRes.Intent, intentRes.Confidence, time.Since(routerStart))
	} else {
		log.Printf("[ai router] fallback (no confident classification) duration=%s", time.Since(routerStart))
	}

	// Slot sanity guard: spend at most one query, and only when the machine slot
	// actually drives a forced-by-name decision (read_metric/read_agg/production).
	// Never force a tool with a hallucinated machine — an unresolved slot degrades
	// dispatchIntent's choice to tool_choice:"required" instead.
	machineValid := false
	if routerOK && intentRes.Machine != "" &&
		(intentRes.Intent == "read_metric" || intentRes.Intent == "read_agg" || intentRes.Intent == "production") {
		_, machineValid = resolveMachineID(ctx, user.OrgId, intentRes.Machine)
	}

	firstToolChoice, roundCap := dispatchIntent(intentRes, routerOK, focused, inlineData, role, machineValid, chartExists)

	var finalText string
	var toolLog []toolExecution

	// Read turns don't need the verbose preview_* schemas — send everything slim.
	// The error-retry fallbacks and the repair round keep the full `tools` set.
	callTools := tools
	if routerOK && readOnlyIntents[intentRes.Intent] {
		callTools = buildAIToolsWith(role, true)
	}
	for i := 0; i < 5; i++ {
		tc := ""
		if i == 0 {
			tc = firstToolChoice
		}
		resp, err := callAI(msgs, callTools, tc)
		if err != nil {
			// qwen3 reasoning models try to chain tools even on no-tool summary calls.
			// Groq surfaces this as "Tool choice is none" — retry with the full toolset.
			if strings.Contains(err.Error(), "Tool choice is none") {
				resp, err = callAI(msgs, tools, "")
			}
			// We forced tool_choice:"required" on turn 0 (dashboard context present),
			// but the model chose to answer in plain text. That's a valid answer —
			// retry with auto so it can.
			// ponytail: required is an optimization, not a hard constraint.
			if err != nil && strings.Contains(err.Error(), "Tool choice is required") {
				resp, err = callAI(msgs, callTools, "")
			}
		}
		if err != nil {
			var rl *rateLimitError
			if errors.As(err, &rl) {
				return &middleware.AppError{StatusCode: 429, Code: "RATE_LIMIT",
					Message: rl.Error(), Details: fiber.Map{"retryAfter": rl.seconds}}
			}
			return middleware.NewAppError(502, "AI_ERROR", fmt.Sprintf("AI API error: %v", err))
		}
		if len(resp.Choices) == 0 {
			return middleware.NewAppError(502, "AI_ERROR", "AI returned no choices")
		}

		choice := resp.Choices[0]

		if choice.FinishReason != "tool_calls" || len(choice.Message.ToolCalls) == 0 {
			if choice.Message.Content != nil {
				finalText = *choice.Message.Content
			}
			break
		}

		msgs = append(msgs, choice.Message)

		var roundPersisted []Message
		var roundLog []toolExecution
		msgs, roundPersisted, roundLog = ctrl.runToolRound(c, ctx, body.ConversationID, choice.Message.ToolCalls, msgs)
		newMessages = append(newMessages, roundPersisted...)
		toolLog = append(toolLog, roundLog...)

		// Cap chained tool rounds, then force a text summary. roundCap comes from
		// dispatchIntent (0 for a focused-widget message, else 1) — each round
		// re-sends the full ~3k context, so more rounds blow the 8k/min limit.
		// Non-focused keeps 2 rounds for the get_machines → show_metric ×N fan-out.
		// Outer loop cap is the hard stop.
		if i >= roundCap {
			callTools = nil
		}
	}

	// Verify-then-repair (Phase 5), scoped to requests where at least one tool
	// executed — pure chat (toolLog empty) skips this entirely: zero added cost.
	if len(toolLog) > 0 {
		finalText = ctrl.verifyAndMaybeRepair(c, ctx, verifyRequest{
			convID:      body.ConversationID,
			userMessage: body.Message,
			contextText: body.Context,
			orgID:       user.OrgId,
			msgs:        msgs,
			tools:       tools,
			intentRes:   intentRes,
			routerOK:    routerOK,
			toolLog:     toolLog,
			finalText:   finalText,
		}, &newMessages)
	}

	assistantMsg, err := ctrl.repo.AddMessage(ctx, body.ConversationID, "assistant", finalText, nil, nil, nil)
	if err != nil {
		return err
	}
	newMessages = append(newMessages, *assistantMsg)

	return c.JSON(fiber.Map{"success": true, "data": newMessages, "intent": chatIntentResponse(intentRes, routerOK)})
}

// runToolRound dispatches every tool call from one Groq response, persists each
// as a "tool" message, appends the tool result into msgs for the next completion
// call, and returns the accumulated log entries for verification. Shared by the
// main loop above and the single repair round (verifyAndMaybeRepair) so both stay
// byte-identical in how they dispatch/persist/append.
func (ctrl *Controller) runToolRound(c *fiber.Ctx, ctx context.Context, convID string, calls []aiToolCall, msgs []aiMessage) ([]aiMessage, []Message, []toolExecution) {
	var persisted []Message
	var log []toolExecution
	for _, tc := range calls {
		toolInputRaw := json.RawMessage(tc.Function.Arguments)

		result, dispatchErr := ctrl.dispatch(c, tc.Function.Name, toolInputRaw)
		resultJSON, _ := json.Marshal(result)
		if dispatchErr != nil {
			resultJSON, _ = json.Marshal(map[string]any{"error": dispatchErr.Error()})
		}

		tn := tc.Function.Name
		toolMsg, _ := ctrl.repo.AddMessage(ctx, convID, "tool",
			"Tool executed: "+tc.Function.Name, &tn,
			toolInputRaw, json.RawMessage(resultJSON))
		if toolMsg != nil {
			persisted = append(persisted, *toolMsg)
		}

		resultStr := string(resultJSON)
		msgs = append(msgs, aiMessage{
			Role:       "tool",
			ToolCallID: tc.ID,
			Content:    &resultStr,
		})
		log = append(log, toolExecution{name: tc.Function.Name, args: tc.Function.Arguments, resultJSON: resultStr})
	}
	return msgs, persisted, log
}

// ── Verify-then-repair (Phase 5) ─────────────────────────────────────────────

// verifyRequest bundles the per-request inputs verifyAndMaybeRepair needs — kept
// as a struct rather than a long positional argument list since most fields are
// just Chat()'s own locals threaded through unchanged.
type verifyRequest struct {
	convID      string
	userMessage string
	contextText string
	orgID       string
	msgs        []aiMessage      // full conversation so far, for the repair round
	tools       []map[string]any // role-filtered tool set, for the repair round
	intentRes   IntentResult
	routerOK    bool
	toolLog     []toolExecution
	finalText   string
}

// verifyAndMaybeRepair runs the Phase 5 verify-then-repair loop: deterministic
// checks (free) -> LLM verify (router model, bounded) -> at most ONE repair round
// -> a clarifying question if still wrong. Only called when at least one tool
// executed this request (Chat()'s scope-rule gate). Every failure path here
// (verifier timeout/parse error, DB lookup miss, repair-round Groq error)
// degrades to returning req.finalText unchanged — verification can never break
// or block Chat(). newMessages accumulates any tool messages persisted during
// the repair round, same shape as the main loop's.
func (ctrl *Controller) verifyAndMaybeRepair(c *fiber.Ctx, ctx context.Context, req verifyRequest, newMessages *[]Message) string {
	start := time.Now()

	detProblem, detFailed := runDeterministicChecks(ctx, req.orgID, req.contextText, req.toolLog, resolveMachineID, getMachineFieldsForMachine)

	var verdict *VerifyResult
	if !detFailed {
		intentSummary := "router declined"
		if req.routerOK {
			intentSummary = req.intentRes.Intent
		}
		if vr, ok := VerifyAnswer(ctx, req.userMessage, intentSummary, req.finalText, summarizeToolLog(req.toolLog)); ok {
			verdict = &vr
		}
	}

	outcome := decideVerifyOutcome(!detFailed, verdict, false, req.routerOK)
	resultText := req.finalText
	logVerdict := verdictLabel(outcome)

	switch outcome {
	case outcomeDeliver:
		// keep resultText as-is

	case outcomeAskBack:
		resultText = clarifyingQuestionOrFallback(verdict)

	case outcomeRepair:
		problem := detProblem
		if problem == "" && verdict != nil {
			problem = verdict.Problem
		}
		repairMsgs := buildRepairMessages(req.msgs, req.finalText, problem)

		repairedText, repairLog, repairPersisted, repairErr := ctrl.runRepairRound(c, ctx, req.convID, repairMsgs, req.tools)
		if newMessages != nil {
			*newMessages = append(*newMessages, repairPersisted...)
		}

		if repairErr != nil {
			// Repair round's own Groq call failed — degrade to the original answer
			// rather than blocking the request on infrastructure trouble.
			logVerdict = "repair-error"
			break
		}

		// Deliberately checked against repairLog ONLY, not req.toolLog+repairLog: an
		// honest text-only repair (no new tool calls — e.g. it just asks a
		// clarifying question) has an empty repairLog and must read as "checks
		// pass," not re-trip the ORIGINAL round's now-superseded failure and get
		// trapped in askback forever.
		_, detFailedAgain := runDeterministicChecks(ctx, req.orgID, req.contextText, repairLog, resolveMachineID, getMachineFieldsForMachine)
		firstClarify := ""
		if verdict != nil {
			firstClarify = verdict.ClarifyingQuestion
		}
		post := decidePostRepairOutcome(!detFailedAgain, firstClarify, len(repairLog) > 0)
		if post == outcomeAskBack {
			logVerdict = "askback"
			resultText = clarifyingQuestionOrFallback(verdict)
		} else if repairedText != "" {
			logVerdict = "repair"
			resultText = repairedText
		} else {
			// Guard against an empty repair response silently blanking out an
			// otherwise-fine original answer — degrade to the original instead.
			logVerdict = "repair-empty"
		}
	}

	log.Printf("ai verify: verdict=%s intent=%s det=%s dur=%s",
		logVerdict, intentLabel(req.intentRes, req.routerOK), detLabel(!detFailed), time.Since(start))

	return resultText
}

// runRepairRound re-enters the main-model loop once (brief §3 rule 3: roundCap 1
// — up to one round of tool calls, then a forced text summary), mirroring the
// cap pattern of Chat()'s main loop via runToolRound. tool_choice is left auto
// ("") throughout; tools stay attached (role-filtered, passed in by the caller).
// A Groq error here is returned to the caller, which degrades to the original
// (pre-repair) answer rather than failing the request.
func (ctrl *Controller) runRepairRound(c *fiber.Ctx, ctx context.Context, convID string, msgs []aiMessage, tools []map[string]any) (text string, toolLog []toolExecution, persisted []Message, err error) {
	const repairRoundCap = 1
	callTools := tools
	for i := 0; i < 5; i++ {
		resp, callErr := callAI(msgs, callTools, "")
		if callErr != nil && strings.Contains(callErr.Error(), "Tool choice is none") {
			resp, callErr = callAI(msgs, tools, "")
		}
		if callErr != nil {
			return "", toolLog, persisted, callErr
		}
		if len(resp.Choices) == 0 {
			return "", toolLog, persisted, fmt.Errorf("AI returned no choices")
		}

		choice := resp.Choices[0]
		if choice.FinishReason != "tool_calls" || len(choice.Message.ToolCalls) == 0 {
			if choice.Message.Content != nil {
				text = *choice.Message.Content
			}
			return text, toolLog, persisted, nil
		}

		msgs = append(msgs, choice.Message)
		var roundPersisted []Message
		var roundLog []toolExecution
		msgs, roundPersisted, roundLog = ctrl.runToolRound(c, ctx, convID, choice.Message.ToolCalls, msgs)
		persisted = append(persisted, roundPersisted...)
		toolLog = append(toolLog, roundLog...)

		if i >= repairRoundCap {
			callTools = nil
		}
	}
	return text, toolLog, persisted, nil
}

// chatIntentResponse exposes the router's classification (Task 2/3) to the frontend so
// it can consume the server's resolved intent+slots instead of re-parsing question text
// with its own regexes (Task 4). ok:false with zero-value slots when the router declined
// or fell back — the frontend must treat that as "no ephemeral card", never as a signal
// to fall back to its own text parsing.
func chatIntentResponse(res IntentResult, ok bool) fiber.Map {
	fields := res.Fields
	if fields == nil {
		fields = []string{} // marshal as [], never null — matches the documented contract
	}
	return fiber.Map{
		"ok":           ok,
		"intent":       res.Intent,
		"machine":      res.Machine,
		"metric":       res.Metric,
		"fields":       fields,
		"bucket":       res.Bucket,
		"dateRange":    fiber.Map{"start": res.DateRange.Start, "end": res.DateRange.End},
		"targetWidget": res.TargetWidget,
		"status":       res.Status,
		"sku":          res.Sku,
		"confidence":   res.Confidence,
	}
}

// ── Groq HTTP helpers ─────────────────────────────────────────────────────────

func toAITool(t map[string]any) map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        t["name"],
			"description": t["description"],
			"parameters":  t["input_schema"],
		},
	}
}

// toAIToolSlim sends only name + description (no parameters/input_schema).
// The description embeds arg hints so the model knows what JSON to produce.
// Saves ~50–80 tokens per simple tool vs the full schema form.
var slimToolDescriptions = map[string]string{
	"show_metric":         "Show a live metric widget. Args: machine (name, e.g. CW-01), metric (a REAL sensor field key like speed/weight/rejects/throughput — NEVER a display style word), viz (OPTIONAL display style only: value|gauge|trend — never put these words in metric).",
	"get_telemetry_trend": "Get avg/min/max of one metric. Args: machine_id (name), metric (field key), time_range (5m|15m|30m|1h|6h|24h|7d|15d|30d).",
	"get_skus":            "List the SKU values available for a machine. Args: machine_id (name).",
	"preview_dashboard":   "Preview a template dashboard. Args: machine (name), template (machine_overview|machine_production|machine_maintenance).",
	// The three complexSchemaTools below normally keep full schemas; these slim forms
	// are only sent on read-intent turns (see readOnlyIntents) where they must stay
	// callable but their ~850-token schemas are almost certainly dead weight.
	"preview_add_widget":    "Add a widget to the open preview/Active dashboard (staged, no DB write). Args: machine (name), widget {type (daily-count|kpi-card|line-chart|gauge|status-card|table|alarm-panel|chart), title, metric, bucket, sku, status (all|good|reject), fields[], chartType (line|bar|area), points, scaling (shared|dual|normalized), min, max, unit}.",
	"preview_remove_widget": "Remove a widget from the open preview/Active dashboard (staged, no DB write). Args: widget_title (exact displayed title from the dashboard context, not the widget type).",
	"preview_update_widget": "Edit a widget on the open preview/Active dashboard (staged, no DB write). Args: widget_title (current title, verbatim) plus ONLY the fields to change: new_title, machine, type, metric, unit, min, max, start_date/end_date (YYYY-MM-DD), bucket (<n><m|h|d>), sku, status (all|good|reject), fields[], chartType (line|bar|area), points, scaling (shared|dual|normalized).",
}

func toAIToolSlim(t map[string]any) map[string]any {
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
	"preview_add_widget":    true,
	"preview_remove_widget": true,
	"preview_update_widget": true,
}

// previewTools stage a dashboard edit (preview canvas or an open Active dashboard).
// Nothing is persisted until the user clicks Save/Confirm, but staging is still
// gated to admin/editor like a real write — viewers must not be offered these,
// and dispatch() re-checks the same set in case a client forges the tool call.
var previewTools = map[string]bool{
	"preview_dashboard":     true,
	"preview_add_widget":    true,
	"preview_remove_widget": true,
	"preview_update_widget": true,
}

// buildAITools returns the tool list filtered by role. Viewers get read-only
// tools; admin/editor also get the preview_* staging tools.
// Simple tools use slim (description-only) form; complex tools keep full schemas.
func buildAITools(role string) []map[string]any { return buildAIToolsWith(role, false) }

// buildAIToolsWith(role, slimAll): slimAll re-encodes even the complexSchemaTools
// in slim form. Used on read-intent turns, where the preview_* schemas cost ~850
// tokens per call but must stay callable in case the router misclassified —
// dispatch validates the args either way. Exactly two byte-stable variants exist
// (full and all-slim), so both stay provider-cacheable prefixes.
func buildAIToolsWith(role string, slimAll bool) []map[string]any {
	out := make([]map[string]any, 0, len(AllTools()))
	for _, t := range AllTools() {
		name := t["name"].(string)
		if !canWrite(role) && (isWriteTool(name) || previewTools[name]) {
			continue
		}
		if complexSchemaTools[name] && !slimAll {
			out = append(out, toAITool(t))
		} else {
			out = append(out, toAIToolSlim(t))
		}
	}
	return out
}

// callAI sends messages to Groq with the default model. Pass nil tools for a
// plain (no-function-call) request. toolChoice: "" = auto, "required" = force a tool call.
// tokenMeter accumulates total_tokens across every callAIModel invocation. Package-global
// (not per-request) — reset+read it around a known workload (e.g. a live test) to total its cost.
var tokenMeter int64

func resetTokenMeter()      { atomic.StoreInt64(&tokenMeter, 0) }
func loadTokenMeter() int64 { return atomic.LoadInt64(&tokenMeter) }

func callAI(messages []aiMessage, tools []map[string]any, toolChoice string) (*aiResponse, error) {
	resp, _, err := callAIModel(context.Background(), aiModel(), messages, tools, toolChoice)
	return resp, err
}

// callAIModel is callAI with an explicit model and a caller-supplied context (so
// e.g. the intent router can bound its call with a short timeout) — used by the
// bake-off harness to compare candidates too.
// Returns the successful attempt's HTTP round-trip duration (excludes retry sleeps and
// failed attempts) so the bake-off can time model speed, not rate-limit backoff.
func callAIModel(ctx context.Context, model string, messages []aiMessage, tools []map[string]any, toolChoice string) (*aiResponse, time.Duration, error) {
	reqBody := map[string]any{
		"model":                 model,
		"messages":              messages,
		"reasoning_format":      "hidden",
		"max_completion_tokens": aiMaxTokens(),
	}
	if len(tools) > 0 {
		reqBody["tools"] = tools
		if toolChoice != "" {
			// "" = auto, "required"/"none" = string; a leading "{" means a forced-function
			// object (forceFunc) that Groq needs as JSON, not a quoted string.
			if strings.HasPrefix(toolChoice, "{") {
				var tc map[string]any
				if err := json.Unmarshal([]byte(toolChoice), &tc); err == nil {
					reqBody["tool_choice"] = tc
				}
			} else {
				reqBody["tool_choice"] = toolChoice
			}
		}
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, err
	}

	httpClient := &http.Client{Timeout: 90 * time.Second}

	const maxAttempts = 3
	lastErr := fmt.Errorf("the AI service is busy (rate limit). Please wait a few seconds and try again")

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", aiBaseURL(), bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+config.Env.AIApiKey)

		attemptStart := time.Now() // time THIS attempt's round-trip only
		httpResp, err := httpClient.Do(req)
		if err != nil {
			return nil, 0, err
		}
		respBytes, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		attemptLat := time.Since(attemptStart)

		if httpResp.StatusCode == http.StatusTooManyRequests {
			wait := parseRetryAfter(httpResp.Header.Get("Retry-After"), respBytes)
			if wait <= 3*time.Second && attempt < maxAttempts {
				time.Sleep(wait) // quick blip — retry silently
				continue
			}
			return nil, 0, &rateLimitError{seconds: wait.Seconds()} // long wait — surface to user
		}

		var result aiResponse
		if err := json.Unmarshal(respBytes, &result); err != nil {
			return nil, 0, fmt.Errorf("failed to parse AI response: %w", err)
		}
		// ponytail: single choke point for every model call (router/main/verify/repair) —
		// accumulate token usage here so tests/ops can read a run total. Read via loadTokenMeter.
		if result.Usage != nil {
			atomic.AddInt64(&tokenMeter, int64(result.Usage.TotalTokens))
		}
		if result.Error != nil {
			// Groq's function-call parser failed (malformed generation, e.g. gpt-oss
			// leaking a "<|channel|>commentary" token into the tool name). Retry once
			// without tools so the user gets a plain-text reply instead of an error.
			if (strings.Contains(result.Error.Message, "Failed to call a function") ||
				strings.Contains(result.Error.Message, "Tool call validation failed")) && len(tools) > 0 {
				return callAIModel(ctx, model, messages, nil, "")
			}
			return nil, 0, fmt.Errorf("AI API: %s", result.Error.Message)
		}
		return &result, attemptLat, nil
	}

	return nil, 0, lastErr
}

// rateLimitError signals a Groq 429 whose wait is too long to sit on server-side.
// The Chat handler surfaces it as a 429 so the frontend can tell the user to retry.
type rateLimitError struct{ seconds float64 }

func (e *rateLimitError) Error() string {
	return fmt.Sprintf("Rate limit reached. Please try again in %.0fs.", e.seconds)
}

var retryHintRe = regexp.MustCompile(`try again in ([0-9.]+)s`)

func parseRetryAfter(header string, body []byte) time.Duration {
	const maxWait = 30 * time.Second // honor Groq's TPM-window wait (can be ~17s); ceiling guards the 90s client timeout
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

// forceFunc builds an OpenAI/Groq tool_choice object that forces one named function.
// callAIModel detects the leading "{" and sends it as an object rather than a string.
func forceFunc(name string) string {
	return `{"type":"function","function":{"name":"` + name + `"}}`
}

// canWrite reports whether role may perform mutating/preview actions (admin or
// editor). Viewers are read-only — dispatchIntent must never force a preview_*
// tool onto a viewer's tool_choice: their tool set doesn't include it (Task 1
// gating in buildAITools), and forcing a missing function 400s.
func canWrite(role string) bool {
	return role == "admin" || role == "editor"
}

// focusedLineRe / focusedHeaderRe / focused{Machine,Metric,Bucket}Re extract a
// one-line summary of the on-screen [FOCUSED] widget (if any) for ClassifyIntent's
// contextSummary parameter. The dashboard context string is built by the frontend
// (AIAssistantPage.vue buildDashboardContext) as lines shaped like:
//
//   - [FOCUSED] line-chart "Trend" — machine CW-01, metric weight, bucket 1h
//
// focusedContextSummary reformats that into "focused widget: <title> (<type>,
// machine <machine>, metric/bucket <x>)" — the shape router_eval_test.go's
// TestRouterBakeOff cases (Task 2) were scored against.
// customChartRe matches a custom-chart widget line in the frontend dashboard
// context, e.g. `- [FOCUSED] chart "Metrics", ...`. Anchored on the `chart` type
// token so it does NOT match `line-chart "..."`.
var customChartRe = regexp.MustCompile(`(?m)^-\s+(?:\[FOCUSED\]\s+)?chart\s+"`)

var focusedLineRe = regexp.MustCompile(`(?m)^.*\[FOCUSED\].*$`)
var focusedHeaderRe = regexp.MustCompile(`\[FOCUSED\]\s+([\w-]+)\s+"([^"]*)"`)
var focusedMachineRe = regexp.MustCompile(`\bmachine\s+([^\s,]+)`)
var focusedMetricRe = regexp.MustCompile(`\bmetric\s+([^\s,]+)`)
var focusedBucketRe = regexp.MustCompile(`\bbucket\s+([^\s,]+)`)

// focusedContextSummary returns "" when context is empty or carries no [FOCUSED]
// marker, or the marker doesn't match the expected `type "title"` header shape.
func focusedContextSummary(context string) string {
	if context == "" {
		return ""
	}
	line := focusedLineRe.FindString(context)
	if line == "" {
		return ""
	}
	m := focusedHeaderRe.FindStringSubmatch(line)
	if m == nil {
		return ""
	}
	typ, title := m[1], m[2]
	parts := []string{typ}
	if mm := focusedMachineRe.FindStringSubmatch(line); mm != nil {
		parts = append(parts, "machine "+mm[1])
	}
	if mm := focusedMetricRe.FindStringSubmatch(line); mm != nil {
		parts = append(parts, "metric "+mm[1])
	}
	if mm := focusedBucketRe.FindStringSubmatch(line); mm != nil {
		parts = append(parts, "bucket "+mm[1])
	}
	return "focused widget: " + title + " (" + strings.Join(parts, ", ") + ")"
}

// hasMachineSlot reports whether dispatchIntent may safely force a machine-specific
// tool by name: either the router named a machine that resolved in the DB
// (machineValid — checked by the caller, see Chat's slot sanity guard), or no
// machine was named but a focused widget on screen supplies one implicitly.
// Otherwise (no slot and no focus, or a named slot that failed to resolve) forcing
// risks bad/hallucinated args — the caller degrades to tool_choice:"required" so
// the model can fall back to get_machines first.
func hasMachineSlot(res IntentResult, focused bool, machineValid bool) bool {
	if res.Machine == "" {
		return focused
	}
	return machineValid
}

// dispatchIntent is the pure, deterministic replacement for the old regex-based
// tool-choice heuristics (editRe/rangeRe/relDateRe/aggReadRe/bucketRe/compareRe/
// skuRe): the router model classifies (small enum, res/ok), this function decides
// (tool_choice + roundCap) — no I/O, no randomness, unit-testable in isolation.
// machineValid must be pre-computed by the caller (a DB lookup) only when
// res.Machine is non-empty and the intent is machine-specific.
//
// !ok (router failed/declined/ambiguous) is the fallback path: plain tool_choice
// "" (auto) with the same roundCap formula as every other path — indistinguishable
// from having no router at all (pre-router behavior).
// readOnlyIntents are router intents where the turn reads data (or just chats) —
// the preview_* tool schemas are sent slim on these turns (see buildAIToolsWith).
// Edit intents (edit_widget/compare/create_dashboard) and router fallback keep
// the full schemas.
var readOnlyIntents = map[string]bool{
	"chat": true, "read_metric": true, "read_agg": true, "production": true, "alerts": true,
}

func dispatchIntent(res IntentResult, ok bool, focused bool, inlineData bool, role string, machineValid bool, chartExists bool) (toolChoice string, roundCap int) {
	roundCap = 1
	if focused {
		roundCap = 0
	}

	if !ok {
		return "", roundCap
	}

	// Task-1 answer-from-context path, now router-decided: a read the injected
	// context already answers gets no tool call this turn.
	if focused && inlineData && (res.Intent == "chat" || res.Intent == "read_metric" || res.Intent == "read_agg") {
		return "none", roundCap
	}

	switch res.Intent {
	case "chat":
		return "", roundCap
	case "read_metric":
		if hasMachineSlot(res, focused, machineValid) {
			return forceFunc("show_metric"), roundCap
		}
		return "required", roundCap
	case "read_agg":
		if hasMachineSlot(res, focused, machineValid) {
			return forceFunc("get_telemetry_series"), roundCap
		}
		return "required", roundCap
	case "production":
		if hasMachineSlot(res, focused, machineValid) {
			return forceFunc("get_production_count"), roundCap
		}
		return "required", roundCap
	case "alerts":
		// Org-scoped, no machine slot involved — no exception needed.
		return forceFunc("get_active_alerts"), roundCap
	case "edit_widget":
		if !canWrite(role) {
			return "", roundCap
		}
		if focused {
			return forceFunc("preview_update_widget"), roundCap
		}
		return "required", roundCap
	case "compare":
		// A comparison always resolves to a custom chart (type "chart"): update the
		// existing one, or add a new one when the dashboard has none (a line-chart
		// cannot overlay fields, so "focused" alone is not enough to update).
		if !canWrite(role) {
			return "", roundCap
		}
		if chartExists {
			return forceFunc("preview_update_widget"), roundCap
		}
		return forceFunc("preview_add_widget"), roundCap
	case "create_dashboard":
		if canWrite(role) {
			return forceFunc("preview_dashboard"), roundCap
		}
		return "", roundCap
	default:
		// Unreachable: parseIntentResult only returns ok==true for validRouterIntents.
		return "", roundCap
	}
}

// buildAIMessages converts the last few DB messages to Groq/OpenAI format.
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
func buildAIMessages(msgs []Message) []aiMessage {
	// reverse DESC→ASC
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	if len(msgs) > 3 {
		msgs = msgs[len(msgs)-3:]
	}
	var result []aiMessage
	for _, m := range msgs {
		switch m.Role {
		case "user":
			result = append(result, aiMessage{Role: "user", Content: strPtr(m.Content)})
		case "assistant":
			result = append(result, aiMessage{Role: "assistant", Content: strPtr(m.Content)})
		}
	}
	return result
}
