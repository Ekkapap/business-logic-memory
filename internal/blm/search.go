package blm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// blm search — ค้นหาความหมายด้วย index ของ SocratiCode โดยไม่ผ่าน MCP ของมัน (เจ้าของ 2026-09-20: blm เป็น proxy wrapper)
//
// ทำเหมือน codebase_search ของ socraticode 1.14.0 (src/services/qdrant.ts searchChunksWithVector):
//  1. embed คำถามด้วยโมเดล+prefix ตัวเดียวกับที่ index ใช้ (Ollama /api/embed) — คนละโมเดล = คนละ space ค้นไม่เจอ
//  2. Qdrant /points/query: prefetch dense + bm25 (server-side) แล้ว fusion rrf → คะแนนสเกลเดิม 1.0 = อันดับ 1 ทั้งสองฝั่ง
//  3. กรองฝั่ง blm: `.md` ใต้ store ของ blm และใต้ .agentsroom/ (mirror memory) ตัดออกเสมอ · ExcludeMD = ตัด .md ทั้งโปรเจ็ค
//     (filter ของ Qdrant จับ path แบบ exact เท่านั้น จึง over-fetch ×3 แล้วกรองเอง)
// ที่อยู่/โมเดลอ่านจาก .claude/blm.json (section socraticode) → env → ~/.claude/settings.json env → 127.0.0.1

// SearchOpts พารามิเตอร์ของ blm_search / blm search
type SearchOpts struct {
	Query string
	// Limit จำนวนผลสูงสุด (0 = 10)
	Limit int
	// Lang กรองภาษาแบบ exact ตาม label ของ socraticode (typescript = .ts+.tsx, go, markdown, sql …)
	Lang string
	// File กรอง relativePath แบบ exact
	File string
	// ExcludeMD ตัด .md ทั้งโปรเจ็ค (ค่าเริ่มต้นตัดเฉพาะ .md ของ blm store และ .agentsroom/)
	ExcludeMD bool
	// Brief = pointer อย่างเดียว ไม่แนบเนื้อ chunk และไม่บันทึกไฟล์ผล (hook ใช้ — token ต่อ prompt ไม่ถูก cache)
	Brief bool
	// Full = แนบเนื้อ chunk ทั้งก้อนทุกผลใน terminal (แบบ codebase_search) · ค่าเริ่มต้น = สารบัญ 1 บรรทัด/ผล แล้วค่อย Get รายตัว
	Full bool
	// MinScore ตัดผลที่ต่ำกว่า (0 = 0.10 เท่า SEARCH_MIN_SCORE ของ socraticode)
	MinScore float64
	// NoTrust ข้ามการวิเคราะห์ code/comment (trust.go) — เร็วขึ้น 1 embed request · Brief ข้ามเสมอ
	NoTrust bool
}

// SearchHit หนึ่ง chunk · ID = เลขอ้างอิงในสารบัญ (1..n) ใช้กับ Get
type SearchHit struct {
	ID        int     `json:"id"`
	Path      string  `json:"path"`
	StartLine int     `json:"startLine"`
	EndLine   int     `json:"endLine"`
	Language  string  `json:"language"`
	Score     float64 `json:"score"`
	// Preview บรรทัดแรกที่มีเนื้อหาของ chunk (สารบัญ) · Content ทั้ง chunk (เก็บในไฟล์ผลเสมอ, ใส่ใน response เฉพาะ Full/Get)
	Preview string `json:"preview,omitempty"`
	Content string `json:"content,omitempty"`
	// Trust — ชนเพราะโค้ดหรือคอมเมนต์ และสองส่วนไปทางเดียวกันไหม (trust.go) · nil = ไม่ได้วิเคราะห์
	Trust *Trust `json:"trust,omitempty"`
}

// SearchResult ผลลัพธ์ — Terminal คือข้อความที่ CLI พิมพ์ (renderer เดิมของ main.go ใช้ key "terminal")
// SearchID + File = ไฟล์ผล (<store>/tmp/search-result-<id>.json) ที่ `blm search --get <id> --id n,m` / `blm_search {get, ids}` เปิดทีหลัง
type SearchResult struct {
	SearchID string      `json:"searchId,omitempty"`
	File     string      `json:"file,omitempty"`
	CWD      string      `json:"cwd,omitempty"`
	Query    string      `json:"query"`
	Model    string      `json:"model"`
	Hits     []SearchHit `json:"hits"`
	Excluded int         `json:"excluded"`
	Terminal string      `json:"terminal"`
}

