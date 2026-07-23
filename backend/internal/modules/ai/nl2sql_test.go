package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// TestAskAIErrorMapsQuota verifies the shared /ask error mapper turns a provider
// quotaError into 429 QUOTA_EXCEEDED and leaves any other error a 502 — the same
// distinction Chat makes via errors.As on the 429 path.
func TestAskAIErrorMapsQuota(t *testing.T) {
	app := fiber.New()
	app.Get("/quota", func(c *fiber.Ctx) error { return askAIError(c, "x: ", &quotaError{}) })
	app.Get("/other", func(c *fiber.Ctx) error { return askAIError(c, "boom: ", fmt.Errorf("nope")) })

	q, err := app.Test(httptest.NewRequest("GET", "/quota", nil))
	if err != nil {
		t.Fatalf("test quota route: %v", err)
	}
	if q.StatusCode != 429 {
		t.Errorf("quota status = %d, want 429", q.StatusCode)
	}
	body, _ := io.ReadAll(q.Body)
	if !containsJSONCode(body, "QUOTA_EXCEEDED") {
		t.Errorf("quota body = %s, want code QUOTA_EXCEEDED", body)
	}

	o, err := app.Test(httptest.NewRequest("GET", "/other", nil))
	if err != nil {
		t.Fatalf("test other route: %v", err)
	}
	if o.StatusCode != 502 {
		t.Errorf("generic status = %d, want 502", o.StatusCode)
	}
}

