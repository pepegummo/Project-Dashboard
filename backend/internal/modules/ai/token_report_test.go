package ai

// Shared per-case token-report writer for the live eval suites (router, /ask). Mirrors the
// Markdown table shape TestChatFullLoopLive already emits (see writeChatFullLoopReport), so all
// three results docs under llm2viz/ read the same way. Token totals come from the package-global
// tokenMeter (controller.go) — reset it before each metered unit and load it after.

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// writeSuiteTokenReport renders a titled Markdown table (header + rows) plus a trailing summary
// line, logs it, and writes it to path. path is package-dir-relative (e.g.
// "../../../../llm2viz/router-eval-results.md"), the same anchor liveKeyOrSkip uses for .env.
// A write error is logged, never fatal — reporting must not fail a passing test run.
func writeSuiteTokenReport(t *testing.T, path, title string, header []string, rows [][]string, totalTok int64, dur time.Duration) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — %s\n\n", title, time.Now().Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "Model: `%s` · router/judge: `%s` · provider: `%s`\n\n", aiModel(), routerModel(), aiBaseURL())
	fmt.Fprintf(&b, "| %s |\n", strings.Join(header, " | "))
	fmt.Fprintf(&b, "|%s\n", strings.Repeat("---|", len(header)))
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s |\n", strings.Join(r, " | "))
	}
	fmt.Fprintf(&b, "\n**TOTAL: %d rows · %d tokens · %.1fs**\n", len(rows), totalTok, dur.Seconds())

	t.Logf("\n%s", b.String())
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Logf("could not write report to %s: %v", path, err)
	} else {
		t.Logf("report written to %s", path)
	}
}
