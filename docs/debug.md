# IotVision AI — Debug Log

Bugs found in the AI assistant flow, their root cause, and the fix. Newest first.
`✅ Fixed` = applied in code. `🔶 Open` = known, deliberately not fixed yet.

---

## Bug 1 — Groq 502: "Tool choice is required, but model did not call a tool" ✅ Fixed

**Symptom:** Asking anything while a dashboard/preview is on screen returned
`Groq API error: Groq API: Tool choice is required, but model did not call a tool.`
The whole chat turn failed with a 502.

**Root cause:** `backend/internal/modules/ai/controller.go` (`Chat`) forces
`tool_choice:"required"` on the first Groq turn whenever `body.Context != ""`
(a dashboard/preview is visible). When `openai/gpt-oss-120b` chose to answer in
plain text instead of calling a tool, Groq rejected the request. The recovery
block only handled the *opposite* error (`"Tool choice is none"`) — there was no
branch for `"Tool choice is required"`, so it fell through to the 502.

**Fix:** Added a sibling retry that re-calls Groq with auto tool choice when the
error contains `"Tool choice is required"`, making `required` a best-effort nudge:

```go
if err != nil && strings.Contains(err.Error(), "Tool choice is required") {
    resp, err = callGroq(msgs, callTools, "")
}
```

`controller.go`, in the `Chat` loop next to the existing `"Tool choice is none"`
branch.

---

## Bug 2 — Live ("focus") card: add widget / change config does nothing, no error ✅ Fixed

**Symptom:** On the AI Assistant page, with a "Live" card showing (the focus card
from `show_metric`, e.g. after "count chart"), clicking **Add Widget** or editing
a widget's config and saving did nothing. No error, no toast.

**Root cause:** The Live card renders the Add Widget button + config modal
(`frontend/src/components/ai/PreviewCanvasCard.vue`, no variant gate) and emits
`add-widget` / `update-widget` on save — but the **focus-card binding in
`frontend/src/pages/AIAssistantPage.vue` omitted `@add-widget` and
`@update-widget`** (only the `kind:'preview'` binding had them). The emits went
nowhere. The handlers also matched `kind === 'preview'` only.

**Fix:** In `AIAssistantPage.vue`:
- Added `@add-widget="addPreviewWidget"` and `@update-widget="updatePreviewWidget"`
  to the `card.kind === 'focus'` `<PreviewCanvasCard>`.
- Broadened `addPreviewWidget` / `updatePreviewWidget` to
  `find(c => c.kind === 'preview' || c.kind === 'focus')`.

Re-render then happens via the existing `GridStackCanvas` fingerprint watcher
(`components/dashboard/GridStackCanvas.vue`) — no `:key` change needed.

---

## Bug 3 — Live ("focus") card vanishes on browser refresh ✅ Fixed

**Symptom:** A Live card disappeared after refreshing the page.

**Root cause:** The persistence watcher in `AIAssistantPage.vue` only saved
`kind === 'preview'` cards via `PUT /preview-draft`. Focus cards were never
written, and `onMounted` restore only ever rebuilt a `kind:'preview'` card.

**Fix:** In `AIAssistantPage.vue`:
- Save watcher now matches `preview || focus` and stores `kind` in the draft
  payload: `data: { kind, result, args }`.
- Restore rebuilds the card with its saved `kind` (defaults to `'preview'` for
  old drafts), and **prefers a saved card (`draft.data.result`) over auto-loading
  `draft.dashboardId`**, so a pending card wins on reload instead of being
  replaced by a live dashboard.

---

## Bug 4 — Silent add-widget failures (no feedback) ✅ Fixed

**Symptom:** When a widget save failed (or no dashboard was loaded), the config
modal stayed open / nothing happened, with no message.

**Root cause:** `frontend/src/stores/dashboard.store.ts` `addWidget` did
`if (!currentDashboard.value) return;` (silent no-op), and `onSaveWidget` in both
`AIAssistantPage.vue` and `DashboardEditorPage.vue` had no try/catch — a rejected
`addWidget` left the modal open with no toast.

**Fix:**
- `dashboard.store.ts`: `throw new Error('No dashboard loaded')` instead of the
  silent return.
- `onSaveWidget` (both pages): wrapped in `try/catch` → toast on error, `finally`
  closes the modal.

---

## Bug 5 — `ai_preview_drafts` single-row clobber 🔶 Open (out of scope)

**Symptom (latent):** A saved **Preview** card can be wiped when a dashboard gets
selected. The user wasn't hitting this (they were on a focus card with no
dashboard selected), so it's documented, not fixed.

**Root cause:** `ai_preview_drafts` is **one row per user**
(`ON CONFLICT (user_id)`). `backend/internal/modules/ai/repository.go`:
- `UpsertDashboard` sets `data = NULL` (drops any saved preview),
- `UpsertDraft` sets `dashboard_id = NULL`.

`frontend/src/layouts/AppLayout.vue` watches `currentDashboard.id` and calls
`putSelectedDashboard` on *any* change → `UpsertDashboard` → nulls the saved
preview. The restore-ordering fix in Bug 3 mitigates the symptom but not the
clobber itself.

**Proposed fix (not applied):** Stop each upsert from nulling the other column so
the row can hold both a preview and a selected dashboard; decide a precedence rule
on restore (pending preview should win). Risk: a stale preview re-appearing after
the user deliberately selects a dashboard — needs a clear "which wins" rule before
implementing.

---

## Doc drift fixed in `ai-sequence.html`

While updating the sequence diagram to the current flow:
- Model `openai/gpt-oss-20b` → **`openai/gpt-oss-120b`** (matches
  `controller.go` `groqModel`).
- Tool count **15 → 14**; removed `get_latest_telemetry` (not in `AllTools()`);
  READ list is 6 tools.
- Documented `tool_choice:"required"` on turn 0 when `context` is present, with
  the auto retry (Bug 1).
- Focus card described as **editable + persisted across refresh** (Bugs 2 & 3),
  not "ephemeral, not saved".
