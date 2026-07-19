package ai

import (
	"encoding/json"
	"testing"
)

// TestBuildAIToolsPreviewGating verifies buildAITools drops the preview_*
// staging tools for viewers (read-only role) but keeps them for editor/admin —
// a viewer must never be offered a tool that dispatch() will then reject.
func TestBuildAIToolsPreviewGating(t *testing.T) {
	hasPreviewTool := func(tools []map[string]any) bool {
		for _, t := range tools {
			fn, _ := t["function"].(map[string]any)
			name, _ := fn["name"].(string)
			if previewTools[name] {
				return true
			}
		}
		return false
	}

	if got := hasPreviewTool(buildAITools("viewer")); got {
		t.Errorf("buildAITools(%q) contains a preview_* tool, want none", "viewer")
	}
	for _, role := range []string{"editor", "admin"} {
		if got := hasPreviewTool(buildAITools(role)); !got {
			t.Errorf("buildAITools(%q) has no preview_* tool, want at least one", role)
		}
	}
}

// TestBuildAIToolsWithSlimAll verifies the read-intent variant keeps every tool
// callable but strips the complexSchemaTools' full input schemas (the ~850-token
// preview_* payloads) down to the slim description-only encoding.
func TestBuildAIToolsWithSlimAll(t *testing.T) {
	full := buildAIToolsWith("admin", false)
	slim := buildAIToolsWith("admin", true)
	if len(full) != len(slim) {
		t.Fatalf("tool counts differ: full=%d slim=%d — slimAll must never drop a tool", len(full), len(slim))
	}
	for _, tool := range slim {
		fn, _ := tool["function"].(map[string]any)
		name, _ := fn["name"].(string)
		params, _ := fn["parameters"].(map[string]any)
		if params == nil {
			t.Errorf("%s: slim tool has no parameters object", name)
			continue
		}
		if _, hasProps := params["properties"]; hasProps {
			t.Errorf("%s: slimAll variant still carries a full schema (properties present)", name)
		}
		if complexSchemaTools[name] && fn["description"] != slimToolDescriptions[name] {
			t.Errorf("%s: slimAll variant missing its slim description with arg hints", name)
		}
	}
}

// TestChatIntentResponseMarshalsIntentField verifies the Chat() response payload
// (Task 4) marshals the router's IntentResult under the exact key "intent" that the
// frontend now consumes instead of its own regex parsers, and that a router fallback
// (ok=false) still produces zero-value slots rather than omitting the field.
func TestChatIntentResponseMarshalsIntentField(t *testing.T) {
	res := IntentResult{
		Intent:       "read_metric",
		Machine:      "CW-01",
		Metric:       "speed",
		Fields:       []string{"speed"},
		Bucket:       "1h",
		TargetWidget: "gauge",
		Status:       "running",
		Sku:          "SKU-1",
		Confidence:   0.92,
	}
	res.DateRange.Start = "2026-07-01"
	res.DateRange.End = "2026-07-10"

	payload := map[string]any{"success": true, "data": []int{}, "intent": chatIntentResponse(res, true)}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded struct {
		Intent map[string]any `json:"intent"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.Intent == nil {
		t.Fatal("response JSON has no \"intent\" field")
	}
	if decoded.Intent["ok"] != true {
		t.Errorf("intent.ok = %v, want true", decoded.Intent["ok"])
	}
	if decoded.Intent["intent"] != "read_metric" {
		t.Errorf("intent.intent = %v, want read_metric", decoded.Intent["intent"])
	}
	if decoded.Intent["machine"] != "CW-01" {
		t.Errorf("intent.machine = %v, want CW-01", decoded.Intent["machine"])
	}

	// Fallback case: router declined, must still be ok:false with zero-value slots,
	// never omitted — the frontend treats a missing/undefined intent object as a bug,
	// not as "fall back to text parsing" (Task 4 deletes that fallback entirely).
	fallback := chatIntentResponse(IntentResult{}, false)
	fbRaw, err := json.Marshal(fallback)
	if err != nil {
		t.Fatalf("marshal fallback error: %v", err)
	}
	var fbDecoded map[string]any
	if err := json.Unmarshal(fbRaw, &fbDecoded); err != nil {
		t.Fatalf("unmarshal fallback error: %v", err)
	}
	if fbDecoded["ok"] != false {
		t.Errorf("fallback intent.ok = %v, want false", fbDecoded["ok"])
	}
	if fbDecoded["machine"] != "" {
		t.Errorf("fallback intent.machine = %v, want empty string", fbDecoded["machine"])
	}
}
