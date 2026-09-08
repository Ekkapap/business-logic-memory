package blm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
}

// PushAll ยิง memory_save ทุกรายการพร้อมกัน (ทุกรายการคนละโน้ตอยู่แล้ว) แล้ว archive ตัวที่สำเร็จ
// deletes = ชื่อโน้ตปลายทางที่จะลบ (เช่นโน้ตชื่อเก่าหลัง rename)
func (s *Store) PushAll(c *Client, plan []SyncItem, author, role string, deletes []string) ([]PushResult, []PushResult) {
	results := make([]PushResult, len(plan))
	var wg sync.WaitGroup
	for i, item := range plan {
		wg.Add(1)
		go func(i int, item SyncItem) {
			defer wg.Done()
			t := time.Now()
			// scope project เสมอ: กฎธุรกิจเป็นของโปรเจ็คนี้ ไม่ใช่ทั้งบัญชี (เจ้าของย้ำ 2026-09-09) · folder "global/…" = ทั้งโปรเจ็ค ไม่ใช่ทุกโปรเจ็ค
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
			r := PushResult{Name: item.MemorySave.Name, Mode: item.MemorySave.Mode, From: item.From}
			_, err := c.CallTool("memory_save", args)
			r.Ms = time.Since(t).Milliseconds()
			if err != nil {
				r.Error = err.Error()
			} else {
				r.OK = true
				for _, n := range item.From {
					if p, err := s.Archive(n); err == nil {
						r.Archived = append(r.Archived, p)
					}
				}
			}
			results[i] = r
		}(i, item)
	}
	deleted := make([]PushResult, len(deletes))
	for i, name := range deletes {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			t := time.Now()
			_, err := c.CallTool("memory_delete", map[string]any{"name": name})
			r := PushResult{Name: name, Mode: "delete", Ms: time.Since(t).Milliseconds(), OK: err == nil}
			if err != nil {
				r.Error = err.Error()
			}
			deleted[i] = r
		}(i, name)
	}
	wg.Wait()
	return results, deleted
}
