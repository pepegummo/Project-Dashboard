package ai

// Regression for the "role-play the edit instead of changing the widget" bug: a focused
// relative-date request like "ดูวันก่อนหน้า @Trend" ("view previous day") must NOT be
// classified as a no-tool context read. In Chat, contextRead = focused && !editRe &&
// !rangeRe && !skuRe; when it was true the model got the no-tool context-answer prompt and
// promised an edit it could not perform. rangeRe must therefore match these phrases.
// Deterministic — no GROQ_API_KEY needed.

import (
	"encoding/json"
	"testing"
)

func TestRelativeDateStaysOffContextRead(t *testing.T) {
	// Relative-date phrasings that ask to move a chart's window. Each must match editRe OR
	// rangeRe so contextRead is false and the tool/edit path is taken.
	wantRouted := []string{
		"ดูวันก่อนหน้า",
		"อยากดูวันก่อนหน้า",
		"ดูของวันก่อน",
		"สองวันก่อน",
		"อาทิตย์ก่อน",
		"สัปดาห์ก่อน",
		"เดือนก่อน",
		"ข้อมูลที่ผ่านมา",
		"show previous day",
		"เมื่อวาน", // already worked; keep as guard against regressions
	}
	for _, m := range wantRouted {
		if !editRe.MatchString(m) && !rangeRe.MatchString(m) {
			t.Errorf("%q matched neither editRe nor rangeRe → would take no-tool contextRead path", m)
		}
	}

	// Bare "ก่อน" means "first/beforehand", not a date. These must NOT be pulled onto the
	// range path by an over-broad rule (they are legitimate context reads / chit-chat).
	wantNotRouted := []string{
		"ขอดูก่อน",
		"ดูข้อมูลก่อน",
	}
	for _, m := range wantNotRouted {
		if rangeRe.MatchString(m) {
			t.Errorf("%q wrongly matched rangeRe (bare ก่อน should not route as a time range)", m)
		}
	}
}

// Deterministic guard for the forced-tool routing: a relative-date window change on a
// focused chart must trip relDateRe (so preview_update_widget is forced BY NAME), while an
// aggregate read ("ค่าเฉลี่ยเมื่อวาน") must trip aggReadRe so it is NOT forced onto an edit.
func TestRelDateForcesEditButNotAggregate(t *testing.T) {
	forceEdit := []string{
		"อยากดูเมื่อวาน",
		"ดูเมื่อวาน",
		"ดูวันก่อนหน้า",
		"เปลี่ยนเป็นเมื่อวาน",
		"show yesterday",
		"last week",
	}
	for _, m := range forceEdit {
		if !relDateRe.MatchString(m) {
			t.Errorf("%q: relDateRe should match (window edit not forced)", m)
		}
		if aggReadRe.MatchString(m) {
			t.Errorf("%q: aggReadRe should NOT match (would wrongly cancel the forced edit)", m)
		}
	}

	// Aggregate reads: user wants a number for a period, not a window change → must be carved out.
	aggReads := []string{
		"ค่าเฉลี่ยเมื่อวาน",
		"average yesterday",
		"เมื่อวานสูงสุดเท่าไหร่",
		"max last week",
	}
	for _, m := range aggReads {
		if !aggReadRe.MatchString(m) {
			t.Errorf("%q: aggReadRe should match so preview_update_widget is NOT force-called", m)
		}
	}

	// forceFunc must emit a tool_choice object Groq accepts (valid JSON naming the function).
	var tc struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal([]byte(forceFunc("preview_update_widget")), &tc); err != nil {
		t.Fatalf("forceFunc emitted invalid JSON: %v", err)
	}
	if tc.Type != "function" || tc.Function.Name != "preview_update_widget" {
		t.Errorf("forceFunc shape = %+v, want type=function name=preview_update_widget", tc)
	}
}
