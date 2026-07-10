# IotVision AI — Q&A สำหรับ Review / ตอบคำถาม present

ตอบคำถามที่พบบ่อยเกี่ยวกับ AI assistant แบบเข้าใจง่าย อ้างอิงโค้ดจริงใน
`backend/internal/modules/ai/` และ `frontend/src/pages/AIAssistantPage.vue`
คู่กับ `AI_ARCHITECTURE.md` (รายละเอียดลึก) และ `AI_SLIDES_DIAGRAMS.md` (ภาพ present)

---

## อัปเดต 2026-07-10: ระบบ Intent Router (Tasks 1-5 shipped)

ระบบเปลี่ยนจากการเลือก prompt ด้วย regex keyword → ใช้ LLM router (ClassifyIntent) บวก
dispatcher (dispatchIntent) บวก verify loop. Q ที่ขึ้นต่อการออกแบบเดิมได้รับการอัปเดต:
- **Q2, Q8, Q12, Q15** เปลี่ยนแปลงไป (อ่านข้อมูลด้านล่างสำหรับรายละเอียด)
- **Q6** เพิ่มเติม: ตอนนี้มี router bake-off แยกจาก main bake-off
- History cap: ลดจาก 8 → 3 messages



## สรุปสั้นทุกข้อ (ใช้เป็นสารบัญ)

| # | คำถาม | คำตอบ 1 บรรทัด |
|---|--------|----------------|
| Q1 | ทำไม qwen ใช้ token เยอะกว่า | tokenizer หั่นภาษาไทยถี่กว่า ~20% — ข้อความเดียวกันกลายเป็น token มากกว่า เลยชน rate limit ก่อน |
| Q2 | เลือก prompt ยังไง / router ทำงานยังไง | prompt เดียว (systemPromptUnified) + ClassifyIntent (gpt-oss-20b) → JSON intent → dispatchIntent (Go) ตัดสิน tool/role |
| Q3 | mention (@) กับ highlight ต่างกันยังไง | mention = คนชี้ widget เข้า, highlight = AI ชี้กลับ |
| Q4 | AI รู้ข้อมูล/แก้ config ได้ยังไง | AI แค่เลือก tool — backend รัน SQL จริง; setting ที่แก้ได้คือ args ของ `preview_update_widget` |
| Q5 | แผนถัดไปมีอะไร ทำไมยังไม่ทำ | argument-level scoring / SSE / ลด prompt / parallel dispatch — รอวัดก่อนค่อยทำ (data-driven) |
| Q6 | เทียบโมเดลวัดยังไง | router bake-off 32 เคส (20b: 28/32), main bake-off 23 เคส (120b main, 20b router, qwen dropped) |
| Q7 | answerFromContext คืออะไร / ถามข้อมูลบนจอแต่ไม่ @ | "คำตอบอยู่บนจอแล้ว" ต้อง @focus เท่านั้น — ไม่ @ จะเข้าเส้น tool ปกติ (AI fetch DB เอง) |
| Q8 | ควรใช้ AI router ไหม? | ใช้แล้ว (shipped Phase 2) — Groq prompt cache เปลี่ยนเศรษฐศาสตร์; router + verify loop + always-attached tools = safer |
| Q9 | dispatch/executor คืออะไร · คุยเป็น JSON ไหม | dispatch = switch เช็คสิทธิ์แล้วเรียก executor จริง; ทั้ง chain เป็น JSON ตาม OpenAI spec |
| Q10 | ทำไมไม่ใช้ LangGraph / agent framework | ระบบนี้*คือ* agent อยู่แล้ว — loop เองใน Go ไม่กี่ร้อยบรรทัด คุม token ได้ละเอียดกว่า และไม่เพิ่ม dependency |
| Q11 | อธิบาย step ใน diagram (required/dispatch/compact/roundCap) | ไล่ทีละกลไกของ tool loop: บังคับ tool turn แรก → รัน tool → ย่อผล → จำกัดรอบ → บังคับสรุป |
| Q12 | พิมพ์ผิดแต่อยากสร้างจริง router จับได้ยังไง | ClassifyIntent อ่านคำผิดออก (LLM ไม่ regex) + verify loop หาปัญหา → repair/askback ถ้าต้อง |
| Q13 | test แต่ละตัวทำอะไร | unit 4 ตัว (dispatch/เวลา/429) + live 4 ตัว (router bake-off, main bake-off, verify, date-edit) |
| Q14 | AI รู้ได้ไงว่าใช้ tool ไหน ส่ง description ทุกตัวไหม | ใช่ — ส่งชื่อ+description ของ tool ที่ role/บริบทอนุญาตไปทุก request แล้วโมเดลเลือกเอง |
| Q15 | Groq จำ history ไหม ต้องส่งเองไหม | ไม่จำ (stateless) — เราส่ง 3 ข้อความล่าสุดเองทุกครั้ง (ลดจาก 8); prompt cache ช่วยเร็ว/ถูก (ไม่ใช่ความจำ) |

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

