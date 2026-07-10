package ai

// Non-live unit tests for parseIntentResult — no network, no GROQ_API_KEY needed.
// Exercises the strict validation ClassifyIntent relies on: an unknown intent or a
// low-confidence result must fail closed (zero, false) so callers fall back to the
// existing auto-tools chat path instead of acting on a bad classification.

import "testing"

func TestParseIntentResultValidJSON(t *testing.T) {
	raw := `{"intent":"read_metric","machine":"CW-01","metric":"speed","confidence":0.92}`
	got, ok := parseIntentResult(raw)
	if !ok {
		t.Fatalf("parseIntentResult(%s) ok = false, want true", raw)
	}
	if got.Intent != "read_metric" {
		t.Errorf("Intent = %q, want %q", got.Intent, "read_metric")
	}
	if got.Machine != "CW-01" {
		t.Errorf("Machine = %q, want %q", got.Machine, "CW-01")
	}
	if got.Metric != "speed" {
		t.Errorf("Metric = %q, want %q", got.Metric, "speed")
	}
	if got.Confidence != 0.92 {
		t.Errorf("Confidence = %v, want 0.92", got.Confidence)
	}
}

func TestParseIntentResultDateRangeAndFields(t *testing.T) {
	raw := `{"intent":"compare","fields":["speed","temperature"],"dateRange":{"start":"2026-07-01","end":"2026-07-02"},"confidence":0.8}`
	got, ok := parseIntentResult(raw)
	if !ok {
		t.Fatalf("parseIntentResult(%s) ok = false, want true", raw)
	}
	if len(got.Fields) != 2 || got.Fields[0] != "speed" || got.Fields[1] != "temperature" {
		t.Errorf("Fields = %v, want [speed temperature]", got.Fields)
	}
	if got.DateRange.Start != "2026-07-01" || got.DateRange.End != "2026-07-02" {
		t.Errorf("DateRange = %+v, want start=2026-07-01 end=2026-07-02", got.DateRange)
	}
}

func TestParseIntentResultUnknownIntent(t *testing.T) {
	raw := `{"intent":"delete_everything","confidence":0.99}`
	got, ok := parseIntentResult(raw)
	if ok {
		t.Fatalf("parseIntentResult(%s) ok = true, want false (unknown intent)", raw)
	}
	if got.Intent != "" || got.Confidence != 0 {
		t.Errorf("got = %+v, want zero value", got)
	}
}

func TestParseIntentResultLowConfidence(t *testing.T) {
	raw := `{"intent":"chat","confidence":0.2}`
	got, ok := parseIntentResult(raw)
	if ok {
		t.Fatalf("parseIntentResult(%s) ok = true, want false (confidence below floor)", raw)
	}
	if got.Intent != "" || got.Confidence != 0 {
		t.Errorf("got = %+v, want zero value", got)
	}
}

func TestParseIntentResultInvalidJSON(t *testing.T) {
	raw := `{"intent": "chat", "confidence":`
	_, ok := parseIntentResult(raw)
	if ok {
		t.Fatalf("parseIntentResult(%s) ok = true, want false (malformed JSON)", raw)
	}
}

func TestParseIntentResultEmptyIntent(t *testing.T) {
	raw := `{"confidence":0.9}`
	_, ok := parseIntentResult(raw)
	if ok {
		t.Fatalf("parseIntentResult(%s) ok = true, want false (empty intent is not in the enum)", raw)
	}
}
