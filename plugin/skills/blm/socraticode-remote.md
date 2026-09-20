ไทย · **[English](socraticode-remote-EN.md)**

# SocratiCode + LLM + Qdrant — ติดตั้งแบบ local / remote และต่อสาย Claude Code

SocratiCode = plugin ของ Claude Code (`giancarloerra/socraticode`) ที่ index โค้ดเป็น chunk ตามขอบ function (tree-sitter ผ่าน ast-grep) → embed ด้วย **Ollama** → เก็บใน **Qdrant** และสร้าง symbol/call graph · การค้น**ต้องมีทั้งสองบริการออนไลน์** (คำถามถูก embed ก่อนเสมอ ไม่มี keyword-only fallback) · blm ใช้ index นี้ผ่าน `blm_search` / `blm tools status` / `blm graph` โดยไม่ผ่าน MCP ของมัน

## เลือกแบบ

| | `--local` (เดิม) | `--remote <host>` (ใหม่ 2026-09-20) |
|---|---|---|
| Ollama + Qdrant | บนเครื่องนี้ (mac: qdrant binary + launchd, brew ollama · linux: binary + ollama.com · windows: `--docker` เท่านั้น) | บนเครื่องอื่นที่ติดตั้งไว้แล้ว (GPU box, server ใน VPN) — เครื่องนี้ไม่ลงอะไร |
| โมเดล | `nomic-embed-text` 768 มิติ (ค่าเริ่มต้นของ socraticode) | ระบุได้ เช่น `bge-m3` 1024 มิติ (รองรับไทย, context 8192) |
| RAM เครื่อง dev | Qdrant ~900 MB ค้าง + Ollama โหลดโมเดลตอนใช้ | 0 |
| จำค่าไว้ที่ | `~/.claude/settings.json` env | `.claude/blm.json` section `socraticode` + env |

```sh
blm tools install socraticode --local
blm tools install socraticode --remote 192.168.1.50 --embedding-model bge-m3 --embedding-dimensions 1024 --embedding-context-length 8192
blm tools install socraticode --remote        # เครื่องที่สอง หลัง git pull: ใช้ค่าจาก .claude/blm.json
blm tools install socraticode                 # ไม่ใส่โหมด: ไม่มี remote จำไว้ = --local · มี = หยุดแล้วบอกให้เลือก
blm tools status socraticode                  # running · model=… · remote=… · ollama=up · qdrant=up
blm tools update socraticode                  # plugin ล่าสุดจาก marketplace (MCP ของมันรัน npx socraticode@latest อยู่แล้ว)
```

ทั้งสองแบบทำให้ท้ายคำสั่ง: เขียน env 9 ตัว (`OLLAMA_MODE/URL`, `QDRANT_MODE/URL`, `EMBEDDING_MODEL/DIMENSIONS/CONTEXT_LENGTH/QUERY_PREFIX/DOCUMENT_PREFIX`) ลง `~/.claude/settings.json` · ต่อ hooks `blm hook session-start|prompt|grep-nudge` และบรรทัด socraticode ใน `~/.claude/statusline-command.sh` (ดู [hooks-statusline.md](hooks-statusline.md)) · ถอดเฉพาะของ blm เอง ของคนอื่น (เช่น graft) อยู่ครบและมี note บอกวิธีถอน · จบด้วย `/plugin` → reconnect socraticode → `codebase_health` ต้องเห็น external ทั้งคู่ → `codebase_index` ถ้า collection ของโปรเจ็คยังไม่มีบน Qdrant นั้น

## โมเดล embedding

- query กับ index **ต้องโมเดลเดียวกัน** (space เดียวกัน) เปลี่ยนโมเดล = index ใหม่ทั้ง repo
- `nomic-embed-text` อังกฤษเป็นหลัก prefix `search_query: ` / `search_document: ` · `bge-m3` รองรับไทย ไม่ใช้ prefix (ว่างทั้งคู่) context 8192 · `multilingual-e5-large` context แค่ 512 (chunk 2000 ตัวอักษรโดนตัด) จึงไม่แนะนำ
- โมเดลที่ socraticode ไม่รู้จัก (bge-m3) ต้องระบุ `--embedding-dimensions` + `--embedding-context-length` เอง (`MODEL_CONTEXT_LENGTHS` ใน embedding-config.ts ไม่มี)
- `blm search` อ่านโมเดล/prefix จาก `.claude/blm.json` ก่อน → env → `~/.claude/settings.json` → 127.0.0.1

