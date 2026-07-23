# แผนพัฒนา AI Assistant (`/ai`) + Ask-Data (`/ask`)

> อัปเดต 2026-07-23 · อ้างอิงโค้ดจริง ณ commit ปัจจุบัน · เมื่อเอกสารกับโค้ดขัดกัน **ให้เชื่อโค้ด**

---

## หลักการจัดลำดับ

งานในเอกสารนี้เรียงตาม **ราคาต่อผลลัพธ์** ไม่ใช่ความน่าสนใจ เกณฑ์ 3 ข้อ:

1. **ไม่กินโควตาเพิ่ม มาก่อนเสมอ** — เพดาน 200k tokens/วัน = ~42 คำถาม `/ask` หรือ ~17–22 เทิร์น `/ai` **ต่อทั้งองค์กร** งานที่เพิ่ม token ต่อเทิร์นคือการ *ลดจำนวนครั้งที่ผู้ใช้ใช้ได้ต่อวัน* ต้องมีเหตุผลชัด
2. **ของที่มีอยู่แล้วต่อท่อ มาก่อนของใหม่** — หลายข้อในนี้ราคาถูกเพราะ reuse ไม่ใช่เพราะเล็ก
3. **อะไรที่ยังไม่มีข้อมูลว่าเป็นปัญหาจริง ให้รอ log** — ไม่เดาแทนผู้ใช้

**เพดานที่ตั้งเองทั้งหมดปรับได้ด้วยค่าคงที่ตัวเดียว** (retry 3, tool round 5, row cap 5000, history 3) — ข้อจำกัดจริงที่ปรับไม่ได้มีข้อเดียวคือโควตา

---

## Phase 0 — เสร็จแล้ว (2026-07-23)

| งาน | สรุป |
|---|---|
| แก้หลาย widget ในคำสั่งเดียว | slot `multiTarget` ใน `classify_intent` → `dispatchIntent` คืน `"required"` แทน `forceFunc` → model เรียก `preview_update_widget` ทีละ widget ในรอบเดียว `runToolRound` รันครบ |

แตะ: `router.go` · `schema.go` · `controller.go` (dispatch + system prompt) · tests 3 ไฟล์ · docs 3 ไฟล์
**ยังค้าง:** live `TestRouterBakeOff` (33 เคส × 2 model) ยืนยันว่า router ตั้ง flag ถูกจริง

---

# ส่วนที่ 1 — Ask-Data (`/ask`)

## P1 — ทำก่อน ไม่กินโควตาเลย

### 1.1 ส่งกราฟจาก /ask ไปขึ้น dashboard

**ปัญหา:** ผู้ใช้ได้กราฟที่ต้องการแล้ว แต่เอาไปไว้บนแดชบอร์ดไม่ได้ ต้องไปสร้าง widget ใหม่เองทั้งหมด งานที่ระบบทำไปแล้วถูกทิ้งทุกครั้ง

**ทำอย่างไร:** เพิ่ม widget type ใหม่ (เช่น `sql-chart`) ที่เก็บ `{question, sql, echartOption}` แล้ว render ด้วยการเรียก `POST /ai/run-sql` — **กลไกนี้มีครบแล้ว** เพราะ Boards ทำแบบเดียวกันเป๊ะ (`boards.go` — เก็บ 3 ฟิลด์นี้ แล้ว re-run SQL ตอนเปิดเพื่อให้ข้อมูลสด)

- Backend: เพิ่มชนิด widget ใน `machines`/`dashboard_widgets` config — **ไม่ต้องเขียน endpoint ใหม่** `RunSQL` มีแล้ว (`routes.go`)
- Frontend: component ใหม่ใน `components/widgets/` ที่เรียก `api.runSql()` แล้วส่งเข้า ECharts ตัวเดิม + ปุ่ม "ส่งไป dashboard" บน `AskDataPage.vue`
- ต้องคิด: SQL ที่ฝังใน widget ยังต้องผ่าน `validateSQL` + `runScoped` ตอนรันทุกครั้ง (ต้องไม่มีทางลัด) และ widget ของ org A ต้องรันด้วย GUC ของ org A เสมอ

**ราคา:** frontend เป็นหลัก · **0 token** · **ความเสี่ยงต่ำ** เพราะ security path เดิมไม่เปลี่ยน
**แก้ข้อจำกัด:** "กราฟ /ask ใช้ซ้ำบนแดชบอร์ดไม่ได้"

### 1.2 Export CSV / PNG

