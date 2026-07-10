# IotVision AI — Q&A สำหรับ Review / ตอบคำถาม present

ตอบคำถามที่พบบ่อยเกี่ยวกับ AI assistant แบบเข้าใจง่าย อ้างอิงโค้ดจริงใน
`backend/internal/modules/ai/` และ `frontend/src/pages/AIAssistantPage.vue`
คู่กับ `AI_ARCHITECTURE.md` (รายละเอียดลึก) และ `AI_SLIDES_DIAGRAMS.md` (ภาพ present)

## สรุปสั้นทุกข้อ (ใช้เป็นสารบัญ)

| # | คำถาม | คำตอบ 1 บรรทัด |
|---|--------|----------------|
| Q1 | ทำไม qwen ใช้ token เยอะกว่า | tokenizer หั่นภาษาไทยถี่กว่า ~20% — ข้อความเดียวกันกลายเป็น token มากกว่า เลยชน rate limit ก่อน |
| Q2 | เลือก prompt ยังไง / แต่ละ prompt ต่างกันยังไง | regex ดู 3 สัญญาณ (needsTools / hasContext / answerFromContext) → เลือก 1 ใน 4 prompt ที่เบาที่สุดที่พอ |
| Q3 | mention (@) กับ highlight ต่างกันยังไง | mention = คนชี้ widget เข้า, highlight = AI ชี้กลับ |
| Q4 | AI รู้ข้อมูล/แก้ config ได้ยังไง | AI แค่เลือก tool — backend รัน SQL จริง; setting ที่แก้ได้คือ args ของ `preview_update_widget` |
| Q5 | แผนถัดไปมีอะไร ทำไมยังไม่ทำ | argument-level scoring / SSE / ลด prompt / parallel dispatch — รอวัดก่อนค่อยทำ (data-driven) |
| Q6 | เทียบโมเดลวัดยังไง | bake-off 24 เคสไทย วัด first-decision accuracy + token + latency → เลือก gpt-oss-20b |
| Q7 | answerFromContext คืออะไร / ถามข้อมูลบนจอแต่ไม่ @ | "คำตอบอยู่บนจอแล้ว" ต้อง @focus เท่านั้น — ไม่ @ จะเข้าเส้น tool ปกติ (AI fetch DB เอง) |
| Q8 | ควรเปลี่ยน regex เป็น AI/vector router ไหม | ไม่ — regex ตัดสินแค่ขนาด prompt ไม่ใช่สมอง, มี sentinel เป็น safety net แล้ว, eval ยัง 100% |
| Q9 | dispatch/executor คืออะไร · คุยเป็น JSON ไหม | dispatch = switch เช็คสิทธิ์แล้วเรียก executor จริง; ทั้ง chain เป็น JSON ตาม OpenAI spec |
| Q10 | ทำไมไม่ใช้ LangGraph / agent framework | ระบบนี้*คือ* agent อยู่แล้ว — loop เองใน Go ไม่กี่ร้อยบรรทัด คุม token ได้ละเอียดกว่า และไม่เพิ่ม dependency |
| Q11 | อธิบาย step ใน diagram (required/dispatch/compact/roundCap) | ไล่ทีละกลไกของ tool loop: บังคับ tool turn แรก → รัน tool → ย่อผล → จำกัดรอบ → บังคับสรุป |
| Q12 | พิมพ์ผิดแต่อยากสร้างจริง Minimal จับได้ยังไง | โมเดลตอบ `NEED_TOOLS` เป๊ะ ๆ → backend retry ครั้งเดียวด้วย prompt เต็ม + tools (sentinel ไม่ลง DB) |
| Q13 | test แต่ละตัวทำอะไร | unit 4 ตัว (gate/เวลา/429) + live 2 ตัว (sentinel, bake-off 24 เคส) |
| Q14 | AI รู้ได้ไงว่าใช้ tool ไหน ส่ง description ทุกตัวไหม | ใช่ — ส่งชื่อ+description ของ tool ที่ role/บริบทอนุญาตไปทุก request แล้วโมเดลเลือกเอง |
| Q15 | Groq จำ history ไหม ต้องส่งเองไหม | ไม่จำ (stateless) — เราส่ง 8 ข้อความล่าสุดเองทุกครั้ง; prompt cache ช่วยเร็ว/ถูกขึ้นแต่ไม่ใช่ความจำ |

---

## Q1 — ทำไม qwen ใช้ token เยอะกว่า (~3,282 vs ~2,697)

**ข้อความที่ส่งไปเหมือนกันเป๊ะทั้ง 3 โมเดล แต่แต่ละตัว "นับ token คนละวิธี"**

