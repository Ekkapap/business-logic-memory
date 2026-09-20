package blm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// socraticode ต้องอยู่ยาวเหมือนตอนเป็น MCP ใน session (เจ้าของ 2026-09-20: "process ต้องอยู่ยาว ไม่ว่าจะสั่งอะไร remove status index"
// — ทุกอาการที่เจอ: lock ค้าง / collection โผล่กลับ / index ถูก skip / watched by another process มาจาก blm เปิด-ปิดมันทุกคำสั่งแล้ว
// ฆ่าโปรเซสที่กำลังทำงานลับหลัง โดยไม่รู้ว่ามันทำอะไรอยู่)
//
// รูปแบบ: blm เปิด socraticode ครั้งเดียวต่อโปรเจ็คใน daemon ของตัวเอง (`blm sc-daemon <root>` — detached, log ที่ <store>/logs/)
// daemon ถือ stdio ของ socraticode และฟัง unix socket <store>/socraticode.sock · ทุก `blm tools socraticode <fn>` / blm_sc_* ต่อ socket
// ส่ง {name,args} รับ {text,error} แล้วตัดสาย — socraticode ไม่ถูกแตะ ไม่ถูกฆ่า auto-resume/watcher/lock เป็นเรื่องของมันเหมือนใน session
// daemon ปิดเมื่อ: `blm tools socraticode shutdown` (ปิด stdin ให้ socraticode จบเอง) หรือ socraticode ตายเอง

type scReq struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}
type scResp struct {
	Text  string `json:"text"`
	Error string `json:"error,omitempty"`
}

// socket อยู่ใน tmp (ไม่ใช่ store: path ของ unix socket จำกัดความยาว และ store อาจถูก sandbox ห้ามเขียน)
func scSocket(root string, c Config) string {
	abs, _ := filepath.Abs(root)
	return filepath.Join(os.TempDir(), "blm-sc-"+SocratiCodeProjectID(abs)+".sock")
}
func scLog(root string, c Config) string {
	return filepath.Join(root, c.Store, "logs", "socraticode.log")
}

// scEntry — คำสั่งเปิด socraticode: สำเนาที่ npx cache ไว้รันด้วย node ตรง ๆ (`npx --prefer-online` ถาม registry ทุกครั้ง ค้างได้) · env ของ
// remote/local จาก .claude/blm.json · log ของมันเองลงไฟล์ (stderr ผ่าน pipe หายได้)
func scEntry(root string, c Config) mcpEntry {
	logFile := scLog(root, c)
	e := mcpEntry{Command: "npx", Args: []string{"-y", "socraticode@latest"}, Env: map[string]string{}, LogFile: logFile}
	if bin := cachedSocratiCode(); bin != "" {
		e = mcpEntry{Command: "node", Args: []string{bin}, Env: map[string]string{}, LogFile: logFile}
	}
	e.Env["SOCRATICODE_LOG_LEVEL"] = "debug"
	e.Env["SOCRATICODE_LOG_FILE"] = logFile
	if c.SocratiCode != nil {
		for _, kv := range c.SocratiCode.Env() {
			if k, v, ok := strings.Cut(kv, "="); ok && !strings.HasPrefix(k, "-") {
				e.Env[k] = v
			}
		}
	}
	// เจ้าของ 2026-09-20: ก่อนเปิด ดูก่อนว่าโปรเจ็คนี้มี index บน Qdrant ไหม — ไม่มี (ยังไม่เคย / โดน remove) = เปิดแบบไม่ให้ resume/
	// watch เอง (AUTO_RESUME=off, WATCHER=manual — manual เพื่อให้ blm สั่ง codebase_watch start ได้หลัง index จบ) · มี = default ของ socraticode
	if !scIndexExists(root, c) {
		e.Env["SOCRATICODE_AUTO_RESUME"] = "off"
		e.Env["SOCRATICODE_WATCHER"] = "manual"
	}
	return e
}

