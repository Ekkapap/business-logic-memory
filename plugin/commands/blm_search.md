---
description: ค้นหาความหมายในโค้ดผ่าน index ของ SocratiCode (ไทย/อังกฤษ) โดย blm เอง — คะแนน RRF เหมือน codebase_search, ตัด .md ของ blm/.agentsroom ออกให้, --exclude md ตัดทุก .md
argument-hint: "<คำถาม>" [--lang typescript] [--file <path>] [--exclude md] [--limit N] [--full]  ·  --get <search-id> --id 1,3
---

แยก "$ARGUMENTS": ข้อความในเครื่องหมายคำพูด (หรือคำทั้งหมดที่ไม่ใช่ flag) = query · `--lang x` → lang · `--file p` → file · `--exclude md` → excludeMd:true · `--limit N` → limit · `--brief` → brief:true
เรียก `blm_search { query, lang?, file?, excludeMd?, limit?, full? }` แล้วพิมพ์ `terminal` ตามตัวอักษร — ผลเป็นสารบัญ 1 บรรทัด/ผล + `search-id`
- `--get <search-id> --id 1,3` → `blm_search { get:"<search-id>", ids:"1,3" }` เปิดเฉพาะ chunk ที่เลือก (บรรทัดจริงจากไฟล์)
- คะแนน: 1.0 = อันดับ 1 ทั้งฝั่งความหมายและคำตรง · 0.5 = อันดับ 1 ฝั่งเดียว — วัดด้วยว่าไฟล์ที่ถูกอยู่ top-3 ไหม ไม่ใช่ตัวเลข
- ผลว่าง/ error "stack" → บอกให้เจ้าของดู `blm tools status socraticode` ห้ามวน retry
