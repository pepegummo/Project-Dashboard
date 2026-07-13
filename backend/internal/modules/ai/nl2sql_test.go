package ai

import (
	"encoding/json"
	"testing"
)

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
			name:      "unknown series type rejected",
			option:    json.RawMessage(`{"series": [{"type": "heatmap"}]}`),
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
