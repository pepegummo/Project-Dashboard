# Router eval (classify_intent) live results — 2026-07-22 00:39

Model: `claude-sonnet-5` · router/judge: `gpt-5.4-mini` · provider: `https://gen.ai.kku.ac.th/api/v1/chat/completions`

| model | label | message | want | got | pass | tokens | latency |
|---|---|---|---|---|---|---|---|
| gpt-5.4-mini | greeting | สวัสดีครับ | chat | chat | PASS | 1023 | 1.61s |
| gpt-5.4-mini | read-speed | speed ของ CW-01 เท่าไหร่ | read_metric | (declined) | FAIL | 0 | 0.00s |
| gpt-5.4-mini | english-read | what's the speed of CW-01 | read_metric | (declined) | FAIL | 0 | 0.00s |
| gpt-5.4-mini | all-metrics | ขอดูทุกค่าของ CW-01 หน่อย | read_metric | chat | FAIL | 1033 | 1.32s |
| gpt-5.4-mini | detail-analytical-focused | @Speed Trend แนวโน้มเป็นยังไง วิเคราะห์หน่อย | read_agg | read_agg | PASS | 1056 | 1.61s |
| gpt-5.4-mini | change-preview-edit | เปลี่ยน metric เป็น temperature | edit_widget | edit_widget | PASS | 1053 | 1.83s |
| gpt-5.4-mini | add-preview-widget | เพิ่ม widget อุณหภูมิ CW-01 ด้วย | edit_widget | edit_widget | PASS | 1062 | 1.89s |
| gpt-5.4-mini | delete-preview-widget | ลบ widget Trend ออก | edit_widget | edit_widget | PASS | 1051 | 1.28s |
| gpt-5.4-mini | add-to-active-dashboard | เพิ่ม widget speed ของ CW-01 ด้วย | edit_widget | edit_widget | PASS | 1059 | 1.37s |
| gpt-5.4-mini | remove-from-active-dashboard | ลบ widget Speed Gauge ออก | edit_widget | edit_widget | PASS | 1053 | 1.45s |
| gpt-5.4-mini | add-custom-chart | เพิ่มกราฟรวม speed กับ throughput ของ CW-01 | edit_widget|compare | compare | PASS | 1064 | 1.35s |
| gpt-5.4-mini | create | สร้าง dashboard ของ CW-01 ให้หน่อย | create_dashboard | create_dashboard | PASS | 1032 | 1.52s |
| gpt-5.4-mini | typo-create | ส้างแดชบอด cw-01 ให้หน่อย | create_dashboard | create_dashboard | PASS | 1035 | 1.36s |
| gpt-5.4-mini | list-dashboards | มี dashboard อะไรบ้าง | chat | chat | PASS | 1025 | 2.20s |
| gpt-5.4-mini | list-skus | CW-01 มี SKU อะไรบ้าง | chat | production | FAIL | 1032 | 1.29s |
| gpt-5.4-mini | active-alerts | ตอนนี้มีแจ้งเตือนอะไรบ้าง | alerts | alerts | PASS | 1028 | 3.33s |
| gpt-5.4-mini | alert-rule-trap | ตั้ง alert ให้หน่อย ถ้า speed ของ CW-01 เกิน 100 ให้เตือน | alerts | alerts | PASS | 1043 | 1.44s |
| gpt-5.4-mini | trap-action-but-read | ถ้าฉันอยากสร้าง dashboard แล้วตอนนี้มีเครื่องอะไรบ้าง | chat | chat | PASS | 1035 | 4.52s |
| gpt-5.4-mini | ambiguous-fix | แก้ให้หน่อย | not-ok (ambiguous, declining is correct) | (declined) | PASS | 1023 | 2.59s |
| gpt-5.4-mini | read-no-machine | speed เท่าไหร่ | read_metric | read_metric | PASS | 1027 | 1.34s |
| gpt-5.4-mini | focused-gauge-analytical | แนวโน้มเป็นยังไง วิเคราะห์หน่อย | read_agg | read_agg | PASS | 1052 | 5.08s |
| gpt-5.4-mini | focused-count-now | ตอนนี้เท่าไหร่ | production | read_metric | FAIL | 1051 | 1.59s |
| gpt-5.4-mini | focused-alarm-panel | ตอนนี้เป็นยังไงบ้าง | alerts | alerts | PASS | 1045 | 1.43s |
| gpt-5.4-mini | compound-read-write | เพิ่ม widget อุณหภูมิ CW-01 ด้วย แต่ก่อนอื่นบอกหน่อยตอนนี้ speed เท่าไหร่ | read_metric|edit_widget | edit_widget | PASS | 1078 | 1.49s |
| gpt-5.4-mini | typo-create-th | ส้างแดชบอด cw-01 | create_dashboard | create_dashboard | PASS | 1032 | 1.43s |
| gpt-5.4-mini | typo-create-en | creat dashbord for cw-01 | create_dashboard | create_dashboard | PASS | 1031 | 1.35s |
| gpt-5.4-mini | synonym-read | how fast is CW-01 running | read_metric | read_metric | PASS | 1032 | 1.32s |
| gpt-5.4-mini | bucket-edit | อยากดู 22 นาที | edit_widget | edit_widget | PASS | 1070 | 1.20s |
| gpt-5.4-mini | relative-date-edit | ดูของเมื่อวาน | edit_widget | edit_widget | PASS | 1055 | 1.28s |
| gpt-5.4-mini | agg-production-read | ผลิตกี่ชิ้นใน 22 นาที | production | production | PASS | 1033 | 2.12s |
| gpt-5.4-mini | compare-metrics | เปรียบเทียบ speed กับ temp | compare | compare | PASS | 1030 | 1.53s |
| gpt-5.4-mini | greeting-short | สวัสดี | chat | chat | PASS | 1022 | 1.38s |

**TOTAL: 32 rows · 31265 tokens · 130.5s**
