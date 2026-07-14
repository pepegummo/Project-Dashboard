## Layer 2: Query Planning

### 2.1 Query Generator — `emitSQL` (nl2sql.go)

โครงสร้าง view ที่โมเดลอ้างอิงได้จริง (สร้างโดย `buildSchemaContext`) มีแค่ 3
view read-only ที่ org-scope มาแล้ว — ตายตัว ไม่ใช่ placeholder ที่ inject มาแบบ
dynamic ทั้งฐานข้อมูลแบบ `{{ DATABASE_SCHEMA }}` ในเอกสารเดิม:

```
v_telemetry(machine_id uuid, machine_name text, ts timestamptz, data jsonb)
v_machines(id uuid, name text, type text, status text)
v_machine_fields(machine_id uuid, machine_name text, key text, label text, unit text)
```

metric value ทุกตัวอยู่ใน `data` JSONB column เดียวของ `v_telemetry` ต้องอ่านผ่าน
`(data->>'key')::float` สำหรับตัวเลข หรือ `data->>'key'` เฉยๆ สำหรับ text
dimension (เช่น `sku`)

กฎที่ฝังใน system prompt ของ `emitSQL` (ทั้งใน `buildSchemaContext` และใน `sp`
ของ `emitSQL` เอง, nl2sql.go):

- **1 SELECT เดียว** ห้าม semicolon ห้าม CTE ห้าม INSERT/UPDATE/DELETE/DDL
- คำถามเกี่ยวกับช่วงเวลา/trend ("last N hours", "over time", "per hour",
  "trend", "แนวโน้ม") ต้องคืน time series ด้วย `GROUP BY time_bucket('<interval>',
  ts) AS bucket` และ `ORDER BY bucket` — ไม่ใช่ scalar เดี่ยว โดยเลือก interval
  ให้ window 24h ได้ราว 24 จุด (1 hour), window 7d ได้ราว 7 จุด (1 day)
- **relative window ต้อง bound ด้วย `now()` เสมอ** — ทั้งภาษาไทยและอังกฤษ
  ("ย้อนหลัง N", "ล่าสุด" / "past/last N units", "recent", "latest") ต้องแปลงเป็น
  `WHERE ts > now() - interval 'N <unit>'` ห้าม hardcode วันที่ (คำถามข้ามภาษา
  map เข้ากฎ SQL เดียวกัน)
- **match ชื่อเครื่องด้วย `ILIKE '%code%'`** ไม่ใช่ `=` เพราะชื่อจริงมี prefix
  บรรยาย (เช่น user พิมพ์ "CW-01" แต่ชื่อจริงคือ "Checkweigher CW-01") ใช้กฎ
  เดียวกันกับ `v_machines.name`
- คำถามแบบ "มี X อะไรบ้าง / มีค่าอะไรบ้าง" (listing) ไม่ใช่ time series — ใช้
  `SELECT DISTINCT data->>'sku' AS sku FROM v_telemetry WHERE data->>'sku' IS
  NOT NULL ORDER BY sku`
- ตั้ง column alias ให้อ่านง่ายเสมอ (`bucket`, `machine_name`, `avg_speed`, ...)
  และใส่ `LIMIT` เสมอ **สูงสุด 5000** (ไม่ใช่ default 1000 แบบเอกสารเดิม) —
  aggregate ข้อมูลดิบเป็น bucket/group แทนที่จะดึงทุกแถว

ตัวอย่างจริงที่ฝังอยู่ใน prompt ของ `emitSQL` คำต่อคำ:

```sql
-- "avg speed last 24h for CW-01"
SELECT time_bucket('1 hour', ts) AS bucket, avg((data->>'speed')::float) AS avg_speed
FROM v_telemetry WHERE machine_name ILIKE '%CW-01%' AND ts > now() - interval '24 hours'
GROUP BY bucket ORDER BY bucket LIMIT 5000

-- "which SKUs does CW-01 run" (a listing question, answerable=true)
SELECT DISTINCT data->>'sku' AS sku FROM v_telemetry WHERE machine_name ILIKE '%CW-01%'
AND data->>'sku' IS NOT NULL ORDER BY sku LIMIT 100
```