## Q2 — เลือก prompt อย่างไร / system prompt เดียวหรือหลาย / router คืออะไร

ตอนนี้ใช้ **prompt เดียว** (`systemPromptUnified`) ส่งทุก request บวก
full tool set พร้อมเสมอ (Groq prompt cache ช่วยเร็ว/ถูกลง 50% input discount).

แทน regex keyword gate, ตอนนี้มี **intent router** สองชั้น:

1. **`ClassifyIntent`**: เรียก `openai/gpt-oss-20b` ด้วย forced `classify_intent`
   tool → JSON `{intent, machine, metric, ...}` ที่ confidence floor 0.5 ↑. ต่ำกว่า 0.5 หรือ
   invalid JSON → fallback (routerOK = false) = safe-mode: ใช้ tools ปกติแบบเดิม
2. **`dispatchIntent`**: pure Go function ที่ map intent → tool_choice + roundCap
   ตรงกัน + role gate (preview tools ให้ editor/admin เท่านั้น). ถ้า router failed → auto tools

**ผล:** Model ที่เลือก tool/decide action (AI ยังตัดสินใจจริง), Go ตัดสินเรื่องรอบต่างๆ
(เลือก model ไหน, บังคับ tool ไหน, ตัดโอกาส round กี่ครั้ง). ไม่มี "escape hatch sentinel" อีก
เพราะ router จับ typo ได้ + verify loop + always-attached tools ทำ safety net ที่ดีกว่า

---

## Q3 — highlight, mention ทำงานยังไง

คนละเรื่องแต่ทำงานคู่กัน (`AIAssistantPage.vue`):

**Mention (@) = คนชี้ว่า "สนใจ widget ตัวนี้"**
- คลิก widget → กลายเป็น chip (`mentionedWidgets`) แนบไปกับคำถาม
- ผล 2 อย่าง:
  1. context ที่ส่งไปหดเหลือเฉพาะ widget ที่ mention ติดป้าย `[FOCUSED]` → AI ไม่หลงไปตอบตัวอื่น
  2. `@` (ตัวแปร `focused`) เป็นหนึ่งใน input ที่ `dispatchIntent` ใช้ตัดสิน tool_choice — เคสที่
     intent สอดคล้องกับ widget ที่ focus อาจได้ tool_choice `required` ให้ลงมือกับตัวนั้นทันที
     (เดิมมี regex `needsTools` เป็นตัวกระตุ้น — ตอนนี้ถูกแทนด้วย router + dispatchIntent)

**Highlight = AI ตอบเสร็จแล้วชี้กลับว่า "หมายถึงตัวนี้"**
- หลัง AI แตะ widget ไหน `previewHighlightId` ชี้ไปตัวนั้น → ขึ้นกรอบเรืองรอบ widget บน canvas
  (`setAiHighlight` → `highlightWidgetById`)

> **mention = คนชี้เข้า, highlight = AI ชี้กลับ** — คุยตรง widget ไม่ต้องพิมพ์ชื่อยาว

---

## Q4 — AI รู้ข้อมูลได้ยังไง / config ยังไง / setting อะไร / แกน x,y

**รู้ข้อมูลยังไง:** AI ไม่เดา มันแค่เลือกว่า "เรียก tool ไหน" แล้ว backend รัน **SQL จริงบน
TimescaleDB** (`tool_actions.go`) เอาผลป้อนกลับให้สรุปเป็นภาษาคน (ทุก tool = 1 query จริง)

**setting ที่ AI แก้ได้** (`preview_update_widget`, `schema.go`):
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
| **ลดขนาด prompt** | *(เดิม: ตัด `ContextExt` ที่ซ้ำซ้อน — ตอนนี้ prompt รวมเป็น `systemPromptUnified` ตัวเดียวแล้ว ข้อนี้ไม่ apply แล้ว)* | ตัดได้อย่างปลอดภัยต่อเมื่อมี eval วัดก่อน — "วัดก่อน ค่อยตัด" |
| **Parallel tool dispatch** | ยิงหลาย tool พร้อมกัน | query DB เร็วระดับ ms อยู่แล้ว คอขวดคือรอบคุยกับ LLM → ไม่คุ้ม (YAGNI) |

