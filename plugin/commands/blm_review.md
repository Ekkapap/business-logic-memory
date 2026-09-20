---
description: เปิดหน้ารีวิว update (รายการ main topic) ไฟล์เดิมในเบราว์เซอร์ แล้วเข้าโหมด live รอเจ้าของ — หน้านี้ไม่มีวันจบ ส่วนที่จบคือหน้าย่อยของแต่ละ topic
argument-hint: [ว่าง = /r/update ไฟล์เดิม | <id> = รีวิวนั้น]
---

ทำตามลำดับ ไม่ต้องถาม:

0. มี waiter พื้นหลัง (`blm update --wait`) รันค้างจากรอบก่อน → **หยุดตัวเดิมก่อน** (TaskStop ตาม id ที่ได้ตอนสั่ง) แล้วค่อยเริ่มใหม่ — ห้ามมีสองตัวพร้อมกัน

1. `blm_update {html:true, open:true}` → ได้ `url`/`id` และ blm เปิดเบราว์เซอร์ของเจ้าของที่ **URL คงที่** `/r/update` ให้เอง (ต่อรีวิวเดิมที่ยังเปิดอยู่ ไม่สร้างไฟล์ใหม่) — **ห้าม** `open` จาก Bash (sandbox ตัด LaunchServices) · "$ARGUMENTS" มี id → `blm_update {from:"<id>", html:true}`
2. blm ลอง `open` → Chrome ตรง ๆ → Safari ให้เองตามลำดับ · `next` บอก "browser opened" หรือ "could not open" — อย่างหลังให้บอก URL เจ้าของเปิดเอง (ไม่ต้องไปกด Chrome extension เอง)
3. ตอบเจ้าของบรรทัดเดียว: URL + id ของไฟล์ที่เปิด แล้วเริ่ม waiter พื้นหลัง (Bash `run_in_background`): `cd <project> && blm update --wait --from update --timeout 540` — คุณถูกปลุกเมื่อเจ้าของติ๊ก/แชต/submit หรือครบเวลา (ครบเวลา = เริ่ม waiter ใหม่ ไม่ต้องรายงาน)
4. เมื่อถูกปลุก: `blm_update {from:"update"}` → ทำ `todo` (ตอบแชตด้วย `proposal.answers`, เสนอหัวข้อด้วย `proposal.topics`) แล้วกลับข้อ 3

ไฟล์ของหน้านี้แตะได้ผ่าน `blm_review {action: get|list|read|write|delete, path}` เท่านั้น (Bash ห้ามเขียน store) · `delete` ต้องถามเจ้าของแล้วส่ง `confirm:<ชื่อไฟล์>` · รายละเอียดการรีวิว/proposal ดู /blm_update
