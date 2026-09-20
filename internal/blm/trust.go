package blm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// ความน่าเชื่อถือของผลค้น (เจ้าของ 2026-09-20): hit ที่ชนเพราะ **โค้ด** เชื่อได้ · ชนเพราะ **คอมเมนต์** อย่างเดียว
// เชื่อน้อยลง (คอมเมนต์อาจเล่าสิ่งที่โค้ดไม่ทำแล้ว) · ชนทั้งคู่และไปทางเดียวกัน = เชื่อได้เต็มที่ — socraticode ไม่มีชั้นนี้
//
// chunk ของ socraticode ตัดตามขอบ function/class จึงมีคอมเมนต์หัวฟังก์ชันกับโค้ดอยู่ก้อนเดียวกัน → เทียบ "ข้างในก้อน":
//  1. แยก chunk เป็น 2 ส่วน: ข้อความคอมเมนต์ / โค้ดล้วน (parser ตามภาษาแบบบรรทัด)
//  2. embed ทั้งสองส่วนด้วยโมเดลเดียวกับ index (document prefix) → cosine กับ vector ของคำถาม = codeSim / commentSim
//     และ cosine ระหว่างสองส่วนกันเอง = agree (คอมเมนต์เล่าเรื่องเดียวกับโค้ดไหม)
//  3. ป้าย: code · both ✓ · both · comment ~ · comment ⚠ (ดู trustLabel)
// ponytail: threshold เป็นค่าคงที่ตั้งจากผลจริงรอบแรก — ปรับที่ trustMargin/trustAgree เมื่อทดสอบความน่าเชื่อถือกับเจ้าของแล้ว
// ponytail: ไม่มี cache ของ embedding ต่อ chunk — 1 request/ค้น (2 ข้อความ × N hit) บน GPU < 0.5 วิ; ใส่ cache ตาม sha256(chunk) ถ้าช้า

const (
	trustMargin = 0.08 // |commentSim - codeSim| ≤ margin = ชนทั้งคู่ (both)
	trustAgree  = 0.60 // cosine(code, comment) ≥ นี้ = คอมเมนต์กับโค้ดไปทางเดียวกัน
)

// Trust ผลวิเคราะห์ต่อ hit — เก็บลงไฟล์ผลด้วย
type Trust struct {
	// Origin: code | both | comment · Label: ข้อความในสารบัญ (code · both ✓ · both · comment ~ · comment ⚠)
	Origin     string  `json:"origin"`
	Label      string  `json:"label"`
	CodeSim    float64 `json:"codeSim"`
	CommentSim float64 `json:"commentSim"`
	// Agree = cosine(code, comment) · -1 = ไม่มีคอมเมนต์ในก้อน
	Agree float64 `json:"agree"`
}

