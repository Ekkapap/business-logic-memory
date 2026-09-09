package blm

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// linkPath (เจ้าของกำหนดสเปค 2026-09-09 แทน linkMemoryLines): ทำงานทุกบรรทัดของ blm.md ระดับคำ ไม่ใช่ทั้งบรรทัด
//   - คำที่เป็น path ไฟล์/โฟลเดอร์ที่มีอยู่จริงในโปรเจ็ค (src/lib/db.ts, wireguard/PLANNING.md, src/lib/sso/, src/lib/sso/*)
//     → [ชื่อไฟล์](path จาก root) — ใช้ได้ทั้ง code: verify: และกลางประโยค
//   - ในบรรทัด memory: คำที่เป็นชื่อโน้ต (ใน blm/ ก่อน ไม่มีค่อย mirror รวมชื่อซ้อนโฟลเดอร์ a/b) → [ชื่อ.md](path จาก root)
//   - ไม่ครอบปลายทางด้วย <> (parser ของ AgentsRoom ไม่รับ) · [ ] ในชื่อ escape เป็น \[ \] และ percent-encode ใน path · ช่องว่าง = %20
//   - ที่เป็นลิงก์อยู่แล้วไม่แตะ นอกจาก normalize รูปเก่า `](<path>)` และ path relative จาก blm/ (../memory/…, name.md) ให้เป็น path จาก root
//   - คำอื่นในบรรทัดคงเดิม ไม่แทรกบรรทัดว่าง
var (
	mdLinkRe    = regexp.MustCompile(`\[[^\]]*\]\((?:<[^>]*>|[^)\s]*)\)`)
	oldDestRe   = regexp.MustCompile(`\]\(<([^>]*)>\)`)
	pathTokenRe = regexp.MustCompile("^[`(\"']*([A-Za-z0-9_./*\\[\\]-]+?)([`)\"',.;:?]*)$")
)

func (s *Store) linkPaths(content string) string {
	lines := strings.Split(content, "\n")
	inFence := false
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			inFence = !inFence
			continue
		}
		if inFence || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "|") {
			continue
		}
		l = s.normalizeLinks(l)
		isMemory := strings.HasPrefix(l, "memory:")
		// แยกส่วนที่เป็นลิงก์อยู่แล้วออก แล้วประมวลผลเฉพาะข้อความรอบ ๆ
		var out strings.Builder
		last := 0
		for _, m := range mdLinkRe.FindAllStringIndex(l, -1) {
			out.WriteString(s.linkWords(l[last:m[0]], isMemory))
			out.WriteString(l[m[0]:m[1]])
			last = m[1]
		}
		out.WriteString(s.linkWords(l[last:], isMemory))
		lines[i] = out.String()
	}
	return strings.Join(lines, "\n")
}

// normalizeLinks รูปเก่า `](<path>)` → `](path)` และ path ที่ relative จากโฟลเดอร์ blm/ → path จาก root
func (s *Store) normalizeLinks(l string) string {
	return oldDestRe.ReplaceAllStringFunc(l, func(m string) string {
		p := oldDestRe.FindStringSubmatch(m)[1]
		switch {
		case strings.HasPrefix(p, "../") || !strings.Contains(p, "/"):
			p = filepath.ToSlash(filepath.Clean(filepath.Join(s.rel(s.Dir), p)))
		}
		return "](" + encodePath(p) + ")"
	})
}

// linkWords แทนคำที่เป็น path/ชื่อโน้ตในข้อความธรรมดา (ไม่มีลิงก์อยู่ก่อน)
func (s *Store) linkWords(text string, memoryLine bool) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	words := strings.Split(text, " ")
	for i, w := range words {
		m := pathTokenRe.FindStringSubmatch(w)
		if m == nil {
			continue
		}
		lead := w[:len(w)-len(m[1])-len(m[2])]
		core, trail := m[1], m[2]
		if link := s.linkFor(core, memoryLine); link != "" {
			words[i] = lead + link + trail
		}
	}
	return strings.Join(words, " ")
}

// linkFor คืนลิงก์สำหรับคำหนึ่ง หรือ "" ถ้าไม่ใช่ไฟล์/โน้ตที่มีอยู่จริง
func (s *Store) linkFor(tok string, memoryLine bool) string {
	if tok == "" || tok == "." || tok == ".." || strings.HasPrefix(tok, "http") {
		return ""
	}
	// path ไฟล์/โฟลเดอร์ในโปรเจ็ค (ต้องมี / หรือนามสกุล กันคำธรรมดาอย่าง "session")
	if strings.Contains(tok, "/") || strings.Contains(tok, ".") {
		p := strings.TrimSuffix(tok, "/*")
		p = strings.TrimSuffix(p, "*")
		if p != "" && !strings.HasPrefix(p, "/") && !strings.Contains(p, "..") {
			if fi, err := os.Stat(filepath.Join(s.Root, p)); err == nil {
				name := filepath.Base(p)
				if fi.IsDir() {
					p = strings.TrimSuffix(p, "/") + "/"
					name = p
				}
				return "[" + escapeText(name) + "](" + encodePath(p) + ")"
			}
		}
	}
	if !memoryLine {
		return ""
	}
	// ชื่อโน้ต (บรรทัด memory: เท่านั้น — คำอย่าง "blm" โผล่ทั่วไฟล์ ลิงก์ทุกที่ไม่ได้)
	name := strings.TrimSuffix(tok, ".md")
	if s.Has(name) {
		return "[" + name + ".md](" + encodePath(s.rel(filepath.Join(s.Dir, name+".md"))) + ")"
	}
	if s.MirrorDir != "" {
		if folder, ok := s.FindTargetFolder(name); ok {
			return "[" + name + ".md](" + encodePath(s.rel(filepath.Join(s.MirrorDir, folder, name+".md"))) + ")"
		}
		if strings.Contains(name, "/") {
			if p := s.mirrorFileBySuffix(name + ".md"); p != "" {
				return "[" + filepath.Base(name) + ".md](" + encodePath(s.rel(p)) + ")"
			}
		}
	}
	return ""
}

func (s *Store) mirrorFileBySuffix(suffix string) string {
	found := ""
	_ = filepath.WalkDir(s.MirrorDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || found != "" || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(filepath.ToSlash(p), "/"+suffix) {
			found = p
		}
		return nil
	})
	return found
}

func escapeText(s string) string {
	return strings.NewReplacer("[", `\[`, "]", `\]`).Replace(s)
}

func encodePath(p string) string {
	return strings.NewReplacer(" ", "%20", "[", "%5B", "]", "%5D").Replace(filepath.ToSlash(p))
}