func containsJSONCode(body []byte, code string) bool {
	var parsed struct {
		Error struct{ Code string } `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false
	}
	return parsed.Error.Code == code
}

func TestParseSQLEmission(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		wantSQL     string
		wantClarify string
		wantErr     error // non-nil: exact error to match with errors.Is; else just "any error"
		wantAnyErr  bool
	}{
		{
			name:        "clarification set",
			raw:         `{"answerable":true,"sql":"","clarification":"Which machine and metric?"}`,
			wantClarify: "Which machine and metric?",
		},
		{
			name:        "clarification wins over sql when both set",
			raw:         `{"answerable":true,"sql":"SELECT 1","clarification":"Which machine?"}`,
			wantClarify: "Which machine?",
		},
		{
			name:    "not answerable and no clarification -> errNotDataQuestion",
			raw:     `{"answerable":false,"sql":""}`,
			wantErr: errNotDataQuestion,
		},
		{
			name:    "answerable but empty sql and no clarification -> errNotDataQuestion",
			raw:     `{"answerable":true,"sql":""}`,
			wantErr: errNotDataQuestion,
		},
		{
			name:    "valid sql passes through",
			raw:     `{"answerable":true,"sql":"SELECT 1 FROM v_machines LIMIT 1"}`,
			wantSQL: "SELECT 1 FROM v_machines LIMIT 1",
		},
		{
			name:       "malformed JSON errors",
			raw:        `{not json`,
			wantAnyErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseSQLEmission(c.raw)
			if c.wantErr != nil {
				if !errors.Is(err, c.wantErr) {
					t.Fatalf("got err=%v, want errors.Is(err, %v)", err, c.wantErr)
				}
				return
			}
			if c.wantAnyErr {
				if err == nil {
					t.Fatalf("expected an error, got none (result=%+v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.SQL != c.wantSQL {
				t.Errorf("SQL = %q, want %q", got.SQL, c.wantSQL)
			}
			if got.Clarification != c.wantClarify {
				t.Errorf("Clarification = %q, want %q", got.Clarification, c.wantClarify)
			}
		})
	}
}

func TestHasNumericColumn(t *testing.T) {
	cases := []struct {
		name string
		cols []string
		rows [][]any
		want bool
	}{
		{"text only", []string{"name"}, [][]any{{"CW-01"}, {"CW-02"}}, false},
		{"int column", []string{"name", "count"}, [][]any{{"CW-01", int64(5)}}, true},
		{"float column", []string{"bucket", "avg"}, [][]any{{"09:00", 42.5}}, true},
		{"leading nils then int", []string{"n"}, [][]any{{nil}, {int64(3)}}, true},
		{"empty", []string{"name"}, nil, false},
	}
	for _, c := range cases {
		if got := hasNumericColumn(c.cols, c.rows); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestValidateSQL(t *testing.T) {
	ok := []string{
		`SELECT time_bucket('1 hour', ts) AS bucket, avg((data->>'speed')::float) AS avg_speed FROM v_telemetry WHERE machine_name = 'CW-01' GROUP BY bucket ORDER BY bucket LIMIT 500`,
		`select name, status from v_machines order by name limit 100;`, // trailing semicolon allowed
		`SELECT key, label, unit FROM v_machine_fields LIMIT 50`,
	}
	for _, s := range ok {
		if _, err := validateSQL(s); err != nil {
			t.Errorf("expected valid, got error: %v\n  sql: %s", err, s)
		}
	}

	bad := []string{
		`DELETE FROM v_telemetry`,                                             // write
		`SELECT 1; DROP TABLE users`,                                          // multi-statement
		`INSERT INTO v_machines VALUES (1)`,                                   // write
		`SELECT * FROM users`,                                                 // base table (cross-org leak)
		`SELECT * FROM telemetry_raw`,                                         // base table, bypasses org view
		`SELECT * FROM machines m, organizations o`,                           // comma join to base table
		`SELECT password_hash FROM v_machines JOIN users ON true`,             // base table in join
		`SELECT pg_sleep(10)`,                                                 // system function... actually via pg_ table? ensure denied
		`UPDATE v_machines SET name = 'x'`,                                    // write
		`WITH x AS (SELECT * FROM telemetry_raw) SELECT * FROM x`,             // base table inside CTE
	}
	for _, s := range bad {
		if _, err := validateSQL(s); err == nil {
			t.Errorf("expected rejection, got none for: %s", s)
		}
	}
}

func TestSanitizeEChartOption(t *testing.T) {
	cases := []struct {
		name      string
		option    json.RawMessage
		cols      []string
		checkFn   func(t *testing.T, result json.RawMessage)
		wantValid bool
	}{
		{
			name: "valid line series with matching encode",
			option: json.RawMessage(`{
				"title": {"text": "Test"},
				"series": [{"type": "line", "encode": {"x": "bucket", "y": "avg_speed"}}],
				"xAxis": {"type": "time"}
			}`),
			cols:       []string{"bucket", "avg_speed"},
			wantValid:  true,
			checkFn: func(t *testing.T, result json.RawMessage) {
				var m map[string]any
				if err := json.Unmarshal(result, &m); err != nil {
					t.Errorf("result should be valid JSON: %v", err)
					return
				}
				if m == nil || m["series"] == nil {
					t.Error("expected series in result")
					return
				}
				series, ok := m["series"].([]any)
				if !ok || len(series) == 0 {
					t.Error("expected non-empty series array")
					return
				}
				s, ok := series[0].(map[string]any)
				if !ok {
					t.Error("expected first series to be object")
					return
				}
				if s["type"] != "line" {
					t.Errorf("expected series type 'line', got %v", s["type"])
				}
				if s["data"] != nil {
					t.Error("expected series.data to be removed")
				}
			},
		},
		{
			name: "duplicate series (same type+encode, per-machine names) collapse to one",
			option: json.RawMessage(`{
				"series": [
					{"type": "line", "name": "CW-01", "encode": {"x": "bucket", "y": "avg_speed"}},
					{"type": "line", "name": "CB-01", "encode": {"x": "bucket", "y": "avg_speed"}}
				]
			}`),
			cols:      []string{"bucket", "machine_name", "avg_speed"},
			wantValid: true,
			checkFn: func(t *testing.T, result json.RawMessage) {
				var m map[string]any
				if err := json.Unmarshal(result, &m); err != nil {
					t.Errorf("result should be valid JSON: %v", err)
					return
				}
				series, _ := m["series"].([]any)
				if len(series) != 1 {
					t.Errorf("expected duplicate series collapsed to 1, got %d", len(series))
				}
			},
		},
		{
			name: "distinct series (different encode y) both kept",
			option: json.RawMessage(`{
				"series": [
					{"type": "line", "encode": {"x": "bucket", "y": "avg_speed"}},
					{"type": "line", "encode": {"x": "bucket", "y": "max_speed"}}
				]
			}`),
			cols:      []string{"bucket", "avg_speed", "max_speed"},
			wantValid: true,
			checkFn: func(t *testing.T, result json.RawMessage) {
				var m map[string]any
				if err := json.Unmarshal(result, &m); err != nil {
					t.Errorf("result should be valid JSON: %v", err)
					return
				}
				series, _ := m["series"].([]any)
				if len(series) != 2 {
					t.Errorf("expected both distinct series kept, got %d", len(series))
				}
			},
		},
		{
			name: "dataset and series.data stripped",
			option: json.RawMessage(`{
				"dataset": {"source": [[1, 2], [3, 4]]},
				"series": [{"type": "bar", "data": [1, 2, 3], "encode": {"x": "col1", "y": "col2"}}],
				"xAxis": {"type": "category"}
			}`),
			cols:       []string{"col1", "col2"},
			wantValid:  true,
			checkFn: func(t *testing.T, result json.RawMessage) {
				var m map[string]any
				json.Unmarshal(result, &m)
				if m["dataset"] != nil {
					t.Error("expected dataset to be removed")
				}
				series := m["series"].([]any)
				if series[0].(map[string]any)["data"] != nil {
					t.Error("expected series.data to be removed")
				}
			},
		},
		{
			name: "valid heatmap series with x/y/value encode survives",
			option: json.RawMessage(`{
				"series": [{"type": "heatmap", "encode": {"x": "hour", "y": "machine_name", "value": "avg_speed"}}],
				"visualMap": {"inRange": {"color": ["#22c55e", "#ef4444"]}},
				"xAxis": {"type": "category"}, "yAxis": {"type": "category"}
			}`),
			cols:      []string{"hour", "machine_name", "avg_speed"},
			wantValid: true,
			checkFn: func(t *testing.T, result json.RawMessage) {
				var m map[string]any
				if err := json.Unmarshal(result, &m); err != nil {
					t.Errorf("result should be valid JSON: %v", err)
					return
				}
				if m["visualMap"] == nil {
					t.Error("expected visualMap to survive sanitize")
				}
				series, _ := m["series"].([]any)
				if len(series) != 1 || series[0].(map[string]any)["type"] != "heatmap" {
					t.Errorf("expected one heatmap series, got %v", m["series"])
				}
			},
		},
		{
			name:      "heatmap with encode value on missing column rejected",
			option:    json.RawMessage(`{"series": [{"type": "heatmap", "encode": {"x": "hour", "y": "machine_name", "value": "missing"}}]}`),
			cols:      []string{"hour", "machine_name"},
			wantValid: false,
		},
		{
			name:      "unknown series type rejected",
			option:    json.RawMessage(`{"series": [{"type": "radar"}]}`),
			cols:      []string{},
			wantValid: false,
		},
		{
			name: "encode referencing missing column rejected",
			option: json.RawMessage(`{
				"series": [{"type": "line", "encode": {"x": "bucket", "y": "missing_column"}}]
			}`),
			cols:      []string{"bucket"},
			wantValid: false,
		},
		{
			name:      "invalid JSON returns empty object",
			option:    json.RawMessage(`{invalid json}`),
			cols:      []string{},
			wantValid: false,
		},
		{
			name: "encode with array values",
			option: json.RawMessage(`{
				"series": [{"type": "bar", "encode": {"x": "category", "y": ["value1", "value2"]}}]
			}`),
			cols:       []string{"category", "value1", "value2"},
			wantValid:  true,
			checkFn: func(t *testing.T, result json.RawMessage) {
				var m map[string]any
				if err := json.Unmarshal(result, &m); err != nil {
					t.Errorf("result should be valid: %v", err)
				}
				// Just verify it parses; the main test is that it doesn't return "{}"
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizeEChartOption(tc.option, tc.cols)
			if tc.wantValid {
				if string(result) == "{}" {
					t.Error("expected valid option, got empty object")
				}
				if tc.checkFn != nil {
					tc.checkFn(t, result)
				}
			} else {
				if string(result) != "{}" {
					t.Errorf("expected empty object for invalid input, got: %s", string(result))
				}
			}
		})
	}
}
