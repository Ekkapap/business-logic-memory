package blm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConflictWorkflow(t *testing.T) {
	s, _, root := tmpStore(t, BackendAgentsRoom)
	p := mirrorNote(t, root, "blm", "# Authentication\n\n## LINE Login\n- rule one\n- rule two\nmemory: x\n", "2026-09-01T00:00:00Z")
	if _, _, err := s.Edit("blm", "- rule two", "- rule two (mine)"); err != nil {
		t.Fatal(err)
	}
	// cloud แก้บรรทัดเดียวกัน → ชน
	_ = os.WriteFile(p, []byte("---\nname: \"blm\"\nfolder: \"features\"\nupdatedAt: \"2026-09-02T00:00:00Z\"\n---\n\n# Authentication\n\n## LINE Login\n- rule one\n- rule two (cloud)\nmemory: x\n"), 0o644)
	reports, err := s.OpenConflict("blm", "Authentication", "LINE Login", "cloud says cloud, draft says mine")
	if err != nil || len(reports) != 1 || reports[0].Status != "wait" || !strings.Contains(reports[0].File, "/[wait] line-login-20") {
		t.Fatalf("open: %v %+v", err, reports)
	}
	d, _ := s.Get("blm")
	if !strings.Contains(d.Content, "# Authentication [Conflict: #1](conflicts/%5Bwait%5D%20line-login-20") || !strings.Contains(d.Content, "## LINE Login [Conflict: #1](conflicts/") {
		t.Fatalf("main and sub topic must be tagged: %q", d.Content)
	}
	if r := s.Rules("", "agent"); len(r.Conflicts) != 1 {
		t.Fatalf("rules must surface conflicts: %+v", r.Conflicts)
	}
	// ยังไม่ติ๊ก → ร่างถูกเขียนด้วยฝั่ง B ชั่วคราว ป้ายค้าง และ pending บอก #1
	res, err := s.ResolveConflicts("blm")
	if err != nil || res["ok"] != false || len(res["pending"].([]string)) != 1 {
		t.Fatalf("pending expected: %v %v", err, res)
	}
	if d, _ := s.Get("blm"); !strings.Contains(d.Content, "[Conflict: #1]") || strings.Contains(d.Content, "<<<<<<<") {
		t.Fatalf("pending draft must keep the tag and no markers: %q", d.Content)
	}
	// เจ้าของแก้ block B แล้วติ๊ก
	file := filepath.Join(root, reports[0].File)
	raw, _ := os.ReadFile(file)
	if slug("2026-09-09 — blm_graph reads SocratiCode's graph") != "2026-09-09-blm-graph" {
		t.Fatalf("slug must cut at a word boundary: %q", slug("2026-09-09 — blm_graph reads SocratiCode's graph"))
	}
	if !strings.Contains(string(raw), "## ต่างกันตรงไหน") || !strings.Contains(string(raw), "> - rule two (mine)") || len(filepath.Base(reports[0].File)) > 48 {
		t.Fatalf("report must lead with the A/B difference and have a short name: %s\n%s", reports[0].File, raw)
	}
	// ยื่นซ้ำสำหรับร่างเดิม → แทนรายงานเก่า id เดิม ไม่งอกเป็น #2
	if again, err := s.OpenConflict("blm", "Authentication", "LINE Login", "again"); err != nil || len(again) != 1 || again[0].ID != 1 || len(s.ListConflicts()) != 1 {
		t.Fatalf("re-open must replace: %v %+v", err, again)
	}
	raw, _ = os.ReadFile(file)
	edited := strings.ReplaceAll(string(raw), "- rule two (mine)", "- rule two (owner edited)")
	edited = strings.Replace(edited, "- [ ] เอาฝั่ง **B**", "- [x] เอาฝั่ง **B**", 1)
	_ = os.WriteFile(file, []byte(edited), 0o644)
	if list := s.ListConflicts(); list[0].Chosen != "B" {
		t.Fatalf("chosen %+v", list)
	}
	res, err = s.ResolveConflicts("blm")
	if err != nil || res["ok"] != true {
		t.Fatalf("resolve: %v %v", err, res)
	}
	d, _ = s.Get("blm")
	if !strings.Contains(d.Content, "- rule two (owner edited)") || strings.Contains(d.Content, "[Conflict") || strings.Contains(d.Content, "<<<<<<<") || d.Base != "2026-09-02T00:00:00Z" {
		t.Fatalf("resolved draft: %q base %s", d.Content, d.Base)
	}
	list := s.ListConflicts()
	if len(list) != 1 || list[0].Status != "done" || !strings.Contains(list[0].File, "/[done] line-login-20") {
		t.Fatalf("done expected: %+v", list)
	}
	if _, err := os.Stat(filepath.Join(s.conflictsDir(), "blm.merged.md")); err == nil {
		t.Fatal("merged snapshot must be removed after resolve")
	}
}

func TestConflictOnOtherNoteTagsRules(t *testing.T) {
	s, _, root := tmpStore(t, BackendAgentsRoom)
	mirrorNote(t, root, "blm", "# Authentication\n\n## LINE Login\n- rule one\n\n# Portal\n\n## LINE Login\n- other\n", "2026-09-01T00:00:00Z")
	p := mirrorNote(t, root, "line-login", "# LINE Login note\n\n## Flow\n- step a\n- step b\n", "2026-09-01T00:00:00Z")
	if _, _, err := s.Edit("line-login", "- step b", "- step b (mine)"); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(p, []byte("---\nname: \"line-login\"\nfolder: \"features\"\nupdatedAt: \"2026-09-02T00:00:00Z\"\n---\n\n# LINE Login note\n\n## Flow\n- step a\n- step b (cloud)\n"), 0o644)
	reports, err := s.OpenConflict("line-login", "Authentication", "LINE Login", "ทดสอบ")
	if err != nil || len(reports) != 1 {
		t.Fatalf("open: %v %+v", err, reports)
	}
	rules, _ := s.Get("blm")
	if strings.Count(rules.Content, "[Conflict: #1](conflicts/") != 2 || !strings.Contains(rules.Content, "# Authentication [Conflict: #1](") || strings.Contains(rules.Content, "# Portal [Conflict") {
		t.Fatalf("blm.md must be tagged at Authentication › LINE Login only: %q", rules.Content)
	}
	if d, _ := s.Get("line-login"); !strings.Contains(d.Content, "## Flow [Conflict: #1](conflicts/") || strings.Contains(d.Content, "# LINE Login note [Conflict") {
		t.Fatalf("the note must be tagged at the heading of the clashing region: %q", d.Content)
	}
	if reports[0].NoteHeading != "## Flow" {
		t.Fatalf("noteHeading: %+v", reports[0])
	}
	file := filepath.Join(root, reports[0].File)
	raw, _ := os.ReadFile(file)
	_ = os.WriteFile(file, []byte(strings.Replace(string(raw), "- [ ] เอาฝั่ง **A**", "- [x] เอาฝั่ง **A**", 1)), 0o644)
	if res, err := s.ResolveConflicts("line-login"); err != nil || res["ok"] != true {
		t.Fatalf("resolve: %v %v", err, res)
	}
	rules, _ = s.Get("blm")
	if strings.Contains(rules.Content, "[Conflict") {
		t.Fatalf("tags must be gone after resolve: %q", rules.Content)
	}
	if d, _ := s.Get("line-login"); !strings.Contains(d.Content, "- step b (cloud)") || strings.Contains(d.Content, "[Conflict") {
		t.Fatalf("A chosen: %q", d.Content)
	}
}
