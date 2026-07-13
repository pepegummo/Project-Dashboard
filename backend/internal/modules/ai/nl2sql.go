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
			"answerable": map[string]any{"type": "boolean", "description": "false ONLY for a greeting, chit-chat, or clearly non-factory input (then leave sql empty). A data-listing question ('which SKUs', 'what machines', 'list values') is answerable=true."},
			"sql":        map[string]any{"type": "string", "description": "One SELECT over the allowed v_ views. No semicolons, no CTEs, no writes. Always include a LIMIT."},
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
- Numeric metric: (data->>'speed')::float.
- A "which/what <dimension> are available / exist / does X run" question (e.g. SKUs) is a listing query, NOT a time series: SELECT DISTINCT data->>'sku' AS sku FROM v_telemetry WHERE data->>'sku' IS NOT NULL ORDER BY sku (filter by machine with machine_name ILIKE '%<code>%').
- Match a machine by name with ILIKE '%<code>%' (e.g. machine_name ILIKE '%CW-01%'), NEVER exact =. Names include a descriptive prefix, so the user's "CW-01" is stored as "Checkweigher CW-01". Same for v_machines.name.
- Give columns clear aliases (bucket, machine_name, avg_speed, ...). Always add LIMIT (<= 5000). Aggregate raw readings into buckets or groups rather than returning every row.`)
	return b.String()
}

// prevTurn carries the immediately-previous Ask-Data turn so a follow-up ("make it a
// bar chart", "group by day", "เอาเป็นกราฟแท่ง") can refine it instead of being rejected.
type prevTurn struct {
	Question string
	SQL      string
}

// sqlFixup carries a failed SQL attempt and its error for the retry loop in AskData.
type sqlFixup struct {
	SQL string
	Err string
}

func emitSQL(ctx context.Context, question, schema string, prev *prevTurn, fixup *sqlFixup) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	sp := "You translate a factory-analytics question into ONE read-only Postgres SELECT by calling emit_sql. Never reply in prose.\n\n" +
		"Example — \"avg speed last 24h for CW-01\":\n" +
		"SELECT time_bucket('1 hour', ts) AS bucket, avg((data->>'speed')::float) AS avg_speed " +
		"FROM v_telemetry WHERE machine_name ILIKE '%CW-01%' AND ts > now() - interval '24 hours' " +
		"GROUP BY bucket ORDER BY bucket LIMIT 5000\n\n" +
		"Example — \"which SKUs does CW-01 run\" (a listing question, answerable=true):\n" +
		"SELECT DISTINCT data->>'sku' AS sku FROM v_telemetry WHERE machine_name ILIKE '%CW-01%' " +
		"AND data->>'sku' IS NOT NULL ORDER BY sku LIMIT 100\n\n" +
		"A \"which/what values are available\" listing question IS answerable — return the distinct values; " +
		"set answerable=false ONLY for a greeting or chit-chat.\n\n"
	if prev != nil {
		sp += "The user previously asked: \"" + prev.Question + "\"\nwhich ran this SQL:\n" + prev.SQL +
			"\nIf the new message refines or restyles that chart (a different chart type, grouping, interval, " +
			"filter, or metric) rather than starting a new topic, adapt the previous SQL to answer it and set " +
			"answerable=true — for a pure chart-type change ('make it a bar chart') return the SAME SQL unchanged. " +
			"Only set answerable=false for a greeting or chit-chat.\n\n"
	}
	if fixup != nil {
		sp += "Your previous attempt:\n" + fixup.SQL + "\nfailed with this Postgres/validation error:\n" +
			fixup.Err + "\nReturn a corrected query.\n\n"
	}
	sp += schema
	msgs := []groqMessage{{Role: "system", Content: &sp}, {Role: "user", Content: strPtr(question)}}
	tools := []map[string]any{toGroqTool(emitSQLTool)}
	resp, _, err := callGroqModel(ctx, groqModel, msgs, tools, forceFunc("emit_sql"))
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 || len(resp.Choices[0].Message.ToolCalls) == 0 {
		return "", errors.New("no SQL generated")
	}
	var a struct {
		Answerable bool   `json:"answerable"`
		SQL        string `json:"sql"`
	}
	if err := json.Unmarshal([]byte(resp.Choices[0].Message.ToolCalls[0].Function.Arguments), &a); err != nil {
		return "", err
	}
	if !a.Answerable || strings.TrimSpace(a.SQL) == "" {
		return "", errNotDataQuestion
	}
	return a.SQL, nil
}

// emitProse answers a question that isn't a SQL query (an explanation or follow-up like
// "how do they differ") in plain text — the fallback for emitSQL's answerable=false branch.
// Grounded in the same schema context; a plain completion (no tools).
func emitProse(ctx context.Context, question, schema string, prev *prevTurn) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	sp := "You are a factory-data assistant for an IoT dashboard. Answer the user's question directly and " +
		"concisely in prose, in the SAME language as the question (Thai or English). Use the schema below to " +
		"ground your answer in the real machines, metrics, and units. Do not output SQL or code unless asked.\n\n"
	if prev != nil {
		sp += "For context, the user's previous question was: \"" + prev.Question + "\" (it ran SQL: " + prev.SQL + ").\n\n"
	}
	sp += schema
	msgs := []groqMessage{{Role: "system", Content: &sp}, {Role: "user", Content: strPtr(question)}}
	resp, _, err := callGroqModel(ctx, groqModel, msgs, nil, "")
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
- Set xAxis.type: 'time' for a timestamp/bucket column, 'category' for names. Add a short title, tooltip{trigger:'axis'}, and a legend when there are multiple series.
- If the user's message explicitly names a chart type (bar/line/pie/scatter, or the same in another language e.g. Thai "กราฟแท่ง"=bar, "กราฟเส้น"=line, "วงกลม"=pie), use THAT type even if another would be more typical.
- Column names and a few sample rows are given below for type inference only.`

