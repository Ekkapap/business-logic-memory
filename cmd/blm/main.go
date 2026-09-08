// blm — business-logic-memory: binary เดียวเป็นทั้ง CLI ที่ผู้ใช้เรียกตรง (blm status …), MCP server (blm mcp)
// และ hook guard (blm guard) ข้ามแพลตฟอร์ม เจ้าของกำหนด 2026-09-09: ไม่ต้องมี bun/node/jq บนเครื่องผู้ใช้
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Ekkapap/business-logic-memory/internal/blm"
	"github.com/Ekkapap/business-logic-memory/internal/cli"
	"github.com/Ekkapap/business-logic-memory/internal/mcp"
)

const usage = `blm <command> [args]

  init [path] [--agentsroom | --obsidian | --dir <p> --backend <cli>] [--tools a,b] [--docker] [--no-plugin] [--sandbox] [--marketplace <dir|owner/repo>]
                                  run inside the project root · --tools installs the listed tools when missing (--docker = socraticode via Docker) · --force skips the project-root check
  status [--json]                 readiness, paths, rules, temp notes, tools, stats
  scan [path] [--json]            survey the repo: sizes, token estimate, sub-projects (candidate main topics)
  report [name] [--json]          latest report
  tools <action> [--docker] [tool] status|get|install|start|stop|restart|gen-graph|help  (socraticode|obsidian|graphify)
  sync --push|--pull [--apply] [--author a] [--role r] [--delete a,b]   plan, or with --apply push everything to AgentsRoom in parallel (no agent involved)
  edit <target> <find> <replace>  edit lines of a backend note locally (mirror → draft), push later with sync
  get <name> · save <name> <file|-> [--target t] [--mode m] [--folder f] [--description d] · update … · patch <name> <find> <replace> · delete <name>
  path                            add the blm folder to the user's PATH (prints the command if it cannot)
  guard                           PreToolUse hook: reads JSON on stdin, denies Bash writes inside the store
  mcp                             MCP server (stdio) — the Claude Code plugin runs this
  version`

