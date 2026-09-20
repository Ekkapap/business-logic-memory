package blm

import (
	"io"
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
	// LogFile — stderr ของ server ทั้งหมดถูก append ลงไฟล์นี้ด้วย (เจ้าของ 2026-09-20: ห้าม error เงียบ ทุกโปรเซสต้องมี log)
	LogFile string `json:"-"`
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
	stdin   io.WriteCloser
	stderr  *tailBuf // บรรทัดท้าย ๆ ของ stderr — แนบใน error เมื่อ server ตายก่อนตอบ (เช่น npm EPERM) แทนที่จะเงียบ
	in      *json.Encoder
	mu      sync.Mutex
	nextID  int
	pending map[int]chan rpcReply
	Timeout time.Duration
}

// Connect spawn server และ initialize · Timeout ต่อ call ค่าเริ่ม 180 วิ (AgentsRoom เคยช้าเกิน 2 นาที)
func Connect(e mcpEntry) (*Client, error) {
	cmd := exec.Command(e.Command, e.Args...)
	setProcessGroup(cmd) // ลูกอยู่ใน process group ของตัวเอง → Close/Ctrl-C ฆ่าทั้งกลุ่ม (npx → node ลูกหลาน) ไม่เหลือ orphan index ต่อ
	cmd.Env = os.Environ()
	for k, v := range e.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	tb := &tailBuf{}
	cmd.Stderr = tb
	if e.LogFile != "" {
		_ = os.MkdirAll(filepath.Dir(e.LogFile), 0o755)
		if f, err := os.OpenFile(e.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintf(f, "\n=== %s %s %s\n", time.Now().Format(time.RFC3339), e.Command, strings.Join(e.Args, " "))
			cmd.Stderr = io.MultiWriter(tb, f)
			tb.closer = f
		}
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("cannot start MCP server (%s): %w", e.Command, err)
	}
	c := &Client{cmd: cmd, stdin: stdin, stderr: tb, in: json.NewEncoder(stdin), pending: map[int]chan rpcReply{}, Timeout: 180 * time.Second}
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
			}{"MCP server exited before answering" + c.stderr.tail()}}
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

// Close — ฆ่าทั้งกลุ่มทันที (2026-09-20: เคยเปลี่ยนเป็น "ปิด stdin รอ 3 วิ" แล้วพัง — 3 วินั้นพอให้ auto-resume ของ socraticode
// สร้าง collection ที่เพิ่ง remove กลับมาแล้วเริ่ม index ก่อนถูกฆ่า ทิ้งซากบน Qdrant · one-shot = ได้คำตอบแล้วจบ)
func (c *Client) Close() {
	if c.cmd.Process != nil {
		killProcessGroup(c.cmd)
		_ = c.cmd.Process.Kill()
		_, _ = c.cmd.Process.Wait()
	}
	if c.stderr != nil && c.stderr.closer != nil {
		_ = c.stderr.closer.Close()
	}
}

// StderrTail — บรรทัดท้าย ๆ ของ stderr ของ server (log ของ socraticode) ไว้โชว์ตอนดูเหมือนค้าง
func (c *Client) StderrTail() string { return strings.TrimPrefix(c.stderr.tail(), " — stderr: ") }