**ทำอย่างไร:** CSV สร้างจาก `columns` + `rows` ที่มีอยู่ในมือฝั่ง client แล้ว (`Blob` + `URL.createObjectURL`) · PNG ใช้ `getDataURL()` ของ ECharts instance

**ราคา:** ~50 บรรทัดใน `AskDataPage.vue` · **0 token** · ไม่แตะ backend เลย
**แก้ข้อจำกัด:** "เอาผลลัพธ์ไปทำรายงานต่อไม่ได้"

### 1.3 อนุญาต `WITH` / CTE

**ปัญหา:** `validateSQL` บังคับให้ขึ้นต้นด้วย `SELECT` (`nl2sql.go:52`) → CTE ตกทั้งที่ไม่ได้อันตรายกว่า subquery เลย คำถามวิเคราะห์ ("ช่วงไหน speed ต่ำกว่าค่าเฉลี่ย") ตอนนี้ต้องเขียน subquery ซ้ำสองรอบ ซึ่ง model มักเขียนพลาดแล้วเสีย retry ฟรี

**ทำอย่างไร:** ยอมรับ prefix `with` เพิ่ม แล้วเช็คว่ามี `select` ตามมา ด่านอื่นไม่ต้องแตะ — blacklist keyword, deniedTables, read-only tx, GUC, row cap ทำงานเหมือนเดิมทั้งหมด
- ต้องระวัง: `WITH ... AS (INSERT ... RETURNING)` เป็นท่าเขียนข้อมูลผ่าน CTE ที่มีจริงใน Postgres — แต่ `sqlForbidden` จับ `insert|update|delete` อยู่แล้ว และ read-only tx เป็นด่านสุดท้าย ยังปลอดภัย
- อัปเดต prompt rule `nl2sql.go:196` จาก "no CTEs" เป็นอนุญาต

**ราคา:** ~3 บรรทัด + 2 unit test · **0 token** (แถมน่าจะ *ลด* เพราะ retry น้อยลง)
**แก้ข้อจำกัด:** "คำถามวิเคราะห์ซับซ้อนเขียน SQL ไม่ผ่าน"

### 1.4 E2E test ฝั่ง browser

**ปัญหา:** coverage ปัจจุบันไปถึง Fiber handler + TimescaleDB จริง แล้วหยุด — ชั้น `withDataset`, การ split เส้นตามเครื่อง, การ merge visualMap ของ heatmap ไม่มีอะไรตรวจ

**ทำอย่างไร:** Playwright ยิงคำถามจริง 3–5 เคส แล้ว assert ว่ามี canvas ขึ้น + จำนวน series ถูก
**ราคา:** ต้องมี AI จริงตอบ = **กินโควตา** → รันเป็น manual/nightly ไม่ใช่ทุก commit หรือ stub `/ai/ask` ไว้ก็ได้ถ้าอยากเทสเฉพาะชั้น render

---

## P2 — ทำถัดไป มีต้นทุน token

### 2.1 Thread memory ยาวกว่า 1 เทิร์น

**ปัญหา:** `prev` เก็บแค่เทิร์นก่อนหน้า 1 เทิร์น (`nl2sql.go:210`) — อ้างถึงกราฟเมื่อ 3 คำถามก่อนไม่ได้

**ทำอย่างไร:** ให้ FE ส่ง `context[]` เป็น array 2–3 เทิร์น (state อยู่ที่ FE อยู่แล้ว ไม่ต้องมี DB) แล้ว `emitSQL` เลือกอ้างเทิร์นที่เกี่ยว
**ราคา:** +~300–600 tokens/คำถาม (SQL เดิมถูกแนบเข้า prompt เพิ่ม) → ~42 คำถาม/วัน เหลือ ~35
**ตัดสินใจ:** คุ้มถ้าผู้ใช้ถามต่อเนื่องจริง — **ดู log ก่อน** ว่ามีคนถามย้อนเกิน 1 เทิร์นบ่อยแค่ไหน

### 2.2 Streaming

**ปัญหา:** ผู้ใช้รอ 5–24 วินาทีโดยไม่มี feedback (เคสช้าสุดที่วัดได้ 23.9s — prose path + judge)

**ทำอย่างไร:** SSE จาก Fiber · ทยอยส่ง "กำลังเขียน SQL" → "กำลังรัน" → "กำลังวาด" ก็พอ **ไม่จำเป็นต้อง stream ตัวอักษร** เพราะคำตอบหลักคือกราฟ ไม่ใช่ prose
**ราคา:** 0 token (แค่เปลี่ยนวิธีส่ง) · แต่แตะ handler + FE + error path ทั้งหมด
**คุ้ม:** ความรู้สึกช้าคือ complaint อันดับ 1 ของระบบแบบนี้เสมอ — และเวอร์ชัน "แค่บอกสถานะ" ราคาถูกกว่า streaming จริงมาก

