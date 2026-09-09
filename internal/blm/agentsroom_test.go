package blm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestHelperProcess = fake AgentsRoom MCP: ตอบ initialize, memory_save (ช้า 200ms ให้เห็นว่าขนานจริง), memory_delete, memory_list
func TestHelperProcess(t *testing.T) {
	if os.Getenv("BLM_FAKE_MCP") != "1" {
		return
	}
	sc := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for sc.Scan() {
		line := append([]byte(nil), sc.Bytes()...)
		wg.Add(1)
		go func() { // server จริงเป็น node async — จำลองให้รับหลาย call พร้อมกัน
			defer wg.Done()
			handleFake(line, enc, &mu)
		}()
	}
	wg.Wait()
	os.Exit(0)
}

var (
	fakeMu       sync.Mutex
	fakeSaved    = map[string]string{"old-note": "2026-01-01T00:00:00Z"}
	fakeInflight int
)

func handleFake(line []byte, enc *json.Encoder, mu *sync.Mutex) {
	fakeMu.Lock()
	fakeInflight++
	fakeMu.Unlock()
	defer func() { fakeMu.Lock(); fakeInflight--; fakeMu.Unlock() }()
	{
		var m struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name string         `json:"name"`
				Args map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if json.Unmarshal(line, &m) != nil || m.ID == nil {
			return
		}
		switch m.Method {
		case "initialize":
			mu.Lock()
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": *m.ID, "result": map[string]any{"serverInfo": map[string]any{"name": "fake"}}})
			mu.Unlock()
		case "tools/call":
			if m.Params.Name == "memory_list" {
				fakeMu.Lock()
				var notes []map[string]string
				for n, at := range fakeSaved {
					notes = append(notes, map[string]string{"name": n, "folder": "features", "updatedAt": at})
				}
				fakeMu.Unlock()
				body, _ := json.Marshal(map[string]any{"notes": notes})
				mu.Lock()
				_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": *m.ID, "result": map[string]any{"content": []map[string]any{{"type": "text", "text": string(body)}}}})
				mu.Unlock()
				return
			}
			time.Sleep(200 * time.Millisecond)
			name := fmt.Sprint(m.Params.Args["name"])
			text := "saved " + name
			isErr := false
			if name == "boom" {
				text, isErr = "note boom rejected", true
			}
			if os.Getenv("AR_PROJECT_ID") == "" {
				text, isErr = "missing AR_PROJECT_ID env", true
			}
			// จำลอง AgentsRoom จริง: ตอบ ok แต่รับจริงเฉพาะเมื่อไม่มี call อื่นค้าง (ขนาน = เข้าตัวเดียว) · ลบ = เอาออก
			fakeMu.Lock()
			if !isErr {
				if m.Params.Name == "memory_delete" {
					delete(fakeSaved, name)
				} else if fakeInflight <= 1 || name == "a" {
					fakeSaved[name] = time.Now().UTC().Add(time.Second).Format(time.RFC3339)
				}
			}
			fakeMu.Unlock()
			mu.Lock()
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": *m.ID, "result": map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": isErr}})
			mu.Unlock()
		}
	}
}

func fakeEntry() mcpEntry {
	return mcpEntry{Command: os.Args[0], Args: []string{"-test.run=TestHelperProcess"}, Env: map[string]string{"BLM_FAKE_MCP": "1", "AR_PROJECT_ID": "p1"}}
}

