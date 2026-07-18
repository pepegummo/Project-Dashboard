package ai

// Ask-Data feature: natural language → hardened SQL → rows → LLM-authored ECharts
// option. Separate from the structured-tool chat path. Security model (see the plan
// and migrate.go views): the LLM may only reference org-scoped views; every query
// runs in a read-only tx with statement_timeout and the app.current_org GUC set from
// the caller's JWT, so a generated query can neither write nor escape its org.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"iot-dashboard/internal/database"
	"iot-dashboard/internal/middleware"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// allowedViews are the only relations a generated query may read.
var allowedViews = []string{"v_telemetry", "v_machines", "v_machine_fields"}

// sqlForbidden fast-fails on write/DDL keywords. The read-only tx is the true guard;
// this is defense-in-depth + a clearer error than a Postgres failure.
// ponytail: whole-word scan, not a parser. Add pg_query_go only if we allow richer SQL.
var sqlForbidden = regexp.MustCompile(`(?i)\b(insert|update|delete|drop|alter|create|grant|revoke|truncate|copy|merge|into|call|do)\b`)

// deniedTables blocks any reference to a real base table (or system catalog) — the
// only way to read cross-org data. Allowed v_ views are scrubbed out before scanning
// so "v_machines" doesn't trip the "machines" rule.
var deniedTables = regexp.MustCompile(`(?i)\b(telemetry_raw|telemetry_aggregates|machines|machine_fields|users|organizations|factories|production_lines|dashboards|dashboard_widgets|alerts|alert_events|ai_boards|ai_board_charts|ai_messages|ai_conversations|ai_preview_drafts|user_organizations|audit_logs|information_schema|pg_[a-z_]+)\b`)

// validateSQL enforces: single SELECT, no write keywords, no base-table access.
// Returns the trimmed, semicolon-free query on success.
func validateSQL(sqlText string) (string, error) {
	s := strings.TrimSpace(sqlText)
	s = strings.TrimSuffix(strings.TrimSpace(s), ";")
	if s == "" {
		return "", errors.New("empty SQL")
	}
	if strings.Contains(s, ";") {
		return "", errors.New("only a single statement is allowed")
	}
	low := strings.ToLower(s)
	if !strings.HasPrefix(low, "select") {
		return "", errors.New("only SELECT queries are allowed")
	}
	if sqlForbidden.MatchString(low) {
		return "", errors.New("query contains a disallowed keyword")
	}
	scrub := low
	for _, v := range allowedViews {
		scrub = strings.ReplaceAll(scrub, v, " ")
	}
	if m := deniedTables.FindString(scrub); m != "" {
		return "", fmt.Errorf("relation %q is not queryable — use the v_ views", m)
	}
	return s, nil
}

// runScoped executes a validated SELECT for one org inside a read-only transaction.
// Org isolation comes from the app.current_org GUC (the views filter on it); writes
// are blocked by the read-only tx; runaway queries by statement_timeout + a row cap.
func runScoped(ctx context.Context, orgID, sqlText string) (cols []string, rows [][]any, err error) {
	const maxRows = 5000
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	conn, err := database.Pool.Acquire(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer conn.Release()

	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	if _, err = tx.Exec(ctx, "SET LOCAL statement_timeout = '5s'"); err != nil {
		return nil, nil, err
	}
	// is_local=true → scoped to this tx, cleared on rollback.
	if _, err = tx.Exec(ctx, "SELECT set_config('app.current_org', $1, true)", orgID); err != nil {
		return nil, nil, err
	}

	r, err := tx.Query(ctx, sqlText)
	if err != nil {
		return nil, nil, err
	}
	defer r.Close()

	for _, fd := range r.FieldDescriptions() {
		cols = append(cols, string(fd.Name))
	}
	for r.Next() {
		if len(rows) >= maxRows {
			break
		}
		vals, verr := r.Values()
		if verr != nil {
			return nil, nil, verr
		}
		rows = append(rows, vals)
	}
	if err = r.Err(); err != nil {
		return nil, nil, err
	}
	return cols, rows, nil
}

// ── LLM calls ────────────────────────────────────────────────────────────────

var emitSQLTool = map[string]any{
	"name":        "emit_sql",
	"description": "Return a single read-only Postgres SELECT that answers the user's question.",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"answerable", "sql"},
		"properties": map[string]any{
			"answerable":    map[string]any{"type": "boolean", "description": "false ONLY for a greeting, chit-chat, clearly non-factory input, or a question about a previous chart/result itself (how it was computed, its interval) — then leave sql empty. A data-listing question ('which SKUs', 'what machines', 'list values') is answerable=true."},
			"sql":           map[string]any{"type": "string", "description": "One SELECT over the allowed v_ views. No semicolons, no CTEs, no writes. Always include a LIMIT."},
			"clarification": map[string]any{"type": "string", "description": "Set ONLY when the question IS about factory data but you cannot determine WHAT to query — no identifiable metric/machine/dimension. ONE short question in the user's language, offering concrete choices from the schema. Leave empty when a sensible default exists (no time range → assume last 24h). Never set together with sql."},
		},
	},
}

