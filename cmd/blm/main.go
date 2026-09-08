// blm — business-logic-memory: binary เดียวเป็นทั้ง CLI ที่ผู้ใช้เรียกตรง (blm status …), MCP server (blm mcp)
// และ hook guard (blm guard) ข้ามแพลตฟอร์ม เจ้าของกำหนด 2026-09-09: ไม่ต้องมี bun/node/jq บนเครื่องผู้ใช้
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/Ekkapap/business-logic-memory/internal/blm"
	"github.com/Ekkapap/business-logic-memory/internal/cli"
	"github.com/Ekkapap/business-logic-memory/internal/mcp"
)

const usage = `blm <command> [args]

  init [path] [--agentsroom | --obsidian | --dir <p> --backend <cli>] [--tools a,b] [--docker] [--no-install] [--no-plugin] [--sandbox] [--marketplace <dir|owner/repo>]
                                  run inside the project root · --tools installs the listed tools when missing (--docker = socraticode via Docker) · --force skips the project-root check
  status [--json]                 readiness, paths, rules, temp notes, tools, stats
  scan [path] [--json]            survey the repo: sizes, token estimate, sub-projects (candidate main topics)
  graph [query] [--rebuild] [--html]   built-in code graph: hubs, folder clusters, who-imports/calls-whom; --html writes <store>/graph.html
  report [name] [--json]          latest report
  tools <action> [--docker] [tool] status|get|install|start|stop|restart|gen-graph|help  (socraticode|obsidian|graphify)
  sync --push|--pull [--apply] [--parallel] [--author a] [--role r] [--delete a,b]   plan, or with --apply push to AgentsRoom one by one (--parallel = all at once)
  edit <target> <find> <replace>  edit lines of a backend note locally (mirror → draft), push later with sync
  diff <name> · merge <name> mine|cloud|content [file]   see what changed on the cloud vs your draft, then resolve
  conflicts [<id>] [-i] [--all]   waiting conflicts, one per block with its report path · <id> prints the report · -i pick → read → decide (c/i) · --all includes done
  resolve <name> [--keep current|incoming]  apply the ticked block · --keep decides from the terminal without opening the file
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
		lines := cli.Init(o)
		if len(lines) > 0 && strings.HasPrefix(lines[0], "stop") { // ถูกปฏิเสธก่อนเริ่ม: ยังไม่มีอะไรพิมพ์
			fmt.Println(strings.Join(lines, "\n"))
		} else {
			fmt.Println(lines[len(lines)-1]) // บรรทัด next (ที่เหลือพิมพ์สดไปแล้ว)
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
	case "graph":
		a := map[string]any{"rebuild": flags["rebuild"] != "", "html": flags["html"] != ""}
		if len(rest) > 0 {
			a["query"] = rest[0]
			asJSON = true
		}
		res, err = srv.Call("blm_graph", a)
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
		a := map[string]any{"direction": "push", "apply": flags["apply"] != "", "parallel": flags["parallel"] != ""}
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
	case "diff":
		if len(rest) < 1 {
			return fmt.Errorf("blm diff <name>")
		}
		res, err = srv.Call("blm_diff", map[string]any{"name": rest[0]})
		asJSON = true
	case "merge":
		if len(rest) < 2 {
			return fmt.Errorf("blm merge <name> mine|cloud|content [file|-]")
		}
		a := map[string]any{"name": rest[0], "keep": rest[1]}
		if len(rest) > 2 {
			c, rerr := readContent(rest[2])
			if rerr != nil {
				return rerr
			}
			a["content"] = c
		}
		res, err = srv.Call("blm_merge", a)
		asJSON = true
	case "conflicts", "conflict":
		if flags["i"] != "" || flags["interactive"] != "" {
			return interactiveConflicts(srv)
		}
		a := map[string]any{"all": flags["all"] != ""}
		if len(rest) > 0 {
			id, convErr := strconv.Atoi(strings.TrimPrefix(rest[0], "#"))
			if convErr != nil {
				return fmt.Errorf("blm conflicts [<id>]")
			}
			a["id"] = float64(id)
		}
		res, err = srv.Call("blm_conflicts", a)
	case "resolve":
		if len(rest) < 1 {
			return fmt.Errorf("blm resolve <note> [--keep current|incoming]")
		}
		a := map[string]any{"name": rest[0]}
		if k := flags["keep"]; k != "" {
			a["keep"] = k
		}
		res, err = srv.Call("blm_resolve", a)
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
			if h, _ := m["html"].(string); h != "" {
				fmt.Println("html:", h, " (open it in a browser)")
			}
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
		case "json", "push", "pull", "docker", "apply", "parallel", "rebuild", "html":
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

// interactiveConflicts เมนูเทอร์มินัลล้วน (เจ้าของ 2026-09-09: ไม่มี checkbox แบบ TUI ก็ใช้คำสั่งล้วนได้):
// แสดงรายการที่ค้าง → พิมพ์เลข → เห็นรายงานแบบ git → c (current = ร่างในเครื่อง) / i (incoming = cloud) / s (ข้าม) → รายการนั้นหายจาก list
func interactiveConflicts(srv *mcp.Server) error {
	in := bufio.NewReader(os.Stdin)
	for {
		res, err := srv.Call("blm_conflicts", map[string]any{})
		if err != nil {
			return err
		}
		m := res.(map[string]any)
		fmt.Println(m["terminal"])
		open := 0
		for _, c := range m["conflicts"].([]blm.ConflictReport) {
			if c.Status == "wait" {
				open++
			}
		}
		if open == 0 {
			return nil
		}
		fmt.Print("report id to review (Enter = quit): ")
		line, _ := in.ReadString('\n')
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
		if line == "" {
			return nil
		}
		id, convErr := strconv.Atoi(line)
		if convErr != nil {
			fmt.Println("not a number")
			continue
		}
		show, err := srv.Call("blm_conflicts", map[string]any{"id": float64(id)})
		if err != nil {
			fmt.Println(err)
			continue
		}
		sm := show.(map[string]any)
		fmt.Println(sm["terminal"])
		note := sm["conflict"].(blm.ConflictReport).Draft
		fmt.Print("keep [c]urrent (local draft) · [i]ncoming (cloud) · [s]kip: ")
		ans, _ := in.ReadString('\n')
		keep := ""
		switch strings.ToLower(strings.TrimSpace(ans)) {
		case "c", "current":
			keep = "current"
		case "i", "incoming":
			keep = "incoming"
		default:
			continue
		}
		out, err := srv.Call("blm_resolve", map[string]any{"name": note, "keep": keep})
		if err != nil {
			fmt.Println(err)
			continue
		}
		om := out.(map[string]any)
		fmt.Printf("resolved: kept %s for %s · %v\n", keep, note, om["done"])
		if om["ok"] == true {
			fmt.Println("push with: blm sync --apply")
		}
	}
}