- prompt / tool / คำถามไทย ที่ยิงในการทดสอบเป็นชุดเดียวกัน — เนื้อหาไม่ได้ยาวกว่า
- แต่ละโมเดลมี **tokenizer** (ตัวหั่นข้อความเป็นชิ้น) ต่างกัน ภาษาไทยไม่มีเว้นวรรค +
  เป็นอักษรที่ไม่ใช่อังกฤษ → tokenizer ที่ไม่ได้ปรับมาเพื่อไทยจะหั่นถี่กว่า
- ของ qwen หั่นไทยละเอียดกว่าของ gpt-oss ~20% → ข้อความเดียวกันกลายเป็น token มากกว่า

**ผลกระทบ:** token/request สูงกว่า → ชนเพดาน Groq ฟรี 8,000 token/นาที เร็วกว่า →
qwen จึงโดน rate limit 10/23 เคส ในขณะที่ gpt-oss ไม่โดนเลย

> "ไม่ใช่ qwen พูดเยอะ แต่มันนับคำแบบเปลืองกว่าสำหรับภาษาไทย"

---

## Q2 — เลือก prompt อย่างไร / แต่ละ system prompt ต่างกันอย่างไร / needsTools, context คืออะไร

ตัดสินด้วยกฎ (regex) ที่ backend **ก่อน** เรียก AI — จ่าย token เท่าที่ข้อความนั้นต้องใช้จริง
ดูจาก 3 สัญญาณ (`controller.go:380-394`):

1. **needsTools?** — ข้อความ*น่าจะ*ต้องใช้ข้อมูล/ลงมือไหม: regex หา keyword เกี่ยวกับ metric/คำสั่ง
   (speed, สร้าง, ลบ, เพิ่ม, alert…) หรือมี `@` mention — เจออย่างเดียวก็ผ่าน
2. **hasContext?** — frontend แนบ snapshot ของ dashboard/preview ที่เปิดบนจอมาด้วยไหม
   (`body.Context != ""`) — "context" ก็คือข้อความสรุปสถานะจอ ณ ตอนนั้น (widget อะไร ค่าอะไร)
3. **answerFromContext?** — คำตอบอยู่ใน context นั้นแล้วหรือยัง (ดู Q7)

แล้วเลือก 1 ใน 4 prompt (`controller.go:398-405`) — ต่างกันที่ "บรรจุกฎอะไร":

| กรณี | Prompt | tool | บรรจุอะไร |
|------|--------|------|-----------|
| ทักทาย/คุยเล่น | Minimal (~300 tok) | ไม่มี | identity + กฎภาษา + "ตอบสั้น" + กฎ `NEED_TOOLS` sentinel (ดู Q12) |
| ข้อมูลอยู่บนจอแล้ว | ContextAnswer | ไม่มี — ตอบรอบเดียว | identity + ภาษา + "ห้ามเรียก tool ตอบจาก context ที่แนบ" |
| ถาม/สั่งทั่วไป | Base | มี | สมองหลัก: TOOL SELECTION + SLOT FILLING + WIDGET TYPES |
| แก้ widget บนจอ | Base + ContextExt | มี + กฎ preview | ทุกอย่างของ Base **บวก** กฎ preview-staging + routing `@Widget`/`[FOCUSED]` |

**ทำไมทำแบบนี้:** แค่ทักทายไม่ควรจ่ายค่ากฎเลือก tool/widget แยก layer ทำให้ทักทายประหยัด
~300 token, ไม่มี dashboard ก็ตัดกฎ preview อีก ~100 token และ Base ถูกล็อกให้ byte เหมือนเดิม
ทุกครั้ง เพื่อให้ Groq ใช้ prompt cache ซ้ำได้

**ถ้าไม่เจอ keyword เลย จะจบที่ Minimal ตายตัวไหม?** ไม่ — Minimal มี escape hatch: ถ้าข้อความ
ที่หลุดมาจริง ๆ แล้วขอข้อมูล/สั่งงาน (เช่นพิมพ์ผิดจน regex มองไม่เห็น) โมเดลจะตอบ `NEED_TOOLS`
แล้ว backend ยกระดับไปเส้น tool เต็มให้เองอีกรอบ — ดู Q12

---

## Q3 — highlight, mention ทำงานยังไง

คนละเรื่องแต่ทำงานคู่กัน (`AIAssistantPage.vue`):

**Mention (@) = คนชี้ว่า "สนใจ widget ตัวนี้"**
- คลิก widget → กลายเป็น chip (`mentionedWidgets`) แนบไปกับคำถาม
- ผล 2 อย่าง:
  1. context ที่ส่งไปหดเหลือเฉพาะ widget ที่ mention ติดป้าย `[FOCUSED]` → AI ไม่หลงไปตอบตัวอื่น
  2. `@` ในข้อความกระตุ้น needsTools + บางเคสบังคับ `tool_choice: required` ให้ลงมือกับตัวนั้น

**Highlight = AI ตอบเสร็จแล้วชี้กลับว่า "หมายถึงตัวนี้"**
- หลัง AI แตะ widget ไหน `previewHighlightId` ชี้ไปตัวนั้น → ขึ้นกรอบเรืองรอบ widget บน canvas
  (`setAiHighlight` → `highlightWidgetById`)

