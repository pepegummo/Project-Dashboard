## Layer 5: Orchestration (แนวคิดการต่อ pipeline)

Pseudocode ด้านล่างคือ control flow จริงของ handler `AskData`
(`POST /ai/ask`, `backend/internal/modules/ai/nl2sql.go`) ไม่ใช่ pipeline
ทั่วไปแบบเดิมที่มีหลาย layer/หลายไฟล์ต่อกันอีกต่อไป — ทุก step ที่เขียนว่า
"LLM" คือ Groq call จริงด้วย forced `tool_choice`, ทุก step ที่เขียนว่า
"deterministic" คือ Go function ล้วน ไม่มี network call

```
1. prev = build prevTurn จาก request.context
   - context.sql ไม่ว่าง            → prev = {Question, SQL}           (โหมด SQL ต่อยอด)
   - context.clarification ไม่ว่าง  → prev = {Question, Clarification} (โหมดตอบ clarification)
   - ทั้งคู่ว่าง                     → prev = nil                       (เทิร์นแรก)

2. schema = buildSchemaContext(orgID)   # deterministic — 2 query จริงจาก v_machines/v_machine_fields

3. for attempt in 1..3:                              # SQL retry loop
     emission = call(emitSQL, question, schema, prev, fixup)   # LLM: gpt-oss-120b, forceFunc("emit_sql")

     if emission == errNotDataQuestion:               # answerable=false, ไม่มี clarification
         answer = call(emitProse, question, schema, prev)      # LLM: gpt-oss-120b, no tool
         return {answer}                                       # จบ turn — prose

     if emission.Clarification != "":
         return {clarification: emission.Clarification}        # จบ turn — รอ user ตอบ ไม่รัน SQL เลย

     sqlText, err = validateSQL(emission.SQL)                  # deterministic
     if err:
         if attempt < 3: fixup = {SQL: emission.SQL, Err: err}; continue
         else: return HTTP 400 {error, sql}

     cols, rows, err = runScoped(orgID, sqlText)                # deterministic — read-only tx, 5s statement_timeout, 5000-row cap
     if err:
         if attempt < 3: fixup = {SQL: sqlText, Err: err}; continue
         else: return HTTP 400 {error, sql}

     break   # SQL สำเร็จ ออกจาก retry loop

4. option = "{}"                                                # default: table
   if len(rows) > 0 and hasNumericColumn(cols, rows):            # deterministic gate — ตัดสินว่ามีทาง chart ได้ไหม
       option, err = call(emitEChart, question, cols, sample(rows,20), "")      # LLM: gpt-oss-120b
       if err:
           option, err = call(emitEChart, question, cols, sample(rows,20), prevErr=err)  # retry 1 ครั้งพร้อม error feedback
       if err == nil:
           option = sanitizeEChartOption(option, cols)            # deterministic — strip dataset/data, whitelist type, validate encode

5. if option != "{}":                                             # judge เฉพาะ chart turn — table/prose ไม่ผ่านจุดนี้
       verdict, ok = call(verifyAskChart, question, sqlText, cols, sample(rows,5), option)  # LLM: gpt-oss-20b
       if ok and not verdict.MatchesIntent:
           # repair 1 รอบเท่านั้น — ไม่มี judge เรียกซ้ำรอบสอง
           emission2 = call(emitSQL, question, schema, prev, fixup={sqlText, "verifier: "+verdict.Problem})
           if emission2 มี SQL ใช้ได้ (ไม่ error, ไม่ใช่ clarification):
               sql2 = validateSQL(emission2.SQL)
               cols2, rows2 = runScoped(orgID, sql2)
               if len(rows2) > 0 and hasNumericColumn(cols2, rows2):
                   opt2 = call(emitEChart, question, cols2, sample(rows2,20), "")   # ไม่ retry ในรอบ repair นี้
                   opt2 = sanitizeEChartOption(opt2, cols2)
                   if opt2 != "{}":
                       sqlText, cols, rows, option = sql2, cols2, rows2, opt2       # ผ่านทุกจุด → ใช้ผลลัพธ์ที่ซ่อมแล้ว
           # ล้มเหลวจุดไหนก็ตามในรอบ repair → option = "{}" บน rows เดิมของคำตอบแรก (ไม่ error)

6. return {sql: sqlText, columns: cols, rows: rows, echartOption: option}
```

3 response shape ที่เป็นไปได้จริงของ `AskData` (HTTP 200 ทั้งหมด ยกเว้น SQL
พังครบ 3 ครั้งในขั้นตอนที่ 3 ซึ่งเป็น 400):

- **`{answer: string}`** — prose turn (ไม่ใช่คำถามเกี่ยวกับ data)
- **`{clarification: string}`** — คำถามเกี่ยวกับ data แต่ under-specified, รอ
  user ตอบก่อน
- **`{sql, columns, rows, echartOption}`** — data turn ปกติ, `echartOption`
  เป็น `"{}"` แปลว่า render เป็นตาราง, เป็น object จริงแปลว่าเป็นกราฟ

ไม่มี `chart_choice` / `viz_spec` / `qa_result` เป็น object แยกกันแบบเอกสารเดิม
อีกแล้ว — ทุกอย่างพับเข้า loop เดียวใน `AskData` ซึ่งเป็น Go function เดียว
ไม่มี orchestration layer แยกออกมาต่างหาก (pseudocode นี้จึงอธิบาย control flow
ของ `AskData` เอง ไม่ใช่โค้ด orchestrator อีกไฟล์)