func emitEChart(ctx context.Context, question string, cols []string, sample [][]any) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	payload, _ := json.Marshal(map[string]any{"question": question, "columns": cols, "sampleRows": sample})
	sp := echartSystemPrompt
	uc := string(payload)
	msgs := []groqMessage{{Role: "system", Content: &sp}, {Role: "user", Content: &uc}}
	tools := []map[string]any{toGroqTool(emitEChartTool)}
	resp, _, err := callGroqModel(ctx, groqModel, msgs, tools, forceFunc("emit_echart_option"))
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
	for _, s := range series {
		delete(s, "data")

		// Check type is allowed.
		if t, ok := s["type"].(string); !ok || !slices.Contains([]string{"line", "bar", "pie", "scatter"}, t) {
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
			Question string `json:"question"`
			SQL      string `json:"sql"`
		} `json:"context"`
	}
	if err := c.BodyParser(&body); err != nil || strings.TrimSpace(body.Question) == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "question is required"}})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	var prev *prevTurn
	if body.Context != nil && strings.TrimSpace(body.Context.SQL) != "" {
		prev = &prevTurn{Question: body.Context.Question, SQL: body.Context.SQL}
	}
	question := strings.TrimSpace(body.Question)
	schema := buildSchemaContext(ctx, user.OrgId)

	// Retry loop: up to 3 attempts to generate and validate SQL.
	var cols []string
	var rows [][]any
	var sqlText string
	var fixup *sqlFixup
	for attempt := 1; attempt <= 3; attempt++ {
		rawSQL, err := emitSQL(ctx, question, schema, prev, fixup)
		if errors.Is(err, errNotDataQuestion) {
			// Not a SQL query — answer in prose instead.
			answer, perr := emitProse(ctx, question, schema, prev)
			if perr != nil {
				return c.Status(502).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "could not answer: " + perr.Error()}})
			}
			return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"answer": answer}})
		}
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "could not generate a query: " + err.Error()}})
		}

		sqlText, err = validateSQL(rawSQL)
		if err != nil {
			if attempt < 3 {
				fixup = &sqlFixup{SQL: rawSQL, Err: err.Error()}
				continue
			}
			return c.Status(400).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "generated query rejected: " + err.Error()}, "sql": rawSQL})
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
	option := json.RawMessage("{}")
	if len(rows) > 0 && hasNumericColumn(cols, rows) {
		echartOpt, err := emitEChart(ctx, body.Question, cols, sampleRows(rows, 20))
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "could not build a chart: " + err.Error()}, "sql": sqlText})
		}
		option = sanitizeEChartOption(echartOpt, cols)
	}
	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{
		"sql":          sqlText,
		"columns":      cols,
		"rows":         rows,
		"echartOption": option,
	}})
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