### 2.3 หลาย series / 2 แกน Y

**สถานะจริง:** `sanitizeEChartOption` **ไม่ได้บล็อก** series ที่ `encode` ต่างกัน — ตัวที่ห้ามคือ prompt (`nl2sql.go:350`) ที่สั่งให้ปล่อยมา series เดียวเสมอ
**ทำอย่างไร:** ผ่อน prompt ให้ยิง 2 series ได้เมื่อผู้ใช้ขอเทียบคนละหน่วย + ตั้ง `yAxisIndex` · ต้องเพิ่มเทสกันกรณี dedup ตัด series ผิดตัว
**ราคา:** prompt ยาวขึ้นเล็กน้อย · **ความเสี่ยงปานกลาง** — เปิดพื้นที่ให้กราฟผิดโดยไม่มีใครร้องขอ
**ตัดสินใจ:** รอจนมีคนถามจริง

---

## P3 — ระยะยาว

| งาน | เนื้อหา | เงื่อนไขที่ควรทำ |
|---|---|---|
| เปิด view read-only เพิ่ม | ตอนนี้เห็นแค่ 3 views (`v_telemetry`, `v_machines`, `v_machine_fields`) — เพิ่ม `v_dashboards` / `v_alert_rules` ให้ตอบ "มี dashboard อะไรบ้าง" ได้ | ต้องสร้าง view ที่กรอง `app.current_org` ให้ถูกก่อน · ขยาย `deniedTables` allowlist ตาม |
| Cache SQL ของคำถามซ้ำ | hash คำถาม+org → SQL ที่เคยผ่าน judge แล้ว ข้าม `emitSQL` ไปเลย | **ต้องมี log ก่อน** ว่าคำถามซ้ำจริง · ประหยัดได้ ~2,500 tokens/ครั้งที่ hit |
| Multi-result (2 กราฟใน 1 คำตอบ) | ต้องแก้ response shape เป็น array + FE render หลายการ์ด + judge ตัดสินทีละชุด + `prev` เลือกอันไหนเป็นบริบท | **แพงสุดในเอกสารนี้** แตะ 4 ชั้น — ทำเมื่อมีคนบ่นซ้ำๆ เท่านั้น |

---

# ส่วนที่ 2 — AI Assistant (`/ai`)

## P1 — ทำก่อน ไม่กินโควตาเลย

### 1.1 เปิด element-click บนหน้า dashboard editor

**ปัญหา:** `elementPickMode` เปิดเฉพาะตอนอยู่หน้า `/ai` — logic ทั้งหมดมีแล้ว แค่ยังไม่ mount ที่หน้าอื่น
**ทำอย่างไร:** เปิด flag ที่ `DashboardEditor` + ต่อ `lastElementClick` เข้ากล่องแชท
**ราคา:** frontend ล้วน · **0 token**

### 1.2 Token metering ต่อ intent เก็บลง DB

**ปัญหา:** `tokenMeter` (`controller.go:926`) เป็น package-global รีเซ็ตทุกครั้ง ใช้ได้แค่ในเทส — production ไม่รู้ว่า intent ไหนกินเท่าไหร่จริง
**ทำอย่างไร:** เขียน `{intent, tokens, verdict, det, duration}` ลงตารางเล็กๆ ทุก request (log line มีข้อมูลครบแล้วที่ `controller.go:721`)
**ราคา:** 1 migration + 1 insert · **0 token**
**ทำไมสำคัญ:** นี่คือ **วัตถุดิบของงาน P3 เกือบทุกข้อ** — calibrate confidence, cache, ปรับ roundCap ต้องใช้ข้อมูลนี้ทั้งหมด ทำก่อนแล้วรอข้อมูลสะสม

### 1.3 ปิด live BakeOff ของ multiTarget

รัน `TestRouterBakeOff` ยืนยันว่า router ตั้ง `multiTarget` ถูกจริงในเคส 2 widget (งานที่ค้างจาก Phase 0)

---

## P2 — ทำถัดไป มีต้นทุน token

### 2.1 Tool จัดการ alert rule แบบ preview → confirm

**ปัญหา:** "ตั้ง alert ให้หน่อยถ้า speed เกิน 100" ทำไม่ได้ — มีแต่ `get_active_alerts` (อ่านอย่างเดียว) และ router มี intent `alerts` รอไว้แล้ว แต่ปลายทางไม่มี tool