// PushResult ผลของหนึ่งรายการในแผน
type PushResult struct {
	Name     string   `json:"name"`
	Mode     string   `json:"mode"`
	From     []string `json:"from"`
	OK       bool     `json:"ok"`
	Ms       int64    `json:"ms"`
	Error    string   `json:"error,omitempty"`
	// Response ข้อความที่ server ตอบ (ตัดสั้น) — 2026-09-09 พบว่า server ตอบ "ok" ทั้งที่ไม่ได้เขียน (ยิงขนาน 6 ตัว เข้าจริง 1)
	Response string `json:"response,omitempty"` // เฉพาะตอนล้ม
	Folder   string `json:"folder,omitempty"`   // folder ที่ backend วางโน้ตจริง (อาจต่างจากที่ขอ)
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
	// ก่อนส่ง: ร่าง replace ที่มี base ต้องไม่เก่ากว่า cloud (คนอื่นแก้ระหว่างร่างค้าง) — ชนแล้วไม่ส่ง ให้เจ้าของดู blm_diff/blm_merge
	before, _ := c.ListNotes()
	skip := map[int]bool{}
	for i, item := range plan {
		if item.MemorySave.Mode != "replace" || item.Base == "" {
			continue
		}
		if n, ok := before[item.MemorySave.Name]; ok && n.UpdatedAt > item.Base {
			skip[i] = true
			results[i] = PushResult{Name: item.MemorySave.Name, Mode: item.MemorySave.Mode, From: item.From, Error: "conflict: cloud updated " + n.UpdatedAt + " after the draft base " + item.Base + " — run blm_diff then blm_merge; draft kept"}
		}
	}
	save := func(i int, item SyncItem) {
		t := time.Now()
		text, err := c.CallTool("memory_save", saveArgs(item, author, role))
		r := &results[i]
		r.Name, r.Mode, r.From = item.MemorySave.Name, item.MemorySave.Mode, item.From
		r.Ms = time.Since(t).Milliseconds()
		r.OK = err == nil
		if err != nil {
			r.Error = err.Error()
			r.Response = short(text) // ตอบกลับของ backend เฉพาะตอนล้ม (สำเร็จ = ok/verified/folder พอแล้ว — lean)
		} else {
			r.Folder = folderOf(text)
		}
	}
	del := func(i int, name string) {
		t := time.Now()
		text, err := c.CallTool("memory_delete", map[string]any{"name": name})
		r := &deleted[i]
		r.Name, r.Mode = name, "delete"
		r.Ms = time.Since(t).Milliseconds()
		r.OK = err == nil
		if err != nil {
			r.Error = err.Error()
			r.Response = short(text)
		}
	}
	if parallel {
		var wg sync.WaitGroup
		for i, item := range plan {
			if skip[i] {
				continue
			}
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
			if !skip[i] {
				save(i, item)
			}
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
		if skip[i] {
			continue
		}
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
			// เจ้าของ 2026-09-20: โน้ตอยู่ที่เดิมเสมอ ไม่ย้ายไป .synced (AgentsRoom เองก็ทับไฟล์เดิม ประวัติมีใน history/ แล้ว)
			// → หลัง push ทุกร่างกลายเป็นสำเนา checkout: mode replace, Base = updatedAt ที่ backend รับ, เนื้อ = ของ backend
			// (replace = เนื้อเดิมตรงกันอยู่แล้ว · append = ต้องดึงจาก backend เพราะ mirror ของ desktop เขียนช้ากว่า tool return)
			if fn, err := s.Get(from); err == nil && fn.Mode == "replace" {
				_ = s.Rebase(from, n.UpdatedAt)
				continue
			}
			if err := s.adoptFromBackend(c, from, results[i].Name, n); err != nil {
				// ดึงไม่ได้ → สำเนาจาก mirror (อาจยังเก่า: Base เก่ากว่า → Pull รอบหน้าดึงทับให้เอง) · ไม่มีใน mirror → แค่เลื่อน base
				if _, e := s.Checkout(results[i].Name); e != nil {
					_ = s.Rebase(from, n.UpdatedAt)
				} else if from != results[i].Name {
					_ = s.Delete(from)
				}
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


// folderOf — ดึง "folder" ชั้นบนสุดจากคำตอบ memory_save (JSON) · ว่างเมื่ออ่านไม่ได้
func folderOf(text string) string {
	var v struct {
		Folder string `json:"folder"`
	}
	if i := strings.Index(text, "{"); i >= 0 {
		_ = json.Unmarshal([]byte(text[i:]), &v)
	}
	return v.Folder
}

// tailBuf เก็บ stderr ไว้แค่ 2 KB ท้ายสุด
type tailBuf struct {
	mu     sync.Mutex
	b      []byte
	closer io.Closer // ไฟล์ log (ปิดตอน Close)
}

func (t *tailBuf) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.b = append(t.b, p...)
	if len(t.b) > 2048 {
		t.b = t.b[len(t.b)-2048:]
	}
	t.mu.Unlock()
	return len(p), nil
}

func (t *tailBuf) tail() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := strings.TrimSpace(string(t.b))
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	return " — stderr: " + strings.Join(lines, " | ")
}

// adoptFromBackend — ร่าง append ที่ push แล้ว: ดึงโน้ตเต็มจาก backend (memory_get) มาเขียนทับร่างเป็นสำเนา checkout ชื่อเดิม
func (s *Store) adoptFromBackend(c *Client, from, target string, n ListedNote) error {
	text, err := c.CallTool("memory_get", map[string]any{"note": target, "scope": "project"})
	var d struct {
		OK          bool     `json:"ok"`
		Folder      string   `json:"folder"`
		Content     string   `json:"content"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
	}
	if err == nil {
		err = json.Unmarshal([]byte(text), &d)
	}
	if err == nil && !d.OK {
		err = fmt.Errorf("memory_get %s: %.120s", target, text)
	}
	if err != nil {
		return err
	}
	if from != target { // ร่างใช้ชื่อชั่วคราว → เก็บภายใต้ชื่อโน้ตจริง
		_ = s.Delete(from)
	}
	folder := d.Folder
	if folder == "" {
		folder = n.Folder
	}
	_ = os.MkdirAll(filepath.Join(s.Dir, ".base"), 0o755)
	_ = os.WriteFile(filepath.Join(s.Dir, ".base", target+".md"), []byte(d.Content), 0o644)
	_, err = s.save(Input{Name: target, Target: target, Mode: "replace", Folder: folder, Description: d.Description, Tags: d.Tags, Content: d.Content, HasContent: true, Base: n.UpdatedAt}, "synced")
	return err
}