// scEndpoint ที่อยู่ + โมเดลของ stack ที่ index โปรเจ็คนี้
type scEndpoint struct {
	Ollama, Qdrant, Model, QueryPrefix, DocPrefix, CollectionPrefix string
}

// scEndpointFor ลำดับเดียวกับ qdrantBase: blm.json → env → ~/.claude/settings.json → local
func scEndpointFor(root string) scEndpoint {
	e := scEndpoint{CollectionPrefix: os.Getenv("QDRANT_COLLECTION_PREFIX")}
	if r := Load(root).SocratiCode; r != nil {
		e.Ollama, e.Qdrant = strings.TrimRight(r.OllamaURL, "/"), strings.TrimRight(r.QdrantURL, "/")
		e.Model, e.QueryPrefix, e.DocPrefix = r.EmbeddingModel, r.EmbeddingQueryPrefix, r.EmbeddingDocumentPrefix
		if e.Model == "" {
			e.Model, e.QueryPrefix, e.DocPrefix = "nomic-embed-text", "search_query: ", "search_document: "
		}
		return e
	}
	env := map[string]string{}
	if home, _ := os.UserHomeDir(); home != "" {
		if raw, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json")); err == nil {
			var s struct {
				Env map[string]string `json:"env"`
			}
			if json.Unmarshal(raw, &s) == nil {
				env = s.Env
			}
		}
	}
	get := func(k, def string) string {
		if v, ok := os.LookupEnv(k); ok {
			return v
		}
		if v, ok := env[k]; ok {
			return v
		}
		return def
	}
	e.Ollama = strings.TrimRight(get("OLLAMA_URL", "http://127.0.0.1:11434"), "/")
	e.Qdrant = qdrantBase(root)
	if e.Qdrant == "" {
		e.Qdrant = strings.TrimRight(get("QDRANT_URL", "http://127.0.0.1:6333"), "/")
	}
	e.Model = get("EMBEDDING_MODEL", "nomic-embed-text")
	e.QueryPrefix = get("EMBEDDING_QUERY_PREFIX", "search_query: ")
	e.DocPrefix = get("EMBEDDING_DOCUMENT_PREFIX", "search_document: ")
	return e
}

// Search ค้นความหมายผ่าน index ของ SocratiCode
func Search(root string, c Config, o SearchOpts) (*SearchResult, error) {
	q := strings.TrimSpace(o.Query)
	if q == "" {
		return nil, fmt.Errorf("query required")
	}
	if o.Limit <= 0 {
		o.Limit = 10
	}
	if o.MinScore <= 0 {
		o.MinScore = 0.10
	}
	e := scEndpointFor(root)
	vec, err := ollamaEmbed(e.Ollama, e.Model, e.QueryPrefix+q)
	if err != nil {
		return nil, fmt.Errorf("embed via %s (%s): %v — is the stack up? blm tools status socraticode", e.Ollama, e.Model, err)
	}
	coll := e.CollectionPrefix + "codebase_" + SocratiCodeProjectID(root)
	points, err := qdrantHybrid(e.Qdrant, coll, q, vec, o)
	if err != nil {
		return nil, fmt.Errorf("qdrant %s/%s: %v — not indexed? (codebase_index)", e.Qdrant, coll, err)
	}
	res := &SearchResult{Query: q, Model: e.Model, CWD: root}
	for _, p := range points {
		h := SearchHit{Path: p.Payload.RelativePath, StartLine: p.Payload.StartLine, EndLine: p.Payload.EndLine, Language: p.Payload.Language, Score: p.Score}
		if h.Score < o.MinScore {
			continue
		}
		if excludeHit(h.Path, c, o.ExcludeMD) {
			res.Excluded++
			continue
		}
		h.ID = len(res.Hits) + 1
		h.Content = p.Payload.Content
		// preview จากส่วนโค้ด (ไม่ใช่คอมเมนต์ที่ค้างจากก้อนก่อน) — ไฟล์ข้อความใช้ทั้งก้อน
		h.Preview = previewLine(h.Content)
		if !proseLanguage(h.Language) {
			if code, _ := splitComments(h.Content, h.Language); code != "" {
				h.Preview = previewLine(code)
			}
		}
		res.Hits = append(res.Hits, h)
		if len(res.Hits) >= o.Limit {
			break
		}
	}
	// ความน่าเชื่อถือ: ชนโค้ดหรือคอมเมนต์ (trust.go) — brief/NoTrust ข้าม · ล้มก็แค่ไม่มีป้าย
	var trustWarn string
	if !o.Brief && !o.NoTrust && len(res.Hits) > 0 {
		if err := annotateTrust(e, vec, res.Hits); err != nil {
			trustWarn = err.Error()
		}
	}
	// ไฟล์ผล (เนื้อ chunk ครบ) ไว้ให้ Get — brief (hook) ไม่ต้อง
	var saveWarn string
	if !o.Brief && len(res.Hits) > 0 {
		res.SearchID = randomBase36()
		if file, err := saveResultJSON(root, "search", res.SearchID, res); err != nil {
			saveWarn = err.Error()
			res.SearchID = ""
		} else {
			res.File = file
		}
	}
	// response ที่ agent เห็น: เนื้อ chunk เฉพาะ Full — สารบัญไม่แบก content ทั้ง 10 ก้อน
	if !o.Full {
		for i := range res.Hits {
			res.Hits[i].Content = ""
		}
	}
	res.Terminal = renderSearch(res, o, saveWarn, trustWarn)
	return res, nil
}

