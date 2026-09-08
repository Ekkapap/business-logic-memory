package blm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tmpStore(t *testing.T, backend Backend) (*Store, Config, string) {
	root := t.TempDir()
	c := Presets[backend]
	return Open(root, c), c, root
}

func TestStoreCRUDAndHistory(t *testing.T) {
	s, _, _ := tmpStore(t, BackendNone)
	n, err := s.Save(Input{Name: "abc-1", Content: "hello", HasContent: true, Description: "d"})
	if err != nil || n.Target != "abc-1" || n.Mode != "append" {
		t.Fatalf("save: %v %+v", err, n)
	}
	if _, err := s.Save(Input{Name: "Bad Name", Content: "x", HasContent: true}); err == nil {
		t.Fatal("ชื่อไม่ถูกต้องต้อง error")
	}
	if n, _ = s.Update(Input{Name: "abc-1", Content: "world"}); !strings.Contains(n.Content, "hello\n\nworld") {
		t.Fatalf("update: %q", n.Content)
	}
	if _, err := s.Patch("abc-1", "zzz", "y"); err == nil {
		t.Fatal("patch ที่ไม่พบต้อง error")
	}
	if n, _ = s.Patch("abc-1", "world", "there"); !strings.Contains(n.Content, "there") || n.Description != "d" {
		t.Fatalf("patch: %+v", n)
	}
	if err := s.Delete("abc-1"); err != nil || s.Has("abc-1") {
		t.Fatal("delete")
	}
	hist, _ := os.ReadDir(filepath.Join(s.Dir, "history"))
	if len(hist) != 3 { // append, patch, delete
		t.Fatalf("history ต้องมี 3 ไฟล์ ได้ %d", len(hist))
	}
	for _, h := range hist {
		if !strings.HasPrefix(h.Name(), "abc-1-[") {
			t.Fatalf("ชื่อ history ผิด %s", h.Name())
		}
	}
}

const rules = `---
target: blm
mode: replace
---

# วิธีอ่าน
ไม่ใช่กฎ

# Authentication
## Email & Password
memory: x
code: y
updated_at: 2026-09-01T00:00:00Z

## LINE Login
memory: z
code: w
`

func TestRulesFromStoreAndMirror(t *testing.T) {
	// backend none: กฎอยู่ใน store เอง
	s, _, root := tmpStore(t, BackendNone)
	_ = os.MkdirAll(s.Dir, 0o755)
	_ = os.WriteFile(filepath.Join(s.Dir, "blm.md"), []byte(rules), 0o644)
	res := s.Rules("", "user")
	if len(res.Blocks) != 3 {
		t.Fatalf("blocks %d", len(res.Blocks))
	}
	var ruleCount int
	for _, b := range res.Blocks {
		if b.Rule {
			ruleCount++
		}
	}
	if ruleCount != 2 {
		t.Fatalf("rule blocks %d", ruleCount)
	}
	if res.Blocks[1].RefShort != "blm.md:10" || res.Blocks[1].Topic != "Authentication" {
		t.Fatalf("ref/topic ผิด (frontmatter 4 บรรทัดต้องนับรวม): %+v", res.Blocks[1])
	}
	if q := s.Rules("line login", "agent"); len(q.Blocks) != 1 || q.Blocks[0].Heading != "LINE Login" {
		t.Fatalf("query: %+v", q.Blocks)
	}
	if _, err := os.Lstat(filepath.Join(root, ".claude", "blm.md")); err != nil {
		t.Fatalf("symlink สั้นต้องถูกสร้าง: %v", err)
	}
	topics, rulesN := s.TopicCount()
	if topics != 2 || rulesN != 2 {
		t.Fatalf("TopicCount %d %d", topics, rulesN)
	}

	// backend agentsroom: กฎอยู่ใน mirror, โน้ต temp ชื่อเดียวกัน = draft
	s2, _, root2 := tmpStore(t, BackendAgentsRoom)
	dir := filepath.Join(root2, ".agentsroom", "memory", "global", "conventions")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "blm.md"), []byte(rules), 0o644)
	_, _ = s2.Save(Input{Name: "blm-draft", Target: "blm", Content: "x", HasContent: true})
	r2 := s2.Rules("", "agent")
	if r2.Source != ".agentsroom/memory/global/conventions/blm.md" || len(r2.PendingDrafts) != 1 {
		t.Fatalf("mirror: %+v", r2)
	}
	plan := s2.PlanSync(s2.List())
	if len(plan) != 1 || plan[0].MemorySave.Folder != "global/conventions" || !plan[0].TargetExists {
		t.Fatalf("plan: %+v", plan)
	}
	g := s2.GainSummary()
	if g.Reads != 1 || g.ReadsByAgent != 1 {
		t.Fatalf("gain: %+v", g)
	}
}

