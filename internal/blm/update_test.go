package blm

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateReportCoverageCandidatesAndChanges(t *testing.T) {
	root := t.TempDir()
	c := Config{Backend: BackendNone, Store: ".claude/blm"}
	_ = os.MkdirAll(filepath.Join(root, c.Store), 0o755)
	_ = os.WriteFile(filepath.Join(root, c.Store, "blm.md"), []byte("---\ntarget: blm\n---\n\n## Main Business\n| Topic | cue |\n| --- | --- |\n| [Auth](blm/blm-auth.md) | login |\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, c.Store, "blm-auth.md"), []byte("---\ntarget: blm-auth\n---\n\n# Auth\n\n## Password\n- 8 chars\nmemory: -\ncode: [login.ts](src/lib/login.ts), src/app/api/login/route.ts\nupdated_at: 2020-01-01\n"), 0o644)
	g := Graph{BuiltAt: "x", Engine: "regex",
		Nodes:    []GraphNode{{Path: "src/lib/login.ts"}, {Path: "src/other/pay.ts"}, {Path: "docs/guide.md"}, {Path: "src/app/api/login/route.ts"}},
		Clusters: []GraphCluster{{Dir: "src/lib", Files: 1, Symbols: 3}, {Dir: "src/other", Files: 1, Symbols: 9, Top: []string{"charge"}}, {Dir: "docs", Files: 1, Symbols: 2}}}
	raw, _ := json.Marshal(g)
	_ = os.WriteFile(filepath.Join(root, c.Store, "graph.json"), raw, 0o644)
	// git: ไฟล์ที่เปลี่ยนหลัง 2020 — pay.ts (no rule) และ login.ts (covered)
	for _, f := range []string{"src/other/pay.ts", "src/lib/login.ts", "README.md", "src/other/README.md"} {
		_ = os.MkdirAll(filepath.Join(root, filepath.Dir(f)), 0o755)
		_ = os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "c"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	rep, err := Open(root, c).UpdateReport("", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Topics) != 1 || rep.Topics[0].Note != "blm-auth" || strings.Join(rep.Topics[0].Dirs, ",") != "src/app,src/lib" || rep.Topics[0].Oldest != "2020-01-01" {
		t.Fatalf("topics: %+v", rep.Topics)
	}
	if len(rep.Candidates) != 1 || rep.Candidates[0].Dir != "src/other" || rep.Candidates[0].Changed != 1 { // README.md ใน src/other ไม่นับ
		t.Fatalf("candidates (docs-only and covered clusters must be out): %+v", rep.Candidates)
	}
	if rep.Since == "" || !strings.Contains(rep.SinceFrom, "30 days") || len(rep.Changed) != 2 || rep.Changed[0].Dir != "src/other" || rep.Changed[0].Covered || !rep.Changed[1].Covered || strings.Join(rep.Changed[1].Rules, ",") != "Auth › Password" {
		t.Fatalf("changed: since=%s %+v", rep.Since, rep.Changed)
	}
	for _, want := range []string{"src/other", "no rule", "rules: Auth › Password", "not for this page", "next:"} {
		if !strings.Contains(rep.Terminal, want) {
			t.Fatalf("terminal missing %q:\n%s", want, rep.Terminal)
		}
	}
}

func TestLastUpdateMarkerIsTheCutoff(t *testing.T) {
	root := t.TempDir()
	c := Config{Backend: BackendNone, Store: ".claude/blm"}
	s := Open(root, c)
	if err := s.MarkUpdate("blm init"); err != nil {
		t.Fatal(err)
	}
	at, from := s.LastUpdate()
	if len(at) != 10 || !strings.HasPrefix(from, "blm init ") {
		t.Fatalf("marker: %q %q", at, from)
	}
}
