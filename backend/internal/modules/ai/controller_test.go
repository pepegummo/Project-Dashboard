package ai

import "testing"

// TestBuildGroqToolsPreviewGating verifies buildGroqTools drops the preview_*
// staging tools for viewers (read-only role) but keeps them for editor/admin —
// a viewer must never be offered a tool that dispatch() will then reject.
func TestBuildGroqToolsPreviewGating(t *testing.T) {
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

	if got := hasPreviewTool(buildGroqTools("viewer")); got {
		t.Errorf("buildGroqTools(%q) contains a preview_* tool, want none", "viewer")
	}
	for _, role := range []string{"editor", "admin"} {
		if got := hasPreviewTool(buildGroqTools(role)); !got {
			t.Errorf("buildGroqTools(%q) has no preview_* tool, want at least one", role)
		}
	}
}
