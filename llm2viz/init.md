# LLM2Viz — System Prompts รายละเอียดแต่ละ Layer

แต่ละ prompt ด้านล่างออกแบบให้ใช้เป็น system prompt แยกของ LLM call แต่ละตัวใน pipeline
(ไม่ใช่ prompt เดียวทำทุกอย่าง — แยก concern เพื่อให้ debug และตรวจสอบง่าย)

หลักการสำคัญ: ทุก step ที่เป็น "ตรวจสอบเชิงโครงสร้าง" (schema validity, SQL safety)
ควรเป็น **deterministic code** ไม่ใช่ LLM call — ใช้ LLM เฉพาะจุดที่ต้องการ
semantic judgment จริงๆ (intent, chart appropriateness, consistency) เพื่อลด
cost, latency, และความไม่แน่นอนของผลลัพธ์

## Pipeline จริงในโปรเจกต์ (Ask-Data feature)

หลักการข้างบนถูกนำไปใช้จริงใน endpoint เดียว: **`POST /ai/ask`** (handler `AskData`,
`backend/internal/modules/ai/nl2sql.go`) — คนละ handler กับ chat tool-calling
path (`Controller.Chat`, `controller.go`) ที่มี router + verify-then-repair ของตัวเอง
(`router.go`) เอกสารชุดนี้ (layer 1–5) อธิบายเฉพาะ `AskData`

ความต่างจาก design ทั่วไปที่เอกสารชุดนี้เคยอธิบายไว้ก่อนหน้า มี 2 จุดใหญ่:

1. **ไม่มี "JSON only" prompting** — ทุก LLM call ที่ต้องการ output แบบมีโครงสร้าง
   ใช้ Groq function-calling บังคับ `tool_choice` ไปที่ function เดียวด้วย
   `forceFunc(name string)` (`controller.go`) เช่น `forceFunc("emit_sql")`,
   `forceFunc("emit_echart_option")`, `forceFunc("verify_answer")` แทนการสั่งด้วย
   prompt ว่า "ตอบเป็น JSON เท่านั้น" แล้วหวังว่าโมเดลจะเชื่อฟัง — Groq tool schema
   เป็นตัวบังคับโครงสร้างเอาต์พุต ไม่ใช่ prompt wording
2. **โมเดลแค่ 2 ตัว ไม่ใช่โมเดลแยกทุก layer**:
   - **generate** (`emitSQL`, `emitProse`, `emitEChart`) ใช้ `groqModel` =
     `openai/gpt-oss-120b` (ค่าคงที่ใน `controller.go`)
   - **judge** (`verifyAskChart`) ใช้ `routerModel` = `openai/gpt-oss-20b`
     (ค่าคงที่ใน `router.go`) — โมเดลเดียวกับที่ chat path ใช้ทำ intent routing
     (`ClassifyIntent`) และ verify-then-repair (`VerifyAnswer`)

### Layer ↔ Code map

| Layer | ฟังก์ชัน / ไฟล์ | โมเดล / กลไก |
|---|---|---|
| 1.1 Intent (`answerable`) | `emitSQL` → `parseSQLEmission` (nl2sql.go) | openai/gpt-oss-120b, `forceFunc("emit_sql")` |
| 1.2 Slot grounding | `buildSchemaContext` (nl2sql.go) | Go (deterministic) — live query ผ่าน `runScoped` |
| 1.3 Clarification | field `clarification` ใน `emit_sql` call เดียวกับ 1.1 | openai/gpt-oss-120b |
| 2.1 SQL generation | `emitSQL` (nl2sql.go) | openai/gpt-oss-120b, `forceFunc("emit_sql")` |
| 2.2 SQL validation + runtime guard | `validateSQL`, `runScoped` (nl2sql.go) | Go (deterministic) |
| 3.1 Chart-type pick | ฝังใน `echartSystemPrompt` ของ `emitEChart` + gate `hasNumericColumn` | 120b (เลือก type) / Go (ตัดสินใจว่าจะเรียกเลยไหม) |
| 3.2 Chart spec generation | `emitEChart` (nl2sql.go) | openai/gpt-oss-120b, `forceFunc("emit_echart_option")` |
| 4.1 Spec sanitize | `sanitizeEChartOption` (nl2sql.go) | Go (deterministic) |
| 4.2 Self-consistency judge | `verifyAskChart` → `verifyAndRepairChart` (nl2sql.go) | openai/gpt-oss-20b, `forceFunc("verify_answer")` |
| 4.3 Error handling | retry loop ใน `AskData` (SQL ×3, chart ×1) | Go (deterministic) — ไม่มี LLM classifier |
| 5 Orchestration | handler `AskData` (nl2sql.go) เอง | Go |

### Call budget ต่อ 1 turn

อ้าง comment จริงใน `AskData` (nl2sql.go) คำต่อคำ:

> "B1: judge gate, chart turns only (table/prose turns are free — no call). Call
> budget per turn: SQL 1(-3 on retry) + chart 1(-2 on retry) + judge 1 (~1s);
> worst case with the judge's one repair round adds SQL 1 + chart 1(-2) more —
> still well inside the 45s handler ctx, and a repair failure degrades to the
> table, never a 502."

แปลเป็นรูปแบบตาราง (prose/table สองแถวแรกไม่มี comment ตรงคำต่อคำ แต่ไล่จาก
control flow ได้ตรงไปตรงมา):

| ประเภท turn | จำนวน call |
|---|---|
| prose (ไม่ใช่ data question) | `emitSQL` 1 + `emitProse` 1 = **2 calls** |
| table (ไม่มี numeric column ให้ chart) | `emitSQL` **1–3 calls** (retry loop), ไม่มี chart/judge เลย เพราะ `hasNumericColumn` gate ตัดตั้งแต่ก่อนเรียก `emitEChart` |
| chart | SQL 1(-3) + chart 1(-2) + judge 1 (~1s) = **2 + 1 judge** |
| chart, judge สั่ง repair (worst case) | ข้างบน + SQL 1 + chart 1(-2) เพิ่ม |

ทั้งหมดอยู่ภายใน context timeout **45 วินาที** ของ handler เดียว
(`ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)`,
`AskData`)