**ทำไมเลือกแนวนี้:** ยึดลำดับ **เข้าใจถูก > ประหยัด token > เร็ว** เสมอ — ยอมส่ง tool เกินได้
แต่ห้ามพลาดจนสั่งงานไม่ได้ และทุกอย่างต้องมี eval วัดได้ก่อนเปลี่ยน (data-driven ไม่ใช่รู้สึกเอา)

---

## Q6 — AI comparison วัดยังไง แต่ละแกน (quality / latency / token)

**สถานะ (2026-07-10): มี bake-off 2 ชุดแยกกัน** — main chat model กับ router model คนละหน้าที่
คนละ eval:

**Main model bake-off** (`eval_test.go` → `TestBakeOff`, ตารางด้านล่างเป็นผลรันเดิม 2026-07-06,
23 เคสภาษาไทย: ทักทาย/อ่าน/สร้าง-แก้-ลบ/กับดัก) วัด **"การตัดสินใจครั้งแรก"** ว่าเลือก tool ถูกไหม
(`got == want`) — เพราะหัวใจของ AI คือ "เข้าใจว่าผู้ใช้จะเอาอะไร" ที่เหลือเป็น SQL ตายตัว:

| แกน | วัดอะไร | 20b | 120b | qwen |
|-----|---------|-----|------|------|
| **Quality** | เลือก tool ถูกครั้งแรกกี่เคส | **23/23** | 21/22 | 13/13 (เท่าที่ทัน) |
| **Token** | prompt token เฉลี่ย/request | **~2,697** | ~2,698 | ~3,282 |
| **Latency** | จับเวลาเฉพาะรอบ HTTP สำเร็จ (ตัด retry 429 ออก) | **~0.83s** | ~0.92s | ~0.90s |

**⚠️ ข้อสรุปเดิม (superseded):** ตารางข้างบนเคยสรุปว่าเลือก `gpt-oss-20b` เป็น **main model**
เพราะ quality ตันเท่ากันแล้วชนะที่ token+speed ข้อสรุปนี้**ใช้ไม่ได้แล้ว** หลังสถาปัตยกรรม router
เปลี่ยนบทบาทโมเดล — ดูข้อสรุปใหม่ด้านล่าง

**ข้อสรุปปัจจุบัน (2026-07-10):**
- **`openai/gpt-oss-120b`** คือ **live main model** (`groqModel`) — รับหน้าที่ tool-calling loop เต็ม
  ตัว เพราะ reasoning capacity สูงกว่าและต้นทุนแฝงตัดกลบด้วย prompt cache
- **`openai/gpt-oss-20b`** ไม่ได้เป็น main model แล้ว — ย้ายไปเป็น **live router model** (`routerModel`
  ใน `ClassifyIntent`/`VerifyAnswer`) เพราะเร็วและถูกกว่า เหมาะกับงาน classify ที่ต้องยิงทุก request

**Router bake-off** (`router_eval_test.go` → `TestRouterBakeOff`, 32 เคส: 24 legacy + 8 ใหม่รวม
typo) วัด intent classification accuracy:

| Model | Score | หมายเหตุ |
|-------|-------|---------|
| `openai/gpt-oss-20b` | **28/32** | live router — จับ typo ได้ (เช่น "ส้างแดชบอด") |
| `llama-3.1-8b-instant` | 1/32 | เก็บไว้ใน eval เพื่อ record — Groq validator reject forced tool_choice ของโมเดลนี้ |

> **หมายเหตุสำคัญ:** คะแนน router 28/32 ใช้ **corrected scoring** — per-case accept-sets (ยอมรับ
> คำตอบได้มากกว่า 1 แบบต่อเคส) และเคส ambiguous จะ "ผ่าน" ได้ด้วยการปฏิเสธ (decline) ไม่ใช่ต้องเดา
> ถูกเป๊ะ — **ไม่ apples-to-apples** กับ main bake-off ที่เช็คแบบ strict `got == want` ห้ามเทียบตัวเลข
> สองชุดตรงๆ

> เลือกโมเดลแบบวัดจริง ทำซ้ำได้ (`go test -run TestBakeOff` / `-run TestRouterBakeOff`) ไม่ใช่ "รู้สึกว่าตัวนี้ดี"