// scIndexExists — collection codebase_<projectId> มีอยู่บน Qdrant ไหม (HTTP ตรง ไม่ผ่าน socraticode)
func scIndexExists(root string, c Config) bool {
	if c.SocratiCode == nil || c.SocratiCode.QdrantURL == "" {
		return true // ไม่รู้ที่อยู่ Qdrant = อย่าเดา ใช้ default
	}
	abs, _ := filepath.Abs(root)
	cl := &http.Client{Timeout: 5 * time.Second}
	resp, err := cl.Get(strings.TrimRight(c.SocratiCode.QdrantURL, "/") + "/collections/codebase_" + SocratiCodeProjectID(abs))
	if err != nil {
		return true
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

// RunScDaemon — ตัว daemon (รันใน `blm sc-daemon <root>`): เปิด socraticode แล้วให้บริการทาง socket จนกว่าจะถูกสั่ง shutdown หรือ socraticode ตาย
func RunScDaemon(root string) error {
	c := Load(root)
	sock := scSocket(root, c)
	_ = os.MkdirAll(filepath.Dir(sock), 0o755)
	_ = os.Remove(sock)
	cl, err := Connect(scEntry(root, c))
	if err != nil {
		return err
	}
	cl.Timeout = 30 * time.Minute // remove/update ของ socraticode รอ batch จบได้หลายนาที
	ln, err := net.Listen("unix", sock)
	if err != nil {
		cl.Close()
		return err
	}
	defer os.Remove(sock)
	died := make(chan struct{})
	go func() { _, _ = cl.cmd.Process.Wait(); close(died) }()
	go func() { <-died; ln.Close() }()
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-died:
				return fmt.Errorf("socraticode exited")
			default:
				return err
			}
		}
		go func(conn net.Conn) {
			defer conn.Close()
			var req scReq
			if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&req); err != nil {
				return
			}
			if req.Name == "__shutdown__" { // ปิดแบบให้ socraticode จบเอง: ปิด stdin แล้วรอ (ไม่ฆ่า)
				_ = json.NewEncoder(conn).Encode(scResp{Text: "shutting down"})
				conn.Close()
				if cl.stdin != nil {
					_ = cl.stdin.Close()
				}
				select {
				case <-died:
				case <-time.After(60 * time.Second):
				}
				ln.Close()
				os.Exit(0)
			}
			text, err := cl.CallTool(req.Name, req.Args)
			resp := scResp{Text: text}
			if err != nil {
				resp.Error = err.Error()
			}
			_ = json.NewEncoder(conn).Encode(resp)
		}(conn)
	}
}

// ScCall — เรียก tool ของ socraticode ผ่าน daemon · แบบ Claude Code (เจ้าของ 2026-09-20): คำสั่งแรกที่แตะ = เปิด daemon เหมือนเปิด session
// แล้ว socraticode auto-resume เอง (index ที่มีอยู่ → watcher + catch-up ไม่ index ใหม่) · ตัวเดียวต่อโปรเจ็ค (socket) ไม่ซ้ำซ้อน
// · shutdown ต้องสั่งชัด ๆ (start=false = ห้ามเปิดใหม่ เช่น shutdown ตอนไม่มี daemon) · timeout = เวลารอคำตอบของ call นี้
func ScCall(root string, c Config, name string, args map[string]any, timeout time.Duration, start bool) (string, error) {
	sock := scSocket(root, c)
	conn, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		if !start {
			return "", fmt.Errorf("socraticode is not running for this project")
		}
		if err := startScDaemon(root, c); err != nil {
			return "", err
		}
		deadline := time.Now().Add(60 * time.Second)
		for {
			if conn, err = net.DialTimeout("unix", sock, 2*time.Second); err == nil {
				break
			}
			if time.Now().After(deadline) {
				return "", fmt.Errorf("socraticode daemon did not come up in 60s — see %s", scLog(root, c))
			}
			time.Sleep(300 * time.Millisecond)
		}
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if err := json.NewEncoder(conn).Encode(scReq{Name: name, Args: args}); err != nil {
		return "", err
	}
	var resp scResp
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&resp); err != nil {
		return "", fmt.Errorf("socraticode daemon: %w (see %s)", err, scLog(root, c))
	}
	if resp.Error != "" {
		return resp.Text, fmt.Errorf("%s", resp.Error)
	}
	return resp.Text, nil
}

// startScDaemon — เปิด `blm sc-daemon <root>` แยกจาก terminal (setsid) stdout/stderr ลง <store>/logs/socraticode-daemon.log
func startScDaemon(root string, c Config) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logDir := filepath.Join(root, c.Store, "logs")
	_ = os.MkdirAll(logDir, 0o755)
	f, err := os.OpenFile(filepath.Join(logDir, "socraticode-daemon.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	fmt.Fprintf(f, "\n=== %s start\n", time.Now().Format(time.RFC3339))
	cmd := exec.Command(exe, "sc-daemon", root)
	cmd.Dir = root
	cmd.Stdout, cmd.Stderr = f, f
	setProcessGroup(cmd) // ไม่ตายตาม terminal/Ctrl-C ของคำสั่งที่เปิดมัน
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // ไม่ให้เป็น zombie ถ้าเรายังอยู่; ปกติเราจบก่อน
	return nil
}
