package blm

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SaveReport เก็บรายงาน markdown ใน store/reports/<วันที่>-<name>.md
// terminal โชว์แบบสั้น ไฟล์นี้เปิดใน AgentsRoom/Obsidian แล้วเรนเดอร์ลิงก์สวย และเป็นประวัติว่าวันไหน agent เข้าใจตรง/ไม่ตรงข้อไหน
func (s *Store) SaveReport(name, content string) (string, error) {
	if !nameRe.MatchString(name) {
		return "", fmt.Errorf("report name must be kebab-case: %q", name)
	}
	dir := filepath.Join(s.Dir, "reports")
	_ = os.MkdirAll(dir, 0o755)
	file := filepath.Join(dir, time.Now().Format("2006-01-02")+"-"+name+".md")
	return s.rel(file), os.WriteFile(file, []byte(strings.TrimRight(content, "\n")+"\n"), 0o644)
}

type Report struct {
	File    string   `json:"file"`
	Content string   `json:"content"`
	All     []string `json:"all"`
}

// LatestReport รายงานล่าสุด (กรองด้วยชื่อได้) — nil ถ้ายังไม่มี
func (s *Store) LatestReport(name string) *Report {
	dir := filepath.Join(s.Dir, "reports")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var all []string
	for _, e := range entries {
		f := e.Name()
		if !strings.HasSuffix(f, ".md") || len(f) < 15 {
			continue
		}
		if name == "" || f[11:len(f)-3] == name {
			all = append(all, f)
		}
	}
	if len(all) == 0 {
		return nil
	}
	sort.Strings(all)
	last := all[len(all)-1]
	raw, _ := os.ReadFile(filepath.Join(dir, last))
	return &Report{File: s.rel(filepath.Join(dir, last)), Content: string(raw), All: all}
}

// CheckReport ประกอบรายงานผลตรวจกฎทั้งฉบับจากสิ่งที่ agent ตัดสิน (before/after เป็นข้อความ rows เป็นผล)
// คืน terminal สำหรับพิมพ์ + path ไฟล์ที่บันทึก — agent ไม่ต้องจัดคอลัมน์หรือประกอบไฟล์เอง
func (s *Store) CheckReport(project, scope, before, after string, rows []CheckRow, trigger string) (map[string]any, error) {
	lay := RenderCheck(rows, 40)
	now := time.Now().Format("2006-01-02 15:04:05")
	if scope == "" {
		scope = "All"
	}
	md := strings.Join([]string{
		"# " + project + " — Business Logic Check " + now,
		"", "**Agent** : Current Understanding `Before Test` -> `" + scope + "`", "", before, "",
		"## " + project + " Testing Result", "", lay.Markdown, "", lay.Summary, "",
		"**Agent** : Current Understanding `After Test` -> `" + scope + "`", "", after,
	}, "\n")
	file, err := s.SaveReport("businesslogic", md)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.Result]++
	}
	s.RecordStat(map[string]any{"event": "check", "query": scope, "passed": counts["PASSED"], "notPassed": counts["NOT PASSED"], "unknown": counts["UNKNOWN"], "trigger": trigger})
	terminal := strings.Join([]string{
		"**Agent** : Current Understanding `Before Test` -> `" + scope + "`", before, "",
		"## " + project + " Testing Result", "", "**Business Logic Check** `" + now + "`", "", lay.Terminal, "", lay.Summary,
		"`Full Report` " + file, "",
		"**Agent** : Current Understanding `After Test` -> `" + scope + "`", after,
	}, "\n")
	return map[string]any{"ok": true, "file": file, "terminal": terminal, "summary": lay.Summary}, nil
}
