---
description: อัปเดต blm เอง — binary (dev checkout git pull+build / global GitHub release) + plugin (claude plugin update blm@blm) แล้วบอกให้ reconnect
argument-hint: [--check] [--binary | --plugin]
---

แยก "$ARGUMENTS": `--check` → check:true · `--binary` → binary:true · `--plugin` → plugin:true แล้วเรียก `blm_selfupdate {…}` พิมพ์ `terminal` ตามตัวอักษร
- `reconnect:true` → บอกเจ้าของบรรทัดเดียว: รัน `/reload-plugins` (session ยังถือ plugin เก่า — commands/skills/เวอร์ชันไม่เปลี่ยนจนกว่าจะ reload) แล้ว `/mcp reconnect plugin:blm:blm` (MCP ที่รันอยู่ยังเป็น binary เก่า)
- ขั้นไหน ✘ ให้รายงานตามนั้น ห้าม retry เอง (git pull ล้ม = มี local change ให้เจ้าของจัดการ)