**ทำอย่างไร:** เพิ่ม `preview_alert_rule` + apply — **ก็อป pattern จาก `preview_add_widget` ได้เกือบทั้งหมด** (stage ไว้ ไม่เขียน DB → ผู้ใช้กด Confirm → เขียนจริง)
- role gate เหมือน preview_* ตัวอื่น (`viewer` ตัดออก)
- deterministic check: metric ต้องมีจริงบนเครื่องนั้น — reuse `checkFieldsExist` (`verify.go:45`) ได้ตรงๆ

**ราคา:** +1 tool schema ในถาด (~150 tokens ต่อเทิร์นที่ส่ง tool ครบชุด) · เทิร์นที่ force tool เดียวไม่กระทบ
**ข้อควรระวัง:** alert rule เป็นของที่ "ยิงจริง" ตอน telemetry เข้า — ตั้งผิดแล้วสแปมทั้งโรงงาน ต้องมี preview + confirm เสมอ ห้ามมี fast path

> หมายเหตุ: alarm/alert panel ไม่ใช่โฟกัสหลักของโปรเจกต์ — ข้อนี้ทำเมื่อมีคนขอเท่านั้น

### 2.2 History แบบ summarize แทน cap 3 ข้อความ

**ปัญหา:** `buildAIMessages` ตัดเหลือ 3 rows (`controller.go:1245`) — เหตุผลเดิมชัดเจน: transcript เคยเป็นต้นทุน input-token อันดับ 1 และทำให้ focused follow-up ทะลุ 8k/min

**ทำอย่างไร:** สรุปบทสนทนาเก่าเป็นย่อหน้าเดียวเก็บไว้ใน `ai_conversations` แล้วส่งย่อหน้านั้นแทนข้อความดิบ
**ราคา:** ต้องเรียก model เพิ่มเพื่อสรุป (ใช้ model เล็กได้) · net อาจ **ประหยัด** ถ้าบทสนทนายาว แต่ **เปลืองกว่า** ถ้าสั้น
**ตัดสินใจ:** ทำต่อเมื่อ metering (1.2) แสดงว่าบทสนทนายาวเกิน ~6 ข้อความบ่อยจริง

### 2.3 ปรับ `roundCap` ตาม intent

**ปัญหา:** `roundCap` = 1 คงที่ (0 เมื่อโฟกัส) → โซ่ tool 3 ชั้นทำไม่ได้ เช่น `list_dashboards` → อ่าน widget ข้างใน → แก้ทีละตัว
**ทำอย่างไร:** ให้ `dispatchIntent` คืน 2 เฉพาะ intent ที่รู้ว่าต้อง fan-out
**ราคา:** +1 คำเรียก ≈ +3k tokens เฉพาะ intent นั้น (17 เทิร์น/วัน → 13 สำหรับเทิร์นชนิดนั้น)

---

## P3 — ระยะยาว (ต้องมีข้อมูลจาก 1.2 ก่อน)

| งาน | เนื้อหา |
|---|---|
| Calibrate เส้น confidence 0.5 | ตอนนี้ 0.5 ตั้งจากการเดา · วิธีวัด: ดึง log ที่ `verdict=repair` มา 30–50 เคส ให้คนอ่านว่าคำตอบเดิมผิดจริงไหม → ได้ false-positive rate ตัวจริง แล้วค่อยขยับเส้น |
| Router few-shot / fine-tune จาก log | 27/32 ในรันล่าสุด แต่ **2 ใน 5 ที่ตกเป็น provider declined ไม่ใช่จัดประเภทผิด** และอีก 2 มี answer-from-context กันไว้ → ปัญหาจริงเล็กกว่าตัวเลข ทำเมื่อ log ยืนยันว่าพลาดซ้ำแบบเดิม |
| Cap จำนวน tool call ต่อรอบ | ตอนนี้ `runToolRound` (`controller.go:597`) ไม่จำกัดจำนวน call ในหนึ่งรอบ — เปิดกว้างขึ้นหลังใส่ `multiTarget` · ถ้า metering เห็นเทิร์นที่ยิงรัวจนเปลือง ให้ตัดที่ ~8 calls แล้วคืน error เป็น tool result ให้ model รู้ตัว |
| Streaming | เหมือน /ask — ส่งสถานะระหว่างทางก็พอ |

---

# ส่วนที่ 3 — งานร่วม 2 หน้า

## 3.1 โควตา — ต้องคุยก่อนเรื่องฟีเจอร์

⚠️ **200k tokens/วัน = ~42 คำถาม `/ask` หรือ ~17–22 เทิร์น `/ai` ต่อทั้งองค์กร**

