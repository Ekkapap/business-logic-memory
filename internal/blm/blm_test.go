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
	applied := 0
	for _, t := range st.Tools {
		if t.Applied {
			applied++
		}
	}
	if st.Reports != 1 || st.Gain.Checks != 1 || applied != 3 || !st.Ready {
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

func TestScanRepoSubProjects(t *testing.T) {
	root := t.TempDir()
	w := func(p, body string) {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o755)
		_ = os.WriteFile(filepath.Join(root, p), []byte(body), 0o644)
	}
	w(".gitignore", "node_modules/\ndist/\n")
	w(".socraticodeignore", "generated/\n")
	w("package.json", "{}")
	w("README.md", "# app")
	w("src/a.ts", strings.Repeat("x", 400))
	w("src/b.tsx", "y")
	w("node_modules/big/index.js", strings.Repeat("z", 5000))
	w("generated/out.ts", "q")
	w("vpn/go.mod", "module vpn")
	w("vpn/PLANNING.md", "# plan")
	w("vpn/cmd/main.go", "package main")
	w("vpn/cmd/README.md", "nested — must not be a second sub-project")
	w(".agentsroom/memory/features/x.md", "note")
	w(".claude/settings.json", "{}")
	r := ScanRepo(root, "", 0)
	if r.Files != 9 || r.Ignored < 2 { // dot-files (.gitignore …) ไม่นับ · node_modules/generated/.claude ถูกข้าม
		t.Fatalf("files %d ignored %d (node_modules/generated/.claude must be skipped)", r.Files, r.Ignored)
	}
	if r.MemoryNotes != 1 {
		t.Fatalf("memory notes %d", r.MemoryNotes)
	}
	if len(r.SubProjects) != 1 || r.SubProjects[0].Path != "vpn" || r.SubProjects[0].Files != 4 {
		t.Fatalf("sub-projects %+v", r.SubProjects)
	}
	if !strings.Contains(strings.Join(r.SubProjects[0].Markers, ","), "PLANNING.md") {
		t.Fatalf("markers %v", r.SubProjects[0].Markers)
	}
	if r.TokensApprox != r.Bytes/4 || r.Warning != "" {
		t.Fatalf("tokens %d warning %q", r.TokensApprox, r.Warning)
	}
	if small := ScanRepo(root, "", 10); small.Warning == "" {
		t.Fatal("token warning expected")
	}
	out := RenderScan(r)
	if !strings.Contains(out, "vpn") || !strings.Contains(out, "Sub-projects") {
		t.Fatal(out)
	}
}

func TestGraphBuildAndQuery(t *testing.T) {
	s, _, root := tmpStore(t, BackendNone)
	w := func(p, body string) {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o755)
		_ = os.WriteFile(filepath.Join(root, p), []byte(body), 0o644)
	}
	w("tsconfig.json", `{"compilerOptions": { /* block */ "paths": {"@/*": ["./src/*"]}}, "include": ["**/*.ts"]} // trailing comment`)
	w("src/lib/auth/session.ts", "export function createSession() {}\nexport const SESSION_TTL = 1\n")
	w("src/lib/auth/login.ts", "import { createSession } from '@/lib/auth/session';\nimport x from './helpers'\nexport async function requestLogin() {}\n")
	w("src/lib/auth/helpers.ts", "export const h = 1\n")
	w("src/features/Login/index.tsx", "import { requestLogin } from '@/lib/auth/login'\nexport default function Login() {}\n")
	w("vpn/go.mod", "module npmnet\n")
	w("vpn/cmd/main.go", "package main\nimport (\n\t\"npmnet/internal/store\"\n)\nfunc main() {}\n")
	w("vpn/internal/store/store.go", "package store\nfunc NewToken() string { return \"\" }\ntype Store struct{}\n")
	w("docs/a.md", "# Auth\nsee [[b]] and [c](./c.md)\n")
	w("docs/b.md", "# B\n")
	w("docs/c.md", "# C\n")
	w("node_modules/x/index.js", "module.exports = 1")
	w(".gitignore", "node_modules/\n")
	g, err := s.BuildGraph("")
	if err != nil {
		t.Fatal(err)
	}
	if g.Files != 9 {
		t.Fatalf("files %d (node_modules must be skipped, tsconfig is config)", g.Files)
	}
	edges := map[string]bool{}
	for _, e := range g.Edges {
		edges[e.From+"->"+e.To] = true
	}
	for _, want := range []string{
		"src/lib/auth/login.ts->src/lib/auth/session.ts",
		"src/lib/auth/login.ts->src/lib/auth/helpers.ts",
		"src/features/Login/index.tsx->src/lib/auth/login.ts",
		"vpn/cmd/main.go->vpn/internal/store/store.go",
		"docs/a.md->docs/b.md",
		"docs/a.md->docs/c.md",
	} {
		if !edges[want] {
			t.Fatalf("missing edge %s in %v", want, g.Edges)
		}
	}
	if len(g.Hubs) == 0 || g.Hubs[0].In < 1 {
		t.Fatalf("hubs %+v", g.Hubs)
	}
	hits := GraphQuery(g, "session", 5)
	if len(hits) == 0 || hits[0].Path != "src/lib/auth/session.ts" || len(hits[0].UsedBy) != 1 || hits[0].Symbols[0] != "createSession" {
		t.Fatalf("query: %+v", hits)
	}
	if _, ok := s.LoadGraph(); !ok {
		t.Fatal("graph.json must be cached")
	}
	if out := RenderGraph(g); !strings.Contains(out, "Hubs") || !strings.Contains(out, "src/lib") {
		t.Fatal(out)
	}
}