---

## Q7 — "คำตอบอยู่บนจอแล้ว ไม่ต้องเรียก tool" ตัดสินยังไง (เดิมเรียก `answerFromContext`)

**อัปเดต:** กลไกเดิม (`answerFromContext` = `inlineData || contextRead`, เช็คด้วย regex `editRe`/
`rangeRe`/`skuRe`, แล้วสลับไป `systemPromptContextAnswer`) **ถูกลบทั้งหมด**. ตอนนี้การตัดสินใจ
"ไม่ต้องเรียก tool" มาจาก **`dispatchIntent`** ล้วนๆ โดยใช้สัญญาณเดียวกันในหลักการแต่คำนวณต่างจุด:

- **`focused`** — ผู้ใช้ `@`-mention widget (context ที่ส่งไปมีป้าย `[FOCUSED]`)
- **`inlineData`** — คำถามเชิงวิเคราะห์กับกราฟที่ focus → frontend แนบข้อมูลเส้นกราฟที่ render
  บนจอมาใน context (ยังมีคำว่า `"on-screen data"` เหมือนเดิม — ฝั่ง frontend ไม่เปลี่ยน)
- **`intent`** — ผลจาก `ClassifyIntent`: ถ้าเป็น `read_*` หรือ `chat` (ไม่ใช่ edit/create/compare)

เมื่อ **focused + inlineData + intent เป็น read/chat** พร้อมกัน → `dispatchIntent` คืน
`tool_choice: "none"` → โมเดลตอบจาก context ที่แนบมาโดยไม่เรียก tool เลย (ยังคงประหยัด token +
เลี่ยง rate limit เหมือนเดิม) ส่วนกฎ **ON-SCREEN DATA** ที่บอกโมเดลว่า "มีข้อมูลอยู่ในนี้แล้ว ให้อ่าน
ตรงนั้น" ตอนนี้อยู่ใน `systemPromptUnified` (prompt เดียว ไม่มี `ContextAnswer` แยกแล้ว)

**guardrail:** แค่ `@`-focus ไม่พอ — ถ้า intent เป็น `edit_widget`/`compare`/`create_dashboard`
→ dispatchIntent ให้ preview tools แทน (ไปทาง 2c); ถ้า intent เป็น `read_agg` แต่ไม่มี inlineData
(ข้อมูลที่ถามไม่ได้อยู่บนจอ) → dispatchIntent บังคับ tool อ่านจาก DB จริง (ไปทาง 2b)

**แล้วถ้าถามข้อมูลที่อยู่บนจอ แต่ไม่ได้ @ widget ล่ะ?**
`focused = false` → เงื่อนไขไม่ครบ → `dispatchIntent` ไม่ตั้ง tool_choice เป็น "none" → โมเดลมักเรียก
tool fetch จาก DB แทน ได้คำตอบถูกเหมือนกันแค่แพงกว่า 1 รอบ tool — เหตุผลที่ควรสอนผู้ใช้ให้ @ widget
เวลาถามเจาะตัวใดตัวหนึ่ง (ระบบไม่มี Minimal/sentinel fallback อีกแล้ว — full tool set ถูกแนบเสมอ
ไม่ว่าเคสไหน ดู Q12)

---

## Q8 — ควรใช้ AI router ไหม? ตอบแล้ว — ใช้แล้ว (Phase 2 shipped 2026-07-10)

**สรุป: ใช้ LLM router แล้ว — Groq prompt cache เปลี่ยนเศรษฐศาสตร์**

เดิม (ดู Q8 เก่า): regex เป็นเพียง "ประตูคัด prompt ขนาด" ไม่ใช่สมอง, อันตรายคือ false negative
(บอก ไม่ต้อง tool ทั้งที่ต้อง). ตรรมชาติของปัญหา: regex ไม่ได้คำพิมพ์ผิด ("ส้างแดชบอด").

ตอนนี้เปลี่ยน (shipped 2026-07):
1. **ClassifyIntent** (LLM router) แทน regex keyword gate — อ่านคำผิดออก, slot extraction ชัดเจน
2. **always-attached tools** — ไม่มีแบ่ง 4 prompt แล้ว, full role-filtered toolset ทุกครั้ง
3. **verify loop** — ตรวจสอบผลก่อนตอบ, repair/askback ถ้าต้อง (ไม่แค่ role-play)
4. **Groq prompt cache** — `systemPromptUnified` byte-stable + Groq automatic caching ทำให้รอบที่ 2+ เร็ว/ถูก
   → router cost (1 call per message) จ่ายได้ (cache save กลบทั่ว)

