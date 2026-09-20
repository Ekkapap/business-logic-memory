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
	"sort"
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
  grep "<term1> <term2> ..." [--path <dir> | --file <file>] [--max-line N=20] [--max-result N=10] [--ext .md,.ts,...] [--sc] [--json]
                                  search terms in files: case-insensitive substring match per term · --sc uses SocratiCode if available · --json for machine output
  cat --grep-id <id> [--result-id <n,m>] [--context N=3] [--json]
                                  read grep result file and display hits with context
  report [name] [--json]          latest report
  tools <action> [tool] [--docker | --local | --remote <host> [--embedding-model m --embedding-dimensions n --embedding-context-length n]]
                                  status|get|install|start|stop|restart|gen-graph|help · tools: socraticode|obsidian|graphify|tree-sitter|embedding
                                  socraticode --local = Qdrant+Ollama on this machine (as before) · --remote <host> = use a server that already runs them,
                                  saved to .claude/blm.json + ~/.claude/settings.json env · no flag = remote if saved, else local · "blm tools help" for examples
  sync --push|--pull [--apply] [--parallel] [--author a] [--role r] [--delete a,b]   plan, or with --apply push to AgentsRoom one by one (--parallel = all at once)
  diff <name> · merge <name> mine|cloud|content [file]   see what changed on the cloud vs your draft, then resolve
  conflicts [<id>] [-i] [--all]   waiting conflicts, one per block with its report path · <id> prints the report · -i pick → read → decide (c/i) · --all includes done
  conflict mark                   put [Conflict] marks into blm.md and the notes for every waiting report (reports alone change nothing)
  restore <history-file> <note>   put a history/ snapshot back as the note (blm restore <note> lists snapshots)
  resolve <name> [--keep current|incoming] [--no-push]  apply the decision, then push the note so the backend equals local (--no-push to skip)
  get <name> · create <name> <file|-> [--target t] [--mode m] [--folder f] [--description d] · append <name> <file|-> · patch <name> <find> <replace> · replace <name> <file|-> [--confirm] · delete <name>
  path                            add the blm folder to the user's PATH (prints the command if it cannot)
  guard                           PreToolUse hook: reads JSON on stdin, denies Bash writes inside the store
  mcp                             MCP server (stdio) — the Claude Code plugin runs this
  version`

const grepHelp = `blm grep — ค้นหาคำศัพท์ในไฟล์

รูปแบบ:
  blm grep "<term1> <term2> ..." [--path <dir> | --file <file>] [--max-line N=20] [--max-result N=10] [--ext .ts,.md] [--sc] [--json] [--term "..."]

ตัวเลือก:
  --path <dir>      ค้นหา recursive ในโฟลเดอร์นี้ (ค่าเริ่มต้น = current directory)
  --file <file>     ค้นหาในไฟล์เดียวเท่านั้น (ไม่ใช้กับ --path)
  --max-line N      บรรทัดสูงสุดของ snippet window (ค่าเริ่มต้น 20; clamp ±N/2 รอบ hit)
  --max-result N    hits สูงสุดต่อคำศัพท์ (ค่าเริ่มต้น 10)
  --ext .ts,.md     รายการนามสกุลที่ค้นหา (ค่าเริ่มต้น .md,.txt,.ts,.tsx,.js,.jsx,.php,.sql,.sh,.go)
  --sc              ใช้ SocratiCode semantic search ถ้าพร้อม (fallback = regular scan)
  --imports         รวมบรรทัด import/export/require (ค่าเริ่มต้น = ข้าม)
  --json            ผลลัพธ์ JSON แทนตารางที่อ่านง่าย
  --term "..."      เพิ่มคำศัพท์เพิ่มเติม (ใช้ซ้ำได้)

ผลลัพธ์ (ตารางเริ่มต้น):
  term              ศัพท์ที่ค้นหา
  path:line         ไฟล์และบรรทัดที่ตรงกับ (1-indexed)
  snippet (start-end) ช่วงบรรทัดของ window (start และ end inclusive, 1-indexed)
  lines             จำนวนบรรทัดของ snippet
  text              บรรทัดที่ตรงกัน trimmed

Directory ที่ skip โดยอัตโนมัติ:
  node_modules, .git, .next, dist, build, vendor

ตัวอย่าง:
  blm grep "session login" --path src --max-line 30
  blm grep --term "portal" --term "snapshot" --file src/lib/portal/portal-snapshot.ts --json
  blm grep "config database" --ext .ts,.js --max-result 20 --sc`

const catHelp = `blm cat — แสดงผล grep result ที่บันทึกไว้พร้อม context

