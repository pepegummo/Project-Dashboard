## Layer 4: Output Verification

### 4.1 `sanitizeEChartOption` — deterministic, ไม่ใช่ LLM

เอกสารเดิมเรียกขั้นนี้ว่า Spec Schema Validator และบอกให้เป็น deterministic
code อยู่แล้ว (`ajv`/`jsonschema` ตรวจ Vega-Lite schema) — โปรเจกต์นี้ทำแบบ
เดียวกันแต่เขียนเอง (`sanitizeEChartOption`, nl2sql.go) เพราะ target เป็น
ECharts option ไม่ใช่ Vega-Lite ที่มี schema library สำเร็จรูปให้ใช้:

1. `delete(m, "dataset")` — strip dataset ทิ้งไม่ว่าโมเดลจะใส่มาหรือไม่ (dataset
   จริงมาจาก `withDataset()` ฝั่ง frontend เท่านั้น ดูข้อ 3.2)
2. normalize `series` เป็น `[]map[string]any` เสมอ (`emitEChart` อาจคืนมาเป็น
   object เดี่ยวหรือ array) — รูปแบบอื่นถือว่า invalid → คืน `"{}"` ทันที
3. ต่อ series แต่ละตัว: `delete(s, "data")` (strip inline data ของ series),
   เช็ค `type` ต้องอยู่ใน whitelist `line`/`bar`/`pie`/`scatter` เท่านั้น — ไม่ใช่
   type นี้ หรือไม่มี `type` เลย → คืน `"{}"` ทันที (ทั้ง option ไม่ใช่แค่ series
   นั้น)
4. `validateEncodeValue` เช็คว่าทุกชื่อ column ที่ `encode` อ้างถึง (ไม่ว่าจะเป็น
   string เดี่ยวหรือ `[]any` ของ string) มีอยู่จริงใน `cols` ที่มาจาก query
   result — อ้าง column ที่ไม่มีจริง → `"{}"`
5. series ที่ผ่านทั้งหมดว่างเปล่า (0 ตัว) → `"{}"` เช่นกัน (chart ที่ไม่มี
   series render อะไรไม่ได้อยู่ดี)

`"{}"` คือ contract เดียวกับที่ frontend ใช้เป็นสัญญาณ "แสดงเป็นตาราง" —
`isTabular()` ใน `AskDataPage.vue` เช็คแค่ `Object.keys(option).length === 0`

### 4.2 `verifyAskChart` — LLM-as-judge เฉพาะ chart turn

เอกสารเดิมเป็น Self-Consistency Checker ที่เช็ค 4 มิติแยก (relevance,
data_alignment, chart_appropriateness, misleading_risk) ทุก turn ที่มีกราฟ —
ของจริงคือ `verifyAskChart` (nl2sql.go) เช็คมิติเดียวคือ "SQL/chart ตอบคำถามที่
ถามจริงไหม" และรัน **เฉพาะ turn ที่มี chart เท่านั้น** (เงื่อนไข
`if string(option) != "{}"` ใน `AskData`) — turn ที่เป็นตารางหรือ prose ไม่เสีย
call นี้เลย

โมเดล: `routerModel` = `openai/gpt-oss-20b` (เดียวกับที่ chat path ใช้ทำ
`ClassifyIntent`/`VerifyAnswer` — router.go) เรียกผ่าน `VerifyAnswerTool`
schema เดิม (`schema.go`) + `parseVerifyResult` (router.go) ที่ chat path ใช้
อยู่แล้ว — ไม่สร้าง schema/parser ใหม่ ใช้ของเดิมซ้ำ, timeout 6 วินาที,
`forceFunc("verify_answer")`

Prompt เฉพาะของ Ask-Data (`askVerifyPrompt`, ~200 tok, static เพื่อให้ Groq
prompt-cache ได้ — mirror `verifySystemPrompt` ของ chat path) คำต่อคำ:

