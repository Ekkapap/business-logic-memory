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
	if err != nil || len(reports) != 1 || reports[0].Status != "wait" || !strings.HasSuffix(reports[0].File, "1-authentication-line-login.wait.md") {
		t.Fatalf("open: %v %+v", err, reports)
	}
	d, _ := s.Get("blm")
	if !strings.Contains(d.Content, "## LINE Login [Conflict: #1]") {
		t.Fatalf("heading must be tagged: %q", d.Content)
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
	edited := strings.Replace(string(raw), "- rule two (mine)", "- rule two (owner edited)", 1)
	bIdx := strings.Index(edited, "## Block B")
	edited = edited[:bIdx] + strings.Replace(edited[bIdx:], "- [ ] เลือก block นี้", "- [x] เลือก block นี้", 1)
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
	if len(list) != 1 || list[0].Status != "done" || !strings.HasSuffix(list[0].File, ".done.md") {
		t.Fatalf("done expected: %+v", list)
	}
	if _, err := os.Stat(filepath.Join(s.conflictsDir(), "blm.merged.md")); err == nil {
		t.Fatal("merged snapshot must be removed after resolve")
	}
}