> **mention = คนชี้เข้า, highlight = AI ชี้กลับ** — คุยตรง widget ไม่ต้องพิมพ์ชื่อยาว

---

## Q4 — AI รู้ข้อมูลได้ยังไง / config ยังไง / setting อะไร / แกน x,y

**รู้ข้อมูลยังไง:** AI ไม่เดา มันแค่เลือกว่า "เรียก tool ไหน" แล้ว backend รัน **SQL จริงบน
TimescaleDB** (`tool_actions.go`) เอาผลป้อนกลับให้สรุปเป็นภาษาคน (ทุก tool = 1 query จริง)

**setting ที่ AI แก้ได้** (`preview_update_widget`, `schema.go:165-191`):
- `metric` (ค่าที่วัด), `machine`, `type` (ชนิด widget)
- `bucket` (ช่วงเวลาต่อแท่ง), `start_date`/`end_date`
- `unit`, `min`, `max`, `new_title`
- `sku`, `status`
- กราฟ custom: `fields` (ซ้อนหลายค่า), `chartType` (line/bar/area), `points`, `scaling`

**แกน x, y (กราฟ):**
- **x = เวลา** → คุมด้วย `bucket` (รายชั่วโมง/รายวัน) จาก `get_telemetry_series` ที่หั่นเวลาเป็นช่วง
- **y = ค่า metric** → คุมด้วย `metric` + `min`/`max` (ขอบเขต) + `unit` (หน่วย)

> "AI ปรับ widget เหมือนคนเปิด config เอง แค่สั่งเป็นประโยค เช่น 'เปลี่ยนเป็น temperature
> รายชั่วโมง' → มันเซ็ต metric + bucket ให้"

---

## Q5 — element ใน next plan จะทำอย่างไร / ทำไมเลือกวิธีนี้

สิ่งที่จงใจเลื่อนไว้ทำต่อ พร้อมเหตุผลที่ยังไม่ทำตอนนี้
(จาก `plans/2026-07-03-ai-optimization.md` + ท้าย `AI_ARCHITECTURE.md`):

| แผนถัดไป | ทำอะไร | ทำไมยังไม่ทำตอนนี้ |
|----------|--------|---------------------|
| **Argument-level scoring** | วัดว่า AI ใส่ค่า argument ถูกไหม (metric/bucket ถูกตัว) ไม่ใช่แค่เลือก tool ถูก | 2 โมเดล gpt-oss แม่นเรื่องเลือก tool จนตันเท่ากัน ต้องวัดละเอียดขึ้นถึงแยกออก |
| **SSE streaming** (ตอบทีละคำ) | ทำให้ "รู้สึก" เร็วขึ้นมาก | ต้องรื้อ API + loop frontend ใหม่ และ latency เป็นความสำคัญอันดับ 3 → แยกเป็นแผนของมันเอง |
| **ลดขนาด prompt** | `ContextExt` ~800 token มีบางส่วนซ้ำ | ตัดได้อย่างปลอดภัยต่อเมื่อมี eval วัดก่อน — "วัดก่อน ค่อยตัด" |
| **Parallel tool dispatch** | ยิงหลาย tool พร้อมกัน | query DB เร็วระดับ ms อยู่แล้ว คอขวดคือรอบคุยกับ LLM → ไม่คุ้ม (YAGNI) |

**ทำไมเลือกแนวนี้:** ยึดลำดับ **เข้าใจถูก > ประหยัด token > เร็ว** เสมอ — ยอมส่ง tool เกินได้
แต่ห้ามพลาดจนสั่งงานไม่ได้ และทุกอย่างต้องมี eval วัดได้ก่อนเปลี่ยน (data-driven ไม่ใช่รู้สึกเอา)

---

## Q6 — AI comparison วัดยังไง แต่ละแกน (quality / latency / token)

**วัดยังไง:** bake-off harness (`eval_test.go` → `TestBakeOff`) ยิง **24 เคสภาษาไทย**
(ทักทาย/อ่าน/สร้าง-แก้-ลบ/กับดัก; เคสที่ 24 `typo-create` เพิ่ม 2026-07-08 — คะแนนในตาราง
มาจาก run 23 เคสของ 2026-07-06) เข้าโมเดลจริงบน Groq เช็ค **"การตัดสินใจครั้งแรก"** ว่าเลือก
tool ถูกไหม (`got == want`) — เพราะหัวใจของ AI คือ "เข้าใจว่าผู้ใช้จะเอาอะไร" ที่เหลือเป็น SQL ตายตัว