func TestPushAllParallel(t *testing.T) {
	s, _, _ := tmpStore(t, BackendAgentsRoom)
	for _, n := range []string{"a", "b", "c", "d", "boom"} {
		_, _ = s.Save(Input{Name: n, Content: "x", HasContent: true, Description: "d"})
	}
	c, err := Connect(fakeEntry())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	pushed, deleted := s.PushAll(c, s.PlanSync(s.List()), "tester", "qa", []string{"old-note"}, false)
	ok := 0
	for _, r := range pushed {
		if r.Verified {
			ok++
		} else if r.Name != "boom" || !strings.Contains(r.Error, "rejected") {
			t.Fatalf("unexpected failure %+v", r)
		}
	}
	// ยิงทีละตัว (server จำลองรับเฉพาะเมื่อไม่มี call ค้าง) → ทุกตัวยกเว้น boom ต้องยืนยันได้ในรอบเดียว ไม่มี retry
	if ok != 4 || len(deleted) != 1 || !deleted[0].Verified {
		t.Fatalf("pushed %+v deleted %+v", pushed, deleted)
	}
	if left := s.List(); len(left) != 1 || left[0].Name != "boom" {
		t.Fatalf("only the failed note must remain: %+v", left)
	}
	// โหมดขนานกับ server จำลองที่รับทีละตัว: ต้องรายงานว่าไม่เข้า (ไม่ retry) และร่างยังอยู่
	_ = s.Delete("boom")
	for _, n := range []string{"p1", "p2", "p3"} {
		_, _ = s.Save(Input{Name: n, Content: "x", HasContent: true, Description: "d"})
	}
	t0 := time.Now()
	par, _ := s.PushAll(c, s.PlanSync(s.List()), "tester", "qa", nil, true)
	if time.Since(t0) > 900*time.Millisecond {
		t.Fatal("parallel mode must not serialise")
	}
	verified, kept := 0, 0
	for _, r := range par {
		if r.Verified {
			verified++
		} else if r.Error == "" {
			t.Fatalf("unverified item must carry an error: %+v", r)
		}
	}
	for _, n := range s.List() {
		if strings.HasPrefix(n.Name, "p") {
			kept++
		}
	}
	if verified >= len(par) || kept != len(par)-verified {
		t.Fatalf("parallel: verified %d kept %d of %d", verified, kept, len(par))
	}
}

func TestLoadAgentsRoomMCP(t *testing.T) {
	root := t.TempDir()
	if _, err := LoadAgentsRoomMCP(root); err == nil {
		t.Fatal("missing .mcp.json must error")
	}
	_ = os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{"mcpServers":{"AgentsRoom-MCP":{"command":"node","args":["x.cjs"],"env":{"AR_PROJECT_ID":"p"}}}}`), 0o644)
	e, err := LoadAgentsRoomMCP(root)
	if err != nil || e.Command != "node" || e.Env["AR_PROJECT_ID"] != "p" {
		t.Fatalf("%v %+v", err, e)
	}
}

func TestEditFromMirror(t *testing.T) {
	s, _, root := tmpStore(t, BackendAgentsRoom)
	dir := filepath.Join(root, ".agentsroom", "memory", "features")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "line-login.md"), []byte("---\nname: \"line-login\"\ndescription: \"Contains LINE\"\nfolder: \"features\"\ntags: [\"auth\"]\n---\n\n# LINE\n- rule one\n- rule two\n- rule three\n"), 0o644)
	n, err := s.Patch("line-login", "rule two", "rule 2 (changed)")
	if err != nil {
		t.Fatal(err)
	}
	if n.Mode != "replace" || n.Folder != "features" || n.Description != "Contains LINE" || len(n.Tags) != 1 || !strings.Contains(n.Content, "rule 2 (changed)") || strings.Contains(n.Content, "rule two") {
		t.Fatalf("draft wrong: %+v", n)
	}
	// แก้ครั้งที่สองต้องต่อจากร่างเดิม ไม่ใช่ mirror
	n2, err := s.Patch("line-login", "rule one", "rule 1")
	if err != nil || !strings.Contains(n2.Content, "rule 2 (changed)") || !strings.Contains(n2.Content, "rule 1") {
		t.Fatalf("second edit must build on the draft: %v %q", err, n2.Content)
	}
	if _, err := s.Patch("line-login", "nope", "x"); err == nil {
		t.Fatal("no match must error")
	}
	if _, err := s.Patch("unknown-note", "a", "b"); err == nil {
		t.Fatal("unknown target must error")
	}
	plan := s.PlanSync(s.List())
	if len(plan) != 1 || plan[0].MemorySave.Mode != "replace" || plan[0].MemorySave.Folder != "features" {
		t.Fatalf("plan %+v", plan)
	}
}
