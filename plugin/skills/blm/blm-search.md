ไทย · **[English](blm-search-EN.md)**

# blm_search — ค้นโค้ดด้วยความหมาย แล้วอ่านผลให้เป็น

## คืออะไร

`blm_search` (CLI `blm search`) ค้นผ่าน index ของ SocratiCode **โดยไม่ผ่าน MCP ของมัน**: embed คำถามด้วยโมเดล/prefix ตัวเดียวกับที่ index ใช้ (อ่านจาก `.claude/blm.json` section socraticode → env → `~/.claude/settings.json`) แล้ว Qdrant hybrid = ความหมาย (dense) + คำตรง (BM25) รวมด้วย RRF — วิธีเดียวกับ `codebase_search` ทุกประการ จึงได้ผลและคะแนนเท่ากัน · ต่างที่ blm **ทำเพิ่ม** 3 อย่างซึ่ง socraticode ไม่มี: สารบัญแทนก้อนโค้ด 10 ก้อน, กรอง `.md` ที่ไม่ใช่โค้ด, และคอลัมน์ **origin** (เชื่อได้แค่ไหน)

ถามเป็นประโยค ไทยหรืออังกฤษ ไม่ต้องรู้ชื่อ symbol: `"เข้าสู่ระบบด้วย LINE ต้องกรอก OTP อีกไหม"` · `"cookie consent policy version localStorage"`

## ผล = สารบัญ 2 บรรทัดต่อผล

```
search "ผู้ใช้ต้องยืนยันอีเมลก่อนเข้าสู่ระบบไหม" — 4 hits (model bge-m3 · score: RRF, 1.0 = top of both semantic and keyword)
  id   score  path:lines                                     origin
  #1   0.50   src/lib/auth/verify-email.ts:L20-L58           code 0.62
       [typescript]  export type VerifyIntent = "signup" | "login";
  #2   0.50   src/app/api/auth/login/route.ts:L30-L64        both ✓ 0.74
       [typescript]  export async function POST(request: Request): Promise<Response> {
  #3   0.45   src/features/Login/index.tsx:L40-L85           comment ~ 0.67
       [typescript]  export function LoginForm({
search-id: 1n7wzapf   open a hit: blm search --get 1n7wzapf --id 1,3
```

- บรรทัด 1: `#id` · `score` · `path:Lstart-Lend` · `origin` · บรรทัด 2: `[lang]` + preview (บรรทัดแรกที่มีเนื้อในส่วนโค้ด ข้าม import/คอมเมนต์/`}`)
- **ไม่แบกเนื้อ chunk** — response ของ MCP เบา เปิดอ่านเฉพาะที่เลือกด้วย `get`

## อ่านต่อเฉพาะที่เลือก — `get`

- `blm_search {get:"1n7wzapf", ids:"1,3", context:5}` / `blm search --get 1n7wzapf --id 1,3 --context 5` → **บรรทัดจริงจากไฟล์** (start..end ± context พิมพ์เลขบรรทัด) ไม่ใช่ที่แคชไว้ ไฟล์หายจึงค่อยใช้เนื้อที่เก็บ
- ไฟล์ผลอยู่ `<store>/tmp/search-result-<id>.json` (ที่เดียวกับ `blm grep`) เก็บเนื้อ chunk + origin ทั้งหมด
- `full:true` / `--full` = แนบเนื้อทุกก้อนในครั้งเดียว (แบบ codebase_search เดิม) ใช้เมื่อผลน้อยและต้องดูทั้งหมด · `--brief` = pointer ล้วน ไม่บันทึกไฟล์ (hook ใช้)

## score — RRF ของ Qdrant (สเกลเดียวกับ codebase_search)

`1/(2+อันดับ)` ต่อฝั่ง บวกสองฝั่ง: **1.0** = อันดับ 1 ทั้งความหมายและคำตรง · **0.5** = อันดับ 1 ฝั่งเดียว อีกฝั่งไม่ติด · 0.83 = ½+⅓ · 0.45 = ¼+⅕ · ต่ำกว่า `minScore` 0.10 ถูกตัด · **วัดด้วยอันดับ** (ไฟล์ที่ถูกอยู่ top-3 ไหม) ไม่ใช่ตัวเลข — เจ้าของกำหนด · ภาษาไทยที่เขียนติดกันเป็น token เดียวสำหรับ BM25 จึงได้คะแนนจากฝั่งความหมายเป็นหลัก

## origin — เชื่อผลนี้ได้แค่ไหน (blm วิเคราะห์เอง)

chunk ของ socraticode ตัดตามขอบ function/class จึงมี**คอมเมนต์หัวฟังก์ชันกับโค้ดอยู่ก้อนเดียวกัน** blm แยกสองส่วน แล้ว embed ทั้งคู่ด้วยโมเดลเดิม (1 request ต่อการค้น) → เทียบกับคำถาม (`codeSim` / `commentSim`) และเทียบกันเอง (`agree` = cosine(code, comment) = ตัวเลขท้ายป้าย)

