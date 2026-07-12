package ai

import "testing"

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