// errNotDataQuestion signals the input wasn't a factory-data question (greeting, etc.).
var errNotDataQuestion = errors.New("that doesn't look like a question about your factory data — try asking about machine speed, counts, or trends")

var emitEChartTool = map[string]any{
	"name":        "emit_echart_option",
	"description": "Return an ECharts option object (no data — a dataset is injected at render time) that best visualizes the result.",
	"input_schema": map[string]any{
		"type":     "object",
		"required": []string{"option"},
		"properties": map[string]any{
			"option": map[string]any{"type": "object", "description": "A complete ECharts option: title, tooltip, xAxis, yAxis, legend, series[] with type and encode. Reference result columns by name via encode; do NOT embed data or dataset."},
		},
	},
}

// buildSchemaContext describes the queryable views plus this org's machine names and
// metric keys, so the SQL model targets real fields. Reuses runScoped for org scoping.
func buildSchemaContext(ctx context.Context, orgID string) string {
	var b strings.Builder
	b.WriteString(`You may ONLY query these read-only Postgres + TimescaleDB views (org data is already filtered):

v_telemetry(machine_id uuid, machine_name text, ts timestamptz, data jsonb)
  - one row per reading; metric values live in `)
	b.WriteString("`data`")
	b.WriteString(` JSONB. Read a metric as (data->>'<key>')::float.
v_machines(id uuid, name text, type text, status text)
v_machine_fields(machine_id uuid, machine_name text, key text, label text, unit text)

`)

	if _, rows, err := runScoped(ctx, orgID, "SELECT name FROM v_machines ORDER BY name LIMIT 200"); err == nil && len(rows) > 0 {
		names := make([]string, 0, len(rows))
		for _, r := range rows {
			names = append(names, fmt.Sprint(r[0]))
		}
		b.WriteString("Machines: " + strings.Join(names, ", ") + "\n")
	}
	if _, rows, err := runScoped(ctx, orgID, "SELECT DISTINCT key, label, COALESCE(unit,'') FROM v_machine_fields ORDER BY key LIMIT 200"); err == nil && len(rows) > 0 {
		keys := make([]string, 0, len(rows))
		for _, r := range rows {
			keys = append(keys, fmt.Sprintf("%v (%v %v)", r[0], r[1], r[2]))
		}
		b.WriteString("Metric keys (data->>'key'): " + strings.Join(keys, ", ") + "\n")
	}
	b.WriteString("The data JSONB also holds TEXT dimensions (not numeric), notably `sku` (product/SKU code) — read as data->>'sku'. List available values with SELECT DISTINCT data->>'sku'.\n")

	b.WriteString(`
Rules:
- Exactly ONE SELECT. No semicolons, no CTEs, no INSERT/UPDATE/DELETE/DDL.
- Any question about a time range or trend ("last N hours/days", "over time", "per hour", "trend", "history") MUST return a time series: GROUP BY time_bucket('<interval>', ts) AS bucket and ORDER BY bucket, giving many rows — never a single scalar. Pick the interval so a 24h window yields ~24 points (1 hour), a 7d window ~7 (1 day).
- A relative window ("past/last N units", "recent", "latest", or the same in other languages — e.g. Thai "ย้อนหลัง N", "ล่าสุด") MUST be bounded with WHERE ts > now() - interval 'N <unit>'. ALWAYS use now() — never hardcode a date, never leave the window unbounded. now() is the implicit upper bound, so no end filter is needed. Questions in any language map to this same SQL.
- Numeric metric: (data->>'speed')::float. When reading a metric, ALWAYS filter AND data->>'<key>' IS NOT NULL — machines that never report that metric must not appear in the result.
- A "which/what <dimension> are available / exist / does X run" question (e.g. SKUs) is a listing query, NOT a time series: SELECT DISTINCT data->>'sku' AS sku FROM v_telemetry WHERE data->>'sku' IS NOT NULL ORDER BY sku (filter by machine with machine_name ILIKE '%<code>%').
- Match a machine by name with ILIKE '%<code>%' (e.g. machine_name ILIKE '%CW-01%'), NEVER exact =. Names include a descriptive prefix, so the user's "CW-01" is stored as "Checkweigher CW-01". Same for v_machines.name.
- Give columns clear aliases (bucket, machine_name, avg_speed, ...). Always add LIMIT (<= 5000). Aggregate raw readings into buckets or groups rather than returning every row.`)
	return b.String()
}