- pool แชร์ **ต่อค่ายโมเดล** ไม่ใช่ต่อโมเดล → เทสหนักด้วย sonnet ลาก generation ตายไปด้วย (เหตุผลที่ย้าย router/judge ไป `gpt-5.4-mini` คนละ pool)
- pool เดียวกันแชร์กับการรันเทส — วงเต็ม 1 รอบ ≈ 183k = เกือบทั้งวัน **รันได้วันละครั้ง**
- โควตาหมด → 429 `QUOTA_EXCEEDED` (คนละอันกับ `RATE_LIMIT` ที่ retry สั้นๆ ได้)

**ทางเลือก เรียงตามความเร็วที่ทำได้:**

| ทาง | ผล | ต้นทุน |
|---|---|---|
| ย้าย provider | ปลดเพดานทันที | env 4 ตัว (`AI_BASE_URL`/`AI_MODEL`/`AI_ROUTER_MODEL`/`AI_API_KEY`) — เคยย้าย Groq → KKU มาแล้ว |
| แยก key ต่อทีม | กันคนเดียวใช้หมด | ต้องมี key เพิ่ม |
| Cache (ask 3.2) | ลดการใช้ต่อคำถามซ้ำ | ต้องมี log ก่อน |
| ลด token ต่อเทิร์นอีก | ได้อีกไม่มาก | รอบที่แล้วลดไป 11% แล้ว ผลไม้ที่ห้อยต่ำเก็บหมดแล้ว |

**ถ้าจะเปิดให้ผู้ใช้หลายสิบคน ข้อนี้คือเงื่อนไขบังคับ ไม่ใช่ทางเลือก**

## 3.2 Observability (`/ai` 1.2 ขยายไปคลุม `/ask`)

เก็บ per-request: `feature`, `intent`, `tokens`, `verdict`, `duration`, `repaired`, `retry count`
→ ปลดล็อกงาน P3 เกือบทั้งหมด และตอบคำถาม "ระบบแม่นแค่ไหนจริงๆ" ได้โดยไม่ต้องเดา

---

# ลำดับที่แนะนำ

**รอบที่ 1 — ของที่ไม่กินโควตา (ทำได้แม้โควตาเหลือน้อย)**
1. `/ai` token metering ลง DB ← ทำก่อนเพื่อนเพราะทุกอย่างหลังจากนี้ใช้ข้อมูลนี้
2. `/ask` ส่งกราฟไป dashboard (reuse `RunSQL` + pattern ของ Boards)
3. `/ask` export CSV/PNG
4. `/ask` เปิด `WITH`/CTE
5. `/ai` element-click บนหน้า editor
6. ปิด live BakeOff ของ `multiTarget`

**รอบที่ 2 — UX ที่คนรู้สึกได้**
7. Streaming สถานะระหว่างทาง (ทั้ง 2 หน้า)
8. E2E browser test

**รอบที่ 3 — รอข้อมูลจาก metering ก่อนตัดสินใจ**
9. thread memory ยาวขึ้น · history summarize · roundCap ตาม intent · calibrate confidence · cache

**ตลอดเวลา:** เรื่องโควตาต้องมีคำตอบก่อนเปิดใช้จริงหลายคน

---

# ที่ตัดออก และเหตุผล

| ไม่ทำ | เพราะ |
|---|---|
| Fine-tune router ตอนนี้ | 27/32 โดย 2 เคสเป็น provider declined + 2 เคสมี answer-from-context กันไว้ — ปัญหาจริงเล็กกว่าตัวเลขมาก ไม่มี log ยืนยันว่าคุ้ม |
| กราฟชนิดใหม่ (gauge/radar/treemap/sankey) | ตกจาก allowlist เพราะ **โครงสร้าง** ไม่ใช่รสนิยม — เราลบ `series[].data` ทิ้งเสมอ ชนิดที่ต้องเขียนตัวเลขเองหรือต้องเดาสเกล (min/max) จึงวาดไม่ได้โดยนิยาม · `funnel` เป็นตัวเดียวที่เพิ่มได้ทันทีถ้ามีข้อมูลขั้นตอนการผลิต (แก้ 2 จุด: `nl2sql.go:533` + prompt) |
| อนุญาต SQL กว้างกว่านี้ (base table) | ด่านสุดท้ายอยู่ใน Postgres โดยตั้งใจ — เปิด base table = ทิ้ง org isolation ทั้งระบบ **ไม่ทำ** |
| ให้ AI สร้าง dashboard เองโดยไม่ต้อง Confirm | preview → confirm เป็น safety ไม่ใช่ friction **ไม่ทำ** |