| ป้าย | หมายถึง | ทำอะไรต่อ |
|---|---|---|
| `code` | คำถามชน**โค้ด**โดยตรง (หรือก้อนไม่มีคอมเมนต์) | เชื่อได้ |
| `both ✓ 0.74` | ชนทั้งโค้ดและคอมเมนต์ และสองส่วนไปทางเดียวกัน (agree ≥ 0.60) | เชื่อได้เต็มที่ |
| `both 0.55` | ชนทั้งคู่ แต่คอมเมนต์กับโค้ดไม่ค่อยตรงกัน | อ่านโค้ด |
| `comment ~ 0.67` | ชน**แต่คอมเมนต์** แต่คอมเมนต์เล่าเรื่องเดียวกับโค้ด | อ่านโค้ดยืนยันหนึ่งครั้ง |
| `comment ⚠ 0.52` | ชนแต่คอมเมนต์ และโค้ดในก้อนเดียวกันพูดอีกเรื่อง | **คอมเมนต์อาจเก่า** ห้ามเชื่อคอมเมนต์ อ่านโค้ด · ถ้าโค้ดขัดกับคอมเมนต์จริง แจ้งเจ้าของว่ามีคอมเมนต์สวนทาง ไม่แก้เงียบ |
| `doc` | ไฟล์ที่ไม่ใช่โค้ดที่ tree-sitter รู้จัก (.md .sql .json .yaml .css .html …) ไม่มีโค้ดให้เทียบ | อ่านตามที่มันเป็น: spec/plan/schema |

- ก้อนที่เป็นคอมเมนต์ล้วน (header block) = `comment ⚠` เสมอ · ก้อนที่เริ่มกลาง `/* … */` (socraticode ตัดกลางบล็อกได้) แยกถูกแล้ว
- เกณฑ์ `trustMargin` 0.08 (ต่างกันเกินนี้ = ชนฝั่งเดียว) และ `trustAgree` 0.60 เป็นค่าตั้งต้น ปรับใน `internal/blm/trust.go` เมื่อวัดจริงแล้ว · `noTrust:true` / `--no-trust` ข้าม (ประหยัด 1 request)

## ไฟล์อะไรอยู่ในผล — `.md` `.sql` และ `db/schema/`

- **`.md` ใต้ store ของ blm และ `.agentsroom/`** (โน้ต, mirror memory) ถูกตัดออก**เสมอ** — ความจำไม่ใช่โค้ด ค้นความจำใช้ `blm` / `blm grep` · `.md` อื่น (`.planning/`, `design/`, README) ยังอยู่ในผลเป็น `doc` เพราะ spec ต้องหาเจอ · `excludeMd:true` / `--exclude md` ตัด `.md` ทั้งโปรเจ็ค · `lang:"typescript"` (= .ts+.tsx) / `go` / `markdown` เอาทีละภาษา
- **SQL: index เฉพาะ `db/schema/<table>.sql`** — snapshot ของ schema ปัจจุบันจาก DEV (สคริปต์ของโปรเจ็คเป็นคน gen หลัง migrate ทุกครั้ง — ชื่อคำสั่งแล้วแต่โปรเจ็ค เช่น `db:schema`) ตารางละไฟล์: คอลัมน์/default/null, constraint, index, trigger, `comment on column` · migration (`db/postgres/*.sql`) และ dump **ไม่ index**: migration คือประวัติ หลายไฟล์เล่าตารางเดียวกันคนละเวลา search แยกไม่ออกว่าอันไหนยังจริง · dump คือข้อมูล (PII) · `.socraticodeignore`: `*.sql` ยกเว้น `!db/schema/*.sql`
- กติกา dev ⊇ prod: snapshot จาก DEV เท่านั้น · สคริปต์เดียวกันแบบ `--prod` (อ่านอย่างเดียว) ใช้เทียบว่าอะไรรอ deploy และเตือนถ้า prod มีสิ่งที่ dev ไม่มี

## ลำดับที่ควรทำ

1. `blm_search {query}` → ดู origin ของ top-3
2. `get` เฉพาะที่เชื่อได้หรือต้องยืนยัน (ไม่ `full:true` ทั้ง 10 ถ้ายังไม่รู้ว่าอันไหนใช่)
3. ค่อยแก้ · Grep/Glob ใช้เมื่อรู้ชื่อ symbol แน่นอนแล้ว (hook `grep-nudge` จะเตือนถ้า grep ทั้งที่มี index)
4. ผลว่าง / error บอกว่า `blm tools status socraticode` = stack ปิดหรือยังไม่ index → บอกเจ้าของ ไม่ retry