รูปแบบ:
  blm cat --grep-id <id> [--result-id <n,m,p>] [--context N=3] [--json]

ตัวเลือก:
  --grep-id <id>     ID ของ grep result ที่ต้องการอ่าน (ได้จากคำสั่ง blm grep)
  --result-id <n,m>  หมายเลข hit ที่ต้องการแสดง คั่นด้วยเครื่องหมายจุลภาค (ค่าเริ่มต้น = ทั้งหมด)
  --context N        บรรทัดเพิ่มเติมก่อนและหลัง hit (ค่าเริ่มต้น 3)
  --json             ผลลัพธ์ JSON แทนตารางที่อ่านง่าย

ผลลัพธ์ (ตารางเริ่มต้น):
  resultId          หมายเลข hit ในผล grep
  path:line         ไฟล์และบรรทัด
  context           จำนวนบรรทัด context
  lines             บรรทัดที่อ่านได้พร้อม highlight บรรทัดที่ตรงกับ

ตัวอย่าง:
  blm cat --grep-id 7f2k9q1x --result-id 2
  blm cat --grep-id 7f2k9q1x --result-id 1,3,5 --context 5
  blm cat --grep-id 7f2k9q1x --json`

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
	// ไม่มี .claude/blm.json ในโฟลเดอร์นี้ = รันผิดที่ (เจ้าของเจอบ่อย 2026-09-09: "backend none has no sync target" ไม่บอกอะไร)
	// แต่ --help ไม่ต้องมี config เลย
	flags, rest := splitFlags(args)
	if flags["help"] != "" || flags["h"] != "" {
		if cmd == "grep" {
			fmt.Println(grepHelp)
			return nil
		}
		if cmd == "cat" {
			fmt.Println(catHelp)
			return nil
		}
	}
	if _, err := os.Stat(filepath.Join(root, blm.ConfigFile)); err != nil && cmd != "init" && cmd != "version" && cmd != "path" && cmd != "guard" && cmd != "mcp" && cmd != "grep" && cmd != "cat" {
		return fmt.Errorf("no %s here (%s) — run blm inside the project folder, e.g. cd <project> && blm %s", blm.ConfigFile, root, cmd)
	}
	srv := mcp.New(root)
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
		if flags["help"] != "" || flags["h"] != "" {
			fmt.Println(blm.ToolsHelp)
			return nil
		}
		// เรียกตรง (ไม่ผ่าน MCP) เพื่อ stream output ของ script ออก terminal ทันที
		opts := blm.ToolOpts{Docker: flags["docker"] != "", Local: flags["local"] != "", Remote: flags["remote"] != ""}
		if flags["remote"] != "1" {
			opts.RemoteHost = flags["remote"]
		}
		opts.SC = blm.SocratiCodeConfig{
			OllamaURL: flags["ollama-url"], QdrantURL: flags["qdrant-url"],
			EmbeddingModel: flags["embedding-model"], EmbeddingDimensions: flags["embedding-dimensions"], EmbeddingContextLength: flags["embedding-context-length"],
			EmbeddingQueryPrefix: flags["embedding-query-prefix"], EmbeddingDocumentPrefix: flags["embedding-document-prefix"],
		}
		text, terr := blm.Tools(root, blm.Load(root), action, tool, opts, os.Stdout)
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
	case "get", "delete":
		if len(rest) < 1 {
			return fmt.Errorf("blm %s <name>", cmd)
		}
		res, err = srv.Call("blm_"+cmd, map[string]any{"name": rest[0]})
	case "create", "append", "replace":
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
		if flags["confirm"] != "" {
			a["confirm"] = true
		}
		res, err = srv.Call("blm_"+cmd, a)
	case "diff":
		if len(rest) < 1 {
			return fmt.Errorf("blm diff <name>")
		}
		res, err = srv.Call("blm_diff", map[string]any{"name": rest[0]})
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
	case "conflicts", "conflict":
		if len(rest) > 0 && rest[0] == "mark" {
			res, err = srv.Call("blm_conflict", map[string]any{"action": "mark"})
			break
		}
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
	case "restore":
		// blm restore <history-file> <note> · blm restore <note> = list history files (เจ้าของกำหนดรูปคำสั่ง 2026-09-09)
		switch len(rest) {
		case 1:
			res, err = srv.Call("blm_restore", map[string]any{"name": rest[0]})
		case 2:
			res, err = srv.Call("blm_restore", map[string]any{"history": rest[0], "name": rest[1]})
		default:
			return fmt.Errorf("blm restore <history-file> <note>   (blm restore <note> lists its history)")
		}
	case "resolve":
		if len(rest) < 1 {
			return fmt.Errorf("blm resolve <note> [--keep current|incoming]")
		}
		a := map[string]any{"name": rest[0]}
		if k := flags["keep"]; k != "" {
			a["keep"] = k
		}
		if flags["no-push"] != "" {
			a["push"] = false
		}
		res, err = srv.Call("blm_resolve", a)
	case "patch":
		if len(rest) < 3 {
			return fmt.Errorf("blm patch <name> <find> <replace>")
		}
		res, err = srv.Call("blm_patch", map[string]any{"name": rest[0], "find": rest[1], "replace": rest[2]})
	case "grep":
		if flags["help"] != "" || flags["h"] != "" {
			fmt.Println(grepHelp)
			return nil
		}
		if len(rest) < 1 {
			return fmt.Errorf("blm grep \"<term1> <term2> ...\" [--path <dir> | --file <file>] [--max-line N=20] [--max-result N=10] [--ext .md,.ts,...] [--sc] [--json]")
		}
		// Collect all terms: first from the positional arg, then from repeated --term flags
		var allTerms []string
		terms := strings.Fields(rest[0])
		allTerms = append(allTerms, terms...)
		if t := flags["term"]; t != "" {
			allTerms = append(allTerms, t)
		}
		a := map[string]any{"terms": strings.Join(allTerms, " ")}
		if f := flags["file"]; f != "" {
			a["file"] = f
		}
		if p := flags["path"]; p != "" {
			a["path"] = p
		}
		if ml := flags["max-line"]; ml != "" {
			a["maxLine"] = toNum(ml, 20)
		}
		if mr := flags["max-result"]; mr != "" {
			a["maxResult"] = toNum(mr, 10)
		}
		if e := flags["ext"]; e != "" {
			a["ext"] = e
		}
		if flags["sc"] != "" {
			a["sc"] = true
		}
		if flags["imports"] != "" {
			a["imports"] = true
		}
		res, err = srv.Call("blm_grep", a)
	case "cat":
		if flags["help"] != "" || flags["h"] != "" {
			fmt.Println(catHelp)
			return nil
		}
		grepID := flags["grep-id"]
		if grepID == "" {
			return fmt.Errorf("blm cat --grep-id <id> [--result-id <n,m>] [--context N=3] [--json]")
		}
		a := map[string]any{"grepId": grepID}
		if rid := flags["result-id"]; rid != "" {
			a["resultId"] = rid
		}
		if ctx := flags["context"]; ctx != "" {
			a["context"] = toNum(ctx, 3)
		}
		res, err = srv.Call("blm_cat", a)
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
	if asJSON {
		out, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	fmt.Print(renderHuman(res))
	return nil
}

// renderHuman พิมพ์ผลลัพธ์ให้คนอ่าน (เจ้าของ 2026-09-09: JSON ใน CLI อ่านยาก) · --json ยังได้ของเดิม
// รู้จักรูปที่ใช้บ่อย: แผน sync, ผล push, โน้ตหนึ่งใบ, blm_grep · ที่เหลือพิมพ์เป็น key: value ย่อหน้าตามชั้น ข้าม response ดิบ
func renderHuman(res any) string {
	var b strings.Builder
	// ผลจาก srv.Call เป็น type ของ Go (struct/slice) — วนผ่าน JSON ให้เป็น map/[]any รูปเดียวเหมือนที่ agent เห็น
	var generic any
	if raw, err := json.Marshal(res); err == nil {
		_ = json.Unmarshal(raw, &generic)
	}
	m, ok := generic.(map[string]any)
	if !ok {
		out, _ := json.MarshalIndent(res, "", "  ")
		return string(out) + "\n"
	}
	// blm_grep: table of hits using existing Table formatter
	if hits, ok := m["hits"].([]any); ok {
		if len(hits) == 0 {
			return "no matches\n"
		}
		grepID := str(m["id"])
		header := []string{"id", "term", "path:line", "snippet", "lines", "chars", "text"}
		var rows [][]string
		for _, h := range hits {
			hit, _ := h.(map[string]any)
			resultID := fmt.Sprint(int(num(hit["resultId"])))
			term, path := str(hit["term"]), str(hit["path"])
			start, end, lines, chars, text := int(num(hit["start"])), int(num(hit["end"])), int(num(hit["lines"])), int(num(hit["chars"])), str(hit["text"])
			pathLine := path + ":" + fmt.Sprint(int(num(hit["line"])))
			snippet := fmt.Sprintf("%d-%d", start, end)
			rows = append(rows, []string{resultID, term, pathLine, snippet, fmt.Sprint(lines), fmt.Sprint(chars), text})
		}
		table := blm.Table(header, rows)
		if grepID != "" {
			if !strings.HasSuffix(table, "\n") {
				table += "\n"
			}
			table += fmt.Sprintf("grep-id: %s\n", grepID)
		}
		return table
	}

	// blm_cat: display cat results with file content
	if results, ok := m["results"].(map[string]any); ok {
		if len(results) == 0 {
			return "no results found\n"
		}
		header := []string{"id", "path:line", "context", "code"}
		type resultItem struct {
			id  int
			res map[string]any
		}
		var items []resultItem
		for _, r := range results {
			res, _ := r.(map[string]any)
			if rid, ok := res["resultId"].(float64); ok {
				items = append(items, resultItem{int(rid), res})
			}
		}
		// Sort by ID
		sort.Slice(items, func(i, j int) bool { return items[i].id < items[j].id })

		var rows [][]string
		for _, item := range items {
			res := item.res
			rid := item.id
			path := str(res["path"])
			line := int(num(res["line"]))
			ctx := int(num(res["context"]))
			lines, _ := res["lines"].([]any)
			hit := int(num(res["hitLine"]))

			pathLine := fmt.Sprintf("%s:%d", path, line)
			contextStr := fmt.Sprintf("±%d", ctx)

			// Format code lines with hit highlighted (use actual file line numbers)
			var codeLines []string
			// Calculate the actual line number of the first line in the context
			firstLineNum := line - hit + 1
			for i, l := range lines {
				actualLineNum := firstLineNum + i
				lineStr := str(l)
				if i+1 == hit {
					codeLines = append(codeLines, fmt.Sprintf(">>> %d: %s", actualLineNum, lineStr))
				} else {
					codeLines = append(codeLines, fmt.Sprintf("    %d: %s", actualLineNum, lineStr))
				}
			}
			code := strings.Join(codeLines, "\n")

			rows = append(rows, []string{fmt.Sprint(rid), pathLine, contextStr, code})
		}
		return blm.Table(header, rows)
	}

	// โน้ตหนึ่งใบ (blm get): หัว + เนื้อหาเต็ม
	if c, ok := m["content"].(string); ok && m["target"] != nil {
		fmt.Fprintf(&b, "%s  (%s → %s · %s · base %s · updated %s)\n\n%s\n", str(m["name"]), str(m["mode"]), str(m["folder"]), map[bool]string{true: "edited", false: "synced"}[m["dirty"] == true], str(m["base"]), str(m["updatedAt"]), c)
		return b.String()
	}
	if plan, ok := m["plan"].([]any); ok {
		if len(plan) == 0 {
			b.WriteString("nothing to push — every note in blm/ matches the backend\n")
		} else {
			fmt.Fprintf(&b, "plan — %d note(s) differ from the backend (preview, nothing sent)\n", len(plan))
			for _, it := range plan {
				x, _ := it.(map[string]any)
				fmt.Fprintf(&b, "  %-22s %-8s → %-22s %6.0f bytes", str(x["name"]), str(x["mode"]), str(x["folder"]), num(x["bytes"]))
				if x["targetExists"] == false {
					b.WriteString("  (new on the backend)")
				}
				b.WriteString("\n")
				if w, ok := x["warnings"].([]any); ok {
					for _, l := range w {
						fmt.Fprintf(&b, "      ! %v\n", l)
					}
				}
			}
			b.WriteString("send: blm sync --push --apply\n")
		}
		return b.String()
	}
	if pushed, ok := m["pushed"].([]any); ok {
		if len(pushed) == 0 {
			b.WriteString("nothing to push\n")
		}
		for _, it := range pushed {
			x, _ := it.(map[string]any)
			mark := "✔"
			if x["verified"] != true {
				mark = "✘"
			}
			fmt.Fprintf(&b, "%s %-22s %-8s %5.0f ms", mark, str(x["name"]), str(x["mode"]), num(x["ms"]))
			if e := str(x["error"]); e != "" {
				fmt.Fprintf(&b, "  %s", e)
			}
			b.WriteString("\n")
		}
		if d, ok := m["deleted"].([]any); ok && len(d) > 0 {
			fmt.Fprintf(&b, "deleted on backend: %d\n", len(d))
		}
		if pr, ok := m["pull"].(map[string]any); ok {
			b.WriteString(renderHuman(pr))
		}
		fmt.Fprintf(&b, "%d pushed · %v failed · %.0f ms\n", len(pushed), m["failed"], num(m["ms"]))
		if n := str(m["next"]); n != "" {
			b.WriteString(n + "\n")
		}
		return b.String()
	}
	if pr, ok := m["pull"].(map[string]any); ok {
		for _, k := range []string{"refreshed", "conflicts", "upToDate"} {
			if l, ok := pr[k].([]any); ok && len(l) > 0 {
				fmt.Fprintf(&b, "%-10s %s\n", k+":", joinAny(l))
			}
		}
		if n := str(m["next"]); n != "" {
			b.WriteString(n + "\n")
		}
		return b.String()
	}
	if name := str(m["name"]); name != "" && m["mode"] != nil && m["ok"] != nil {
		state := "ok"
		if m["ok"] != true {
			state = "failed"
		}
		fmt.Fprintf(&b, "%s  %s  (%s → %s, %.0f bytes)\n", state, name, str(m["mode"]), str(m["folder"]), num(m["bytes"]))
		if chk, ok := m["check"].(map[string]any); ok && chk["needsConfirm"] == true {
			fmt.Fprintf(&b, "needs confirm: %s\n  old %.0f → new %.0f bytes · %.0f existing line(s) disappear\n", str(chk["reason"]), num(chk["oldBytes"]), num(chk["newBytes"]), num(chk["changedLines"]))
			if r := str(chk["removed"]); r != "" {
				b.WriteString("  first lines that would go:\n    " + strings.ReplaceAll(r, "\n", "\n    ") + "\n")
			}
		}
		if n := str(m["next"]); n != "" {
			b.WriteString(n + "\n")
		}
		return b.String()
	}
	writeKV(&b, m, "")
	return b.String()
}

func writeKV(b *strings.Builder, m map[string]any, indent string) {
	keys := make([]string, 0, len(m))
	for k := range m {
		if k == "response" || k == "content" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch v := m[k].(type) {
		case map[string]any:
			fmt.Fprintf(b, "%s%s:\n", indent, k)
			writeKV(b, v, indent+"  ")
		case []any:
			if len(v) == 0 {
				fmt.Fprintf(b, "%s%s: -\n", indent, k)
				continue
			}
			fmt.Fprintf(b, "%s%s:\n", indent, k)
			for _, it := range v {
				if im, ok := it.(map[string]any); ok {
					fmt.Fprintf(b, "%s  -\n", indent)
					writeKV(b, im, indent+"    ")
				} else {
					fmt.Fprintf(b, "%s  - %v\n", indent, it)
				}
			}
		default:
			fmt.Fprintf(b, "%s%s: %v\n", indent, k, v)
		}
	}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func num(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	}
	return 0
}

func toNum(s string, def int) float64 {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return float64(def)
	}
	return float64(n)
}

func joinAny(l []any) string {
	parts := make([]string, 0, len(l))
	for _, x := range l {
		parts = append(parts, fmt.Sprint(x))
	}
	return strings.Join(parts, ", ")
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
		// -i / -all แบบขีดเดียวก็รับ (เจ้าของพิมพ์ `blm conflicts -i` 2026-09-09) ยกเว้นเลขติดลบ
		if !strings.HasPrefix(a, "-") || len(a) < 2 || (a[1] >= '0' && a[1] <= '9') {
			rest = append(rest, a)
			continue
		}
		k := strings.TrimLeft(a, "-")
		if eq := strings.Index(k, "="); eq >= 0 {
			flags[k[:eq]] = k[eq+1:]
			continue
		}
		switch k {
		case "json", "push", "pull", "docker", "apply", "parallel", "rebuild", "html", "i", "interactive", "all", "no-push", "confirm", "sc", "imports", "help", "h":
			flags[k] = "1"
		case "local":
			flags[k] = "1"
		default:
			// ค่าถัดไปเป็น flag อีกตัว (เช่น `--remote --embedding-model …`) = flag นี้ไม่มีค่า
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
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
		fmt.Printf("resolved: kept %s for %s · %v · %v\n", keep, note, om["done"], om["next"])
	}
}
