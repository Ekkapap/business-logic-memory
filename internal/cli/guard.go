package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/Ekkapap/business-logic-memory/internal/blm"
)

const denyMessage = "Error: blm  Deny [Edit/Delete] allow only plugin blm | Allow read with all command"

// Guard = PreToolUse hook ของ Bash: ห้ามคำสั่งที่ "ปลายทาง" คือไฟล์ใต้ store (หรือ blm.md สั้น) — เขียนได้ทางเดียวคือ MCP/CLI ของ blm
// ตรวจปลายทางของคำสั่งเขียน ไม่ใช่แค่มีคำว่า blm ในข้อความ: heredoc ที่พูดถึงมัน หรือ `blm status` ต้องผ่าน · คำสั่งอ่านผ่านทุกตัว
// แทน shell+jq เดิม เพื่อให้ Windows ใช้ได้ด้วย
func Guard(root string, in io.Reader, out io.Writer) error {
	raw, err := io.ReadAll(in)
	if err != nil {
		return nil
	}
	var payload struct {
		ToolInput struct {
			Command string `json:"command"`
		} `json:"tool_input"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.ToolInput.Command == "" {
		return nil
	}
	if Denies(blm.Load(root).Store, payload.ToolInput.Command) {
		res := map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": denyMessage}}
		b, _ := json.Marshal(res)
		fmt.Fprintln(out, string(b))
	}
	return nil
}

// Denies ตัดสินจากข้อความคำสั่ง (แยกไว้ test ได้)
func Denies(store, cmd string) bool {
	// ต้องจบที่ขอบ path (/ ช่องว่าง quote ตัวคั่น หรือท้ายข้อความ) — ไม่งั้น `.claude/blm.json` (config อ่านได้) โดนเพราะขึ้นต้นด้วย `.claude/blm`
	p := "(" + regexp.QuoteMeta(filepath.ToSlash(store)) + `|\.agentsroom/blm\.md|\.claude/blm\.md|(^|/)blm\.md)([/'"[:space:];&|]|$)`
	if runtime.GOOS == "windows" {
		cmd = strings.ReplaceAll(cmd, `\`, "/")
	}
	t := `[^[:space:];&|"']*` + p
	patterns := []string{
		`(^|[;&|[:space:]])(rm|mv|cp|tee|touch|mkdir|rmdir|truncate|ln|chmod|chown|del|erase|rd|move|copy|ren|sed[[:space:]]+-i[^[:space:]]*)([[:space:]]+[^;&|]*)?[[:space:]]` + t,
		`>>?[[:space:]]*['"]?` + t,
		`(^|[;&|[:space:]])(python3?|bun|node|perl|ruby|php|pwsh|powershell)[[:space:]].*` + p,
	}
	for _, re := range patterns {
		if regexp.MustCompile(re).MatchString(cmd) {
			return true
		}
	}
	return false
}

// EnsurePath ให้โฟลเดอร์ของ binary นี้อยู่บน PATH ของผู้ใช้ · ทำเองได้ = ทำ ไม่ได้ = คืนคำสั่งให้ผู้ใช้รัน (เจ้าของกำหนด 2026-09-09)
func EnsurePath() string {
	exe, err := os.Executable()
	if err != nil {
		return "cannot resolve blm binary path: " + err.Error()
	}
	dir := filepath.Dir(exe)
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.Clean(p) == filepath.Clean(dir) {
			return dir + " already on PATH"
		}
	}
	if runtime.GOOS == "windows" {
		ps := fmt.Sprintf(`[Environment]::SetEnvironmentVariable('Path', [Environment]::GetEnvironmentVariable('Path','User') + ';%s', 'User')`, dir)
		if _, err := shell("powershell", "-NoProfile", "-Command", ps); err == nil {
			return "added " + dir + " to PATH (User) — open a new terminal"
		}
		return "could not update PATH; run in PowerShell:\n  " + ps
	}
	home, _ := os.UserHomeDir()
	rc := filepath.Join(home, ".zshrc")
	if sh := os.Getenv("SHELL"); strings.Contains(sh, "bash") {
		rc = filepath.Join(home, ".bashrc")
	} else if strings.Contains(sh, "fish") {
		line := fmt.Sprintf("fish_add_path %s", dir)
		if _, err := shell("fish", "-c", line); err == nil {
			return "added " + dir + " to PATH (fish)"
		}
		return "run: " + line
	}
	line := fmt.Sprintf("\n# blm\nexport PATH=\"%s:$PATH\"\n", dir)
	f, err := os.OpenFile(rc, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Sprintf("cannot write %s (%v); run:\n  echo 'export PATH=\"%s:$PATH\"' >> %s && source %s", rc, err, dir, rc, rc)
	}
	defer f.Close()
	_, _ = f.WriteString(line)
	return fmt.Sprintf("added %s to PATH in %s — open a new terminal or: source %s", dir, rc, rc)
}