// previewLine — บรรทัดแรกของ chunk ที่มีเนื้อ (ข้าม import/คอมเมนต์เปิด/ว่าง) ตัดที่ 80 ตัวอักษร
func previewLine(content string) string {
	fallback := ""
	for _, l := range strings.Split(content, "\n") {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if fallback == "" {
			fallback = t
		}
		if strings.HasPrefix(t, "import ") || strings.HasPrefix(t, "/**") || strings.HasPrefix(t, "*") || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "---") || strings.HasPrefix(t, "</") || !hasWord(t) {
			continue // import / คอมเมนต์ / แท็กปิด / บรรทัดที่มีแต่วงเล็บ-ปีกกา (`}`, `);`) ไม่บอกอะไร
		}
		fallback = t
		break
	}
	if r := []rune(fallback); len(r) > 80 {
		return string(r[:79]) + "…"
	}
	return fallback
}

// SearchGet — เปิด chunk ที่เลือกจากไฟล์ผล: อ่านบรรทัดจริงจากไฟล์ (สด) ตาม startLine..endLine ± context · ไฟล์หาย = ใช้ content ที่เก็บไว้
func SearchGet(root, searchID string, ids []int, context int) (*SearchResult, error) {
	file, err := findResultFile(root, "search", searchID)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var saved SearchResult
	if err := json.Unmarshal(raw, &saved); err != nil {
		return nil, fmt.Errorf("cannot parse %s: %v", file, err)
	}
	want := map[int]bool{}
	for _, id := range ids {
		want[id] = true
	}
	out := &SearchResult{SearchID: searchID, File: file, CWD: saved.CWD, Query: saved.Query, Model: saved.Model}
	var b strings.Builder
	fmt.Fprintf(&b, "search %s  %s", Dim(searchID), Dim("· "+saved.Query))
	for _, h := range saved.Hits {
		if len(want) > 0 && !want[h.ID] {
			continue
		}
		start, end := h.StartLine, h.EndLine
		body := h.Content
		numbered := false
		if lines := fileLines(filepath.Join(saved.CWD, h.Path)); lines != nil {
			start = maxInt(1, h.StartLine-context)
			end = minInt(len(lines), h.EndLine+context)
			body = strings.Join(lines[start-1:end], "\n")
			numbered = true
		}
		h.Content = body
		h.StartLine, h.EndLine = start, end
		out.Hits = append(out.Hits, h)
		fmt.Fprintf(&b, "\n\n%s  %s:L%d-L%d  %s  [%s]", Cyan(fmt.Sprintf("#%d", h.ID)), h.Path, start, end, Dim(fmt.Sprintf("%.2f", h.Score)), h.Language)
		if h.Trust != nil {
			b.WriteString("  " + trustCell(h.Trust))
		}
		for i, l := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
			if numbered {
				fmt.Fprintf(&b, "\n%s %s", Dim(fmt.Sprintf("%5d", start+i)), l)
			} else {
				b.WriteString("\n      " + l)
			}
		}
	}
	if len(out.Hits) == 0 {
		return nil, fmt.Errorf("no hit with id %v in search %s (ids 1..%d)", ids, searchID, len(saved.Hits))
	}
	out.Terminal = b.String()
	return out, nil
}