// splitComments แยกบรรทัดคอมเมนต์ออกจากโค้ดตาม label ภาษาของ socraticode
// ponytail: ตัดตามบรรทัด ไม่ parse string literal — `//` ใน URL (`://`) ไม่นับ, `/* */` ข้ามบรรทัดได้; พอสำหรับป้าย ไม่ใช่ tokenizer
func splitComments(content, language string) (code, comment string) {
	line, blockOpen, blockClose := "//", "/*", "*/"
	switch language {
	case "python", "shell", "yaml", "toml", "ruby", "r", "perl", "dockerfile", "makefile", "ini":
		line, blockOpen, blockClose = "#", "", ""
	case "sql", "lua", "haskell":
		line, blockOpen, blockClose = "--", "/*", "*/"
		if language == "lua" {
			blockOpen, blockClose = "--[[", "]]"
		}
	case "html", "xml", "vue", "svelte", "markdown":
		line, blockOpen, blockClose = "", "<!--", "-->"
	case "css", "scss", "less":
		line = ""
	}
	var codeB, comB strings.Builder
	// chunk ที่เริ่มกลาง block comment (socraticode ตัดก้อนกลาง /** … */ ได้): มี */ ก่อน /* ตัวแรก → บรรทัดก่อนหน้านั้นคือคอมเมนต์
	inBlock := false
	if blockOpen != "" {
		if c := strings.Index(content, blockClose); c >= 0 {
			if o := strings.Index(content, blockOpen); o < 0 || o > c {
				inBlock = true
			}
		}
	}
	for _, l := range strings.Split(content, "\n") {
		t := strings.TrimSpace(l)
		if inBlock {
			if i := strings.Index(t, blockClose); i >= 0 {
				comB.WriteString(strings.TrimSpace(t[:i]) + "\n")
				rest := strings.TrimSpace(t[i+len(blockClose):])
				inBlock = false
				if rest != "" {
					codeB.WriteString(rest + "\n")
				}
			} else {
				comB.WriteString(strings.TrimPrefix(t, "*") + "\n")
			}
			continue
		}
		if i := strings.Index(t, blockOpen); blockOpen != "" && i >= 0 {
			if i > 0 { // `select 1; /* block */` — โค้ดก่อน block
				codeB.WriteString(strings.TrimSpace(t[:i]) + "\n")
			}
			body := t[i+len(blockOpen):]
			if i := strings.Index(body, blockClose); i >= 0 {
				comB.WriteString(strings.TrimSpace(body[:i]) + "\n")
				if rest := strings.TrimSpace(body[i+len(blockClose):]); rest != "" {
					codeB.WriteString(rest + "\n")
				}
			} else {
				comB.WriteString(strings.TrimSpace(strings.TrimPrefix(body, "*")) + "\n")
				inBlock = true
			}
			continue
		}
		if line != "" && strings.HasPrefix(t, line) {
			comB.WriteString(strings.TrimSpace(t[len(line):]) + "\n")
			continue
		}
		// คอมเมนต์ท้ายบรรทัด (`x = 1 // why`) — หา " //" ที่มีช่องว่างนำ จึงไม่ชน `://` ใน URL
		if line == "//" {
			if i := strings.Index(t, " //"); i > 0 {
				codeB.WriteString(strings.TrimSpace(t[:i]) + "\n")
				comB.WriteString(strings.TrimSpace(t[i+3:]) + "\n")
				continue
			}
		} else if line == "#" {
			if i := strings.Index(t, " #"); i > 0 {
				codeB.WriteString(strings.TrimSpace(t[:i]) + "\n")
				comB.WriteString(strings.TrimSpace(t[i+2:]) + "\n")
				continue
			}
		}
		if t != "" {
			codeB.WriteString(t + "\n")
		}
	}
	return strings.TrimSpace(codeB.String()), strings.TrimSpace(comB.String())
}

func cosine(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// ollamaEmbedMany — /api/embed รับ input เป็น array → vector ต่อข้อความในลำดับเดิม (ข้อความว่างต้องกรองก่อน ollama ปฏิเสธ)
func ollamaEmbedMany(base, model string, inputs []string) ([][]float64, error) {
	raw, _ := json.Marshal(map[string]any{"model": model, "input": inputs})
	cl := http.Client{Timeout: 60 * time.Second}
	resp, err := cl.Post(base+"/api/embed", "application/json", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ollama /api/embed → %d", resp.StatusCode)
	}
	var out struct {
		Embeddings [][]float64 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Embeddings) != len(inputs) {
		return nil, fmt.Errorf("ollama returned %d embeddings for %d inputs", len(out.Embeddings), len(inputs))
	}
	return out.Embeddings, nil
}

// codeLanguage — ภาษาที่ socraticode แยก symbol ด้วย tree-sitter (ast-grep) จริง (graph-symbols.ts extractSymbolsAndCalls)
// นอกรายการนี้ (.md .txt .sql .json .yaml .css .html …) = ไม่มี "โค้ด" ให้เทียบ → origin = doc (เจ้าของ 2026-09-20: ต้องเป็นไฟล์โค้ดจริง)
func codeLanguage(lang string) bool {
	switch lang {
	case "javascript", "typescript", "tsx", "jsx", "python", "go", "rust", "java", "kotlin", "scala", "csharp",
		"c", "cpp", "ruby", "php", "swift", "bash", "shell", "lua", "dart", "elixir", "gdscript", "vue", "svelte":
		return true
	}
	return false
}

// codeFile — ไฟล์นี้เป็นโค้ดตามรายการเดียวกับ codeLanguage (ตัดสินจากนามสกุล) · blm update ใช้กรอง "เฉพาะ code" (เจ้าของ 2026-09-20:
// README/config/fonts ไม่ใช่ความรู้ของ blm ไม่ต้องให้ติ๊ก ignore ทีละอัน)
func codeFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".py", ".go", ".rs", ".java", ".kt", ".kts", ".scala", ".cs",
		".c", ".h", ".cpp", ".cc", ".hpp", ".rb", ".php", ".swift", ".sh", ".bash", ".lua", ".dart", ".ex", ".exs", ".gd", ".vue", ".svelte":
		return true
	}
	return false
}