ต้นทุน vs ประโยชน์:
- **ต้นทุนเพิ่ม:** 1 รอบ router call (~600 tok, cached) ทุก request
- **ประโยชน์:** catch typos (ส้างแดชบอด CW-01), slot extraction sure (ไม่ hallucinate machine), 
  intent + slots → safer downstream dispatch

| ส่วน | Q8 เก่า | Q8 ใหม่ |
|-----|---------|---------|
| Classifier | regex keyword gate | LLM ClassifyIntent (gpt-oss-20b forced tool) |
| Typo safety | sentinel (Minimal → NEED_TOOLS → retry) ✓ extra cost ~200-2700 tok | router reads typos directly ✓ slot extraction sure |
| Safety net | tool_choice:required + fallback + sentinel | always-attached tools + verify loop + fallback |
| Cost | no extra route → layers save token | +1 route (cached) / request → total balanced |
| Eval | main model 23/23, no sentinel case measured | router 28/32, main model 21/22 |

**เหตุผล:** prompt cache economics มาก — byte-stable prefix คืนผลตั้งแต่ call ที่ 2 (50% input discount).
router ไม่ได้"ฟรี" แต่ margin ของมัน (speed + safety) ตัดได้ดี. ไม่ใช่ vector embedding ที่ก้อยหนัก.

---

## Q9 — `dispatch()` / tool executor / tool_actions.go / dashboard_action.go คืออะไร · AI คุยกับ backend เป็น JSON ไหม

**`dispatch()` = "พนักงานเดินตั๋ว"** — รับชื่อ tool + args จาก Groq แล้วเรียกฟังก์ชันจริงให้ถูกตัว. มันคือ `switch` ตามชื่อ tool: เช็คสิทธิ์ก่อน (write tool ต้อง admin/editor) → route ไปยัง executor:

- **`tool_actions.go`** — executor ของ **read tools** (`show_metric`, `get_telemetry_series`, `get_production_count`, `get_skus`, `get_active_alerts`, …). แต่ละตัวยิง **SQL จริงบน TimescaleDB** ผ่าน domain service (scope ตาม org)
- **`dashboard_action.go`** — executor ของ **preview tools** (`preview_dashboard/add/update/remove_widget`). resolve machine id + fields จาก DB (READ เท่านั้น) แล้วคืน "แผน" ให้ frontend stage — **ไม่เขียน DB**

> เปรียบเทียบ: Groq = สมองที่บอก "อยากได้ speed ของ CW-01" · `dispatch()` = คนรับคำสั่งเดินไปหยิบ · `tool_actions.go`/`dashboard_action.go` = มือที่ลงไป query DB จริง

**AI คุยกับ backend เป็น JSON — ใช่ ทั้ง chain เป็น JSON:**

| ขา | รูปแบบ | โค้ด |
|----|--------|------|
| Frontend → Backend | JSON `{conversationId, message, context}` POST `/api/ai/chat` | `controller.go` Chat handler |
| Backend → Groq | JSON body (messages + tools) | `json.Marshal(reqBody)` |
| Groq → Backend (ขอ tool) | JSON: `finish_reason:"tool_calls"` + args เป็น **JSON string** | struct `groqToolCall` |
| Backend รัน tool → ป้อนกลับ | ผลลัพธ์ `json.Marshal(result)` แนบเป็น message `role:"tool"` | `dispatch()` |
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
| คุม token / rate limit | คุมได้ระดับบรรทัด: slim schema, roundCap, intent router + dispatchIntent, verify loop — จำเป็นมากเพราะ budget ฟรี 8k tok/min | abstraction ห่อไว้ ปรับละเอียดยาก + overhead ของ framework เอง |
| Debug | อ่าน loop ตรง ๆ 1 ไฟล์, deterministic ทดสอบได้ | ต้อง trace ผ่าน graph/abstraction หลายชั้น |

**ปรัชญาเดียวกับ Q8 (custom router):** ความต้องการตอนนี้เล็กและชัด — เครื่องมือที่เบาที่สุดที่พอ
ชนะ **ควรกลับมาคิดใหม่เมื่อ:** ต้องการหลาย agent ทำงานร่วมกัน, workflow วิ่งยาวข้ามวัน
ต้อง checkpoint/resume, หรือ orchestration ซับซ้อนเกิน loop เดียวจะดูแลไหว