func fileLines(path string) []string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
}

// excludeHit — .md ของ blm store / .agentsroom/ ไม่ใช่โค้ด (โน้ต, mirror memory) ตัดเสมอ · all = ตัด .md ทุกที่
func excludeHit(rel string, c Config, all bool) bool {
	if !strings.HasSuffix(strings.ToLower(rel), ".md") {
		return false
	}
	if all {
		return true
	}
	rel = filepath.ToSlash(rel)
	store := strings.TrimSuffix(filepath.ToSlash(c.Store), "/") + "/"
	return strings.HasPrefix(rel, ".agentsroom/") || (c.Store != "" && strings.HasPrefix(rel, store))
}

// renderSearch — สารบัญ: 1 บรรทัดต่อผล (id · score · path:L-L · [lang] · preview) + วิธีเปิดรายตัว · Full = แนบเนื้อทุกก้อน
func renderSearch(r *SearchResult, o SearchOpts, saveWarn, trustWarn string) string {
	if len(r.Hits) == 0 {
		return fmt.Sprintf("no results ≥ %.2f for %q %s", o.MinScore, r.Query, Dim("(model "+r.Model+")"))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "search %q — %d hits %s", r.Query, len(r.Hits), Dim(fmt.Sprintf("(model %s · score: RRF, 1.0 = top of both semantic and keyword", r.Model)))
	if r.Excluded > 0 {
		b.WriteString(Dim(fmt.Sprintf(" · %d .md excluded", r.Excluded)))
	}
	b.WriteString(Dim(")"))
	// สารบัญ 2 บรรทัดต่อผล (เจ้าของ 2026-09-20): บรรทัด 1 = id · score · path:lines · origin · บรรทัด 2 (เยื้องตรงคอลัมน์ score) = [lang] preview
	showTrust := !o.Brief && !o.NoTrust
	idW, pathW := 3, 0
	for _, h := range r.Hits {
		idW = maxInt(idW, DisplayWidth(fmt.Sprintf("#%d", h.ID)))
		pathW = maxInt(pathW, DisplayWidth(fmt.Sprintf("%s:L%d-L%d", h.Path, h.StartLine, h.EndLine)))
	}
	indent := strings.Repeat(" ", 2+idW+2)
	header := "  " + Dim(PadEnd("id", idW)) + "  " + Dim(PadEnd("score", 5)) + "  " + Dim(PadEnd("path:lines", pathW))
	if showTrust {
		header += "  " + Dim("origin")
	}
	b.WriteString("\n" + header)
	withTrust := false
	for _, h := range r.Hits {
		line := "  " + Cyan(PadEnd(fmt.Sprintf("#%d", h.ID), idW)) + "  " + PadEnd(fmt.Sprintf("%.2f", h.Score), 5) + "  " + PadEnd(fmt.Sprintf("%s:L%d-L%d", h.Path, h.StartLine, h.EndLine), pathW)
		if h.Trust != nil {
			withTrust = true
			line += "  " + trustCell(h.Trust)
		} else if showTrust {
			line += "  " + Dim("—")
		}
		b.WriteString("\n" + line)
		b.WriteString("\n" + indent + Dim("["+h.Language+"]") + "  " + Dim(h.Preview))
	}
	if withTrust {
		b.WriteString("\n" + Dim("origin: code = the query matched the code itself · both ✓ = matched code and comment, and they agree · both = matched both, weak agreement · comment ~ = only the comment matched but it agrees with its code · comment ⚠ = only the comment matched and the code says something else (stale comment?) · doc = not a tree-sitter code file (.md .sql .json …), nothing to cross-check · number = agreement cosine(code, comment)"))
	}
	if trustWarn != "" {
		b.WriteString("\n" + Yellow("note:") + " origin not analysed (" + trustWarn + ")")
	}
	if o.Full {
		for _, h := range r.Hits {
			fmt.Fprintf(&b, "\n\n%s  %s:L%d-L%d", Cyan(fmt.Sprintf("#%d", h.ID)), h.Path, h.StartLine, h.EndLine)
			for _, line := range strings.Split(strings.TrimRight(h.Content, "\n"), "\n") {
				b.WriteString("\n      " + line)
			}
		}
	}
	switch {
	case r.SearchID != "":
		fmt.Fprintf(&b, "\n%s %s   %s", Dim("search-id:"), r.SearchID, Dim("open a hit: blm search --get "+r.SearchID+" --id 1,3  ·  blm_search {get:\""+r.SearchID+"\", ids:[1,3]}  ·  --full for everything inline"))
	case saveWarn != "":
		b.WriteString("\n" + Yellow("note:") + " result not saved (" + saveWarn + ") — rerun with --full to see chunk bodies")
	}
	return b.String()
}