// proseLanguage คงไว้ให้ preview/ทดสอบเดิม = ตรงข้ามกับ codeLanguage
func proseLanguage(lang string) bool { return !codeLanguage(lang) }

// trustLabel — ป้ายจากตัวเลข (กติกาเจ้าของ: code = เชื่อ · comment อย่างเดียว = เชื่อน้อยลง · ทั้งคู่ไปทางเดียวกัน = 100%)
func trustLabel(t *Trust) {
	switch {
	case t.Agree < 0: // ไม่มีคอมเมนต์
		t.Origin, t.Label = "code", "code"
	case t.CommentSim-t.CodeSim > trustMargin:
		t.Origin = "comment"
		if t.Agree >= trustAgree {
			t.Label = "comment ~" // ชนคอมเมนต์ แต่คอมเมนต์เล่าเรื่องเดียวกับโค้ด
		} else {
			t.Label = "comment ⚠" // ชนแต่คอมเมนต์ และโค้ดไม่ไปทางเดียวกัน — น่าสงสัยว่าคอมเมนต์เก่า
		}
	case t.CodeSim-t.CommentSim > trustMargin:
		t.Origin, t.Label = "code", "code"
	default:
		t.Origin = "both"
		if t.Agree >= trustAgree {
			t.Label = "both ✓"
		} else {
			t.Label = "both"
		}
	}
}

// annotateTrust เติม Trust ให้ทุก hit ด้วย embed ครั้งเดียว (2 ข้อความต่อ hit ที่มีคอมเมนต์) · ล้ม = ปล่อยว่าง ไม่ทำให้ search ล้ม
func annotateTrust(e scEndpoint, queryVec []float64, hits []SearchHit) error {
	type part struct {
		hit  int
		code string
		com  string
	}
	var parts []part
	var inputs []string
	for i := range hits {
		if !codeLanguage(hits[i].Language) { // ไม่ใช่ไฟล์โค้ดที่ tree-sitter รู้จัก: ไม่มีโค้ดให้เทียบ ไม่ใช่ "code" และไม่ใช่ "comment"
			hits[i].Trust = &Trust{Origin: "doc", Label: "doc", Agree: -1}
			continue
		}
		code, com := splitComments(hits[i].Content, hits[i].Language)
		if com == "" || code == "" {
			hits[i].Trust = &Trust{Agree: -1}
			trustLabel(hits[i].Trust)
			if code == "" && com != "" { // ก้อนที่เป็นคอมเมนต์ล้วน (header block) — ชนได้ทางเดียว
				hits[i].Trust.Origin, hits[i].Trust.Label = "comment", "comment ⚠"
			}
			continue
		}
		parts = append(parts, part{i, code, com})
		inputs = append(inputs, e.DocPrefix+code, e.DocPrefix+com)
	}
	if len(inputs) == 0 {
		return nil
	}
	vecs, err := ollamaEmbedMany(e.Ollama, e.Model, inputs)
	if err != nil {
		return err
	}
	for k, p := range parts {
		cv, mv := vecs[2*k], vecs[2*k+1]
		t := &Trust{CodeSim: cosine(queryVec, cv), CommentSim: cosine(queryVec, mv), Agree: cosine(cv, mv)}
		trustLabel(t)
		hits[p.hit].Trust = t
	}
	return nil
}