| แกน | วัดอะไร | 20b | 120b | qwen |
|-----|---------|-----|------|------|
| **Quality** | เลือก tool ถูกครั้งแรกกี่เคส | **23/23** | 21/22 | 13/13 (เท่าที่ทัน) |
| **Token** | prompt token เฉลี่ย/request | **~2,697** | ~2,698 | ~3,282 |
| **Latency** | จับเวลาเฉพาะรอบ HTTP สำเร็จ (ตัด retry 429 ออก) | **~0.83s** | ~0.92s | ~0.90s |

**สรุปการตัดสิน:** quality ตันเท่ากัน → ตัดสินที่ token + speed + รอด rate limit → เลือก
**gpt-oss-20b** (ถูกสุด เร็วสุด cache-friendly) 120b ไม่แม่นกว่าเลยไม่คุ้มจ่ายเพิ่ม

> เลือกโมเดลแบบวัดจริง ทำซ้ำได้ (`go test -run TestBakeOff`) ไม่ใช่ "รู้สึกว่าตัวนี้ดี"

---

## Q7 — `answerFromContext` คือยังไง

กรณี **"คำตอบอยู่บนจอแล้ว ไม่ต้องดึง DB ซ้ำ"** เป็นจริงเมื่ออย่างใดอย่างหนึ่งจริง
(`controller.go:386-394`):

- **`inlineData`** — คำถามเชิงวิเคราะห์ (แนวโน้ม/ทำนาย) กับกราฟที่ focus → frontend แนบ
  **ข้อมูลเส้นกราฟที่ render บนจอ** มาใน context (มีคำว่า `"on-screen data"`) → AI อ่านตรงนั้นตอบเลย
- **`contextRead`** — `@`-focus widget แล้วถามค่าปัจจุบัน/ช่วง/config ที่ context มีอยู่แล้ว
  **และไม่ใช่** การแก้ (`editRe`), ถามช่วง/เฉลี่ย (`rangeRe`), ถาม SKU (`skuRe`)

ผล → ใช้ `systemPromptContextAnswer` ("ห้ามเรียก tool ตอบจาก context") + ไม่ส่ง tool →
ตอบรอบเดียว ประหยัด token + เลี่ยง rate limit

**guardrail:** แค่ `@`-focus ไม่พอ ถ้าเป็นการแก้ → ไปทาง 2c, ถ้าถามช่วง/เฉลี่ย/SKU ที่ไม่ได้อยู่บนจอ
→ กลับไปทาง tool ปกติ (2b) เพื่อ fetch จริง

**แล้วถ้าถามข้อมูลที่อยู่บนจอ แต่ไม่ได้ @ widget ล่ะ — เข้าเคสไหน (Slide 3)?**
เข้าเส้น **Base + ContextExt (มี tools)** ไม่ใช่ ContextAnswer — เพราะทั้งสองเงื่อนไขของ
answerFromContext ต้องมี focus: `inlineData` จะแนบ series บนจอก็ต่อเมื่อ widget ถูก @ + คำถาม
เชิงวิเคราะห์ + เป็น line-chart/daily-count (`AIAssistantPage.vue:510-512`) และ `contextRead`
ก็เช็ค `focused` ตรง ๆ (`controller.go:392`). ไม่ @ = context ที่ส่งไปมี**ทุก widget** (ไม่มีป้าย
`[FOCUSED]`, ไม่มี series แนบ) → AI มักเรียก tool fetch จาก DB แทน ซึ่งได้คำตอบถูกเหมือนกัน
แค่แพงกว่า 1 รอบ tool — นี่คือเหตุผลที่ควรสอนผู้ใช้ให้ @ widget เวลาถามเจาะตัวใดตัวหนึ่ง.
(ถ้าเลวร้ายสุดคือคำถามไม่มี keyword ติด needsTools เลย → ตกไป Minimal → sentinel `NEED_TOOLS`
ยกกลับขึ้นเส้น tool ให้ — ดู Q12)

---

## Q8 — ควรเปลี่ยน regex → ให้ AI เลือกเอง / vector matching ไหม?

**สรุป: ไม่ควรเปลี่ยนตอนนี้**

จุดที่มักเข้าใจผิด: **regex ไม่ได้ตัดสินใจแทน AI** — มันตัดสินแค่ "จะโหลด prompt/tool หนักแค่ไหน"
ส่วน**การตัดสินใจจริง (เรียก tool ไหน, args อะไร) AI ทำเองอยู่แล้ว**ใน tool-loop
ดังนั้น regex เป็นแค่ประตูคัดเบา ๆ ไม่ใช่สมอง → ไม่คุ้มลงทุนหนัก