// ---- Ollama / Qdrant ----------------------------------------------------------

func ollamaEmbed(base, model, input string) ([]float64, error) {
	raw, _ := json.Marshal(map[string]any{"model": model, "input": input})
	cl := http.Client{Timeout: 30 * time.Second}
	resp, err := cl.Post(base+"/api/embed", "application/json", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ollama /api/embed → %d (model %s pulled there?)", resp.StatusCode, model)
	}
	var out struct {
		Embeddings [][]float64 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Embeddings) == 0 || len(out.Embeddings[0]) == 0 {
		return nil, fmt.Errorf("ollama returned no embedding")
	}
	return out.Embeddings[0], nil
}

type scPoint struct {
	Score   float64 `json:"score"`
	Payload struct {
		RelativePath string `json:"relativePath"`
		StartLine    int    `json:"startLine"`
		EndLine      int    `json:"endLine"`
		Language     string `json:"language"`
		Content      string `json:"content"`
	} `json:"payload"`
}

// qdrantHybrid = payload ที่ socraticode ส่ง (qdrant.ts:951–966) · over-fetch ×3 เผื่อกรอง .md ทิ้ง
func qdrantHybrid(base, coll, query string, vec []float64, o SearchOpts) ([]scPoint, error) {
	var must []map[string]any
	if o.File != "" {
		must = append(must, map[string]any{"key": "relativePath", "match": map[string]any{"value": o.File}})
	}
	if o.Lang != "" {
		must = append(must, map[string]any{"key": "language", "match": map[string]any{"value": o.Lang}})
	}
	var filter map[string]any
	if len(must) > 0 {
		filter = map[string]any{"must": must}
	}
	fetch := o.Limit * 3
	if fetch < 30 {
		fetch = 30
	}
	body := map[string]any{
		"prefetch": []map[string]any{
			{"query": vec, "using": "dense", "limit": fetch, "filter": filter},
			{"query": map[string]any{"text": query, "model": "qdrant/bm25"}, "using": "bm25", "limit": fetch, "filter": filter},
		},
		"query":        map[string]any{"fusion": "rrf"},
		"limit":        fetch,
		"filter":       filter,
		"with_payload": []string{"relativePath", "startLine", "endLine", "language", "content"},
	}
	var out struct {
		Result struct {
			Points []scPoint `json:"points"`
		} `json:"result"`
	}
	if err := qdrantPost(base, "/collections/"+coll+"/points/query", body, &out); err != nil {
		return nil, err
	}
	return out.Result.Points, nil
}

// trustCell ป้ายในตาราง: สีตามความน่าเชื่อถือ + agree
func trustCell(t *Trust) string {
	agree := ""
	if t.Agree >= 0 {
		agree = fmt.Sprintf(" %.2f", t.Agree)
	}
	switch t.Origin {
	case "doc":
		return Dim(t.Label)
	case "code":
		return Green(t.Label) + Dim(agree)
	case "both":
		if t.Label == "both ✓" {
			return Green(t.Label) + Dim(agree)
		}
		return t.Label + Dim(agree)
	default:
		if t.Label == "comment ~" {
			return Yellow(t.Label) + Dim(agree)
		}
		return Red(t.Label) + Dim(agree)
	}
}

// hasWord — มีตัวอักษรหรือตัวเลขอย่างน้อยหนึ่งตัว (บรรทัด `}` `);` ไม่ผ่าน)
func hasWord(t string) bool {
	for _, r := range t {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