> framework ไม่ได้ทำให้ "ฉลาดขึ้น" — ความฉลาดอยู่ที่โมเดล+tools ซึ่งเรามีครบแล้วใน loop ที่คุมเองได้ 100%

---

## Q11 — อธิบายกลไกใน diagram ทีละตัว (router, dispatch, compact, roundCap, verify, …)

ไล่ตามเส้นทางจริงของ 1 request (สถาปัตยกรรมปัจจุบัน, อัปเดต 2026-07-10):

1. **`ClassifyIntent`** — เรียก router model (`routerModel`, forced tool `classify_intent`) ก่อน
   อย่างอื่นทั้งหมด ได้ JSON intent + slots (machine/metric/bucket/...) + confidence — ยังไม่แตะ
   main LLM เลย (เดิมเป็น regex เลือก prompt 1 ใน 4 — ขั้นตอนนั้นถูกลบ)
2. **`dispatchIntent`** — pure Go function แปลง intent → tool_choice (`"none"`/`"required"`/`""`)
   + `roundCap`. ปกติโมเดลเลือกเองว่าจะเรียก tool หรือตอบ text (`auto`) แต่ในบางเคส (เช่น
   read/production ที่ machine slot resolve ได้) เรา**บังคับ**รอบแรกด้วย `tool_choice: "required"`
   ให้ต้องเรียก tool ตรงชื่อ — กันโมเดลตอบน้ำ. บังคับเฉพาะ turn 0; ถ้าโมเดลฝืนตอบ text Groq จะ
   error แล้ว backend retry ด้วย `auto` ให้เอง — เป็น optimization ไม่ใช่กฎตายตัว
3. **`dispatch(name, args)`** — "พนักงานเดินตั๋ว": รับชื่อ tool + args (JSON) จาก Groq,
   เช็คสิทธิ์ (write ต้อง admin/editor), แล้ว switch ไปเรียก executor จริง (รายละเอียด Q9)
4. **compact (ลด token)** — ผล query ที่เป็น series/count ยาว ๆ ไม่ส่งกลับดิบ ๆ แต่ย่อเป็นรูป
   `columns + tuples` (`compactSeriesResult`/`compactBucketResult`) ก่อน marshal — ข้อมูลเท่าเดิม
   token ลดลงมาก เพราะไม่ต้องทวนชื่อ field ทุกแถว
5. **`roundCap`** — เบรกคุมจำนวนรอบ tool: ถาม @focused = 0 (ยิง tool รอบเดียวแล้วต้องสรุป), ทั่วไป
   = 1 (ยิงได้ 2 รอบ เผื่อ pattern `get_machines` → `show_metric`). พอเกิน cap จะเซ็ต
   `callTools = nil` → รอบถัดไปโมเดลไม่มี tool ให้เรียก เลย*ต้อง*ตอบเป็นข้อความ. hard cap 5 รอบ
   กันหลุด (ปกติไม่ถึง). ทำไมต้องจำกัด: ทุกรอบ re-send context ~3k token — ปล่อยวนฟรีจะทะลุ
   8k tok/min
6. **`VerifyAnswer` (verify loop)** — ทำงานเฉพาะเมื่อมี ≥1 tool ถูกเรียก (pure chat ไม่จ่ายค่านี้เลย):
   เช็ค deterministic ก่อน (เช่น metric ที่แก้ต้องมีอยู่จริงใน machine_fields) แล้วถ้าจำเป็นเรียก
   router model อีกครั้งเป็น verifier (`verify_answer` forced tool) → ได้ผล pass/repair/askback.
   repair ได้สูงสุด 1 รอบ (ส่งคำตอบเดิม + ปัญหาที่พบกลับไปให้โมเดลแก้); ถ้ายังไม่ผ่านหรือกำกวมตั้งแต่
   ต้น (router ปฏิเสธ + คำตอบไม่ตรง intent) → ตอบกลับด้วยคำถามชี้แจงของ verifier แทน
7. **บันทึกทุก turn** — user (ก่อน loop), tool (ทุกครั้งที่รัน), assistant (ตอนจบ, หลัง verify)
   ลง `ai_messages` แล้วส่ง `newMessages[]` + `intent` กลับให้ frontend render

> จำง่าย ๆ: **ClassifyIntent = แยกประเภทคำขอ · dispatchIntent = ตัดสิน tool/round · dispatch =
> คนวิ่งงาน · compact = ย่อผลก่อนส่ง · roundCap = เบรกบังคับสรุป · VerifyAnswer = ด่านตรวจก่อนส่ง**