| ทางเลือก | ต้นทุนเพิ่ม | ปัญหา |
|----------|-------------|--------|
| **ให้ AI เลือก prompt เอง** | +1 รอบเรียก LLM ก่อนรอบจริง (ทุกข้อความ รวมทักทาย) | ย้อนแย้ง (จะถามต้องส่ง prompt ก่อนอยู่ดี), สู้กับ budget 8k/min, non-deterministic ทดสอบยาก |
| **Vector / semantic router** | embedding model (API=latency+key / local=dependency ใหม่) + ตัวอย่าง + threshold | ผิดข้อจำกัด "ห้าม dependency/Redis/table ใหม่", เกินจำเป็นสำหรับ 4 กลุ่ม, ไทย embedding ไม่แน่นอน, debug ยาก |
| **regex (ปัจจุบัน)** | 0 | pin ด้วย eval ได้ (23/23), 0ms, ไม่มี dependency |

**Trade-off ที่บอกว่าไม่ควรเปลี่ยน:**
1. ความผิดพลาดที่อันตรายคือ **false negative** (บอก "ไม่ต้อง tool" ทั้งที่ต้อง → AI ทำอะไรไม่ได้) —
   regex จงใจตั้งให้ "ใจกว้าง" (keyword เยอะ + เจอ `@` ก็ผ่าน) กัน เคสนี้ได้แน่ ส่วน threshold แบบ
   vector อาจ**เงียบ ๆ พลาด**
2. มี safety net อยู่แล้ว: `tool_choice:required` ถ้าโมเดลปฏิเสธ fallback เป็น `auto`, โมเดลถามกลับได้
   และล่าสุดมี **NEED_TOOLS sentinel** (Q12) ปิดรู false negative จากคำพิมพ์ผิดให้อีกชั้น
3. eval ปัจจุบัน 100% — ไม่มีอะไรให้แก้ (YAGNI)

**ควรกลับมาคิดใหม่เมื่อ:** intent เพิ่มเป็นหลายสิบกลุ่มจน regex ดูแลไม่ไหว, หรือ eval บน traffic
จริงเริ่มตก → แล้ววัดก่อนเปลี่ยน "next lever" ที่ถูกต้องคือ argument-level scoring ไม่ใช่เปลี่ยน router

> ตอนนี้แบ่งถูกแล้ว: **AI ตัดสินเรื่องสำคัญ (tool) / regex ตัดสินแค่เรื่องถูก (ขนาด prompt)**

---

## Q9 — `dispatch()` / tool executor / tool_actions.go / dashboard_action.go คืออะไร · AI คุยกับ backend เป็น JSON ไหม

**`dispatch()` (`controller.go:152`) = "พนักงานเดินตั๋ว"** — รับชื่อ tool + args จาก Groq แล้วเรียกฟังก์ชันจริงให้ถูกตัว. มันคือ `switch` ตามชื่อ tool: เช็คสิทธิ์ก่อน (write tool ต้อง admin/editor) → route ไปยัง executor:

- **`tool_actions.go`** — executor ของ **read tools** (`show_metric`, `get_telemetry_series`, `get_production_count`, `get_skus`, `get_active_alerts`, …). แต่ละตัวยิง **SQL จริงบน TimescaleDB** ผ่าน domain service (scope ตาม org)
- **`dashboard_action.go`** — executor ของ **preview tools** (`preview_dashboard/add/update/remove_widget`). resolve machine id + fields จาก DB (READ เท่านั้น) แล้วคืน "แผน" ให้ frontend stage — **ไม่เขียน DB**

> เปรียบเทียบ: Groq = สมองที่บอก "อยากได้ speed ของ CW-01" · `dispatch()` = คนรับคำสั่งเดินไปหยิบ · `tool_actions.go`/`dashboard_action.go` = มือที่ลงไป query DB จริง

**AI คุยกับ backend เป็น JSON — ใช่ ทั้ง chain เป็น JSON:**

| ขา | รูปแบบ | โค้ด |
|----|--------|------|
| Frontend → Backend | JSON `{conversationId, message, context}` POST `/api/ai/chat` | `controller.go` Chat handler |
| Backend → Groq | JSON body (messages + tools) | `json.Marshal(reqBody)` `controller.go:656` |
| Groq → Backend (ขอ tool) | JSON: `finish_reason:"tool_calls"` + args เป็น **JSON string** | struct `groqToolCall` |
| Backend รัน tool → ป้อนกลับ | ผลลัพธ์ `json.Marshal(result)` แนบเป็น message `role:"tool"` | `controller.go:509-536` |
| Groq → Backend (คำตอบ) | JSON: `finish_reason:"stop"` + ข้อความ | struct `groqResponse` |

- args ที่ Groq ส่งมาเป็น JSON string → `dispatch()` รับเป็น `json.RawMessage` แล้ว unmarshal ตาม tool
- ผล query (rows) ถูก marshal เป็น JSON (series/count ใหญ่ ๆ ถูก **compact** เป็น columns+tuples ก่อนเพื่อลด token) แล้วป้อนกลับให้ Groq สรุปเป็นภาษาคน
- error ก็เป็น JSON (`{"error": ...}`) — Groq อ่านแล้วบอกผู้ใช้ได้