// prevTurn carries the immediately-previous Ask-Data turn so a follow-up ("make it a
// bar chart", "group by day", "เอาเป็นกราฟแท่ง") can refine it instead of being rejected.
// SQL and Clarification are mutually exclusive: a data turn sets SQL, a clarification
// turn (B3) sets Clarification — never both.
type prevTurn struct {
	Question      string
	SQL           string
	Clarification string
}

// sqlFixup carries a failed SQL attempt and its error for the retry loop in AskData.
type sqlFixup struct {
	SQL string
	Err string
}

// sqlEmission is emit_sql's parsed result: either SQL (answerable) or Clarification
// (answerable but under-specified — B3), never both. See parseSQLEmission.
type sqlEmission struct {
	SQL           string
	Clarification string
}

func emitSQL(ctx context.Context, question, schema string, prev *prevTurn, fixup *sqlFixup) (sqlEmission, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	sp := "You translate a factory-analytics question into ONE read-only Postgres SELECT by calling emit_sql. Never reply in prose.\n\n" +
		"Example — \"avg speed last 24h for CW-01\":\n" +
		"SELECT time_bucket('1 hour', ts) AS bucket, avg((data->>'speed')::float) AS avg_speed " +
		"FROM v_telemetry WHERE machine_name ILIKE '%CW-01%' AND ts > now() - interval '24 hours' " +
		"AND data->>'speed' IS NOT NULL GROUP BY bucket ORDER BY bucket LIMIT 5000\n\n" +
		"Example — \"which SKUs does CW-01 run\" (a listing question, answerable=true):\n" +
		"SELECT DISTINCT data->>'sku' AS sku FROM v_telemetry WHERE machine_name ILIKE '%CW-01%' " +
		"AND data->>'sku' IS NOT NULL ORDER BY sku LIMIT 100\n\n" +
		"A \"which/what values are available\" listing question IS answerable — return the distinct values; " +
		"set answerable=false ONLY for a greeting or chit-chat.\n\n" +
		"A question asking to EXPLAIN or DEFINE a metric or term (\"what does X mean\", \"how do X and Y " +
		"differ\", Thai \"อธิบาย\", \"คืออะไร\", \"ต่างกันยังไง\") is answered in prose, not SQL — set " +
		"answerable=false and NEVER set clarification for it.\n" +
		"When a reasonable default interpretation exists, answer with the default instead of asking: no time " +
		"range → last 24h; a fuzzy condition like \"drops/low\" → below that metric's average over the window. " +
		"Set clarification ONLY when no metric, machine, or dimension is identifiable at all.\n\n"
	switch {
	case prev != nil && prev.SQL != "":
		sp += "The user previously asked: \"" + prev.Question + "\"\nwhich ran this SQL:\n" + prev.SQL +
			"\nIf the new message refines or restyles that chart (a different chart type, grouping, interval, " +
			"filter, or metric) rather than starting a new topic, adapt the previous SQL to answer it and set " +
			"answerable=true — for a pure chart-type change ('make it a bar chart') return the SAME SQL unchanged. " +
			"Set answerable=false for a greeting or chit-chat, and ALSO for a question ABOUT the previous " +
			"chart/result itself (how it was computed, what the bucket interval is, what a point means) rather " +
			"than a request for different data — that is answered in prose, not SQL.\n\n"
	case prev != nil && prev.Clarification != "":
		sp += "The user originally asked: \"" + prev.Question + "\", and you asked them a clarifying question: \"" +
			prev.Clarification + "\". The current message is their reply to that question — combine the original " +
			"question and this reply into ONE SQL query that answers it. Do not set clarification again; never ask " +
			"for clarification a second time in a row.\n\n"
	}
	if fixup != nil {
		sp += "Your previous attempt:\n" + fixup.SQL + "\nfailed with this Postgres/validation error:\n" +
			fixup.Err + "\nReturn a corrected query.\n\n"
	}
	sp += schema
	msgs := []aiMessage{{Role: "system", Content: &sp}, {Role: "user", Content: strPtr(question)}}
	tools := []map[string]any{toAITool(emitSQLTool)}
	resp, _, err := callAIModel(ctx, aiModel(), msgs, tools, forceFunc("emit_sql"))
	// The model declining the forced call (prose instead of a tool call, or Groq's
	// "tool choice" validator error) means it judged the question un-SQL-able — a meta
	// question about the previous chart, say. Degrade to the prose path, not a 502;
	// same stance as Chat's fallback (controller.go): forced is an optimization.
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "tool choice") {
			return sqlEmission{}, errNotDataQuestion
		}
		return sqlEmission{}, err
	}
	if len(resp.Choices) == 0 || len(resp.Choices[0].Message.ToolCalls) == 0 {
		return sqlEmission{}, errNotDataQuestion
	}
	return parseSQLEmission(resp.Choices[0].Message.ToolCalls[0].Function.Arguments)
}