---

## Q12 — พิมพ์ผิดแต่อยากให้สร้างจริง router จับได้อย่างไร (ผ่าน ClassifyIntent + verify)

ปัญหาเดิม: "ส้างแดชบอด cw-01" (สะกดผิด) ไม่ติด keyword ใด ๆ ใน regex → ถูกจัดเป็นคุยเล่น
เข้า Minimal ที่**ไม่มี tools** → โมเดลเคยตอบ "กำลังสร้างให้ครับ" ทั้งที่ทำอะไรไม่ได้เลย (role-play)

ทางแก้ (ship 2026-07-10): **LLM router อ่านคำผิดออก** ตรงๆ:

1. **ClassifyIntent** (`router.go`) — LLM เอง (ไม่ regex) อ่านข้อความแม้สะกดผิด → JSON intent.
   "ส้างแดชบอด cw-01" → `{intent: "create_dashboard", confidence: 0.75+}` ✓ catch ได้
2. **dispatchIntent** → map intent → tools + role guard
3. **Main call** + **verify loop** (ถ้า tools รัน) → ตรวจสอบผล (metric exist ไหม, widget valid ไหม)
   → ถ้าต้อง, repair call (router model) → clarifying question ถ้ายังไม่ได้
4. Router fallback safety: confidence < 0.5 หรือ invalid JSON → treat as "not-ok" → auto tools
   (ไม่ขัดจังหวะ, ปลอดภัย)

**ต้นทุน:** ClassifyIntent ~600 tok cached, verify เรียกเฉพาะตอนมี ≥1 tool รัน. โดยรวมคุ้มกว่าวิธี
sentinel เดิม (ก่อนหน้านี้) ที่ต้อง retry เต็มรอบทุกครั้งที่ regex พลาด.

**ผลจริง:** Live router eval 28/32 (typo create-dashboard case ✓ caught). หากไม่แน่ใจ intent
(low confidence) ตัวอื่นๆ (verify) ช่วยจับปัญหา → clarify ผู้ใช้ แทน role-play.

> LLM ดีที่อ่านคำผิด — router + verify ดีที่จับปัญหา + แนะสิ่งที่จะทำ

---

## Q13 — test แต่ละตัวทำอะไร (สั้น ๆ)

| Test | ไฟล์ | ทดสอบอะไร | ชนิด |
|------|------|-----------|------|
| `TestDispatchIntent*` (หลายตัว) | dispatch_test.go | dispatchIntent pure function: intent → tool_choice + roundCap สำหรับทุก intent/role/focus combo | unit |
| `TestParseIntentResult*` (หลายตัว) | router_test.go | parse/validate JSON ที่ router คืนมา (valid/invalid/low-confidence/unknown intent) | unit |
| `TestRunDeterministicChecks*` / `TestDecideVerifyOutcome*` (หลายตัว) | verify_test.go | deterministic checks + verify outcome state machine (pass/repair/askback) | unit |
| `TestToDatetimeLocal` | dashboard_action_test.go | แปลงรูปแบบวันเวลาเป็น datetime-local ของ widget | unit |
| `TestShortTimePtrBangkok` | timezone_test.go | format เวลาโซนกรุงเทพ (UTC+7) ถูกต้อง | unit |
| `TestParseRetryAfter` | ratelimit_test.go | อ่านค่า wait จาก 429 (header/body) + เพดาน 30s | unit |
| `TestChatIntentResponseMarshalsIntentField` | controller_test.go | response JSON มี field `intent` ตามสเปก | unit |
| `TestRouterBakeOff` | router_eval_test.go | ยิง ClassifyIntent 2 models บน 32 เคส typo/intent — 20b: 28/32 (corrected scoring, ดู Q6) | live (ต้อง `GROQ_API_KEY`) |
| `TestVerifyAnswerLive` | verify_live_test.go | ยิง VerifyAnswer จริงบน known-wrong/known-right case | live |
| `TestDateEditRoutesToUpdate` | dateedit_live_test.go | ยิง main model จริง: relative-date edit ยัง route ไป `preview_update_widget` | live |
| `TestBakeOff` | eval_test.go | เทียบโมเดล main 23 เคสไทย วัด first-decision tool accuracy + token + latency | live (~20 นาที, รันเมื่อจะเปลี่ยนโมเดล) |

