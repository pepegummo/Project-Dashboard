## Layer 1: Intent Understanding

### 1.1 Intent Classification — `answerable` ใน `emit_sql`

Design ทั่วไปเดิมใช้ LLM call แยกต่างหากเพื่อจัดประเภทคำถามเป็น enum 5 แบบ
(`qa_only`/`viz_only`/`qa_with_viz`/`followup`/`out_of_scope`) ก่อนจะเข้า pipeline
ส่วนถัดไป — **ของจริงในโปรเจกต์ไม่มี LLM call แยกสำหรับ intent เลย**: `answerable`
เป็นแค่ boolean field หนึ่งใน tool call `emit_sql` เดียวกับที่ generate SQL
(`emitSQLTool`, nl2sql.go) เพราะคำถามที่ตอบได้ส่วนใหญ่ต้อง generate SQL อยู่แล้ว
การแยก classify step ออกมาก่อนจึงเป็นการเสีย call ฟรีๆ โดยไม่ได้ข้อมูลเพิ่ม

Schema description ของ field นี้ (`emitSQLTool.input_schema.properties.answerable`,
nl2sql.go) คำต่อคำ:

> "false ONLY for a greeting, chit-chat, or clearly non-factory input (then leave
> sql empty). A data-listing question ('which SKUs', 'what machines', 'list
> values') is answerable=true."

พูดง่ายๆ คือ binary classification (เป็นคำถามเกี่ยวกับข้อมูลโรงงานหรือไม่) ไม่ใช่
5-enum แบบเดิม ส่วน "followup" กับ "out_of_scope" ที่เอกสารเดิมแยกเป็น intent
ของตัวเอง ในของจริงกลายเป็น:

- **followup** ไม่ต้อง classify แยก — จัดการผ่าน `prevTurn` context: เมื่อ
  `prev.SQL != ""` system prompt ของ `emitSQL` จะบอกโมเดลตรงๆ ว่า "The user
  previously asked... which ran this SQL... If the new message refines or
  restyles that chart... adapt the previous SQL to answer it" แล้วปล่อยให้
  โมเดลตัดสินใจเองว่าเป็นคำถามใหม่หรือ refine ของเดิม (เช่น "เอาเป็นกราฟแท่ง" — SQL
  เดิม ขอแค่เปลี่ยน chart type)
- **out_of_scope** คือ `answerable=false` เฉยๆ — `parseSQLEmission` คืน error
  `errNotDataQuestion` แล้ว `AskData` เรียก `emitProse` แทนเพื่อตอบเป็น prose
  (ไม่ error ไม่ reject คำถาม)

### 1.2 Slot / Entity Extraction — แทนที่ด้วย schema grounding

เอกสารเดิมมี LLM call แยกที่ดึง metric/dimension/time_range/filters/comparison
ออกมาเป็น JSON กลางก่อน แล้วค่อยส่งต่อให้ query generator — **ของจริงไม่มี
extractor แยกเลย** เพราะ `buildSchemaContext` (nl2sql.go) query schema จริงจาก
DB (`v_machines`, `v_machine_fields` ผ่าน `runScoped`) แล้วฝังชื่อ machine/metric
ที่มีอยู่จริงในองค์กรนั้นเข้าไปใน system prompt ของ `emitSQL` ตรงๆ — โมเดล resolve
slot (machine ไหน, metric ไหน) อยู่ใน call เดียวกับที่ generate SQL เลย ไม่มี JSON
กลางที่บอกว่า `metric: null, missing_required: [...]`

`buildSchemaContext` ทำ query จริง 2 ตัวต่อ turn (ผ่าน `runScoped`, org-scoped):
- `SELECT name FROM v_machines ORDER BY name LIMIT 200` → รายชื่อ machine จริง
- `SELECT DISTINCT key, label, COALESCE(unit,'') FROM v_machine_fields ORDER BY
  key LIMIT 200` → metric key จริงพร้อม label/unit

แถมคำอธิบายว่า `sku` เป็น text dimension ที่อยู่ใน `data` JSONB (ไม่ใช่ numeric
metric) เพื่อกันโมเดล hallucinate ว่า sku เป็นตัวเลข

### 1.3 Clarification — field `clarification` ใน `emit_sql` เดียวกัน

เอกสารเดิมมี Clarification Question Generator เป็น LLM call แยกที่รับ
`missing_required` เข้ามาแล้วแต่งคำถามกลับ — **ของจริงรวมเข้ากับ `emit_sql`**
เช่นกัน: field `clarification` ใน `emitSQLTool` (nl2sql.go) มี description
คำต่อคำว่า:

> "Set ONLY when the question IS about factory data but you cannot determine
> WHAT to query — no identifiable metric/machine/dimension. ONE short question
> in the user's language, offering concrete choices from the schema. Leave
> empty when a sensible default exists (no time range → assume last 24h).
> Never set together with sql."

3 กฎที่ตรงกับเจตนาของ 1.3 เดิม:

- **one-question rule** — "ONE short question" (เหมือนเอกสารเดิมข้อ 1.3)
- **default แทนการถาม** — ไม่มีช่วงเวลาไม่ต้องถาม ให้ default เป็น 24 ชั่วโมงล่าสุด
  แทน กฎนี้เขียนแน่นตั้งใจ เพื่อกันการถาม clarification บ่อยเกินไปจนรบกวน user
  โดยไม่จำเป็น
- **`sql` กับ `clarification` set พร้อมกันไม่ได้** — คุมด้วย struct `sqlEmission`
  (มีแค่ `SQL` หรือ `Clarification` อย่างใดอย่างหนึ่ง) และ `parseSQLEmission` ที่
  ให้ clarification ชนะถ้าทั้งคู่ set มาจริง (defensive parsing ฝั่ง Go แม้ prompt
  จะสั่งห้ามไว้แล้ว)

**Reply flow — ห้ามถามซ้ำ:** `prevTurn` มี field `Clarification string` เพิ่ม
จาก `SQL string` เดิม — mutually exclusive กัน (เทิร์นก่อนหน้าตอบด้วย SQL หรือ
ถาม clarification อย่างใดอย่างหนึ่ง ไม่ทั้งคู่) เมื่อ `prev.Clarification != ""`
ระบบจะ inject ข้อความนี้เข้า system prompt ของ `emitSQL` รอบถัดไปคำต่อคำ:

> "The user originally asked: \"...\", and you asked them a clarifying question:
> \"...\". The current message is their reply to that question — combine the
> original question and this reply into ONE SQL query that answers it. Do not
> set clarification again; never ask for clarification a second time in a row."

ฝั่ง handler: `AskData` รับ `context.clarification` จาก request body คู่กับ
`context.sql` (mutually exclusive เหมือนกัน — ถ้า caller ส่งมาทั้งคู่ SQL ชนะ
ตาม logic ใน `AskData`) เมื่อ `emission.Clarification != ""` จะ `return` ทันที
โดยไม่รัน SQL ใดๆ เลย: `{"success": true, "data": {"clarification": "..."}}`
