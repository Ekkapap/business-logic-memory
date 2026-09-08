package blm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mirrorNote(t *testing.T, root, name, body, updated string) string {
	dir := filepath.Join(root, ".agentsroom", "memory", "features")
	_ = os.MkdirAll(dir, 0o755)
	p := filepath.Join(dir, name+".md")
	_ = os.WriteFile(p, []byte("---\nname: \""+name+"\"\ndescription: \"d\"\nfolder: \"features\"\nupdatedAt: \""+updated+"\"\n---\n\n"+body), 0o644)
	return p
}

func TestDiffMergeThreeWay(t *testing.T) {
	s, _, root := tmpStore(t, BackendAgentsRoom)
	p := mirrorNote(t, root, "auth", "line1\nline2\nline3\nline4\n", "2026-09-01T00:00:00Z")
	if _, _, err := s.Edit("auth", "line4", "line4 mine"); err != nil {
		t.Fatal(err)
	}
	d, err := s.Diff("auth")
	if err != nil || d.CloudChanged || d.MineLines != 2 || !strings.Contains(d.DiffMine, "+ line4 mine") {
		t.Fatalf("clean diff: %v %+v", err, d)
	}
	// cloud เปลี่ยนคนละบรรทัด → merge mine ได้ไม่ชน
	_ = os.WriteFile(p, []byte("---\nname: \"auth\"\nfolder: \"features\"\nupdatedAt: \"2026-09-02T00:00:00Z\"\n---\n\nline1 cloud\nline2\nline3\nline4\n"), 0o644)
	d, _ = s.Diff("auth")
	if !d.CloudChanged || d.Overlap || d.CloudLines != 2 {
		t.Fatalf("cloud changed elsewhere: %+v", d)
	}
	out, err := s.Merge("auth", "mine", "")
	if err != nil {
		t.Fatal(err)
	}
	n, _ := s.Get("auth")
	if !strings.Contains(n.Content, "line1 cloud") || !strings.Contains(n.Content, "line4 mine") || n.Base != "2026-09-02T00:00:00Z" {
		t.Fatalf("merged: %q base %s %v", n.Content, n.Base, out)
	}
	// cloud เปลี่ยนบรรทัดเดียวกัน → overlap, merge mine ต้องปฏิเสธ, keep content ผ่าน
	_ = os.WriteFile(p, []byte("---\nname: \"auth\"\nfolder: \"features\"\nupdatedAt: \"2026-09-03T00:00:00Z\"\n---\n\nline1 cloud\nline2\nline3\nline4 cloud\n"), 0o644)
	d, _ = s.Diff("auth")
	if !d.CloudChanged || !d.Overlap {
		t.Fatalf("overlap expected: %+v", d)
	}
	if _, err := s.Merge("auth", "mine", ""); err == nil {
		t.Fatal("overlapping merge must refuse")
	}
	if _, err := s.Merge("auth", "content", "line1 cloud\nline2\nline3\nline4 both\n"); err != nil {
		t.Fatal(err)
	}
	n, _ = s.Get("auth")
	if !strings.Contains(n.Content, "line4 both") || n.Base != "2026-09-03T00:00:00Z" {
		t.Fatalf("manual merge: %+v", n)
	}
	// keep cloud = ทิ้งร่าง (มี history)
	if _, err := s.Merge("auth", "cloud", ""); err != nil || s.Has("auth") {
		t.Fatal("keep cloud must drop the draft")
	}
	// plan ต้องพก base ไปให้ push ตรวจ conflict
	_, _, _ = s.Edit("auth", "line2", "line2 x")
	plan := s.PlanSync(s.List())
	if len(plan) != 1 || plan[0].Base != "2026-09-03T00:00:00Z" {
		t.Fatalf("plan base: %+v", plan)
	}
}

func TestPushSkipsConflicts(t *testing.T) {
	s, _, root := tmpStore(t, BackendAgentsRoom)
	mirrorNote(t, root, "old-note", "a\nb\n", "2026-01-01T00:00:00Z")
	_, _, err := s.Edit("old-note", "b", "b mine")
	if err != nil {
		t.Fatal(err)
	}
	// fake server รายงาน old-note updatedAt 2026-01-01 (= base) → ไม่ชน · ตั้ง base ให้เก่ากว่าเพื่อจำลองว่า cloud เปลี่ยน
	n, _ := s.Get("old-note")
	_, _ = s.save(Input{Name: n.Name, Target: n.Target, Mode: "replace", Content: n.Content, HasContent: true, Base: "2025-12-31T00:00:00Z", Folder: "features", Description: "d"}, "patch")
	c, err := Connect(fakeEntry())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	pushed, _ := s.PushAll(c, s.PlanSync(s.List()), "t", "qa", nil, false)
	if len(pushed) != 1 || pushed[0].Verified || !strings.Contains(pushed[0].Error, "conflict") || !s.Has("old-note") {
		t.Fatalf("conflict must skip and keep the draft: %+v", pushed)
	}
}
