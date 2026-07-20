package ai

// Full-loop /ai/chat integration test: the REAL Chat handler driven over app.Test —
// HTTP parse → persist user msg → router (ClassifyIntent) → dispatchIntent →
// callAIModel loop → runToolRound → dispatch (LIVE tool execution against TimescaleDB)
// → verifyAndMaybeRepair → persisted []Message + intent JSON. This is the path a real
// user's /ai chat request takes, minus only JWT verification (auth is stubbed by
// injecting the same Locals the middleware would).
//
// complex_flows_live_test.go only exercises Chat's DECISION path and FABRICATES tool
// results (nil DB). This file is the missing half: real tool execution, org-scoping,
// conversation persistence, and the verify/repair phase — none of which had ever run
// in a test.
//
// Needs BOTH a live AI key (liveKeyOrSkip) and a reachable DATABASE_URL with seeded
// org data (machine "CW-01" + a user in that org); skips fast otherwise. Run:
//   cd backend && go test ./internal/modules/ai/ -run TestChatFullLoopLive -v -timeout 30m

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"iot-dashboard/internal/database"
	"iot-dashboard/internal/middleware"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

// chatResponse mirrors Chat's fiber.Map response shape.
type chatResponse struct {
	Success bool      `json:"success"`
	Data    []Message `json:"data"`
	Intent  struct {
		OK      bool   `json:"ok"`
		Intent  string `json:"intent"`
		Machine string `json:"machine"`
	} `json:"intent"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

type chatCase struct {
	name    string
	message string
	// wantTool: at least one executed role:"tool" row must carry one of these tool names.
	// Empty => a no-tool prose turn (must have NONE).
	wantTool []string
}

var chatCases = []chatCase{
	// read_metric — resolveMachineID must resolve CW-01, show_metric executes (get_machines
	// tolerated as a fan-out first step).
	{name: "read_metric_speed_th", message: "speed ของ CW-01 เท่าไหร่", wantTool: []string{"show_metric", "get_machines"}},
	// org-scoped alerts — no machine slot.
	{name: "active_alerts_th", message: "ตอนนี้มีแจ้งเตือนอะไรบ้าง", wantTool: []string{"get_active_alerts"}},
	// production count.
	{name: "production_today_th", message: "ผลิตกี่ชิ้นวันนี้ CW-01", wantTool: []string{"get_production_count", "get_machines"}},
	// write/preview path (admin role) — stages a widget, no DB write.
	{name: "preview_add_gauge_th", message: "เพิ่ม gauge วัด temperature ของ CW-01", wantTool: []string{"preview_add_widget", "get_machines"}},
	// prose / no-tool — assistant answers directly, verify/repair skipped.
	{name: "greeting_th", message: "สวัสดีครับ", wantTool: nil},
}

func TestChatFullLoopLive(t *testing.T) {
	liveKeyOrSkip(t)

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping full-loop test")
	}
	ctx := context.Background()
	// Direct pool, not database.Connect — that helper retries 15×3s on a down DB.
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("DB config invalid: %v", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		t.Skipf("DB unreachable: %v", err)
	}
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil; pool.Close() })

	// Same org-join chain the AskData harness uses (machines has no org column).
	var orgID string
	if err := pool.QueryRow(ctx,
		`SELECT f.organization_id::text
		 FROM machines m
		 JOIN production_lines pl ON pl.id = m.production_line_id
		 JOIN factories f         ON f.id = pl.factory_id
		 WHERE m.name ILIKE '%CW-01%' LIMIT 1`,
	).Scan(&orgID); err != nil {
		t.Skipf("seeded machine CW-01 not found (run backfill first): %v", err)
	}

	// ai_conversations.user_id is a FK to users(id), so Sub must be a REAL user in the
	// org. Prefer an admin/editor so the preview/write tools are reachable.
	var userSub, role string
	if err := pool.QueryRow(ctx,
		`SELECT id::text, role FROM users
		 WHERE organization_id = $1 AND role IN ('admin','editor')
		 ORDER BY role LIMIT 1`, orgID,
	).Scan(&userSub, &role); err != nil {
		t.Skipf("no admin/editor user in org %s: %v", orgID, err)
	}

	ctrl := NewController()

	// One conversation for the whole run (messages cascade-delete with it).
	conv, err := ctrl.repo.CreateConversation(ctx, userSub, "chat-fullloop-test")
	if err != nil {
		t.Skipf("could not create conversation: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM ai_conversations WHERE id=$1`, conv.ID)
	})

	// Wire the SAME global error handler production uses (main.go). Without it, a
	// handler that returns *AppError (e.g. a 502 on a provider outage) falls through
	// to Fiber's default handler — status 500 + the plaintext "[AI_ERROR] ..." body —
	// so a plain quota outage decoded as a confusing JSON-parse crash instead of the
	// real 502 the frontend actually receives.
	app := fiber.New(fiber.Config{ErrorHandler: middleware.ErrorHandler})
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &middleware.JwtClaims{Sub: userSub, OrgId: orgID, Role: role})
		return c.Next()
	})
	app.Post("/ai/chat", ctrl.Chat)

	var stats []chatStat
	runStart := time.Now()

	for _, tc := range chatCases {
		t.Run(tc.name, func(t *testing.T) {
			pace()

			raw, _ := json.Marshal(map[string]any{
				"conversationId": conv.ID,
				"message":        tc.message,
			})
			req := httptest.NewRequest("POST", "/ai/chat", bytes.NewReader(raw))
			req.Header.Set("Content-Type", "application/json")

			resetTokenMeter()
			caseStart := time.Now()
			// 90s: Chat's agentic loop + verify/repair can chain several model calls.
			resp, err := app.Test(req, 90000)
			caseDur := time.Since(caseStart)
			caseTokens := loadTokenMeter()
			outcome := "PASS"
			defer func() {
				if t.Failed() {
					outcome = "FAIL"
				}
				stats = append(stats, chatStat{tc.name, caseTokens, caseDur, outcome})
			}()
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			defer resp.Body.Close()
			var out chatResponse
			if derr := json.NewDecoder(resp.Body).Decode(&out); derr != nil {
				t.Fatalf("bad response JSON (status %d): %v", resp.StatusCode, derr)
			}
			if resp.StatusCode != 200 || !out.Success {
				t.Fatalf("status=%d success=%v err=%q", resp.StatusCode, out.Success, out.Error.Message)
			}

			// Collect executed tool rows + the final assistant prose.
			var toolNames []string
			var assistantContent string
			for _, m := range out.Data {
				switch m.Role {
				case "tool":
					if m.ToolName != nil {
						toolNames = append(toolNames, *m.ToolName)
					}
				case "assistant":
					assistantContent = m.Content // trailing assistant row wins
				}
			}

			// Every turn must end with a non-empty prose answer.
			if strings.TrimSpace(assistantContent) == "" {
				t.Errorf("no non-empty assistant answer (tools=%v)", toolNames)
			}

			if len(tc.wantTool) == 0 {
				if len(toolNames) > 0 {
					t.Errorf("expected a no-tool prose turn, but tools executed: %v", toolNames)
				}
				return
			}

			// At least one executed tool must be in the allowed set.
			if !containsAny(toolNames, tc.wantTool) {
				t.Errorf("executed tools %v, want at least one of %v", toolNames, tc.wantTool)
			}
		})
	}

	writeChatFullLoopReport(t, stats, time.Since(runStart))
}

