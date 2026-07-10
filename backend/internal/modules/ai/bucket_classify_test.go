package ai

// Regression for the "อยากดู 22 นาที" bug: a bucket/interval change on a focused count/chart
// widget must route to the EDIT path (force preview_update_widget), not the no-tool
// context-answer path where the model refused ("the widget only supports 15-minute intervals").
// bucketRe must match interval phrases; aggReadRe must still carve out count questions.
// Deterministic — no GROQ_API_KEY needed.

import "testing"

func TestBucketChangeRoutesToEdit(t *testing.T) {
	// Interval-change phrasings → must trip bucketRe and NOT aggReadRe, so bucketEdit forces
	// preview_update_widget. Each must also stay OFF the no-tool contextRead path (bucketRe is
	// in that exclusion list).
	forceEdit := []string{
		"อยากดู 22 นาที",
		"ดู 22 นาที",
		"22m",
		"ทุก 15 นาที",
		"15 minutes",
		"1 ชั่วโมง",
		"รายชั่วโมง",
		"show 30 minutes",
	}
	// Known collision: the bare abbreviation "min" also matches aggReadRe's "min" (minimum), so
	// "30 min" degrades from force-by-name to the generic force-required path (still an edit, just
	// not routed by name). Thai นาที and the full word "minutes" avoid it.
	for _, m := range forceEdit {
		if !bucketRe.MatchString(m) {
			t.Errorf("%q: bucketRe should match (bucket edit not forced)", m)
		}
		if aggReadRe.MatchString(m) {
			t.Errorf("%q: aggReadRe should NOT match (would wrongly cancel the forced edit)", m)
		}
	}

	// Count reads over a window: user wants a NUMBER, not a bar-interval change → aggReadRe must
	// carve them out so they are not forced onto preview_update_widget.
	aggReads := []string{
		"ผลิตกี่ชิ้นใน 22 นาที",
		"count in 30 minutes",
		"เฉลี่ย 15 นาที",
	}
	for _, m := range aggReads {
		if !aggReadRe.MatchString(m) {
			t.Errorf("%q: aggReadRe should match so preview_update_widget is NOT force-called", m)
		}
	}

	// Not an interval at all — must NOT be pulled onto the bucket-edit path.
	notBucket := []string{
		"อยากดูของเมื่อวาน", // a date window, handled by relDateRe not bucketRe
		"ขอดูก่อน",          // "view first" chit-chat
		"สวัสดีครับ",         // greeting
	}
	for _, m := range notBucket {
		if bucketRe.MatchString(m) {
			t.Errorf("%q: bucketRe wrongly matched (no interval here)", m)
		}
	}
}

// Guard for the metric-overlay edit on a focused chart widget: a "compare A, B" phrase must trip
// compareRe (so preview_update_widget is forced to reassign fields[]), while a plain read must not.
func TestCompareRoutesToFieldsEdit(t *testing.T) {
	forceEdit := []string{
		"อยากดูเปรียบเทียบ weight, speed",
		"เปรียบเทียบ weight กับ speed",
		"compare speed and throughput",
		"overlay weight vs reject",
		"เทียบ weight speed",
	}
	for _, m := range forceEdit {
		if !compareRe.MatchString(m) {
			t.Errorf("%q: compareRe should match (fields edit not forced)", m)
		}
	}

	// Not a comparison — must NOT be pulled onto the fields-edit path.
	notCompare := []string{
		"อยากดูน้ำหนักตอนนี้", // single current-value read
		"สวัสดีครับ",           // greeting
	}
	for _, m := range notCompare {
		if compareRe.MatchString(m) {
			t.Errorf("%q: compareRe wrongly matched (not a comparison)", m)
		}
	}
}
