# Skill: Printable A4 HTML Reports (Print to PDF)

> Use when creating or updating a printable manual/report — single-file HTML + screenshots + Mermaid.
> The report **output is Thai** (formal language); this guide is in English.
> Reference example, if the report has one: `{MAIN_HTML}` (adjust paths/content to the project you're working in).

---

## Before you start — set project context

Replace the placeholders below to match the current project (no need to edit this skill every time — note them in the prompt or in the report's `AGENTS.md`):

| Placeholder | Meaning | Example |
|-------------|---------|---------|
| `{REPORT_DIR}` | Report folder | `docs/line-recheck-report/` |
| `{MAIN_HTML}` | Main HTML file | `{REPORT_DIR}index.html` |
| `{IMG_DIR}` | Screenshots | `{REPORT_DIR}images/ui/` |
| `{DOCS_ROOT}` | System docs | `docs/` |
| `{APP_BASE}` | App URL when capturing | `http://localhost` |
| `{SCREENSHOT_SCRIPT}` | Playwright script (if any) | `frontend/scripts/capture-ui-screenshots.mjs` |

---

## Goal

Produce a formal Thai-language report that supports:

- On-screen viewing (continuous scroll)
- **Print / Save as PDF** at A4 portrait
- Typical structure: cover · table of contents · introduction · user section · technical section (if any) · appendix
- UI screenshots and Mermaid diagrams (internet needed on first open — diagrams bake into the PDF when printed)

---

## File structure (suggested)

```
{REPORT_DIR}
├── index.html              # main file (all pages live here)
├── images/ui/*.png         # UI screenshots (or images/ per theme)
├── skill.md                # this file — general workflow
└── AGENTS.md               # (recommended) report/project-specific guide
```

The capture script (if any) usually lives outside `{REPORT_DIR}` — point `OUT_DIR` at `{IMG_DIR}`:

```bash
# example: run the app first, then capture
cd <app-root> && node {SCREENSHOT_SCRIPT}
# or
BASE_URL={APP_BASE} OUT_DIR={IMG_DIR} node {SCREENSHOT_SCRIPT}
```

---

## Core principle: CSS-first, JS only as a fallback

Pages must fit **by construction**, not by JavaScript measuring and shrinking them afterward.

- **Primary approach:** design each page to fit, and use CSS `break-inside: avoid` on atomic blocks (tables, figures, `.note`, list boxes) so they never split across a page.
- The fixed model **1 `.sheet` = 1 A4 page** (below) is a deliberate choice for precise control — fine to use, but it's still CSS-driven layout, not JS.
- Only reach for the JS fitters (`fitSheetsForPrint()`, `expandUiShot()`, `fitDiagram()`, `print-compact`) as a **last-resort fallback** when a specific page still overflows and you can't restructure it. They are patches for the `297mm + overflow:hidden` model fighting the print engine — not the main mechanism.

---

## Minimal skeleton (copy-paste starter)

```html
<!doctype html>
<html lang="th">
<head>
<meta charset="utf-8">
<style>
  @page { size: A4 portrait; margin: 0; }
  body { margin: 0; font-family: "Sarabun", sans-serif; counter-reset: pg; }
  .sheet {
    width: 210mm; height: 297mm; box-sizing: border-box;
    padding: 18mm 16mm; display: flex; flex-direction: column;
    overflow: hidden; page-break-inside: avoid;
  }
  .sheet + .sheet { page-break-before: always; }
  .hdr { display: flex; justify-content: space-between; font-size: 9pt; color: #555; }
  .sec { font-size: 15pt; margin: 6mm 0 3mm; }
  p.indent { text-indent: 10mm; text-align: left; }
  .note, .flow, .route, table, h3.fix-cat { text-indent: 0; }
  table, figure, .note { break-inside: avoid; }
  .ftr { margin-top: auto; font-size: 9pt; color: #555;
         counter-increment: pg; display: flex; justify-content: space-between; }
  .ftr::after { content: counter(pg); }
  @media print { body > script { display: none !important; } }
</style>
</head>
<body>
  <div class="sheet">
    <div class="hdr"><span>ภาคที่ 1</span><span>ข้อ 1</span></div>
    <h2 class="sec">หัวข้อ</h2>
    <p class="indent">เนื้อหา…</p>
    <div class="ftr"><span>ข้อ 1　ชื่อสั้น</span></div>
  </div>
</body>
</html>
```

---

## Report workflow (suggested order)

### 1. Gather content

- Read project docs: README, architecture, workflow, API, troubleshooting, main agent guide
- Split into **user section** (operational language, no deep CLI) vs **technical section** (architecture, deploy, dev troubleshooting) — adjust the number of sections to fit
- The introduction should include at least: document purpose · origin/importance of the system · high-level overview · a table summarizing each section

### 2. Design pagination

- One `<div class="sheet">` = one A4 page
- Add an HTML comment before each sheet, e.g. `<!-- Item 3: task management -->`
- Number pages with a CSS counter (`counter-increment: pg` on `.sheet`, shown via `.ftr::after`)
- **Update the table of contents every time you add/remove/merge a page**

### 3. Write HTML per the pattern

Each page (except cover/section-divider):

```html
<div class="sheet sheet-ui">   <!-- or sheet-diagram / sheet-intro -->
  <div class="hdr"><span>ภาคที่ 1</span><span>ข้อ N</span></div>
  <h2 class="sec">หัวข้อ</h2>
  <p class="meta"><strong>ที่อยู่:</strong> <span class="route">/path</span></p>
  <!-- content -->
  <div class="ftr"><span>ข้อ N　ชื่อสั้น</span></div>
</div>
```

### 4. Language and text style (report stays Thai)

Use formal Thai. Preferred vs avoid:

| Use | Avoid |
|-----|-------|
| ขั้นตอนการดำเนินการ | ทำอะไร |
| การสร้างรายการด้วยตนเอง | สร้างมือ |
| เลือกปุ่ม / ขยายแถว | กด / คลิก (in formal docs) |
| body paragraph `p.indent` | indent inside `.note`, `.flow`, `.route`, tables, list-in-box |

- Main body paragraph: `p.indent` (`text-indent: 10mm`)
- Address/purpose: `p.meta`
- Short description: `p.hint`, `p.scenario`
- Note box: `div.note`
- Path / command: `span.route` or `div.flow`

### 5. UI screenshots

- Class `ui-shot` + `p.fig-caption` after it
- Page with screenshot + long table: add `sheet-ui`
- Very dense page: split the sheet or move the tail to the next page
- The list of routes to capture — define it in `{SCREENSHOT_SCRIPT}` or the report's `AGENTS.md`

### 6. Mermaid (technical section)

- Put it in `<pre class="mermaid">` inside `div.diagram`
- Use `sheet-diagram` on the sheet
- Must load the CDN before printing — wait for `mermaid.run()` to finish
- Avoid Mermaid in the user section (hard to read on paper)

### 7. Print-preview check

1. Open `{MAIN_HTML}` in Chrome/Edge (internet on first open if using the Mermaid CDN)
2. Wait for all diagrams to render (if any)
3. Ctrl+P → A4, margin 0 (per `@page`), enable **Background graphics**
4. Check every page: **content doesn't overflow the footer**, images aren't tiny when space is free, no blank page at the end of the PDF (or delete the trailing page yourself)

### 8. Document version

- Show on the cover: `<div><strong>เวอร์ชันเอกสาร</strong>　1.0</div>`
- Bump when content or page structure changes significantly

---

## Fonts and PDF portability

The PDF's appearance depends on the fonts available where it's generated. If the report relies on system Thai fonts (Sarabun / TH Sarabun), it will render differently on machines that lack them.

- **Recommended:** embed a Thai web font (`@font-face` with the font file, or a self-hosted WOFF2) so every machine renders identically.
- **At minimum:** document the font dependency in the report's `AGENTS.md`, and generate the delivery PDF on a machine that has the font installed.

---

## Key CSS classes

| Class | Use when |
|-------|----------|
| `sheet` | every A4 page |
| `sheet cover` | cover page |
| `sheet chapter` | section-divider page |
| `sheet-intro` | introduction (tighter spacing when printing) |
| `sheet-ui` | page with screenshot / UI table |
| `sheet-diagram` | Mermaid page |
| `print-compact` | added by JS when a page overflows — reduces font/spacing (fallback only) |
| `sheet-last` | last page (helps avoid a blank trailing PDF page) |

Content-specific tables (name by content): `fix-table`, `h3.fix-cat`, `tbl-symptom`, etc.

---

## Print layout (principles)

- `.sheet`: 210×297mm, `display: flex; flex-direction: column`, `overflow: hidden`
- `.ftr`: `margin-top: auto` — footer always pinned to the bottom
- Page breaks in `@media print`:
  - **Recommended:** `.sheet + .sheet { page-break-before: always; }` (doesn't force a break after the last page)
  - **Alternative:** `page-break-after: avoid` on `.sheet-last` or `body > .sheet:last-of-type`
- Hide `<script>` when printing if the document ends with a blank page: `body > script { display: none !important; }`
- Put `break-inside: avoid` on atomic blocks (tables, figures, notes) so they don't split across pages — this is what removes most overflow before any JS runs.

---

## Common problems and fixes

### 1. Print preview — content overflows the footer

**Symptom:** looks fine on screen, but when printing text or tables overlap or spill past the footer.

**Cause:** `.sheet` has a fixed 297mm height + `overflow: hidden`; the footer sits at the bottom via `margin-top: auto`.

**Fix (in order):**

1. **Split the page** — move the tail (table, note, list) to a new sheet.
2. Apply `break-inside: avoid` to the blocks that shouldn't split, and trim content so the page fits by construction.
3. Don't cap `.ui-shot` too low in `@media print` — use a high ceiling (~155mm) and let the image size to the real space.
4. **Fallback only** — if it still overflows and can't be restructured, let JS `fitSheetsForPrint()` run on `beforeprint` and add `print-compact` to shrink font/images.

**Most common on:** pages with screenshot + long table, long schedule/form pages, pages combining a diagram + a table.

### 2. Screenshot is small while the page still has free space

**Cause:** a fixed `max-height` applied to every page.

**Fix:**

- Remove the low cap from print CSS
- **Fallback only** — use `expandUiShot()` on `beforeprint`: compute the space between the image and the next element (or the footer), subtract the `fig-caption` height, and only shrink when `sheetOverflows()` is true.

### 3. Indent (text-indent) leaks into boxes / tables

**Symptom:** text in `.route`, list items in a table, or a note box shows an indent.

**Fix:**

- `text-indent: 0` on `.note`, `.flow`, `.route`, `table`, `h3.fix-cat` and children in troubleshooting tables
- Don't put `p.indent` on a paragraph that contains `.route` — use a plain `<p>`
- Don't use `text-align: justify` on paragraphs (use `left`)

### 4. TOC page numbers don't match

**Cause:** added/removed/merged a sheet without updating the TOC.

**Fix:**

- Count `<div class="sheet"` after every structural change (script or count in the editor)
- Update `toc-item` on the TOC page (use page ranges, e.g. `10–13`)
- Verify the real footer in Print Preview against the numbers in the TOC

### 5. Blank page added at the end of the PDF

**Symptom:** content is done, but the PDF has one extra empty page.

**Possible causes:**

1. `page-break-after: always` on the last sheet
2. A `<script>` after the last sheet
3. The last page's content overflows slightly

**Fix:**

- Use `.sheet + .sheet { page-break-before: always; }` instead of `page-break-after` on every sheet
- Add `sheet-last` on the last page + `page-break-after: avoid !important`
- Hide the script when printing, or move it to `<head defer>`
- If it persists: delete the blank page in the PDF before delivery

### 6. Mermaid doesn't render / disappears when printing

- Needs internet (CDN) when the file is opened
- Wait for `mermaid.run()` to finish before printing
- `fitDiagram()` shrinks the SVG if it exceeds the footer (fallback only)
- The PDF recipient doesn't need internet — the diagrams are already baked into the PDF

### 7. TOC longer than one page

- Reduce sub-items, or use page ranges instead of listing every sub-heading
- Adjust the font/spacing of `.toc-item`

---

## Pre-delivery checklist

- [ ] TOC matches Print Preview page numbers
- [ ] No page overflows the footer (spot-check screenshot + long-table + diagram pages)
- [ ] No blank page at the end of the PDF (or deleted)
- [ ] Formal Thai, consistent throughout the document
- [ ] `th` bold (`font-weight: 700`)
- [ ] Document version on the cover is updated
- [ ] All screenshots present per the report's route list
- [ ] Thai font embedded, or PDF generated on a machine that has it
- [ ] Deliver the **PDF** to readers (no need to ship HTML + image folder)

---

## References (adjust paths per project)

- Report-specific guide (if any): `AGENTS.md` in `{REPORT_DIR}`
- Complete HTML example: `{MAIN_HTML}`
- System docs: `{DOCS_ROOT}`