> **ทุกอย่างในลูปเป็น JSON** — เพราะ Groq ใช้ OpenAI-compatible chat-completions ที่กำหนด tool calling ผ่าน JSON schema. เลข/ข้อมูลจริงทั้งหมดมาจาก DB (JSON แค่ "ห่อ" ให้ LLM อ่าน) ไม่ใช่ AI แต่งเอง

---

## Q10 — ทำไมไม่ใช้ AI agent framework (LangGraph หรืออย่างอื่น)?

จุดที่มักเข้าใจผิด: ระบบนี้**เป็น agent อยู่แล้ว** — มันคือ tool-calling loop (LLM ตัดสินใจ →
รัน tool → ป้อนผลกลับ → วนจนได้คำตอบ) ซึ่งเป็นแกนเดียวกับที่ framework ทุกตัวห่อขาย
คำถามจริงคือ "เขียน loop เอง ~ร้อยกว่าบรรทัด vs เอา framework มาห่อ" และคำตอบมาจาก trade-off:

| ประเด็น | เขียนเอง (ปัจจุบัน) | LangGraph / framework |
|---------|--------------------|----------------------|
| ภาษา | Go — อยู่ใน backend เดิม ไฟล์เดียว | Python/JS → ต้องเพิ่ม **service ใหม่ทั้งก้อน** + ช่องทางคุยข้าม service |
| Dependency | 0 (net/http + encoding/json) | ecosystem ทั้งชุด — ขัดหลักโปรเจกต์ที่เลี่ยง dependency ที่ไม่จำเป็น |
| สิ่งที่ framework ให้ | — | graph state, multi-agent, checkpoint, human-in-the-loop node |
| เราใช้สิ่งเหล่านั้นไหม | ไม่ — มี agent เดียว, loop เดียว, 12 tools, human-in-the-loop ทำที่ frontend (ปุ่ม Confirm/Save) อยู่แล้ว | จ่ายความซับซ้อนสำหรับของที่ไม่ได้ใช้ |
| คุม token / rate limit | คุมได้ระดับบรรทัด: slim schema, roundCap, prompt 4 ชั้น, sentinel — จำเป็นมากเพราะ budget ฟรี 8k tok/min | abstraction ห่อไว้ ปรับละเอียดยาก + overhead ของ framework เอง |
| Debug | อ่าน loop ตรง ๆ 1 ไฟล์, deterministic ทดสอบได้ | ต้อง trace ผ่าน graph/abstraction หลายชั้น |

**ปรัชญาเดียวกับ Q8 (regex router):** ความต้องการตอนนี้เล็กและชัด — เครื่องมือที่เบาที่สุดที่พอ
ชนะ **ควรกลับมาคิดใหม่เมื่อ:** ต้องการหลาย agent ทำงานร่วมกัน, workflow วิ่งยาวข้ามวัน
ต้อง checkpoint/resume, หรือ orchestration ซับซ้อนเกิน loop เดียวจะดูแลไหว

> framework ไม่ได้ทำให้ "ฉลาดขึ้น" — ความฉลาดอยู่ที่โมเดล+tools ซึ่งเรามีครบแล้วใน loop ที่คุมเองได้ 100%

---

## Q11 — อธิบายกลไกใน diagram ทีละตัว (tool_choice=required, dispatch, compact, roundCap, …)

ไล่ตามเส้นทางจริงของ 1 request (Slide 3 ภาพรวม → Slide 4 ซูมเข้า loop):

1. **classify** — regex เลือก prompt 1 ใน 4 + ตัดสินว่าส่ง tools ไหม (Q2) — ยังไม่แตะ LLM เลย
2. **turn 0: `tool_choice=required` คืออะไร** (`controller.go:434`) — ปกติโมเดลเลือกเองว่าจะเรียก
   tool หรือตอบ text (`auto`) แต่ถ้าข้อความมี `@widget` focus เรา**บังคับ**รอบแรกให้ต้องเรียก tool
   เพราะ @ คือสัญญาณชัดว่ามี widget จริงให้ลงมือ — กันโมเดลตอบน้ำ. บังคับเฉพาะ turn 0;
   ถ้าโมเดลฝืนตอบ text Groq จะ error แล้ว backend retry ด้วย `auto` ให้เอง (`:451-455`) —
   เป็น optimization ไม่ใช่กฎตายตัว
3. **`dispatch(name, args)`** (`:152`) — "พนักงานเดินตั๋ว": รับชื่อ tool + args (JSON) จาก Groq,
   เช็คสิทธิ์ (write ต้อง admin/editor), แล้ว switch ไปเรียก executor จริง (รายละเอียด Q9)
4. **compact (ลด token)** — ผล query ที่เป็น series/count ยาว ๆ ไม่ส่งกลับดิบ ๆ แต่ย่อเป็นรูป
   `columns + tuples` (`compactSeriesResult`/`compactBucketResult`) ก่อน marshal — ข้อมูลเท่าเดิม
   token ลดลงมาก เพราะไม่ต้องทวนชื่อ field ทุกแถว
