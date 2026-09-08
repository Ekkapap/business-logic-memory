---
description: จัดการเครื่องมือข้างเคียง socraticode | obsidian | graphify — status · get · install · start · stop · restart · gen-graph
argument-hint: <action> [tool]  เช่น "status" · "install socraticode" · "restart obsidian"
---

แยก "$ARGUMENTS" เป็น action (คำแรก) และ tool (คำสอง ถ้ามี) แล้วเรียก `blm_tools { action, tool }` พิมพ์ `terminal` ที่ได้ตามตัวอักษร
ถ้า tool คืนวิธี/คำสั่งให้ผู้ใช้รันเอง ให้พิมพ์ตามนั้น ห้ามรันแทนโดยไม่ถาม (ผู้ใช้เรียกตรงได้ด้วย `blm tools …`)
