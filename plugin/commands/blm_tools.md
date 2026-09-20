---
description: จัดการเครื่องมือข้างเคียง socraticode | obsidian | graphify | tree-sitter | embedding — status · get · install · start · stop · restart · gen-graph · socraticode ติดตั้งได้สองแบบ --local (บนเครื่องนี้) / --remote <host> (ชี้ server ที่มี Ollama+Qdrant แล้ว)
argument-hint: <action> [tool] [--local | --remote <host> --embedding-model m --embedding-dimensions n --embedding-context-length n | --docker]  เช่น "status" · "install socraticode --local" · "install socraticode --remote 192.168.1.50 --embedding-model bge-m3 --embedding-dimensions 1024 --embedding-context-length 8192" · "restart obsidian"
---

แยก "$ARGUMENTS" เป็น action (คำแรก) tool (คำสอง ถ้ามี) และ flag ที่เหลือ แล้วเรียก `blm_tools { action, tool, local?, remote?, embeddingModel?, embeddingDimensions?, embeddingContextLength?, embeddingQueryPrefix?, embeddingDocumentPrefix?, ollamaUrl?, qdrantUrl?, docker? }` พิมพ์ `terminal` ที่ได้ตามตัวอักษร
- `--local` → `local:true` · `--remote <host>` → `remote:"<host>"` · `--remote` เดี่ยว ๆ → `remote:"config"` (ใช้ค่าที่จำไว้ใน `.claude/blm.json`) · ไม่มี flag → blm เลือกเอง (มี section socraticode ใน config = remote ไม่มี = local)
- ก่อน `install socraticode --remote` ครั้งแรก ถ้า user ไม่ได้ให้ host/model มา ให้ถามเป็นประโยคเดียว ไม่ต้องเดา
ถ้า tool คืนวิธี/คำสั่งให้ผู้ใช้รันเอง ให้พิมพ์ตามนั้น ห้ามรันแทนโดยไม่ถาม (ผู้ใช้เรียกตรงได้ด้วย `blm tools …`)