5. **`roundCap`** (`:538-542`) — เบรกคุมจำนวนรอบ tool: ถาม @focused = 0 (ยิง tool รอบเดียวแล้ว
   ต้องสรุป), ทั่วไป = 1 (ยิงได้ 2 รอบ เผื่อ pattern `get_machines` → `show_metric`). พอเกิน cap
   จะเซ็ต `callTools = nil` → รอบถัดไปโมเดลไม่มี tool ให้เรียก เลย*ต้อง*ตอบเป็นข้อความ.
   ทำไมต้องจำกัด: ทุกรอบ re-send context ~3k token — ปล่อยวนฟรีจะทะลุ 8k tok/min
6. **บันทึกทุก turn** — user (ก่อน loop), tool (ทุกครั้งที่รัน), assistant (ตอนจบ) ลง `ai_messages`
   แล้วส่ง `newMessages[]` กลับให้ frontend render (ข้อความ + preview card)
7. **ทางออกของ loop มี 3 ทาง** — โมเดลตอบ text (จบปกติ), callTools ถูกตัดแล้วเลยต้องตอบ text
   (roundCap), หรือครบ 5 รอบ (hard cap กันหลุด — ปกติไม่ถึง). กรณีพิเศษ: text ที่ตอบคือ
   `NEED_TOOLS` บนเส้น no-tool → ไม่จบ แต่ escalate (Q12)

> จำง่าย ๆ: **required = บังคับลงมือรอบแรก · dispatch = คนวิ่งงาน · compact = ย่อผลก่อนส่ง ·
> roundCap = เบรกบังคับสรุป**

---

## Q12 — พิมพ์ผิดแต่อยากให้สร้างจริง Minimal จับ intent ได้อย่างไร (NEED_TOOLS sentinel)

ปัญหาเดิม: "ส้างแดชบอด cw-01" (สะกดผิด) ไม่ติด keyword ใด ๆ ใน regex → ถูกจัดเป็นคุยเล่น
เข้า Minimal ที่**ไม่มี tools** → โมเดลเคยตอบ "กำลังสร้างให้ครับ" ทั้งที่ทำอะไรไม่ได้เลย (role-play)

ทางแก้ (ship 2026-07-08): ให้ **ตัวโมเดลเองเป็นคนจับ intent แทน regex** — เพราะ LLM อ่านคำผิดออก:

1. `systemPromptMinimal` มีกฎเพิ่ม 1 ข้อ (`controller.go:41-42`): *ถ้าข้อความล่าสุดขอดูข้อมูล/
   สร้าง/แก้/ลบอะไรก็ตาม — แม้สะกดผิด — ให้ตอบว่า `NEED_TOOLS` เท่านั้น* (ทักทายปกติตอบปกติ)
2. backend เจอคำตอบที่เป็น `NEED_TOOLS` เป๊ะ ๆ บนเส้น no-tool (`:487-494`) → สลับเป็น
   `systemPromptBase` (+ContextExt ถ้ามีจอเปิด) + tools ครบ แล้ว**วนใหม่อีก 1 รอบ**
3. guard ด้วย flag `escalated` — ทำได้ครั้งเดียวต่อ request ไม่มีทางวนลูป และคำว่า `NEED_TOOLS`
   **ไม่ถูกบันทึกเป็นคำตอบใน DB** (ผู้ใช้ไม่มีวันเห็น)

**ต้นทุน:** คำทักทายจ่ายเพิ่มแค่ ~40 token (ค่ากฎ 1 บรรทัด); ราคาเต็มของ retry (~2.9k token)
จ่ายเฉพาะตอน regex พลาดจริง ๆ เท่านั้น

**พิสูจน์แล้ว:** live test `TestMinimalPromptSentinel` (typo 2 แบบได้ sentinel, ทักทาย/คุยเล่นไม่ได้)
+ ทดสอบจริงบน stack: "ส้างแดชบอด cw-01 ให้หน่อย" ได้ preview card จริง ไม่ใช่คำตอบเปล่า

> regex เป็นด่านแรกที่เร็วและฟรี — โมเดลเป็นด่านสองที่อ่านคำผิดออก ใช้จุดแข็งคนละอย่าง

---

## Q13 — test แต่ละตัวทำอะไร (สั้น ๆ)

