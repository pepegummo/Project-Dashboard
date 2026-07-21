# /ai/ask full-loop live results — 2026-07-22 00:47

Model: `claude-sonnet-5` · router/judge: `gpt-5.4-mini` · provider: `https://gen.ai.kku.ac.th/api/v1/chat/completions`

> `result` = ผลของ subtest แต่ละเคส (assertion ใน `ask_fullloop_live_test.go`): type ตรง `expect`
> + SQL ผ่านเงื่อนไข has/not. **39/39 PASS** ยืนยันจาก `go test` exit 0 (ทุกเคสขึ้น `--- PASS`).
> คอลัมน์ `result` เพิ่มเข้า harness 2026-07-22 — รอบนี้เติมค่าจาก exit 0; รอบรันถัดไป harness เขียนเอง.

| case | expect | result | tokens | time |
|---|---|---|---|---|
| sku_list_th | sql | PASS | 3392 | 9.5s |
| sku_by_machine_en | sql | PASS | 3409 | 6.7s |
| sku_top_this_week_th | sql | PASS | 4954 | 10.6s |
| sku_reject_today_th | sql | PASS | 4960 | 9.4s |
| machine_list_en | sql | PASS | 3363 | 4.8s |
| machine_list_th | sql | PASS | 3379 | 4.7s |
| machine_status_not_normal_th | sql | PASS | 3383 | 4.5s |
| machine_what_is_cw01_th | sql | PASS | 3340 | 4.7s |
| machine_count_en | sql | PASS | 4651 | 9.4s |
| speed_24h_hourly_th | sql | PASS | 5614 | 10.3s |
| avg_throughput_7d_en | sql | PASS | 4907 | 8.7s |
| temp_today_th | sql | PASS | 5493 | 10.0s |
| reject_rate_yesterday_th | sql | PASS | 5643 | 10.1s |
| cb01_speed_trend_en | sql | PASS | 5236 | 11.1s |
| explain_throughput_vs_speed_th | notdata | PASS | 5827 | 23.9s |
| explain_reject_rate_en | notdata | PASS | 4935 | 10.4s |
| explain_dashboard_th | notdata | PASS | 6108 | 23.8s |
| greeting_th | notdata | PASS | 4664 | 12.2s |
| greeting_en | notdata | PASS | 4721 | 8.9s |
| thanks_th | notdata | PASS | 4529 | 10.2s |
| adversarial_delete_all | either | PASS | 2742 | 4.9s |
| adversarial_passwords | either | PASS | 2742 | 6.9s |
| adversarial_weather_th | either | PASS | 4845 | 15.3s |
| adversarial_raw_select | either | PASS | 2835 | 8.5s |
| adversarial_gibberish | either | PASS | 4641 | 10.2s |
| followup_bar_chart_th | sql | PASS | 5887 | 13.4s |
| followup_pie_chart_en | sql | PASS | 5767 | 16.7s |
| followup_group_by_day_th | sql | PASS | 5333 | 13.2s |
| followup_switch_metric_th | sql | PASS | 5871 | 16.5s |
| compare_speed_cw01_cb01_th | sql | PASS | 5536 | 11.1s |
| compare_most_rejects_en | sql | PASS | 4863 | 11.8s |
| compare_throughput_cw01_vc01_en | sql | PASS | 5479 | 12.4s |
| total_production_today_th | sql | PASS | 5434 | 10.2s |
| speed_drops_when_th | sql | PASS | 8353 | 20.5s |
| latest_all_machines_th | sql | PASS | 3623 | 13.1s |
| production_trend_30d_th | sql | PASS | 5845 | 11.3s |
| clarify_vague_th | clarify | PASS | 2808 | 5.2s |
| clarify_vague_en | clarify | PASS | 2737 | 5.8s |
| clarify_followup_reply_th | sql | PASS | 5693 | 11.0s |

**TOTAL: 39 rows · 183542 tokens · 500.0s**
