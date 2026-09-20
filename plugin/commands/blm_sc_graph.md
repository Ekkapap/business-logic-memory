---
description: สร้าง graph ฉบับของ blm (graph.json + graph.html) จาก symbol graph ของ SocratiCode — แม่นกว่า ast-grep/regex เพราะใช้ index
argument-hint: [ไม่มี]
---

เรียก `blm_sc_graph {}` แล้วพิมพ์ `result` ตามตัวอักษร (nodes · edges · clusters · path ของ graph.json/graph.html) · ต้องมี index ก่อน (`/blm_sc_index`) ไม่มี = บอกเจ้าของให้รันก่อน · จากนั้น `blm_graph {query}` และ `blm_update` ใช้กราฟนี้