| Test | ไฟล์ | ทดสอบอะไร | ชนิด |
|------|------|-----------|------|
| `TestNeedsTools` | dashboard_action_test.go | gate regex: ข้อความไหนควรได้ tools (รวม doc ว่า typo พลาดได้ — sentinel รับ) | unit |
| `TestToDatetimeLocal` | dashboard_action_test.go | แปลงรูปแบบวันเวลาเป็น datetime-local ของ widget | unit |
| `TestShortTimePtrBangkok` | timezone_test.go | format เวลาโซนกรุงเทพ (UTC+7) ถูกต้อง | unit |
| `TestParseRetryAfter` | ratelimit_test.go | อ่านค่า wait จาก 429 (header/body) + เพดาน 30s | unit |
| `TestMinimalPromptSentinel` | sentinel_live_test.go | ยิง Groq จริง: typo ได้ `NEED_TOOLS`, ทักทายไม่ได้ | live (ต้องมี `GROQ_API_KEY`, ~40s) |
| `TestBakeOff` | eval_test.go | เทียบโมเดล 24 เคสไทย วัด first-decision tool accuracy + token + latency | live (~20 นาที, รันเมื่อจะเปลี่ยนโมเดล) |

รันเฉพาะ unit: `cd backend; go test ./internal/modules/ai/` (live 2 ตัว skip เองถ้าไม่มี key;
bake-off ต้องใส่ `-run TestBakeOff -count=1` ถึงจะรัน)

---

## Q14 — AI รู้ได้ไงว่าต้องใช้ tool ไหน? ส่ง description ของทุก tool ไปไหม?

**ใช่ — ส่งรายการ tool ไปให้เลือกทุก request** (ตามสเปก OpenAI tool calling): แต่ละ tool มี
ชื่อ + description ที่เขียนบอกชัดว่าใช้เมื่อไหร่ (`schema.go`) โมเดลอ่านแล้ว**เลือกเอง**ว่าเรียก
ตัวไหนด้วย args อะไร — ไม่มี logic ฝั่งเราไปเดา intent แทน

แต่ "ทุก tool" มีเงื่อนไข 3 ชั้นเพื่อประหยัดและปลอดภัย:

1. **กรองตามสิทธิ์/บริบทก่อนส่ง** — viewer ไม่ได้รับ preview tools; ไม่มี dashboard เปิดก็ไม่ส่ง
   preview tools; `create_custom_dashboard` (เขียน DB จริง) **ไม่ถูกส่งให้โมเดลเลย** — frontend
   เรียกเองหลังผู้ใช้กด Confirm
2. **ส่งแบบ slim** (`toGroqToolSlim`, `controller.go:573`) — tool ทั่วไปส่งแค่ชื่อ + description
   (คำใบ้ args ฝังในนั้น) ~50–80 token/ตัว; ส่ง JSON schema เต็มเฉพาะ tool ที่มีโครงสร้าง widget
   ซ้อน (เช่น `preview_add_widget`)
3. **กฎ TOOL SELECTION ใน systemPromptBase ช่วยชี้ทาง** — บอก pattern ว่าคำถามแบบไหนคู่กับ
   tool ไหน (อ่านค่า → `show_metric`, สร้าง → `preview_dashboard`, …) เสริมกับ description

ความแม่นของการเลือกถูกวัดจริงด้วย bake-off (Q6) — first-decision accuracy ~100% บนเคสที่วัด

> โมเดลเห็น "เมนูเครื่องมือ" พร้อมคำอธิบายทุกครั้ง แล้วหยิบเอง — เราคุมแค่ว่าเมนูมีอะไรให้หยิบ

---

## Q15 — Groq/โมเดลจำประวัติแชตไหม (cache)? แล้วเราต้องส่ง history เองไหม

**ไม่จำ — และต้องส่งเองทุกครั้ง** Chat completions API เป็น **stateless**: แต่ละ request คือ
กระดาษเปล่า โมเดลรู้เฉพาะสิ่งที่แนบไปใน `messages[]` ของ request นั้น จบ request ก็ลืมหมด

ฝั่งเราจัดการอย่างนี้ (`controller.go:368-374`):
- history เก็บใน DB ของเราเอง (`ai_messages`) แล้วแนบไปกับทุก request **แค่ 8 ข้อความล่าสุด**
  (~3–4 turn) — พอให้คุยต่อเนื่อง ไม่บวม token
- tool payload เก่า ๆ ไม่ถูก replay — คำสรุปของ assistant รอบก่อนจับใจความไว้แล้ว

**แล้ว prompt cache ที่พูดถึงคืออะไร?** คนละเรื่องกับความจำ — มันคือ Groq **cache ผลการ
ประมวลผล** ของ prompt ส่วนหัวที่ byte เหมือนเดิมทุกครั้ง (เราจงใจล็อก `systemPromptBase`
ให้นิ่งเพื่อสิ่งนี้) ทำให้ request ถัดไป**เร็วขึ้น/ถูกลง** แต่**ไม่ได้แปลว่าโมเดลจำอะไรได้** —
ข้อความทุกตัวอักษรยังต้องส่งไปเต็ม ๆ ทุกครั้งเหมือนเดิม

> สรุป: ความจำ = DB ของเรา + ส่ง 8 ข้อความล่าสุดไปเอง · cache = แค่ทางลัดการคำนวณ ไม่ใช่ความจำ