// parseSQLEmission is separated from the HTTP call so it's unit-testable without the
// network. Clarification wins over sql when both happen to be set (the model was told
// never to do this, but the parse stays defensive). !answerable with no clarification
// -> errNotDataQuestion (the pre-B3 contract, unchanged for the 36/36 live suite).
func parseSQLEmission(rawJSON string) (sqlEmission, error) {
	var a struct {
		Answerable    bool   `json:"answerable"`
		SQL           string `json:"sql"`
		Clarification string `json:"clarification"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &a); err != nil {
		return sqlEmission{}, err
	}
	if c := strings.TrimSpace(a.Clarification); c != "" {
		return sqlEmission{Clarification: c}, nil
	}
	if !a.Answerable || strings.TrimSpace(a.SQL) == "" {
		return sqlEmission{}, errNotDataQuestion
	}
	return sqlEmission{SQL: a.SQL}, nil
}

// emitProse answers a question that isn't a SQL query (an explanation or follow-up like
// "how do they differ") in plain text — the fallback for emitSQL's answerable=false branch.
// Grounded in the same schema context; a plain completion (no tools). cols/rows, when
// non-empty, are the ACTUAL re-executed result of prev.SQL — without them the model
// invents numbers for "analyze the chart" questions.
func emitProse(ctx context.Context, question, schema string, prev *prevTurn, cols []string, rows [][]any, fixup string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	sp := "You are a factory-data assistant for an IoT dashboard. Answer the user's question directly and " +
		"concisely in prose, in the SAME language as the question (Thai or English). Use the schema below to " +
		"ground your answer in the real machines, metrics, and units. Do not output SQL or code unless asked.\n\n"
	if fixup != "" {
		sp += "Your previous answer was judged as not answering the question:\n" + fixup +
			"\nWrite a corrected answer that addresses the question directly.\n\n"
	}
	if prev != nil && prev.SQL != "" {
		sp += "For context, the user's previous question was: \"" + prev.Question + "\" (it ran SQL: " + prev.SQL + ").\n\n"
		if len(cols) > 0 {
			sp += "That SQL was just re-executed; its ACTUAL result is below (" + fmt.Sprint(len(rows)) +
				" rows, evenly sampled if truncated). Ground EVERY number, time range, and machine name in these " +
				"rows — never invent or estimate values not present. If the rows don't cover what's asked, say so.\n" +
				serializeRows(cols, rows) + "\n"
		}
	} else if prev != nil && prev.Clarification != "" {
		sp += "For context, the user's previous question was: \"" + prev.Question + "\" and you asked them: \"" + prev.Clarification + "\".\n\n"
	}
	sp += schema
	msgs := []aiMessage{{Role: "system", Content: &sp}, {Role: "user", Content: strPtr(question)}}
	resp, _, err := callAIModel(ctx, aiModel(), msgs, nil, "")
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == nil {
		return "", errors.New("no answer generated")
	}
	return strings.TrimSpace(*resp.Choices[0].Message.Content), nil
}

const echartSystemPrompt = `You turn a SQL result into an ECharts option that answers the user's question, by calling emit_echart_option. Never reply in prose.
- Pick the chart type — use ONLY 'line', 'bar', 'pie', or 'scatter': a time-bucket column → line; a category comparison → bar; parts-of-a-whole → pie.
- A dataset with the result rows is injected AT RENDER TIME. Reference result columns BY NAME using encode (e.g. series:[{type:'line', encode:{x:'bucket', y:'avg_speed'}}]). Do NOT include any data arrays or a dataset field yourself.
- Emit exactly ONE series even when the rows contain a category column (e.g. machine_name) — the renderer automatically splits one series into one line per category value. NEVER emit one series per machine/category: without per-series filters they would all draw identical data.
- Set xAxis.type: 'time' for a timestamp/bucket column, 'category' for names. Add a short title, tooltip{trigger:'axis'}, and a legend when there are multiple series.
- If the user's message explicitly names a chart type (bar/line/pie/scatter, or the same in another language e.g. Thai "กราฟแท่ง"=bar, "กราฟเส้น"=line, "วงกลม"=pie), use THAT type even if another would be more typical.
- Column names and a few sample rows are given below for type inference only.`

// emitEChart generates an ECharts option. prevErr, when non-empty, is the previous
// attempt's error text — passed back to the model as one retry (B2); pass "" for a
// fresh (non-retry) call.
func emitEChart(ctx context.Context, question string, cols []string, sample [][]any, prevErr string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	payloadMap := map[string]any{"question": question, "columns": cols, "sampleRows": sample}
	if prevErr != "" {
		payloadMap["previousError"] = prevErr + " — return a corrected option"
	}
	payload, _ := json.Marshal(payloadMap)
	sp := echartSystemPrompt
	uc := string(payload)
	msgs := []aiMessage{{Role: "system", Content: &sp}, {Role: "user", Content: &uc}}
	tools := []map[string]any{toAITool(emitEChartTool)}
	resp, _, err := callAIModel(ctx, aiModel(), msgs, tools, forceFunc("emit_echart_option"))
	if err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 || len(resp.Choices[0].Message.ToolCalls) == 0 {
		return nil, errors.New("no chart generated")
	}
	var a struct {
		Option json.RawMessage `json:"option"`
	}
	if err := json.Unmarshal([]byte(resp.Choices[0].Message.ToolCalls[0].Function.Arguments), &a); err != nil {
		return nil, err
	}
	return a.Option, nil
}

// askVerifyPrompt is a small, static prompt (~200 tok) so Groq can prompt-cache it
// across verify calls, mirroring verifySystemPrompt (router.go) but scoped to
// Ask-Data's SQL+chart turns instead of the chat tool-call path.
const askVerifyPrompt = `You check whether a generated SQL query — and its chart, when one is present — actually answers a factory-data question, by calling verify_answer. Always call the tool — never reply in prose.

The option field may be an empty object {} — that means the result is delivered as a plain table with no chart. In that case judge only whether the SQL and sample rows answer the question; chart-type rules do not apply.

MISMATCH (matches_intent: false) only when the SQL or chart targets a DIFFERENT metric, machine, or time window than the question asked, or (chart present only) the chart type contradicts a chart type the user explicitly requested (e.g. asked for a bar chart but got a pie chart).

A chart type the user explicitly requested (pie/bar/line/scatter, in any language — e.g. Thai "กราฟแท่ง"=bar, "วงกลม"=pie) is correct BY DEFINITION, even if another type would visualize the data better — judge only the DATA (metric, machine, time window), never the style the user chose.

MATCH (matches_intent: true) otherwise — including a result that is imperfect but honestly answers what was asked (fewer points than ideal, a slightly different aggregation, a reasonable default time window when none was specified, an empty result when the data may genuinely contain no matching rows).

If mismatch, set problem to a short specific reason (e.g. "answered temperature, user asked speed"). Leave clarifying_question empty — Ask-Data repairs automatically rather than asking the user.`

// askProseVerifyPrompt mirrors askVerifyPrompt but for prose turns: small and static
// so the provider can prompt-cache it across verify calls.
const askProseVerifyPrompt = `You check whether a prose answer actually addresses a factory-data question, by calling verify_answer. Always call the tool — never reply in prose.

sampleRows, when non-empty, are the ACTUAL rows the answer was grounded in.

MISMATCH (matches_intent: false) only when the answer is about a DIFFERENT topic than the question asked, or states a number that CONTRADICTS the sample rows.

MATCH (matches_intent: true) otherwise — including an imperfect but on-topic answer, a definition/explanation for an explain question, a polite reply to a greeting, or an honest "the data doesn't show this". When sampleRows is empty, judge topicality only — never flag missing numbers.

If mismatch, set problem to a short specific reason. Leave clarifying_question empty — the answer is regenerated automatically rather than asking the user.`

// verifyAskProse judges whether a prose answer addresses question, grounded by the
// same rows emitProse saw. Mirrors verifyAskAnswer exactly: 6s bound, routerModel(),
// forced verify_answer; (zero, false) on ANY failure = no verdict, deliver as-is.
func verifyAskProse(ctx context.Context, question, answer string, cols []string, sample [][]any) (VerifyResult, bool) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	payload, _ := json.Marshal(map[string]any{
		"question":   question,
		"answer":     truncateRunes(answer, 1500),
		"columns":    cols,
		"sampleRows": sample,
	})

	sp := askProseVerifyPrompt
	uc := string(payload)
	msgs := []aiMessage{{Role: "system", Content: &sp}, {Role: "user", Content: &uc}}

	tools := []map[string]any{toAITool(VerifyAnswerTool)}
	resp, _, err := callAIModel(ctx, routerModel(), msgs, tools, forceFunc("verify_answer"))
	if err != nil || len(resp.Choices) == 0 || len(resp.Choices[0].Message.ToolCalls) == 0 {
		return VerifyResult{}, false
	}
	return parseVerifyResult(resp.Choices[0].Message.ToolCalls[0].Function.Arguments)
}

// verifyAskAnswer judges whether sqlText (+ option, which may be the empty table
// signal "{}") actually answer question. Mirrors VerifyAnswer (router.go) exactly:
// 6s bounded timeout, routerModel(), forced verify_answer tool call. Returns (zero,
// false) on ANY error, timeout, or malformed JSON — callers MUST treat false as "no
// verdict" (deliver as-is), never as a mismatch; the verifier's own infrastructure
// failing must never block or repair an otherwise-fine chart.
func verifyAskAnswer(ctx context.Context, question, sqlText string, cols []string, sample [][]any, option json.RawMessage) (VerifyResult, bool) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	var optM map[string]any
	_ = json.Unmarshal(option, &optM)
	payload, _ := json.Marshal(map[string]any{
		"question":   question,
		"sql":        truncateRunes(sqlText, 1500),
		"columns":    cols,
		"sampleRows": sample,
		"option":     optM,
	})

	sp := askVerifyPrompt
	uc := string(payload)
	msgs := []aiMessage{{Role: "system", Content: &sp}, {Role: "user", Content: &uc}}

	tools := []map[string]any{toAITool(VerifyAnswerTool)}
	resp, _, err := callAIModel(ctx, routerModel(), msgs, tools, forceFunc("verify_answer"))
	if err != nil {
		return VerifyResult{}, false
	}
	if len(resp.Choices) == 0 {
		return VerifyResult{}, false
	}
	calls := resp.Choices[0].Message.ToolCalls
	if len(calls) == 0 {
		return VerifyResult{}, false
	}
	return parseVerifyResult(calls[0].Function.Arguments)
}

// hasNumericColumn reports whether any column holds numeric values — the signal that
// a result is chartable. A column is classified by its first non-null value. Pure text
// results (e.g. "list machines") have none, so we render them as a table instead.
func hasNumericColumn(cols []string, rows [][]any) bool {
	for ci := range cols {
		for _, r := range rows {
			if ci >= len(r) || r[ci] == nil {
				continue
			}
			switch r[ci].(type) {
			case int, int16, int32, int64, float32, float64, pgtype.Numeric:
				return true
			}
			break // first non-null value classifies the column; not numeric → next column
		}
	}
	return false
}

// sanitizeEChartOption removes data injection points and validates series references.
// Returns "{}" if the option is invalid or references nonexistent columns.
func sanitizeEChartOption(option json.RawMessage, cols []string) json.RawMessage {
	var m map[string]any
	if err := json.Unmarshal(option, &m); err != nil || m == nil {
		return json.RawMessage("{}")
	}
	delete(m, "dataset")

	// Normalize series to []map[string]any.
	var series []map[string]any
	if rawSeries, ok := m["series"]; ok {
		switch s := rawSeries.(type) {
		case []any:
			for _, item := range s {
				if sm, ok := item.(map[string]any); ok {
					series = append(series, sm)
				}
			}
		case map[string]any:
			series = append(series, s)
		default:
			return json.RawMessage("{}")
		}
	}

	// Validate and clean series.
	validSeries := make([]map[string]any, 0, len(series))
	seen := map[string]bool{}
	for _, s := range series {
		delete(s, "data")

		// Check type is allowed.
		t, ok := s["type"].(string)
		if !ok || !slices.Contains([]string{"line", "bar", "pie", "scatter"}, t) {
			return json.RawMessage("{}")
		}

		// Validate encode references.
		if encodeRaw, ok := s["encode"]; ok {
			if enc, ok := encodeRaw.(map[string]any); ok {
				for _, v := range enc {
					if err := validateEncodeValue(v, cols); err != nil {
						return json.RawMessage("{}")
					}
				}
			}
		}

		// Series sharing type+encode are duplicates by construction — with no
		// per-series data or filter (both stripped/absent here) they render
		// identical rows. The model sometimes emits one per machine expecting a
		// filter it cannot supply; keep the first — the frontend splits a single
		// series into one line per category value.
		encJSON, _ := json.Marshal(s["encode"])
		key := t + string(encJSON)
		if seen[key] {
			continue
		}
		seen[key] = true
		validSeries = append(validSeries, s)
	}
	// A chart with no series can never render — fall back to the table signal.
	if len(validSeries) == 0 {
		return json.RawMessage("{}")
	}
	m["series"] = validSeries

	result, _ := json.Marshal(m)
	return result
}

// validateEncodeValue checks that a single or array of column names exist in cols.
func validateEncodeValue(v any, cols []string) error {
	switch val := v.(type) {
	case string:
		if !slices.Contains(cols, val) {
			return errors.New("column not found: " + val)
		}
	case []any:
		for _, item := range val {
			if s, ok := item.(string); ok && !slices.Contains(cols, s) {
				return errors.New("column not found: " + s)
			}
		}
	}
	return nil
}

func sampleRows(rows [][]any, n int) [][]any {
	if len(rows) <= n {
		return rows
	}
	return rows[:n]
}

// downsampleRows picks ~n rows evenly across the whole result (unlike sampleRows'
// head-only cut) so a trend question sees the full time span, not just its start.
func downsampleRows(rows [][]any, n int) [][]any {
	if len(rows) <= n {
		return rows
	}
	out := make([][]any, 0, n)
	step := float64(len(rows)) / float64(n)
	for i := 0; i < n; i++ {
		out = append(out, rows[int(float64(i)*step)])
	}
	return out
}

// serializeRows renders a result compactly for a prose prompt: one header line of
// column names, then one comma-separated line per row.
func serializeRows(cols []string, rows [][]any) string {
	var b strings.Builder
	b.WriteString(strings.Join(cols, ", "))
	b.WriteByte('\n')
	for _, r := range rows {
		parts := make([]string, len(r))
		for i, v := range r {
			switch t := v.(type) {
			case time.Time:
				parts[i] = t.Format(time.RFC3339)
			case pgtype.Numeric:
				if f, err := t.Float64Value(); err == nil {
					parts[i] = fmt.Sprintf("%g", f.Float64)
				} else {
					parts[i] = fmt.Sprint(v)
				}
			default:
				parts[i] = fmt.Sprint(v)
			}
		}
		b.WriteString(strings.Join(parts, ", "))
		b.WriteByte('\n')
	}
	return b.String()
}

// ── HTTP handlers ────────────────────────────────────────────────────────────

// AskData: question → SQL → rows → ECharts option. POST /ai/ask
func AskData(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	if user == nil {
		return c.Status(401).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "unauthorized"}})
	}
	var body struct {
		Question string `json:"question"`
		Context  *struct {
			Question      string `json:"question"`
			SQL           string `json:"sql"`
			Clarification string `json:"clarification"`
		} `json:"context"`
	}
	if err := c.BodyParser(&body); err != nil || strings.TrimSpace(body.Question) == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "question is required"}})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// prev's SQL and Clarification are mutually exclusive (see prevTurn) — SQL wins
	// if a caller somehow sent both.
	var prev *prevTurn
	if body.Context != nil {
		if s := strings.TrimSpace(body.Context.SQL); s != "" {
			prev = &prevTurn{Question: body.Context.Question, SQL: body.Context.SQL}
		} else if c := strings.TrimSpace(body.Context.Clarification); c != "" {
			prev = &prevTurn{Question: body.Context.Question, Clarification: body.Context.Clarification}
		}
	}
	question := strings.TrimSpace(body.Question)
	schema := buildSchemaContext(ctx, user.OrgId)

	// Retry loop: up to 3 attempts to generate and validate SQL.
	var cols []string
	var rows [][]any
	var sqlText string
	var fixup *sqlFixup
	for attempt := 1; attempt <= 3; attempt++ {
		emission, err := emitSQL(ctx, question, schema, prev, fixup)
		if errors.Is(err, errNotDataQuestion) {
			// Not a SQL query — answer in prose. Re-run the previous turn's SQL first
			// (same validate + org-scoped guards) so an "analyze the chart" answer is
			// grounded in the real rows; on any re-run failure just answer without them.
			var pcols []string
			var prows [][]any
			if prev != nil && prev.SQL != "" {
				if s, verr := validateSQL(prev.SQL); verr == nil {
					if cs, rs, rerr := runScoped(ctx, user.OrgId, s); rerr == nil {
						pcols, prows = cs, downsampleRows(rs, 200)
					}
				}
			}
			answer, perr := emitProse(ctx, question, schema, prev, pcols, prows, "")
			if perr != nil {
				return c.Status(502).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "could not answer: " + perr.Error()}})
			}
			// B1 for prose: judge the answer; on MISMATCH regenerate once with the
			// problem as fixup (no second judge round — bounded cost, same stance as
			// the chart repair). No verdict or a failed regenerate delivers the
			// original answer — never a 502.
			if v, ok := verifyAskProse(ctx, question, answer, pcols, sampleRows(prows, 5)); ok && !v.MatchesIntent {
				if repaired, rerr := emitProse(ctx, question, schema, prev, pcols, prows, "verifier: "+v.Problem); rerr == nil && repaired != "" {
					answer = repaired
				}
			}
			return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"answer": answer}})
		}
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "could not generate a query: " + err.Error()}})
		}
		if emission.Clarification != "" {
			// B3: the question is about factory data but under-specified — ask back
			// instead of guessing. No SQL ran, no chart to build.
			return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"clarification": emission.Clarification}})
		}

		sqlText, err = validateSQL(emission.SQL)
		if err != nil {
			if attempt < 3 {
				fixup = &sqlFixup{SQL: emission.SQL, Err: err.Error()}
				continue
			}
			return c.Status(400).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "generated query rejected: " + err.Error()}, "sql": emission.SQL})
		}

		cols, rows, err = runScoped(ctx, user.OrgId, sqlText)
		if err != nil {
			if attempt < 3 {
				fixup = &sqlFixup{SQL: sqlText, Err: err.Error()}
				continue
			}
			return c.Status(400).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "query failed: " + err.Error()}, "sql": sqlText})
		}
		break
	}

	// Text-only results or empty results have no numeric axis — render as a table.
	// Empty option ({}) is the frontend's "table" signal; also skips a wasted Groq call.
	// B2: one retry on a chart-generation error before degrading to the table — a
	// second failure still leaves option "{}" (HTTP 200, never a 502 here).
	option := json.RawMessage("{}")
	if len(rows) > 0 && hasNumericColumn(cols, rows) {
		echartOpt, err := emitEChart(ctx, body.Question, cols, sampleRows(rows, 20), "")
		if err != nil {
			echartOpt, err = emitEChart(ctx, body.Question, cols, sampleRows(rows, 20), err.Error())
		}
		if err == nil {
			option = sanitizeEChartOption(echartOpt, cols)
		}
	}

	// B1: judge gate on chart AND table turns (prose turns are free — no call; empty
	// results are skipped — nothing to judge beyond the SQL text). Call budget per
	// turn: SQL 1(-3 on retry) + chart 0-2 + judge 1 (~1s); worst case with the
	// judge's one repair round adds SQL 1 + chart 0-1 more — still well inside the
	// 45s handler ctx, and a repair failure degrades to the table signal, never a 502.
	if len(rows) > 0 {
		sqlText, cols, rows, option = verifyAndRepairAnswer(ctx, user.OrgId, question, sqlText, cols, rows, option, schema, prev)
	}

	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{
		"sql":          sqlText,
		"columns":      cols,
		"rows":         rows,
		"echartOption": option,
	}})
}

// verifyAndRepairAnswer runs the B1 judge on a delivered answer — chart AND table
// turns — and, on a MISMATCH verdict, ONE repair round: re-emit SQL with the
// verifier's problem as a fixup, re-run it, and re-chart if the repaired result is
// chartable — no second judge call on the repaired result. If the repaired SQL
// fails to emit, validate, or run, or returns no rows, the ORIGINAL rows are kept
// (chart degraded to the table signal "{}") — the judge must never turn an already
// delivered answer into a 502.
func verifyAndRepairAnswer(ctx context.Context, orgID, question, sqlText string, cols []string, rows [][]any, option json.RawMessage, schema string, prev *prevTurn) (string, []string, [][]any, json.RawMessage) {
	v, ok := verifyAskAnswer(ctx, question, sqlText, cols, sampleRows(rows, 5), option)
	if !ok || v.MatchesIntent {
		return sqlText, cols, rows, option
	}

	emission, err := emitSQL(ctx, question, schema, prev, &sqlFixup{SQL: sqlText, Err: "verifier: " + v.Problem})
	if err != nil || emission.Clarification != "" || emission.SQL == "" {
		return sqlText, cols, rows, json.RawMessage("{}")
	}
	repairedSQL, err := validateSQL(emission.SQL)
	if err != nil {
		return sqlText, cols, rows, json.RawMessage("{}")
	}
	repairedCols, repairedRows, err := runScoped(ctx, orgID, repairedSQL)
	if err != nil || len(repairedRows) == 0 {
		return sqlText, cols, rows, json.RawMessage("{}")
	}

	// Repaired rows accepted. Chart them if chartable; otherwise deliver as a table —
	// ponytail: no second judge round on the repaired result, by design (bounded cost).
	if !hasNumericColumn(repairedCols, repairedRows) {
		return repairedSQL, repairedCols, repairedRows, json.RawMessage("{}")
	}
	repairedOpt, err := emitEChart(ctx, question, repairedCols, sampleRows(repairedRows, 20), "")
	if err != nil {
		return repairedSQL, repairedCols, repairedRows, json.RawMessage("{}")
	}
	return repairedSQL, repairedCols, repairedRows, sanitizeEChartOption(repairedOpt, repairedCols)
}

// RunSQL re-executes a stored query (board reopen → live data). POST /ai/run-sql
// Re-validates even though the SQL came from our DB — cheap, and the guard is the point.
func RunSQL(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	if user == nil {
		return c.Status(401).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "unauthorized"}})
	}
	var body struct {
		SQL string `json:"sql"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "sql is required"}})
	}
	sqlText, err := validateSQL(body.SQL)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": err.Error()}})
	}
	cols, rows, err := runScoped(context.Background(), user.OrgId, sqlText)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "query failed: " + err.Error()}})
	}
	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"columns": cols, "rows": rows}})
}