type chatStat struct {
	name   string
	tokens int64
	dur    time.Duration
	result string
}

// writeChatFullLoopReport dumps a per-case token + latency table (and grand total) both
// to the test log and to llm2viz/chat-fullloop-results.md, next to the /ask results doc.
func writeChatFullLoopReport(t *testing.T, stats []chatStat, total time.Duration) {
	t.Helper()
	var totalTok int64
	var b strings.Builder
	fmt.Fprintf(&b, "# /ai chat full-loop live results — %s\n\n", time.Now().Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "Model: `%s` · router/judge: `%s` · provider: `%s`\n\n", aiModel(), routerModel(), aiBaseURL())
	fmt.Fprintln(&b, "| case | result | total_tokens | time |")
	fmt.Fprintln(&b, "|---|---|---|---|")
	for _, s := range stats {
		totalTok += s.tokens
		fmt.Fprintf(&b, "| %s | %s | %d | %.1fs |\n", s.name, s.result, s.tokens, s.dur.Seconds())
	}
	fmt.Fprintf(&b, "| **TOTAL** | %d cases | **%d** | **%.1fs** |\n", len(stats), totalTok, total.Seconds())

	t.Logf("\n%s", b.String())
	// Repo-root relative from the package dir, same anchor liveKeyOrSkip uses for .env.
	path := "../../../../llm2viz/chat-fullloop-results.md"
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Logf("could not write report to %s: %v", path, err)
	} else {
		t.Logf("report written to %s", path)
	}
}

func containsAny(got, want []string) bool {
	set := map[string]bool{}
	for _, g := range got {
		set[g] = true
	}
	for _, w := range want {
		if set[w] {
			return true
		}
	}
	return false
}