รันเฉพาะ unit: `cd backend; go test ./internal/modules/ai/` (live tests skip เองถ้าไม่มี key;
bake-off ต้องใส่ `-run TestRouterBakeOff -count=1` หรือ `-run TestBakeOff -count=1` ถึงจะรัน)

---

## Q14 — AI รู้ได้ไงว่าต้องใช้ tool ไหน? ส่ง description ของทุก tool ไปไหม?

**ใช่ — ส่งรายการ tool ไปให้เลือกทุก request** (ตามสเปก OpenAI tool calling): แต่ละ tool มี
ชื่อ + description ที่เขียนบอกชัดว่าใช้เมื่อไหร่ (`schema.go`) โมเดลอ่านแล้ว**เลือกเอง**ว่าเรียก
ตัวไหนด้วย args อะไร — ไม่มี logic ฝั่งเราไปเดา intent แทน

แต่ "ทุก tool" มีเงื่อนไข 3 ชั้นเพื่อประหยัดและปลอดภัย:

1. **กรองตามสิทธิ์ก่อนส่ง** (`buildGroqTools`) — viewer ไม่ได้รับ preview/write tools; role
   editor/admin ได้ครบ. *(อัปเดต: preview tools ตอนนี้แนบเสมอไม่ว่าจะมี dashboard เปิดอยู่หรือไม่
   — เดิมเคยกรองด้วย `hasContext` แต่เงื่อนไขนั้นถูกลบไปพร้อมกับ prompt แบบ 4 ชั้น)*
   `create_custom_dashboard` (เขียน DB จริง) **ไม่ถูกส่งให้โมเดลเลย** — frontend เรียกเองหลังผู้ใช้
   กด Confirm
2. **ส่งแบบ slim** (`toGroqToolSlim`) — tool ทั่วไปส่งแค่ชื่อ + description
   (คำใบ้ args ฝังในนั้น) ~50–80 token/ตัว; ส่ง JSON schema เต็มเฉพาะ tool ที่มีโครงสร้าง widget
   ซ้อน (เช่น `preview_add_widget`)
3. **กฎ TOOL SELECTION ใน `systemPromptUnified` ช่วยชี้ทาง** — บอก pattern ว่าคำถามแบบไหนคู่กับ
   tool ไหน (อ่านค่า → `show_metric`, สร้าง → `preview_dashboard`, …) เสริมกับ description

ความแม่นของการเลือกถูกวัดจริงด้วย bake-off (Q6) — first-decision accuracy ~100% บนเคสที่วัด

> โมเดลเห็น "เมนูเครื่องมือ" พร้อมคำอธิบายทุกครั้ง แล้วหยิบเอง — เราคุมแค่ว่าเมนูมีอะไรให้หยิบ

---

## Q15 — Groq/โมเดลจำประวัติแชตไหม (cache)? แล้วเราต้องส่ง history เองไหม

**ไม่จำ — และต้องส่งเองทุกครั้ง** Chat completions API เป็น **stateless**: แต่ละ request คือ
กระดาษเปล่า โมเดลรู้เฉพาะสิ่งที่แนบไปใน `messages[]` ของ request นั้น จบ request ก็ลืมหมด

ฝั่งเราจัดการอย่างนี้ (`buildGroqMessages` in `controller.go`):
- history เก็บใน DB ของเราเอง (`ai_messages`) แล้วแนบไปกับทุก request **แค่ 3 ข้อความล่าสุด** (ลดจาก 8)
  (~1.5 turn) — พอให้ context บริบท ไม่บวม token
- tool payload เก่า ๆ ไม่ถูก replay — คำสรุปของ assistant รอบก่อนจับใจความไว้แล้ว

**แล้ว prompt cache ที่พูดถึงคืออะไร?** คนละเรื่องกับความจำ — มันคือ Groq **cache ผลการ
ประมวลผล** ของ prompt ส่วนหัวที่ byte เหมือนเดิมทุกครั้ง (เราจงใจล็อก `systemPromptUnified`
ให้นิ่งเพื่อสิ่งนี้) ทำให้ request ถัดไป**เร็วขึ้น/ถูกลง** (50% input discount) แต่**ไม่ได้แปลว่า
โมเดลจำอะไรได้** — ข้อความทุกตัวอักษรยังต้องส่งไปเต็ม ๆ ทุกครั้งเหมือนเดิม

> สรุป: ความจำ = DB ของเรา + ส่ง 3 ข้อความล่าสุดไปเอง · cache = แค่ทางลัดการคำนวณ (50% discount) ไม่ใช่ความจำ