## ติดตั้งฝั่ง server (remote) — ตัวอย่าง Windows Server + GPU

ทำใน PowerShell as Administrator ทีละบล็อก · `<host-ip>` = IP ที่เครื่อง dev ถึงได้ (VPN/tailnet) · ผูกบริการกับ IP นั้น ไม่ใช่ `0.0.0.0`

1. NVIDIA driver → `nvidia-smi` เห็นการ์ด
2. Ollama: `irm https://ollama.com/install.ps1 | iex` (ฟ้อง signature ครั้งแรกให้รันซ้ำ หรือโหลด `OllamaSetup.exe` รัน `/VERYSILENT`) → `ollama pull bge-m3` → ทดสอบ `POST /api/embed` ได้ 1024 ตัว และ `ollama ps` = `100% GPU`
3. **รันเป็น service ไม่ใช่ tray app** (`ollama app.exe` = GUI) ด้วย NSSM:
   ```powershell
   C:\nssm\nssm.exe install Ollama "$env:LOCALAPPDATA\Programs\Ollama\ollama.exe" serve
   $m = "$env:USERPROFILE\.ollama\models"
   C:\nssm\nssm.exe set Ollama AppEnvironmentExtra "OLLAMA_HOST=<host-ip>:11434" "OLLAMA_KEEP_ALIVE=24h" "OLLAMA_CONTEXT_LENGTH=8192" "OLLAMA_MODELS=$m"
   C:\nssm\nssm.exe start Ollama
   ```
   `OLLAMA_MODELS` ต้องชี้โฟลเดอร์ที่ pull ไว้ (service เป็น SYSTEM มองคนละ home)
4. Qdrant: `qdrant-x86_64-pc-windows-msvc.zip` จาก GitHub release → `C:\qdrant` · `config\config.yaml`: `service.host: <host-ip>`, `http_port: 6333`, `storage.storage_path: C:\qdrant\storage` → NSSM service `Qdrant` (`qdrant.exe --config-path …`) → `GET /collections` ตอบ
5. Firewall: inbound TCP 11434 และ 6333 เฉพาะช่วง IP ของ VPN (`-RemoteAddress 100.64.0.0/10` สำหรับ tailnet) ไม่เปิดสาธารณะ — ทั้งสองบริการไม่มี auth (`QDRANT_API_KEY`/`OLLAMA_API_KEY` เป็นชั้นสอง)
6. VPN client บน server ถ้าใช้ headscale/tailscale: MSI เงียบ `msiexec /i tailscale-setup-<ver>-amd64.msi /quiet TS_NOLAUNCH=1 TS_UNATTENDEDMODE=always TS_LOGINURL=<control-server>` แล้ว `tailscale up --login-server <control-server> --authkey <key>`
7. จากเครื่อง dev: `curl http://<host-ip>:11434/api/tags` และ `curl http://<host-ip>:6333/` ตอบ → `blm tools install socraticode --remote <host-ip> …`

Linux/macOS ฝั่ง server: `ollama serve` ด้วย `OLLAMA_HOST=<host-ip>:11434` เป็น systemd/launchd + qdrant binary จาก release (แบบเดียวกับที่ `blm tools install socraticode --local` ทำในเครื่อง) — คำสั่ง ssh ครั้งเดียวสำหรับสามระบบยังไม่ทำ

## กับดักที่เจอจริง

- เปลี่ยน env ใน `~/.claude/settings.json` แล้ว MCP ยังใช้ค่าเก่า — อ่านตอนเริ่มเท่านั้น ต้อง `/plugin` reconnect
- `blm search`/statusline ใน sandbox ของ agent: `EPERM`/x509 ต่อ server ไม่ได้ทั้งที่ curl ผ่าน — proxy ของ sandbox; ผลจริงดูจากเทอร์มินัลเจ้าของ
- ปิด Ollama = ค้นไม่ได้ ไม่ใช่แค่ index ไม่ได้ · ปิด Qdrant = index/graph/hash หายจากมุมมองทั้งหมด (อยู่ใน Qdrant ทั้งหมด)
- ลืม `--remote` แล้วสั่ง `install socraticode` เฉย ๆ บนโปรเจ็คที่จำ remote ไว้ → blm หยุดพร้อมคำแนะนำ ไม่ติดตั้ง local ทับ
