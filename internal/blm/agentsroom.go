package blm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AgentsRoom MCP client — blm spawn `app-mcp-server.cjs` ของ AgentsRoom เอง (คำสั่ง+env จาก <project>/.mcp.json)
// แล้วยิง memory_save/memory_delete ตรง ๆ ทาง stdio JSON-RPC
// ทำไม: เจ้าของกำหนด 2026-09-09 ว่าเนื้อโน้ตต้องไม่วิ่งผ่าน context ของ agent — ทุกอย่างทำ local แล้ว save ทั้งก้อนจากที่นี่
// พิสูจน์แล้ว 2026-09-09 ว่า server ตอบเมื่อได้ AR_PROJECT_ID / AR_PROJECT_PATH / AR_SETTINGS_FILE จาก .mcp.json

type mcpEntry struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// LoadAgentsRoomMCP อ่านรายการ AgentsRoom-MCP จาก .mcp.json ของโปรเจ็ค
func LoadAgentsRoomMCP(root string) (mcpEntry, error) {
	raw, err := os.ReadFile(filepath.Join(root, ".mcp.json"))
	if err != nil {
		return mcpEntry{}, fmt.Errorf("no .mcp.json in project — open the project in AgentsRoom once so it writes the AgentsRoom-MCP entry")
	}
	var f struct {
		Servers map[string]mcpEntry `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return mcpEntry{}, err
	}
	e, ok := f.Servers["AgentsRoom-MCP"]
	if !ok || e.Command == "" {
		return mcpEntry{}, fmt.Errorf(".mcp.json has no AgentsRoom-MCP server entry")
	}
	return e, nil
}

type rpcReply struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Client หนึ่ง process ของ AgentsRoom MCP ใช้ยิงหลาย call พร้อมกัน (id แยก reader goroutine เดียว)
type Client struct {
	cmd     *exec.Cmd
	in      *json.Encoder
	mu      sync.Mutex
	nextID  int
	pending map[int]chan rpcReply
	Timeout time.Duration
}

// Connect spawn server และ initialize · Timeout ต่อ call ค่าเริ่ม 180 วิ (AgentsRoom เคยช้าเกิน 2 นาที)
func Connect(e mcpEntry) (*Client, error) {
	cmd := exec.Command(e.Command, e.Args...)
	cmd.Env = os.Environ()
	for k, v := range e.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stderr = nil
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("cannot start AgentsRoom MCP (%s): %w", e.Command, err)
	}
	c := &Client{cmd: cmd, in: json.NewEncoder(stdin), pending: map[int]chan rpcReply{}, Timeout: 180 * time.Second}
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<20), 64<<20)
		for sc.Scan() {
			var m struct {
				ID *int `json:"id"`
				rpcReply
			}
			if json.Unmarshal(sc.Bytes(), &m) != nil || m.ID == nil {
				continue
			}
			c.mu.Lock()
			ch := c.pending[*m.ID]
			delete(c.pending, *m.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- m.rpcReply
			}
		}
		// server ปิด → ปลดทุก call ที่รออยู่
		c.mu.Lock()
		for id, ch := range c.pending {
			ch <- rpcReply{Error: &struct {
				Message string `json:"message"`
			}{"AgentsRoom MCP exited before answering"}}
			delete(c.pending, id)
		}
		c.mu.Unlock()
	}()
	if _, err := c.request("initialize", map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "blm", "version": Version}}); err != nil {
		c.Close()
		return nil, err
	}
	c.mu.Lock()
	_ = c.in.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	c.mu.Unlock()
	return c, nil
}

func (c *Client) request(method string, params any) (json.RawMessage, error) {
	ch := make(chan rpcReply, 1)
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.pending[id] = ch
	err := c.in.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		if r.Error != nil {
			return nil, fmt.Errorf("%s", r.Error.Message)
		}
		return r.Result, nil
	case <-time.After(c.Timeout):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("%s timed out after %s", method, c.Timeout)
	}
}

// CallTool เรียก tools/call แล้วคืนข้อความตอบ (content[0].text) · isError จาก server = error
func (c *Client) CallTool(name string, args map[string]any) (string, error) {
	res, err := c.request("tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return "", err
	}
	var r struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	_ = json.Unmarshal(res, &r)
	text := ""
	if len(r.Content) > 0 {
		text = r.Content[0].Text
	}
	if r.IsError {
		return text, fmt.Errorf("%s", text)
	}
	return text, nil
}

func (c *Client) Close() {
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_, _ = c.cmd.Process.Wait()
	}
}

// PushResult ผลของหนึ่งรายการในแผน
type PushResult struct {
	Name     string   `json:"name"`
	Mode     string   `json:"mode"`
	From     []string `json:"from"`
	OK       bool     `json:"ok"`
	Ms       int64    `json:"ms"`
	Error    string   `json:"error,omitempty"`
	Archived []string `json:"archived,omitempty"`
	// Response ข้อความที่ server ตอบ (ตัดสั้น) — 2026-09-09 พบว่า server ตอบ "ok" ทั้งที่ไม่ได้เขียน (ยิงขนาน 6 ตัว เข้าจริง 1)
	Response string `json:"response,omitempty"`
	// Verified = memory_list หลัง push ยืนยันว่า updatedAt ใหม่กว่าตอนเริ่ม (หรือโน้ตหายไปแล้วสำหรับ delete)
	Verified bool `json:"verified"`
}

// ListedNote รายการจาก memory_list ที่ใช้ตรวจสอบ
type ListedNote struct {
	Name      string `json:"name"`
	Folder    string `json:"folder"`
	UpdatedAt string `json:"updatedAt"`
}

// ListNotes เรียก memory_list แล้วคืน map ชื่อ → โน้ต (ใช้ยืนยันผล push — เชื่อ server ตอบ ok อย่างเดียวไม่ได้)
func (c *Client) ListNotes() (map[string]ListedNote, error) {
	text, err := c.CallTool("memory_list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var d struct {
		Notes []ListedNote `json:"notes"`
	}
	if err := json.Unmarshal([]byte(text), &d); err != nil {
		return nil, fmt.Errorf("memory_list returned non-JSON: %.120s", text)
	}
	out := map[string]ListedNote{}
	for _, n := range d.Notes {
		out[n.Name] = n
	}
	return out, nil
}

func saveArgs(item SyncItem, author, role string) map[string]any {
	args := map[string]any{"name": item.MemorySave.Name, "mode": item.MemorySave.Mode, "content": item.MemorySave.Content, "author": author, "role": role, "scope": "project"}
	if item.MemorySave.Folder != "" {
		args["folder"] = item.MemorySave.Folder
	}
	if item.MemorySave.Description != "" {
		args["description"] = item.MemorySave.Description
	}
	if len(item.MemorySave.Tags) > 0 {
		args["tags"] = item.MemorySave.Tags
	}
	return args
}

func short(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

// PushAll ยิง memory_save ทีละตัว (ค่าเริ่ม) หรือขนานเมื่อ parallel=true — AgentsRoom เวอร์ชันที่ติดตั้ง 2026-09-09 รับขนานได้ตัวเดียว
// (ตอบ ok แต่ไม่เขียน) เจ้าของแจ้งว่าเวอร์ชันใหม่แก้แล้ว จึงคง flag ไว้เปิดทีหลัง · ตรวจด้วย memory_list ครั้งเดียวตอนจบ ไม่ retry ไม่วน:
// ตัวที่ไม่เข้ารายงานพร้อมข้อความของ server แล้วปล่อยร่างไว้ใน store · archive เฉพาะที่ยืนยันแล้ว · deletes ยืนยันด้วยการหายจาก list
func (s *Store) PushAll(c *Client, plan []SyncItem, author, role string, deletes []string, parallel bool) ([]PushResult, []PushResult) {
	start := time.Now().UTC().Add(-2 * time.Second).Format(time.RFC3339)
	results := make([]PushResult, len(plan))
	deleted := make([]PushResult, len(deletes))
	save := func(i int, item SyncItem) {
		t := time.Now()
		text, err := c.CallTool("memory_save", saveArgs(item, author, role))
		r := &results[i]
		r.Name, r.Mode, r.From = item.MemorySave.Name, item.MemorySave.Mode, item.From
		r.Ms = time.Since(t).Milliseconds()
		r.Response = short(text)
		r.OK = err == nil
		if err != nil {
			r.Error = err.Error()
		}
	}
	del := func(i int, name string) {
		t := time.Now()
		text, err := c.CallTool("memory_delete", map[string]any{"name": name})
		r := &deleted[i]
		r.Name, r.Mode = name, "delete"
		r.Ms = time.Since(t).Milliseconds()
		r.Response = short(text)
		r.OK = err == nil
		if err != nil {
			r.Error = err.Error()
		}
	}
	if parallel {
		var wg sync.WaitGroup
		for i, item := range plan {
			wg.Add(1)
			go func(i int, item SyncItem) { defer wg.Done(); save(i, item) }(i, item)
		}
		for i, name := range deletes {
			wg.Add(1)
			go func(i int, name string) { defer wg.Done(); del(i, name) }(i, name)
		}
		wg.Wait()
	} else {
		for i, item := range plan {
			save(i, item)
		}
		for i, name := range deletes {
			del(i, name)
		}
	}
	listed, err := c.ListNotes()
	if err != nil {
		for i := range results {
			if results[i].Error == "" {
				results[i].Error = "verify failed: " + err.Error()
			}
			results[i].OK = false
		}
		return results, deleted
	}
	for i := range results {
		n, ok := listed[results[i].Name]
		results[i].Verified = ok && n.UpdatedAt >= start
		if !results[i].Verified {
			results[i].OK = false
			if results[i].Error == "" {
				results[i].Error = "server answered but memory_list shows no change (updatedAt " + n.UpdatedAt + ") — draft kept in store"
			}
			continue
		}
		for _, from := range results[i].From {
			if p, err := s.Archive(from); err == nil {
				results[i].Archived = append(results[i].Archived, p)
			}
		}
	}
	for i := range deleted {
		_, still := listed[deleted[i].Name]
		deleted[i].Verified = !still
		if still {
			deleted[i].OK = false
			if deleted[i].Error == "" {
				deleted[i].Error = "server answered but the note is still listed"
			}
		}
	}
	return results, deleted
}

// Unarchive ย้ายไฟล์จาก .synced/ กลับเข้า store (ใช้กู้รายการที่ถูก archive ทั้งที่ยังไม่เข้า backend)
// name = ชื่อไฟล์ใน .synced (มี stamp) หรือชื่อโน้ต (เลือกไฟล์ล่าสุดที่ลงท้ายด้วย -<name>.md)
func (s *Store) Unarchive(name string) (string, error) {
	dir := filepath.Join(s.Dir, ".synced")
	entries, _ := os.ReadDir(dir)
	var pick string
	for _, e := range entries {
		if e.Name() == name || strings.HasSuffix(e.Name(), "-"+name+".md") {
			if e.Name() > pick {
				pick = e.Name()
			}
		}
	}
	if pick == "" {
		return "", fmt.Errorf("no archived note %q in .synced", name)
	}
	note := pick
	if i := strings.LastIndex(pick, "Z-"); i >= 0 {
		note = pick[i+2:]
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return "", err
	}
	return note, os.Rename(filepath.Join(dir, pick), filepath.Join(s.Dir, note))
}