โมเดล generate SQL เป็น string ตรงๆ ผ่าน tool call `emit_sql`
(`forceFunc("emit_sql")`) — ไม่มี JSON wrapper ที่มี `expected_columns`/
`explanation` แบบเอกสารเดิม และไม่มี branch ตอบกลับ
`{"error": "field_not_available", ...}` แยก เพราะ "generate ไม่ได้" ในของจริง
แปลว่า `answerable=false` (ไปตอบ prose) หรือ `clarification` (ไปถามกลับ) — ไม่มี
error state ที่สาม

### 2.2 Query Validation — deterministic ล้วน ไม่มี LLM validator

**นี่คือจุดที่ init.md ของเอกสารชุดนี้เองบอกไว้แล้ว**: "ทุก step ที่เป็นตรวจสอบ
เชิงโครงสร้าง ควรเป็น deterministic code ไม่ใช่ LLM call" — โปรเจกต์นี้เลยไม่มี
LLM SQL validator (เอกสารเดิมข้อ 2.2) เลย แทนที่ทั้งหมดด้วย Go function 2 ตัว:

**`validateSQL(sqlText string) (string, error)`** — ตรวจก่อน execute:
1. ต้องเป็น statement เดียว (มี `;` ภายในไม่ได้)
2. trim แล้วต้องขึ้นต้นด้วย `select`
3. `sqlForbidden` — regex กัน write/DDL keyword แบบ whole-word:
   `insert|update|delete|drop|alter|create|grant|revoke|truncate|copy|merge|
   into|call|do`
4. `deniedTables` — regex กันการอ้างถึง base table หรือ system catalog ตรงๆ
   (`telemetry_raw`, `telemetry_aggregates`, `machines`, `users`,
   `organizations`, `information_schema`, `pg_[a-z_]+` ฯลฯ) — scrub ชื่อ view
   ที่อนุญาต (`v_machines`, `v_machine_fields`, `v_telemetry`) ออกจากสตริงก่อน
   สแกน กัน false positive ที่คำว่า "machines" ใน "v_machines" ไปโดน rule ของ
   "machines"

**`runScoped(ctx, orgID, sqlText)`** — runtime guard ตอน execute จริง:
- read-only transaction: `pgx.TxOptions{AccessMode: pgx.ReadOnly}`
- `SET LOCAL statement_timeout = '5s'`
- org isolation ผ่าน `SELECT set_config('app.current_org', $1, true)` (GUC ที่
  ทุก view กรองตามอยู่แล้ว, `is_local=true` เคลียร์เองตอน rollback)
- row cap **5000 แถว** (`maxRows` — ตัด loop ที่ `len(rows) >= maxRows`)
- context timeout **8 วินาที** ครอบทั้ง query

`validateSQL` เป็น defense-in-depth ที่ให้ error message ชัดกว่า error ดิบจาก
Postgres — ตัว guard จริงที่ป้องกัน write และการหลุด org คือ read-only tx +
`app.current_org` GUC scoping ใน `runScoped`

**Retry loop 3 ครั้ง** (`AskData`, `for attempt := 1; attempt <= 3; attempt++`):
เมื่อ `validateSQL` หรือ `runScoped` fail ในรอบที่ < 3 จะสร้าง
`sqlFixup{SQL: emission.SQL, Err: err.Error()}` แล้วเรียก `emitSQL` ใหม่พร้อม
ป้อน error จริงกลับเข้า prompt คำต่อคำ ("Your previous attempt: ... failed with
this Postgres/validation error: ... Return a corrected query.") รอบที่ 3 ยัง
fail อยู่ → return HTTP 400 พร้อม SQL ที่พังไว้ให้ debug (ไม่ retry ต่อ)
