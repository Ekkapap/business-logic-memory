package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ekkapap/business-logic-memory/internal/blm"
)

func TestInitPresetsIdempotentAndMigrate(t *testing.T) {
	for _, tc := range []struct {
		flag  string
		store string
	}{{"", ".claude/blm"}, {"--agentsroom", ".agentsroom/blm"}, {"--obsidian", "blm"}} {
		root := t.TempDir()
		_ = os.MkdirAll(filepath.Join(root, ".git"), 0o755)
		// ของเดิมชื่อ memory-temp รอย้าย (เฉพาะ agentsroom)
		if tc.flag == "--agentsroom" {
			old := filepath.Join(root, ".agentsroom", "memory-temp")
			_ = os.MkdirAll(old, 0o755)
			_ = os.WriteFile(filepath.Join(old, "project-business-logic.md"), []byte("---\ntarget: project-business-logic\nmode: replace\n---\n\n# A\n"), 0o644)
			_ = os.WriteFile(filepath.Join(old, "x.md"), []byte("---\ntarget: project-business-logic\nmode: append\n---\n\nx\n"), 0o644)
		}
		args := []string{root}
		if tc.flag != "" {
			args = append(args, tc.flag)
		}
		o, err := ParseInit(args)
		if err != nil {
			t.Fatal(err)
		}
		var calls []string
		o.Run = func(cmd ...string) (string, error) { calls = append(calls, strings.Join(cmd, " ")); return "", nil }
		log := Init(o)
		c := blm.Load(root)
		if c.Store != tc.store {
			t.Fatalf("%s: store %s", tc.flag, c.Store)
		}
		var settings map[string]any
		raw, _ := os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
		_ = json.Unmarshal(raw, &settings)
		deny := settings["permissions"].(map[string]any)["deny"].([]any)
		if len(deny) != 3 || deny[0] != "Edit("+tc.store+"/**)" {
			t.Fatalf("deny %v", deny)
		}
		if hooks := settings["hooks"].(map[string]any)["PreToolUse"].([]any); len(hooks) != 1 {
			t.Fatalf("hooks %v", hooks)
		}
		Init(o) // รันซ้ำต้องไม่เพิ่มรายการซ้ำ
		raw, _ = os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
		_ = json.Unmarshal(raw, &settings)
		if hooks := settings["hooks"].(map[string]any)["PreToolUse"].([]any); len(hooks) != 1 {
			t.Fatalf("hooks ซ้ำ %v", hooks)
		}
		if tc.flag == "--agentsroom" {
			if _, err := os.Stat(filepath.Join(root, tc.store, "blm.md")); err != nil {
				t.Fatalf("migrate ต้องเปลี่ยนชื่อไฟล์กฎ: %v", err)
			}
			b, _ := os.ReadFile(filepath.Join(root, tc.store, "x.md"))
			if !strings.Contains(string(b), "target: blm") {
				t.Fatalf("migrate target: %s", b)
			}
			if !strings.Contains(strings.Join(log, "\n"), "migrate") {
				t.Fatal(log)
			}
		}
		if !strings.Contains(strings.Join(calls, "\n"), "claude plugin install blm@blm") {
			t.Fatalf("plugin install: %v", calls)
		}
	}
	// รัน init ซ้ำโดยไม่ระบุ backend ต้องคงของเดิม
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	o, _ := ParseInit([]string{root, "--agentsroom"})
	o.Run = func(cmd ...string) (string, error) { return "", nil }
	Init(o)
	o2, _ := ParseInit([]string{root})
	o2.Run = o.Run
	Init(o2)
	if got := blm.Load(root); got.Backend != blm.BackendAgentsRoom {
		t.Fatalf("re-init changed backend to %s", got.Backend)
	}
	// ไม่ระบุ backend แต่มี .agentsroom/ → agentsroom
	root2 := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root2, ".agentsroom", "memory"), 0o755)
	o3, _ := ParseInit([]string{root2})
	o3.Run = o.Run
	Init(o3)
	if got := blm.Load(root2); got.Backend != blm.BackendAgentsRoom || got.Store != ".agentsroom/blm" {
		t.Fatalf("auto-detect agentsroom failed: %+v", got)
	}
	// โฟลเดอร์เปล่าไม่ใช่โปรเจ็ค → ปฏิเสธ เว้นแต่ --force
	empty := t.TempDir()
	o4, _ := ParseInit([]string{empty})
	if log := Init(o4); !strings.HasPrefix(log[0], "stop") || exists(filepath.Join(empty, blm.ConfigFile)) {
		t.Fatalf("empty dir must be refused: %v", log)
	}
	o5, _ := ParseInit([]string{empty, "--force"})
	o5.Run = o.Run
	if log := Init(o5); strings.HasPrefix(log[0], "stop") {
		t.Fatalf("--force must proceed: %v", log)
	}
	if _, err := ParseInit([]string{"--backend", "x"}); err == nil {
		t.Fatal("--backend ต้องคู่ --dir")
	}
}

func TestGuardDenies(t *testing.T) {
	store := ".agentsroom/blm"
	for _, cmd := range []string{
		"rm -rf .agentsroom/blm/x.md",
		"echo hi > .agentsroom/blm/notes.md",
		"python3 -c 'open(\".agentsroom/blm/a.md\",\"w\")'",
		"sed -i '' 's/a/b/' .agentsroom/blm/a.md",
		"mv .agentsroom/blm.md /tmp/x",
		"rm -rf .agentsroom/blm",
	} {
		if !Denies(store, cmd) {
			t.Errorf("ต้อง deny: %s", cmd)
		}
	}
	for _, cmd := range []string{
		"cat .agentsroom/blm/x.md",
		"grep -r foo .agentsroom/blm",
		"blm status",
		"echo 'talking about .agentsroom/blm' > /tmp/out.txt",
		"ls .agentsroom/blm",
		"python3 -c 'import json' .claude/blm.json",
		"echo x > .agentsroom/blm-notes.txt",
	} {
		if Denies(store, cmd) {
			t.Errorf("ต้องผ่าน: %s", cmd)
		}
	}
	var out strings.Builder
	_ = Guard(t.TempDir(), strings.NewReader(`{"tool_input":{"command":"rm .claude/blm/a.md"}}`), &out)
	if !strings.Contains(out.String(), `"permissionDecision":"deny"`) {
		t.Fatal(out.String())
	}
}
