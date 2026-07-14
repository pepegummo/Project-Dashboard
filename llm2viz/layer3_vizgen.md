## Layer 3: Visualization Generation

### 3.1 Chart-type selection — ฝังใน prompt เดียวกับ chart generation + gate ด้วย `hasNumericColumn`

เอกสารเดิมมี Chart Type Selector เป็น LLM call แยกที่รับ column schema +
sample data มาเลือก chart type ก่อน แล้วค่อยส่งต่อให้ Spec Generator —
**โปรเจกต์นี้ไม่มี call แยก**: การเลือก type เป็นแค่กฎในตัว `echartSystemPrompt`
(nl2sql.go) ซึ่งเป็น system prompt เดียวกับที่ generate ECharts option เลย
(ประหยัด 1 LLM call ต่อ chart turn)

กฎเลือก type จาก `echartSystemPrompt` คำต่อคำ:

> "Pick the chart type — use ONLY 'line', 'bar', 'pie', or 'scatter': a
> time-bucket column → line; a category comparison → bar; parts-of-a-whole →
> pie."
>
> "If the user's message explicitly names a chart type (bar/line/pie/scatter,
> or the same in another language e.g. Thai "กราฟแท่ง"=bar, "กราฟเส้น"=line,
> "วงกลม"=pie), use THAT type even if another would be more typical."

ต่างจากเอกสารเดิมตรงที่ไม่มี `histogram`/`stacked_bar` เป็นตัวเลือก — จำกัดแค่ 4
type (`line`/`bar`/`pie`/`scatter`) ซึ่งตรงกับ whitelist ที่ `sanitizeEChartOption`
(layer 4.1) บังคับใช้จริงในขั้นถัดไป และไม่มี `warning` field เรื่องข้อมูลเกิน
1000 แถวเหมือนเดิม เพราะ `LIMIT 5000` ในชั้น SQL (layer 2.1) และการบังคับ
`GROUP BY time_bucket` ทำ aggregate ไว้ตั้งแต่ระดับ SQL แล้ว

**ก่อนจะเรียก `emitEChart` เลยมี deterministic gate**: `hasNumericColumn(cols,
rows)` เช็คว่ามี column ไหนเป็นตัวเลขบ้าง (`int`/`int16`/`int32`/`int64`/
`float32`/`float64`/`pgtype.Numeric`) โดยดูจากค่า non-null แรกของแต่ละ column
เท่านั้น ถ้าไม่มี column ไหนเป็นตัวเลขเลย (เช่นผลลัพธ์เป็น text ล้วนแบบ "list
machines") หรือ `len(rows) == 0` → ข้าม `emitEChart` ไปเลย ตั้ง `option = "{}"`
(สัญญาณตาราง) ทันที — ประหยัดทั้ง call และ latency สำหรับคำถามที่ไม่มีทาง chart
ได้อยู่แล้ว

### 3.2 Chart spec generation — **ECharts option ไม่ใช่ Vega-Lite**

เอกสารเดิมกำหนด output เป็น Vega-Lite spec (`$schema`, `mark`, `encoding`) —
**โปรเจกต์นี้ generate ECharts option object แทนทั้งหมด** (`emitEChartTool`,
`emitEChart`, nl2sql.go) เหตุผล: แอปทั้งตัวใช้ `vue-echarts` render ทุก widget
อยู่แล้ว (LineChart, Gauge, KpiCard, StatusCard, CustomChart ตาม CLAUDE.md) —
ใช้ Vega-Lite แปลว่าต้องเพิ่ม render infrastructure ชุดใหม่ทั้งหมด (library,
theme, container) เพื่อ feature Ask-Data เดียว ในขณะที่ ECharts infrastructure
มีพร้อมใช้ทันที — **zero Vega infrastructure ต้องสร้างใหม่**

**ไม่มี data ฝังใน option** — quote จาก `emitEChartTool.description` คำต่อคำ:

> "Return an ECharts option object (no data — a dataset is injected at render
> time) that best visualizes the result."

โมเดล reference ผลลัพธ์ผ่าน `encode` โดยชื่อ column เท่านั้น เช่น
`series:[{type:'line', encode:{x:'bucket', y:'avg_speed'}}]` — ห้ามใส่ data
array หรือ `dataset` field เอง ข้อบังคับนี้ถูกซ้ำอีกชั้นด้วย
`sanitizeEChartOption` (layer 4.1) ที่ strip `dataset`/`data` ทิ้งไม่ว่าโมเดลจะ
ใส่มาหรือไม่

**Dataset จริงถูก inject ตอน render โดย frontend** — ฟังก์ชัน `withDataset()`
(`frontend/src/pages/AskDataPage.vue`) merge option ที่ได้จากโมเดลเข้ากับ
`{ dataset: { source: [columns, ...rows] } }` แล้วส่งเข้า `vue-echarts` ตรงๆ
โมเดลเองไม่เคยเห็นข้อมูลทั้งหมด — เห็นแค่ column names + sample rows 20 แถวแรก
(`sampleRows(rows, 20)`) พอสำหรับ infer type ของแต่ละ column เท่านั้น

**Retry 1 ครั้งพร้อม error feedback (ของใหม่ ไม่มีในเอกสารเดิม)**:
`emitEChart(ctx, question, cols, sample, prevErr string)` มี parameter
`prevErr` เพิ่มเข้ามา — เมื่อ call แรก error `AskData` จะเรียกซ้ำอีกครั้งโดยส่ง
error text ของครั้งแรกเข้าไปใน `prevErr` ซึ่งจะถูกฝังเป็น
`"previousError": "<err> — return a corrected option"` ใน user payload ของ
call ที่สอง ถ้ายัง error อีก → ปล่อยเป็น table fallback (`option = "{}"`, HTTP
200) ไม่ error กลับไปหา user เลย — ส่วน `sanitizeEChartOption` ที่ reject
option (type ผิด, encode อ้าง column ที่ไม่มีจริง) ไม่มี retry เพิ่ม เพราะเป็นการ
reject เชิงโครงสร้างที่ deterministic แล้ว ไม่ใช่ error จาก Groq API