> "MISMATCH (matches_intent: false) only when the SQL or chart targets a
> DIFFERENT metric, machine, or time window than the question asked, or the
> chart type contradicts a chart type the user explicitly requested (e.g.
> asked for a bar chart but got a pie chart)."
>
> "MATCH (matches_intent: true) otherwise — including a result that is
> imperfect but honestly answers what was asked (fewer points than ideal, a
> slightly different aggregation, a reasonable default time window when none
> was specified)."

**Contract: ไม่มี verdict = ผ่าน ห้าม block เด็ดขาด** — `verifyAskChart` คืน
`(VerifyResult{}, false)` ทุกกรณีที่ error/timeout/JSON parse พัง และ caller
(`verifyAndRepairChart`) ต้องอ่าน `false` ว่า "ไม่มีคำตัดสิน ส่งของเดิมไปเลย"
ไม่ใช่ "mismatch" — infrastructure ของ verifier เองพังต้องไม่ทำให้ chart ที่ดี
อยู่แล้วถูก block หรือ error

**Repair 1 รอบเท่านั้น** (`verifyAndRepairChart`): เมื่อ `ok && !v.MatchesIntent`
จะ re-run ทั้ง chain ใหม่ — `emitSQL` (พร้อม
`sqlFixup{SQL: sqlText, Err: "verifier: " + v.Problem}`) → `validateSQL` →
`runScoped` → เช็ค `hasNumericColumn` → `emitEChart` → `sanitizeEChartOption`
— **ไม่มี judge เรียกซ้ำรอบสองบนผลลัพธ์ที่ซ่อมแล้ว** ล้มเหลวจุดไหนก็ตามใน chain
นี้ (SQL parse ไม่ผ่าน, query error, ไม่มี numeric column, chart generate
ไม่ได้, sanitize reject) → ตกลงเป็น `option = "{}"` (ตาราง) **บน rows เดิมของ
คำตอบแรก** ไม่ใช่ HTTP error — judge ต้องไม่มีทางเปลี่ยนคำตอบที่ส่งได้อยู่แล้ว
ให้กลายเป็น 502

### 4.3 Error handling — แทนที่ Sandbox Execution Error Handler ด้วย retry loop ที่เป็นโค้ด

เอกสารเดิมมี LLM error classifier ที่จัดหมวด error (syntax/field_not_found/
type_mismatch/timeout/permission/other) แล้วตัดสินใจว่า retryable ไหมพร้อม
`fallback_action` หลายแบบ — **โปรเจกต์นี้ไม่มี LLM วิเคราะห์ error เลย** ใช้
retry loop 2 จุดที่เป็น deterministic control flow ล้วนใน `AskData`:

- **SQL: สูงสุด 3 attempt** — error จาก `validateSQL`/`runScoped` (ไม่ว่าจะเป็น
  syntax, column ไม่มีจริง, timeout, หรือ read-only violation) ถูกส่งกลับเข้า
  `sqlFixup.Err` แล้วป้อนกลับให้ `emitSQL` เป็น text ตรงๆ โดยไม่แยกประเภท ให้
  โมเดลอ่าน error message ของ Postgres/validator เองแล้วแก้ — รอบที่ 3 ยัง fail
  → HTTP 400 (ไม่ retry ต่อ)
- **Chart: 1 retry** (layer 3.2) — error จาก `emitEChart` ถูกป้อนกลับเป็น
  `prevErr` ครั้งเดียว ไม่แยกประเภทเช่นกัน ล้มครั้งที่สอง → table fallback

ไม่มีแนวคิด `retryable: true/false` หรือ `fallback_action` แยกหลายแบบ
(`table_only`/`text_only`/`ask_user_to_rephrase`) — fallback มีทางเดียวเสมอคือ
"ตอบเป็นตารางจาก rows ที่มี" (`option = "{}"`) ไม่เคยถึงขั้น "ขอให้ user ถามใหม่"
(ยกเว้น prose path ซึ่งแยกออกไปตั้งแต่ layer 1.1 อยู่แล้ว ไม่ใช่ error path)