func main() {
	if len(os.Args) < 2 {
		fmt.Println(usage)
		return
	}
	cmd, args := os.Args[1], os.Args[2:]
	if fi, err := os.Stdout.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 && os.Getenv("NO_COLOR") == "" && (runtime.GOOS != "windows" || os.Getenv("WT_SESSION") != "") {
		blm.Color = true // ANSI เฉพาะ terminal จริง (Windows: เฉพาะ Windows Terminal) · MCP/pipe = plain
	}
	if err := run(cmd, args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(cmd string, args []string) error {
	root := mcp.Root()
	switch cmd {
	case "mcp":
		mcp.New(root).Serve(os.Stdin, os.Stdout)
		return nil
	case "guard":
		return cli.Guard(root, os.Stdin, os.Stdout)
	case "init":
		o, err := cli.ParseInit(args)
		if err != nil {
			return err
		}
		o.Out = os.Stdout
		for _, line := range cli.Init(o) {
			fmt.Println(line)
		}
		return nil
	case "path":
		fmt.Println(cli.EnsurePath())
		return nil
	case "version", "--version", "-v":
		fmt.Println("blm", blm.Version)
		return nil
	case "help", "-h", "--help":
		fmt.Println(usage)
		return nil
	}
	// ที่เหลือคือ tool ของ MCP เรียกผ่านโค้ดชุดเดียวกัน — ผู้ใช้ได้ผลเหมือน agent โดยไม่ผ่าน AI
	srv := mcp.New(root)
	flags, rest := splitFlags(args)
	asJSON := flags["json"] != ""
	var (
		res any
		err error
	)
	switch cmd {
	case "status":
		res, err = srv.Call("blm_status", nil2map(nil))
	case "scan":
		a := map[string]any{}
		if len(rest) > 0 {
			a["path"] = rest[0]
		}
		res, err = srv.Call("blm_scan", a)
	case "report":
		a := map[string]any{}
		if len(rest) > 0 {
			a["name"] = rest[0]
		}
		res, err = srv.Call("blm_report", a)
	case "tools":
		action, tool := "help", ""
		if len(rest) > 0 {
			action = rest[0]
		}
		if len(rest) > 1 {
			tool = rest[1]
		}
		// เรียกตรง (ไม่ผ่าน MCP) เพื่อ stream output ของ script ออก terminal ทันที
		text, terr := blm.Tools(root, blm.Load(root), action, tool, flags["docker"] != "", os.Stdout)
		if terr != nil {
			return terr
		}
		fmt.Println(text)
		return nil
	case "sync":
		a := map[string]any{"direction": "push", "apply": flags["apply"] != ""}
		if flags["pull"] != "" {
			a["direction"] = "pull"
		}
		if d := flags["done"]; d != "" {
			a["done"] = toAny(strings.Split(d, ","))
		}
		if d := flags["delete"]; d != "" {
			a["delete"] = toAny(strings.Split(d, ","))
		}
		for _, k := range []string{"author", "role"} {
			if v := flags[k]; v != "" {
				a[k] = v
			}
		}
		res, err = srv.Call("blm_sync", a)
		asJSON = true
	case "get", "delete":
		if len(rest) < 1 {
			return fmt.Errorf("blm %s <name>", cmd)
		}
		res, err = srv.Call("blm_"+cmd, map[string]any{"name": rest[0]})
		asJSON = true
	case "save", "update":
		if len(rest) < 2 {
			return fmt.Errorf("blm %s <name> <file|-> [--target t] [--mode m] [--folder f] [--description d]", cmd)
		}
		content, rerr := readContent(rest[1])
		if rerr != nil {
			return rerr
		}
		a := map[string]any{"name": rest[0], "content": content}
		for _, k := range []string{"target", "mode", "folder", "description"} {
			if v := flags[k]; v != "" {
				a[k] = v
			}
		}
		res, err = srv.Call("blm_"+cmd, a)
		asJSON = true
	case "edit":
		if len(rest) < 3 {
			return fmt.Errorf("blm edit <target> <find> <replace>")
		}
		res, err = srv.Call("blm_edit", map[string]any{"target": rest[0], "find": rest[1], "replace": rest[2]})
		asJSON = true
	case "patch":
		if len(rest) < 3 {
			return fmt.Errorf("blm patch <name> <find> <replace>")
		}
		res, err = srv.Call("blm_patch", map[string]any{"name": rest[0], "find": rest[1], "replace": rest[2]})
		asJSON = true
	default:
		return fmt.Errorf("unknown command %q\n%s", cmd, usage)
	}
	if err != nil {
		return err
	}
	if m, ok := res.(map[string]any); ok && !asJSON {
		if t, ok := m["terminal"].(string); ok {
			fmt.Println(t)
			return nil
		}
	}
	if r, ok := res.(*blm.Report); ok && !asJSON {
		fmt.Print(r.Content)
		fmt.Println("file:", r.File)
		return nil
	}
	out, _ := json.MarshalIndent(res, "", "  ")
	fmt.Println(string(out))
	return nil
}

func nil2map(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// splitFlags แยก --k v / --k=v / --flag ออกจาก positional (ไม่ใช้ package flag เพราะ positional ปนกับ flag ได้)
func splitFlags(args []string) (map[string]string, []string) {
	flags := map[string]string{}
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			rest = append(rest, a)
			continue
		}
		k := strings.TrimPrefix(a, "--")
		if eq := strings.Index(k, "="); eq >= 0 {
			flags[k[:eq]] = k[eq+1:]
			continue
		}
		switch k {
		case "json", "push", "pull", "docker", "apply":
			flags[k] = "1"
		default:
			if i+1 < len(args) {
				flags[k] = args[i+1]
				i++
			} else {
				flags[k] = "1"
			}
		}
	}
	return flags, rest
}

func readContent(src string) (string, error) {
	if src == "-" {
		var b strings.Builder
		buf := make([]byte, 32*1024)
		for {
			n, err := os.Stdin.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		return b.String(), nil
	}
	raw, err := os.ReadFile(filepath.Clean(src))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
