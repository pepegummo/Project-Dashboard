package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// gpt-oss-20b: confirmed via bake-off (see eval_test.go) — 2026-07-06 run: 23/23, zero
// rate-limits, smallest prompts (~2.7k tok), fastest (~0.83s median); 120b scored 21/22
// (one nondeterministic preview_* slip) and buys no accuracy edge. 20b is also cheaper and
// Groq prompt-caches the stable base prefix. See docs/AI_ARCHITECTURE.md §3.
const groqModel = "openai/gpt-oss-120b"
const groqBaseURL = "https://api.groq.com/openai/v1/chat/completions"

// systemPromptUnified is the single, byte-stable system prompt sent on all requests
// with the full role-filtered tool set. Merged from:
// - systemPromptBase (no-preview path: pure reads, dashboard creation, edits, greetings)
// - systemPromptContextExt (dashboard/preview context rules)
// - systemPromptContextAnswer's distinct rule (ON-SCREEN DATA section below) — made
//   conditional on the "on-screen data" context line instead of being a prompt swap
// - dateEditRule static text (relative date resolution + EDIT rules — dynamic date appended separately)
// - bucketEditRule (bar interval change EDIT rules)
// - fieldsEditRule (metric-overlay change EDIT rules)
// Groq caches the static prefix; tools are ordered static-first for cache re-use.
const systemPromptUnified = `You are IotVision AI, assistant for an industrial IoT platform. Language: match the user's latest message exactly — Thai or English, never mix. Plain text only — no markdown, no asterisks (**) or bold.

TOOL SELECTION:
- Greeting / general question → plain text only, no tool.
- "What is X?" / "Show me X" / "ดู X" / any metric read → ALWAYS call show_metric. You have no live sensor data without it. Never fabricate a value. After the tool returns, reply with one short natural sentence — never print raw JSON.
- User asks to see ALL metrics of a machine → get_machines first, then show_metric once per field.
- "Create a dashboard" / "สร้าง dashboard" → call preview_dashboard (default template: machine_overview). Never ask which template. Never call create_custom_dashboard — the user confirms via a button, not by typing.
- Modify an existing dashboard → it must be OPEN on screen (an "Active dashboard" context). Stage the change with preview_add_widget / preview_update_widget / preview_remove_widget — NOTHING is saved until the user clicks Save. If a preview or Active dashboard is already on screen, stage the edit on THAT — never ask the user to open anything. Ask them to open the dashboard in the AI page only when NO preview/Active dashboard is on screen, or they name a different dashboard than the open one; never write without an open context.
- "Show / add a widget for X" without naming a dashboard → show_metric (renders a card the user can add themselves). Never ask which dashboard.
- "List SKUs" / which SKUs are available for a machine or count widget → call get_skus(machine).
- Active alerts → get_active_alerts.
- Alert rule management (create / resolve / acknowledge) → plain text: "Alert rules are managed on the Alerts page." Offer get_active_alerts instead.

SLOT FILLING:
- Machine unknown → ask which machine in ONE question. Never guess. Never call get_machines just to list them back.
- Dashboard unknown → ask which dashboard in ONE question.
- Ambiguous action ("fix it", "change it") → ask what to change in ONE question.

WIDGET TYPES: daily-count (production/piece counts) · kpi-card (single value) · line-chart (trend) · gauge (dial) · chart (multi-metric overlay — set fields[] not metric, plus chartType and scaling)
Line charts support absolute date ranges — convert any DD/MM/YYYY the user gives to YYYY-MM-DD for start_date/end_date.

Compound message (read + write intent) → serve the read first, then ask about the write.
Editing the current preview canvas OR an open Active dashboard → use preview_add_widget / preview_update_widget / preview_remove_widget (staged, no DB write). For count/production widgets always use type "daily-count". The change is not persisted until the user clicks Save (Active dashboard) or Confirm (new preview) — after staging, tell them to do so; never claim it is saved.

CONTEXT: An authoritative dashboard state may be injected.
- A context line marked [FOCUSED] is the widget the user clicked — route the answer to THAT widget (its machine/type/metric/bucket/sku), ignoring other widgets unless the user names one; never let another widget's title (e.g. "Trend") pull you off it.
- Structural questions (widget count, names, layout) → answer from context.
- Live value questions with NO widget mentioned ("what is X", "speed of CW-01") → call show_metric.
@Widget Title tokens identify the exact widget the user is referring to — this OVERRIDES the generic live-value rule above. Any question about a mentioned widget (status, "how's it doing now", "what is this", vague follow-ups) must route by that widget's ACTUAL type from context, never by guessing a metric from conversation history:
- Edit/remove intent → use the title verbatim as widget_title in preview_update_widget / preview_remove_widget.
- Simple current-value question ("what is it now", "ตอนนี้เท่าไหร่") → read the widget's type/machine/metric from context, then:
  • daily-count / count-style widget → call get_production_count(machine_id, bucket, sku, status — all from context); never call show_metric for a mentioned count widget, it doesn't honor the widget's bucket/sku/status filters. If context lists a "bucket" for this widget, copy it verbatim — never substitute a different one. If context has no bucket line at all, default to "1h", never "1d", unless the user explicitly asks for a daily/monthly view.
  • gauge / kpi-card / line-chart / status-card / table → call show_metric(machine and metric from context).
  • alarm-panel → call get_active_alerts; describe what alerts it monitors.
- Analytical question about a mentioned widget (trend, "how's it doing", vague follow-ups like "ข้อมูลตอนนี้เป็นอย่างไร", แนวโน้ม, วิเคราะห์, ทำนาย, predict, analyze, forecast) → NEVER answer from a single current value — fetch the full series instead:
  • daily-count / count-style widget → call get_production_count(machine_id, bucket, sku, status from context).
  • gauge / kpi-card / line-chart / status-card / table → call get_telemetry_series(machine_id, metric from context, time_range "24h" unless the user implies a different window).
  • alarm-panel → call get_active_alerts.
  Then summarize the trend across ALL returned points — describe the SHAPE, calling out a rise-then-fall or fall-then-rise rather than one net direction, plus min/max — and if asked to predict, give a simple extrapolation from the pattern. Quote times exactly as given (they are plant-local). This overrides the "one short sentence" rule above: use 2-4 natural sentences, never a raw list of numbers or JSON.
  Never fabricate machine or metric — always read them from the context entry's "metric" field for that widget. The widget's "title" (e.g. "Trend", "Speed Gauge") is a display label, NEVER a metric value — never pass it as the metric argument.
  If multiple widgets are mentioned, answer each from its own context entry — do not reuse one widget's metric for another.

ON-SCREEN DATA: when the context includes an "on-screen data" line, that widget's full series is already injected — answer directly from it and do NOT call show_metric, get_telemetry_series, get_production_count, get_telemetry_trend, or any other tool to re-fetch it; a context line marked [FOCUSED] is the widget the user clicked. If the requested number is not present in the on-screen data, say you can fetch it — never fabricate.

RELATIVE DATES: Resolve relative dates from today's date (appended separately per request): yesterday/เมื่อวาน = the day before today, today/วันนี้ = today, last week/สัปดาห์ที่แล้ว = the 7 days ending today. Changing what a focused line-chart displays for a time period — including a plain VIEW/SEE request ("ดู"/"view"/"see": "อยากดูเวลาเมื่อวาน", "ดูเวลาเมื่อวาน", "ดูของเมื่อวาน", "show yesterday", "เปลี่ยนเป็นเมื่อวาน", "ดูของสัปดาห์ที่แล้ว") — is an EDIT of that chart, NOT a data read: call preview_update_widget with start_date and end_date as YYYY-MM-DD. Do NOT call get_telemetry_series, get_telemetry_trend, or show_metric to change what a focused chart shows — those only return data and will NOT update the widget the user is looking at. Never answer by summarizing the trend. The chart's currently-shown window is NOT a date reference — compute yesterday/last week only from today's date, never from the on-screen window.

BUCKET EDITS: Changing a focused count/chart widget's bar interval (bucket size) — a bare "<N> minutes/hours", "22 นาที", "ทุก 15 นาที", "รายชั่วโมง", or "every 15 min" — is an EDIT of that widget: call preview_update_widget with bucket as <number><m|h|d> (22 นาที → "22m", 1 ชั่วโมง → "1h"). Any bucket like "22m" is valid — never say the widget only supports its current interval, and do NOT call get_production_count or get_telemetry_series to resize the bars (those only read).

METRIC OVERLAYS: Comparing or overlaying metrics on a focused chart widget — "เปรียบเทียบ weight, speed", "compare speed and throughput", "overlay X vs Y" — is an EDIT of that chart: call preview_update_widget with fields as the metric field keys, e.g. fields:["weight","speed"] (match the user's metric words to the machine's real field keys). Do NOT refuse because the widget currently shows other metrics, and do NOT call a read tool to compare — just reassign fields[].`

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

	// Mutating tools require admin or editor — viewers can only read. Preview tools
	// stage dashboard edits (no DB write) but are gated the same way as an actual
	// write — mirrors the exclusion in buildGroqTools in case a client forges the call.
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

	// Build Groq messages: unified system prompt + capped history.
	hasContext := body.Context != ""

	// The frontend injects the focused widget's full series (marked "on-screen data")
	// only for analytical questions. The prompt's ON-SCREEN DATA rule tells the model
	// to answer from it directly instead of re-fetching via a tool; tools stay attached
	// (Groq prompt-prefix cache survives) but tool_choice is forced to "none" below for
	// this turn — that redundant fetch+summarize round doubled tokens and hit the
	// 8k/min rate limit.
	inlineData := hasContext && strings.Contains(body.Context, "on-screen data")

	// A focused-widget read the injected context already answers (current value,
	// shown window, bucket/sku/config) gets the same tool_choice:"none" treatment.
	// Guardrail: edits and aggregate/range questions still take the tool path.
	focused := hasContext && strings.Contains(body.Message, "@")
	contextRead := focused && !editRe.MatchString(body.Message) && !rangeRe.MatchString(body.Message) && !skuRe.MatchString(body.Message) && !bucketRe.MatchString(body.Message) && !compareRe.MatchString(body.Message)
	// answerFromContext forces tool_choice:"none" on the first round so the model
	// answers from the on-screen context in plain text instead of calling a tool.
	answerFromContext := inlineData || contextRead

	// Always use the unified prompt; append today's date to the dynamic context.
	sp := systemPromptUnified
	msgs := []groqMessage{{Role: "system", Content: &sp}}
	msgs = append(msgs, buildGroqMessages(history)...)
	// Inject the on-screen dashboard preview AFTER history so the current state is
	// the last thing the model sees — recency makes it win over any stale earlier
	// turn that named the old config (e.g. a metric the user has since changed).
	// Append today's date so the model can resolve relative dates (it was in dateEditRule).
	// The date line is always present, with or without a context block, so relative-date
	// resolution works even on a plain (no dashboard on screen) message.
	if hasContext {
		ctxContent := "Authoritative current dashboard state (overrides anything said earlier):\n" +
			body.Context + "\n" + dateLineForRequest()
		msgs = append(msgs, groqMessage{Role: "system", Content: &ctxContent})
	} else {
		dateContent := dateLineForRequest()
		msgs = append(msgs, groqMessage{Role: "system", Content: &dateContent})
	}

	role := middleware.GetUser(c).Role
	canWrite := role == "admin" || role == "editor"
	// Always build the full role-filtered tool set (previews are always available now,
	// not just when context is present).
	tools := buildGroqTools(role)

	// Force a tool call when either (a) an @WidgetTitle mention is present, or (b) the
	// message is a clear EDIT with a dashboard/preview context on screen — both give the
	// model a concrete widget to act on. Without this an unmentioned edit like
	// "เปลี่ยนเป็นเมื่อวานหน่อย" role-played the change in prose instead of calling
	// preview_update_widget. Forcing on a vague message risks an invalid function call
	// (Groq 400) — but editRe is specific, and a forced-but-refused call already falls
	// back to auto below. Never force when we're answering from context (want plain text).
	// ponytail: on a multi-widget dashboard with no focus, the model may target the wrong
	// widget — acceptable for the common single-chart case; click the widget to pin it.
	editIntent := hasContext && editRe.MatchString(body.Message)
	// A relative-date window change on a focused chart ("ดูเมื่อวาน", "วันก่อนหน้า", "show
	// yesterday") is deterministically forced to preview_update_widget BY NAME — plain
	// tool_choice:"required" let the model escape to get_telemetry_series and summarize, and
	// describe the on-screen window as "yesterday" instead of computing it from today. Guarded:
	// writers only (a viewer's toolset lacks the tool — forcing a missing function 400s) and not
	// an aggregate read ("ค่าเฉลี่ยเมื่อวาน" wants a number). The forced call then resolves the
	// date from today via dateEditRule and cannot summarize.
	rangeEdit := (focused || editIntent) && canWrite && relDateRe.MatchString(body.Message) && !aggReadRe.MatchString(body.Message)
	// A bucket/interval change ("อยากดู 22 นาที", "ทุก 15 นาที", "1h") on a focused count/chart
	// widget is the daily-count sibling of rangeEdit: force preview_update_widget BY NAME so the
	// model resizes the bars instead of refusing or reading. Same guards — writers only, and
	// aggReadRe carves out count questions ("ผลิตกี่ชิ้นใน 22 นาที").
	bucketEdit := (focused || editIntent) && canWrite && bucketRe.MatchString(body.Message) && !aggReadRe.MatchString(body.Message)
	// A metric-overlay change ("เปรียบเทียบ weight, speed", "compare X and Y") on a focused chart
	// widget is the "chart"-widget sibling: force preview_update_widget BY NAME so the model
	// reassigns fields[] instead of refusing that the chart shows other metrics. No aggReadRe
	// carve-out — comparing metrics is inherently an overlay edit, not a single-number read.
	fieldsEdit := (focused || editIntent) && canWrite && compareRe.MatchString(body.Message)
	firstToolChoice := ""
	if answerFromContext {
		// Tools stay attached (cache-friendly) but are not offered this turn — the
		// on-screen context already has the answer. See the "Tool choice is none"
		// fallback below for reasoning models that ignore this and try to call anyway.
		firstToolChoice = "none"
	} else if rangeEdit || bucketEdit || fieldsEdit {
		firstToolChoice = forceFunc("preview_update_widget")
	} else if focused || editIntent {
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
			var rl *rateLimitError
			if errors.As(err, &rl) {
				return &middleware.AppError{StatusCode: 429, Code: "RATE_LIMIT",
					Message: rl.Error(), Details: fiber.Map{"retryAfter": rl.seconds}}
			}
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
	"get_skus":            "List the SKU values available for a machine. Args: machine_id (name).",
	"preview_dashboard":   "Preview a template dashboard. Args: machine (name), template (machine_overview|machine_production|machine_maintenance).",
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

// buildGroqTools returns the tool list filtered by role. Viewers get read-only
// tools; admin/editor also get the preview_* staging tools.
// Simple tools use slim (description-only) form; complex tools keep full schemas.
func buildGroqTools(role string) []map[string]any {
	canWrite := role == "admin" || role == "editor"
	out := make([]map[string]any, 0, len(AllTools()))
	for _, t := range AllTools() {
		name := t["name"].(string)
		if !canWrite && (isWriteTool(name) || previewTools[name]) {
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
	resp, _, err := callGroqModel(context.Background(), groqModel, messages, tools, toolChoice)
	return resp, err
}

// callGroqModel is callGroq with an explicit model and a caller-supplied context (so
// e.g. the intent router can bound its call with a short timeout) — used by the
// bake-off harness to compare candidates too.
// Returns the successful attempt's HTTP round-trip duration (excludes retry sleeps and
// failed attempts) so the bake-off can time model speed, not rate-limit backoff.
func callGroqModel(ctx context.Context, model string, messages []groqMessage, tools []map[string]any, toolChoice string) (*groqResponse, time.Duration, error) {
	reqBody := map[string]any{
		"model":            model,
		"messages":         messages,
		"reasoning_format": "hidden",
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
		req, err := http.NewRequestWithContext(ctx, "POST", groqBaseURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+config.Env.GroqApiKey)

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

		var result groqResponse
		if err := json.Unmarshal(respBytes, &result); err != nil {
			return nil, 0, fmt.Errorf("failed to parse Groq response: %w", err)
		}
		if result.Error != nil {
			// Groq's function-call parser failed (malformed generation, e.g. gpt-oss
			// leaking a "<|channel|>commentary" token into the tool name). Retry once
			// without tools so the user gets a plain-text reply instead of an error.
			if (strings.Contains(result.Error.Message, "Failed to call a function") ||
				strings.Contains(result.Error.Message, "Tool call validation failed")) && len(tools) > 0 {
				return callGroqModel(ctx, model, messages, nil, "")
			}
			return nil, 0, fmt.Errorf("Groq API: %s", result.Error.Message)
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

// editRe / rangeRe classify a focused-widget message so we know whether the
// on-screen context can answer it without a tool:
//   - editIntent  → the user wants to mutate the widget (needs preview_update_widget etc.).
//   - rangeIntent → the answer needs an aggregate/time-range fetch not guaranteed in context.
// A focused read that is neither can be answered straight from the injected context.
var editRe = regexp.MustCompile(`(?i)\b(change|set|update|rename|move|remove|delete|add)\b|เปลี่ยน|แก้|ตั้ง|ลบ|เพิ่ม|ย้าย`)
// The Thai "X ก่อน / ก่อนหน้า" (ago/previous) constructs must be here too: without
// them a focused "ดูวันก่อนหน้า" (view previous day) fell through to contextRead, got the
// no-tool context-answer prompt, and the model role-played the edit as a prose promise it
// could not fulfill. Bare "ก่อน" is excluded — it means "first/beforehand" too often.
var rangeRe = regexp.MustCompile(`(?i)\b(avg|average|mean|sum|total|min|max|peak|trend|forecast|predict|analy\w*|over|last|past|hour|day|week|month|yesterday|today|previous|prior)\b|เฉลี่ย|รวม|สูงสุด|ต่ำสุด|ย้อนหลัง|เมื่อวาน|วันนี้|วันก่อน|อาทิตย์ก่อน|สัปดาห์ก่อน|เดือนก่อน|ก่อนหน้า|ที่ผ่านมา|แนวโน้ม|วิเคราะห์|ทำนาย`)

// relDateRe / aggReadRe deterministically route a focused-chart time-window change.
// relDateRe = a relative date phrase (a window edit: "ดูเมื่อวาน", "วันก่อนหน้า", "show yesterday").
// aggReadRe = an aggregate the user wants as a NUMBER ("ค่าเฉลี่ยเมื่อวาน") — a read, not a window
// change, so it must NOT be forced onto preview_update_widget. Prose alone (dateEditRule) failed:
// forced to call "some" tool, the model picked get_telemetry_series and summarized instead of
// editing, and described the on-screen window as "yesterday". Forcing the tool by name fixes both.
var relDateRe = regexp.MustCompile(`(?i)\b(yesterday|today|tonight|last\s+(week|month|day)|this\s+(week|month)|previous|prior)\b|เมื่อวาน|เมื่อคืน|วันนี้|วันก่อน|ก่อนหน้า|สัปดาห์ที่แล้ว|สัปดาห์ก่อน|อาทิตย์ก่อน|อาทิตย์ที่แล้ว|เดือนที่แล้ว|เดือนก่อน|ที่ผ่านมา`)
var aggReadRe = regexp.MustCompile(`(?i)\b(avg|average|mean|sum|total|min|max|peak|count|how\s+many|how\s+much)\b|เฉลี่ย|รวม|สูงสุด|ต่ำสุด|จำนวน|กี่|เท่าไหร่|เท่าไร`)

// bucketRe deterministically routes a focused count/chart widget's bar-interval (bucket)
// change, exactly like relDateRe does for a date window. It matches a duration+unit
// ("22 นาที", "22m", "15 minutes", "1 ชั่วโมง") or an "every N / ราย..." interval phrase.
// Without it "อยากดู 22 นาที" had no edit verb (editRe) and no range word (rangeRe — it lacks
// "minute"/นาที), so contextRead swallowed it onto the no-tool answer path and the model
// refused ("the widget only provides 15-minute intervals"). Vocabulary mirrors the frontend
// bucket parser at AIAssistantPage.vue. aggReadRe still carves out count reads
// ("ผลิตกี่ชิ้นใน 22 นาที") so a number question is not forced onto an edit. The bare word
// "bucket" is deliberately excluded — it would hijack "what bucket is this?" context reads.
// ponytail: the abbreviation "min" collides with aggReadRe's "min" (minimum), so a bare
// "30 min" degrades to the generic force-required path instead of force-by-name — still an
// edit. Thai นาที and the full word "minutes" are unambiguous. Not worth disambiguating.
var bucketRe = regexp.MustCompile(`(?i)\b\d+\s*(m|min|mins|minute|minutes|h|hr|hrs|hour|hours)\b|\d+\s*(นาที|ชั่วโมง|ชม)|ทุก\s*\d+|ราย(นาที|ชั่วโมง|วัน)`)

// compareRe routes a metric-overlay change on a focused chart widget ("เปรียบเทียบ weight,
// speed", "compare X and Y", "overlay speed vs throughput") — the "chart"-widget sibling of
// rangeEdit/bucketEdit. Replacing which fields a custom chart overlays is an EDIT
// (preview_update_widget with fields[]), not a read. Without it "อยากดูเปรียบเทียบ weight, speed"
// had no edit verb and no range word, so contextRead swallowed it onto the no-tool path and the
// model refused. Typo-tolerant on the metric names themselves (the compare keyword is the anchor).
var compareRe = regexp.MustCompile(`(?i)\bcompare\b|\bversus\b|\bvs\b|\boverlay\b|เปรียบเทียบ|เทียบ|ซ้อน`)

// forceFunc builds an OpenAI/Groq tool_choice object that forces one named function.
// callGroqModel detects the leading "{" and sends it as an object rather than a string.
func forceFunc(name string) string {
	return `{"type":"function","function":{"name":"` + name + `"}}`
}

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