func TestLayoutWidth(t *testing.T) {
	if DisplayWidth("กี่") != 1 || DisplayWidth("✅") != 2 || DisplayWidth("❌") != 2 || DisplayWidth("✔") != 1 || DisplayWidth("ab") != 2 {
		t.Fatal("displayWidth")
	}
	rows := []CheckRow{
		{"Auth", "Email", "PASSED", "blm.md:10"},
		{"Auth", strings.Repeat("ยาวมาก ", 12), "NOT PASSED", "blm.md:14"},
		{"Notify", "Bell", "UNKNOWN", "blm.md:20"},
	}
	l := RenderCheck(rows, 40)
	if !strings.Contains(l.Summary, "PASSED 1 · ❌ NOT PASSED 1 · UNKNOWN 1") {
		t.Fatal(l.Summary)
	}
	if !strings.HasPrefix(l.Terminal, "**Auth**") || !strings.Contains(l.Terminal, "**UNKNOWN**\n- Notify › Bell") {
		t.Fatal(l.Terminal)
	}
	if !strings.Contains(l.Terminal, "\n  ยาวมาก") {
		t.Fatalf("ชื่อยาวต้องตัดขึ้นบรรทัด indent 2:\n%s", l.Terminal)
	}
	if !strings.Contains(l.Markdown, "[blm.md:10](../../blm.md:10)") {
		t.Fatal(l.Markdown)
	}
}

func TestReportAndStatus(t *testing.T) {
	s, c, _ := tmpStore(t, BackendNone)
	out, err := s.CheckReport("proj", "", "before", "after", []CheckRow{{"A", "B", "PASSED", "blm.md:1"}}, "user")
	if err != nil || !strings.Contains(out["terminal"].(string), "## proj Testing Result") {
		t.Fatalf("%v %v", err, out)
	}
	if r := s.LatestReport("businesslogic"); r == nil || !strings.Contains(r.Content, "| Business Logic |") {
		t.Fatal("latest report")
	}
	c.Tools = []string{"socraticode", "obsidian", "graphify"}
	st := s.Status(c)
	if st.Reports != 1 || st.Gain.Checks != 1 || len(st.Tools) != 3 || !st.Ready || !st.Tools[0].Applied {
		t.Fatalf("status %+v", st)
	}
	if out := RenderStatus(st); !strings.Contains(out, "READY") || !strings.Contains(out, "Tools") || strings.Contains(out, "**") {
		t.Fatal("render status")
	}
}

func TestLoadFallback(t *testing.T) {
	root := t.TempDir()
	if Load(root).Backend != BackendNone {
		t.Fatal("ไม่มีอะไร → none")
	}
	_ = os.MkdirAll(filepath.Join(root, ".agentsroom", "memory"), 0o755)
	if Load(root).Backend != BackendAgentsRoom {
		t.Fatal("มี .agentsroom/memory → agentsroom")
	}
	c := Config{Backend: BackendObsidian, Store: "notes"}
	_ = c.Save(root)
	if got := Load(root); got.Store != "notes" || got.HasSync() {
		t.Fatalf("config: %+v", got)
	}
}
