// Package mcp — MCP server "blm" ผ่าน stdio JSON-RPC แบบบรรทัดต่อบรรทัด ไม่พึ่ง SDK
// (สเปกส่วนที่ใช้มีสามอย่าง: initialize · tools/list · tools/call) · รันด้วย `blm mcp` cwd = โปรเจ็คที่เปิดอยู่ (หรือ BLM_ROOT)
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Ekkapap/business-logic-memory/internal/blm"
)

type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}
func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func enum(desc string, vals ...string) map[string]any {
	return map[string]any{"type": "string", "enum": vals, "description": desc}
}
func strArr(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}

var noteProps = map[string]any{
	"name":        str("temp note name (kebab-case)"),
	"content":     str("markdown body"),
	"target":      str("destination note name (default = name) · business rules = blm"),
	"mode":        enum("how to apply on sync (default append)", "append", "replace"),
	"folder":      str("features | features/<x> | global/architecture|conventions|pitfalls (omit = guessed from mirror)"),
	"description": str("retrieval cue of the destination note (required when creating / replace)"),
	"tags":        strArr(""),
}

func tools(c blm.Config) []tool {
	rowSchema := map[string]any{"type": "array", "items": obj(map[string]any{
		"topic": str(""), "heading": str(""), "result": enum("", "PASSED", "NOT PASSED", "UNKNOWN"), "ref": str("block.refShort e.g. blm.md:29"),
	}, "topic", "heading", "result", "ref")}
	all := []tool{
		{"blm", "Read the project's current business rules (blm.md — the single source of truth, changed only by the owner). No query = whole file · a word/heading = grep, returns the whole block of every matching subtopic with its main topic. Also reports which blocks changed since the last read and whether temp drafts are waiting for confirmation. Call it yourself whenever memory conflicts with code.",
			obj(map[string]any{"query": str("word or heading to search (omit = whole file)"), "trigger": enum("who asked: user (via /blm) or agent on its own (default) — recorded in stats", "user", "agent")})},
		{"blm_get", "Read one temp note in full", obj(map[string]any{"name": noteProps["name"]}, "name")},
		{"blm_save", "Create/overwrite a whole temp note (like memory_save mode replace). Use it to jot what should reach the memory backend at the end of the session instead of calling memory_save mid-task", obj(noteProps, "name", "content")},
		{"blm_update", "Append to an existing temp note (like memory_save mode append); metadata can be changed at the same time", obj(noteProps, "name", "content")},
		{"blm_patch", "Replace an exact string in a temp note (must match exactly once)", obj(map[string]any{"name": noteProps["name"], "find": str(""), "replace": str("")}, "name", "find", "replace")},
		{"blm_delete", "Delete a temp note (the previous version is snapshotted to history/)", obj(map[string]any{"name": noteProps["name"]}, "name")},
		{"blm_edit", "Edit a few lines of an EXISTING backend note without reading it: blm loads the note from the local mirror (or the pending draft), replaces `find` with `replace` (must match exactly once) and stores the whole result as a replace-draft in temp. Returns only 3 lines of context around the change. Push later with blm_sync. Use this instead of memory_get + memory_save.",
			obj(map[string]any{"target": str("backend note name (e.g. line-login-linking)"), "find": str("exact text to replace (must occur once)"), "replace": str("new text")}, "target", "find", "replace")},
		{"blm_scan", "Survey the repo before /blm_init: file/byte/token totals with .gitignore + .socraticodeignore + .ignorememory applied, per-top-dir stats, memory note count, and SUB-PROJECTS (folders with their own go.mod/package.json/PLANNING.md/… — propose each as its own main topic). Generic: no project names hard-coded. Warns when the whole tree exceeds the token budget.",
			obj(map[string]any{"path": str("sub path to survey (omit = whole project)"), "tokenWarn": map[string]any{"type": "number", "description": "warn above this many tokens (default 200000)"}})},
		{"blm_diff", "Compare a replace-draft with the cloud note it was taken from: did the cloud change since (cloudChanged), which lines changed on each side (diffCloud / diffMine, a few lines each), and whether the regions overlap. Only the changed lines leave blm — never whole notes. Run before pushing a draft that has waited a while, or when blm_sync reports a conflict.",
			obj(map[string]any{"name": noteProps["name"]}, "name")},
		{"blm_merge", "Resolve a conflicting draft after blm_diff. keep=mine: re-apply the draft's changes on top of the current cloud version (3-way, refuses on overlapping regions) · keep=cloud: drop the draft (snapshot to history) · keep=content: store the text you merged by hand. Owner decides; the replaced version always lands in history/.",
			obj(map[string]any{"name": noteProps["name"], "keep": enum("", "mine", "cloud", "content"), "content": str("merged body when keep=content")}, "name", "keep")},
		{"blm_graph", "Built-in repo analysis when no SocratiCode/graphify/Obsidian is available: builds (once, cached in <store>/graph.json) a file graph — with tree-sitter (ast-grep, `blm tools install tree-sitter`) from real AST: imports, definitions and cross-file calls; without it a coarse regex fallback (imports only, marked engine=regex). hubs = most imported/called files, clusters per folder with top symbols, query = find files/symbols matching a word with neighbours (who imports/calls whom). Meaning is not in the graph: install `embedding` (Ollama) or infer it yourself.",
			obj(map[string]any{"query": str("word to look up (symbol or path); omit = summary"), "path": str("sub path to build from (omit = whole project)"), "rebuild": map[string]any{"type": "boolean", "description": "rebuild graph.json even if cached"}, "html": map[string]any{"type": "boolean", "description": "also write <store>/graph.html — an interactive force graph to open in a browser"}, "limit": map[string]any{"type": "number"}})},
		{"blm_status", "Everything at once: readiness, binary/project paths, backend/store/mirror, rules file (topics/rules, last read), temp notes + size, history/reports, configured neighbour tools and rtk-gain-style stats — returns `terminal` ready to print", obj(map[string]any{})},
		{"blm_report", "With rows: compose and save a rules check report (the tool aligns columns by real monospace width, writes reports/<date>-businesslogic.md, records a check stat) and returns `terminal` to print verbatim · without rows: read the latest report (filter by name)",
			obj(map[string]any{
				"name":    str("report name, kebab-case (read: omit = latest of any name)"),
				"scope":   str("topic checked (empty = All)"),
				"before":  str("Current Understanding Before Test (markdown)"),
				"after":   str("Current Understanding After Test (markdown)"),
				"rows":    rowSchema,
				"trigger": enum("", "user", "agent"),
				"content": str("save a free-form report (whole markdown) instead of composing from rows"),
			})},
		{"blm_stat", "Record one stat event (summary in blm_status): lookup = owner could not remember, agent searched the rules {query} · override = owner changed a rule after confirming {topic, note?} · fixed = how many NOT PASSED of the last check the agent corrected on its own from the rules file {count}",
			obj(map[string]any{"event": enum("", "lookup", "override", "fixed"), "query": str(""), "topic": str(""), "note": str(""), "count": map[string]any{"type": "number"}}, "event")},
		{"blm_tools", "Manage neighbour tools (socraticode | obsidian | graphify): status · get · install · start · stop · restart · gen-graph · help — acts when it knows the command, otherwise returns the command for the user to run",
			obj(map[string]any{"action": enum("", "status", "get", "install", "start", "stop", "restart", "gen-graph", "help"), "tool": enum("", "socraticode", "obsidian", "graphify"), "docker": map[string]any{"type": "boolean", "description": "socraticode: run Qdrant + Ollama in Docker instead of native (required on Windows)"}}, "action")},
		{"blm_sync", "push: sync temp notes into the backend. apply=true (preferred): blm spawns the AgentsRoom MCP itself, runs memory_save one note at a time (parallel:true to send all at once once the AgentsRoom build supports it), verifies with memory_list once, archives only what is verified and returns ok/verified/error per note — no retries — no note content passes through your context. Without apply: returns the plan for you to execute by hand. pull: refresh mirror/store from the backend before reading rules",
			obj(map[string]any{
				"direction": enum("push (default) | pull", "push", "pull"),
				"apply":     map[string]any{"type": "boolean", "description": "true = blm performs the memory_save/memory_delete calls itself (parallel) and archives; false = return the plan only"},
				"author":    str("agent display name for AgentsRoom (with apply)"),
				"role":      str("canonical role id for AgentsRoom, e.g. fullstack (with apply)"),
				"delete":    strArr("backend notes to delete after the push (e.g. an old name after a rename; with apply)"),
				"done":      strArr("manual mode only: temp notes already pushed successfully → archive them"),
				"names":     strArr("restrict the plan to these notes (omit = all)"),
				"full":      map[string]any{"type": "boolean", "description": "manual mode only: include note bodies in the plan (default false — bodies stay out of your context)"},
				"restore":   strArr("move these notes back from .synced/ into the store (recover items archived by an earlier push that did not really land)"),
				"parallel":  map[string]any{"type": "boolean", "description": "with apply: send all saves at once instead of one by one (default false — the AgentsRoom build installed on 2026-09-09 drops concurrent saves; enable after it is updated)"},
			})},
	}
	if !c.HasSync() {
		// backend ที่ไม่มีปลายทาง (none/obsidian) ไม่ประกาศ blm_sync เลย — เรียกไม่ได้ ไม่ใช่เรียกแล้วบอกว่า skip
		all = all[:len(all)-1]
	}
	return all
}

type Server struct {
	root  string
	cfg   blm.Config
	store *blm.Store
	tools []tool
}

func New(root string) *Server {
	cfg := blm.Load(root)
	return &Server{root: root, cfg: cfg, store: blm.Open(root, cfg), tools: tools(cfg)}
}

func getStr(a map[string]any, k string) string {
	s, _ := a[k].(string)
	return s
}
func getArr(a map[string]any, k string) []string {
	raw, _ := a[k].([]any)
	var out []string
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// Call เรียก tool หนึ่งตัว — ใช้ทั้งจาก JSON-RPC และ CLI (โค้ดชุดเดียว)
func (s *Server) Call(name string, a map[string]any) (any, error) {
	in := blm.Input{Name: getStr(a, "name"), Target: getStr(a, "target"), Mode: getStr(a, "mode"), Folder: getStr(a, "folder"), Description: getStr(a, "description")}
	if v, ok := a["content"].(string); ok {
		in.Content, in.HasContent = v, true
	}
	if _, ok := a["tags"]; ok {
		in.Tags = getArr(a, "tags")
	}
	switch name {
	case "blm":
		trigger := "agent"
		if getStr(a, "trigger") == "user" {
			trigger = "user"
		}
		return s.store.Rules(getStr(a, "query"), trigger), nil
	case "blm_get":
		return s.store.Get(in.Name)
	// เขียน/แก้: คืนแค่ metadata + ขนาด ไม่คืนเนื้อโน้ต (2026-09-09: blm_patch คืนทั้งก้อน 7 ครั้ง = 170 KB เข้า context โดยไม่จำเป็น)
	case "blm_save":
		n, err := s.store.Save(in)
		return brief(n, err), err
	case "blm_update":
		n, err := s.store.Update(in)
		return brief(n, err), err
	case "blm_patch":
		n, err := s.store.Patch(in.Name, getStr(a, "find"), getStr(a, "replace"))
		return brief(n, err), err
	case "blm_delete":
		return map[string]any{"ok": true}, s.store.Delete(in.Name)
	case "blm_edit":
		n, ctx, err := s.store.Edit(getStr(a, "target"), getStr(a, "find"), getStr(a, "replace"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "draft": n.Name, "mode": n.Mode, "folder": n.Folder, "bytes": len(n.Content), "context": ctx, "next": "push with blm_sync {apply:true} when the owner says update memory"}, nil
	case "blm_scan":
		tw, _ := a["tokenWarn"].(float64)
		r := blm.ScanRepo(s.root, getStr(a, "path"), int64(tw))
		return map[string]any{"scan": r, "terminal": blm.RenderScan(r)}, nil
	case "blm_diff":
		return s.store.Diff(in.Name)
	case "blm_merge":
		return s.store.Merge(in.Name, getStr(a, "keep"), getStr(a, "content"))
	case "blm_graph":
		rebuild, _ := a["rebuild"].(bool)
		g, ok := s.store.LoadGraph()
		if !ok || rebuild || getStr(a, "path") != "" {
			var err error
			if g, err = s.store.BuildGraph(getStr(a, "path")); err != nil {
				return nil, err
			}
		}
		if q := getStr(a, "query"); q != "" {
			lim, _ := a["limit"].(float64)
			hits := blm.GraphQuery(g, q, int(lim))
			return map[string]any{"query": q, "hits": hits, "builtAt": g.BuiltAt}, nil
		}
		var htmlPath string
		if h, _ := a["html"].(bool); h {
			htmlPath, _ = s.store.WriteGraphHTML(g)
		}
		res := map[string]any{"html": htmlPath, "summary": map[string]any{"engine": g.Engine, "builtAt": g.BuiltAt, "files": g.Files, "edges": len(g.Edges), "callEdges": g.Calls, "unlinked": g.Unlinked, "hubs": g.Hubs, "clusters": blm.ClustersHead(g.Clusters, 25)}, "terminal": blm.RenderGraph(g)}
		if g.Engine != "ast-grep" {
			res["hint"] = "coarse regex mode — no tree-sitter on this machine. Ask the owner: `blm tools install tree-sitter` (ast-grep) for a real AST + call graph; for meaning either `blm tools install embedding` (Ollama, native or --docker) or infer meaning yourself from the code you read"
		}
		return res, nil
	case "blm_status":
		st := s.store.Status(s.cfg)
		return map[string]any{"status": st, "terminal": blm.RenderStatus(st)}, nil
	case "blm_report":
		if rows, ok := a["rows"].([]any); ok && len(rows) > 0 {
			var cr []blm.CheckRow
			for _, r := range rows {
				m, _ := r.(map[string]any)
				row := blm.CheckRow{Topic: getStr(m, "topic"), Heading: getStr(m, "heading"), Result: getStr(m, "result"), Ref: getStr(m, "ref")}
				if !blm.ValidResult(row.Result) {
					return nil, fmt.Errorf("result must be PASSED | NOT PASSED | UNKNOWN (got %q)", row.Result)
				}
				cr = append(cr, row)
			}
			trigger := "agent"
			if getStr(a, "trigger") == "user" {
				trigger = "user"
			}
			return s.store.CheckReport(filepath.Base(s.root), getStr(a, "scope"), getStr(a, "before"), getStr(a, "after"), cr, trigger)
		}
		if c, ok := a["content"].(string); ok {
			file, err := s.store.SaveReport(in.Name, c)
			return map[string]any{"ok": err == nil, "file": file}, err
		}
		if r := s.store.LatestReport(in.Name); r != nil {
			return r, nil
		}
		return map[string]any{"ok": false, "message": "no reports yet in " + s.cfg.Store + "/reports/"}, nil
	case "blm_stat":
		ev := map[string]any{"event": getStr(a, "event")}
		switch ev["event"] {
		case "lookup":
			ev["query"] = getStr(a, "query")
		case "override":
			ev["topic"], ev["note"] = getStr(a, "topic"), getStr(a, "note")
		case "fixed":
			ev["count"], _ = a["count"].(float64)
			ev["query"] = getStr(a, "query")
		default:
			return nil, fmt.Errorf("event must be lookup | override | fixed")
		}
		return map[string]any{"ok": true, "recorded": s.store.RecordStat(ev)}, nil
	case "blm_tools":
		docker, _ := a["docker"].(bool)
		var log strings.Builder
		out, err := blm.Tools(s.root, s.cfg, getStr(a, "action"), getStr(a, "tool"), docker, &log)
		if log.Len() > 0 {
			out = strings.TrimRight(log.String(), "\n") + "\n" + out
		}
		return map[string]any{"terminal": out}, err
	case "blm_sync":
		if !s.cfg.HasSync() {
			return nil, fmt.Errorf("backend %s has no sync target", s.cfg.Backend)
		}
		apply, _ := a["apply"].(bool)
		full, _ := a["full"].(bool)
		if restore := getArr(a, "restore"); len(restore) > 0 {
			var back []string
			for _, n := range restore {
				note, err := s.store.Unarchive(n)
				if err != nil {
					return nil, err
				}
				back = append(back, note)
			}
			return map[string]any{"ok": true, "restored": back}, nil
		}
		par, _ := a["parallel"].(bool)
		return s.sync(getStr(a, "direction"), getArr(a, "done"), getArr(a, "names"), apply, getStr(a, "author"), getStr(a, "role"), getArr(a, "delete"), full, par)
	}
	return nil, fmt.Errorf("unknown tool %s", name)
}

func (s *Server) sync(direction string, done, names []string, apply bool, author, role string, deletes []string, full, parallel bool) (any, error) {
	if direction == "pull" {
		if s.cfg.Backend == blm.BackendCustom {
			return map[string]any{"command": s.cfg.PullCommand, "next": "run command via Bash, then read the rules again with blm"}, nil
		}
		if apply {
			// memory_list บังคับ AgentsRoom fetch ใหม่และเขียน mirror — เรียกเองไม่ต้องให้ agent ทำ
			c, err := s.agentsRoom()
			if err != nil {
				return nil, err
			}
			defer c.Close()
			t := time.Now()
			if _, err := c.CallTool("memory_list", map[string]any{}); err != nil {
				return nil, err
			}
			return map[string]any{"ok": true, "ms": time.Since(t).Milliseconds(), "next": "mirror " + s.cfg.Mirror + " refreshed — call blm again"}, nil
		}
		return map[string]any{"next": "call AgentsRoom `memory_list` to force a fresh fetch (mirror " + s.cfg.Mirror + " is overwritten with the real notes), then call blm again — or blm_sync {direction:pull, apply:true}"}, nil
	}
	if len(done) > 0 {
		var archived []string
		for _, n := range done {
			p, err := s.store.Archive(n)
			if err != nil {
				return nil, err
			}
			archived = append(archived, p)
		}
		var remaining []string
		for _, n := range s.store.List() {
			remaining = append(remaining, n.Name)
		}
		return map[string]any{"ok": true, "archived": archived, "remaining": remaining}, nil
	}
	notes := s.store.List()
	if len(names) > 0 {
		set := map[string]bool{}
		for _, n := range names {
			set[n] = true
		}
		var only []blm.Note
		for _, n := range notes {
			if set[n.Name] {
				only = append(only, n)
			}
		}
		notes = only
	}
	plan := s.store.PlanSync(notes)
	if apply && s.cfg.Backend == blm.BackendAgentsRoom {
		if len(plan) == 0 && len(deletes) == 0 {
			return map[string]any{"ok": true, "pushed": []any{}, "next": "no temp notes to sync"}, nil
		}
		c, err := s.agentsRoom()
		if err != nil {
			return nil, err
		}
		defer c.Close()
		if author == "" {
			author = "blm"
		}
		if role == "" {
			role = "fullstack"
		}
		t := time.Now()
		pushed, deleted := s.store.PushAll(c, plan, author, role, deletes, parallel)
		failed := 0
		for _, r := range pushed {
			if !r.Verified {
				failed++
			}
		}
		for _, r := range deleted {
			if !r.Verified {
				failed++
			}
		}
		var remaining []string
		for _, n := range s.store.List() {
			remaining = append(remaining, n.Name)
		}
		return map[string]any{"ok": failed == 0, "pushed": pushed, "deleted": deleted, "failed": failed, "ms": time.Since(t).Milliseconds(), "remaining": remaining, "warnings": planWarnings(plan)}, nil
	}
	next := "no temp notes to sync"
	if s.cfg.Backend == blm.BackendCustom {
		for i := range plan {
			it := &plan[i]
			desc, _ := json.Marshal(it.MemorySave.Description)
			r := strings.NewReplacer("{name}", it.MemorySave.Name, "{folder}", it.MemorySave.Folder, "{description}", string(desc), "{file}", filepath.Join(s.store.Dir, it.From[0]+".md"))
			it.Command = r.Replace(s.cfg.PushCommand)
		}
		if len(plan) > 0 {
			next = "every plan item targets a different note — run all plan[].command in parallel (one Bash call joining them with & and wait), then call blm_sync again with done = plan[].from of the items that succeeded"
		}
	} else if len(plan) > 0 {
		next = "every plan item targets a different note — issue ALL memory_save calls in parallel (one message, one tool call per plan[].memory_save, add author/role; AgentsRoom is slow, sequential calls multiply the wait), then call blm_sync again with done = plan[].from of the items that succeeded"
	}
	// รวมโน้ต target เดียวกันแล้ว → ทุกรายการคนละโน้ต ยิงพร้อมกันได้ (เจ้าของสั่ง 2026-09-09: memory_save ช้า ห้ามรอทีละตัว)
	// เนื้อโน้ตไม่ส่งกลับ (มันจะเข้า context ของ agent) เว้นแต่ขอ full เพื่อทำมือ — ปกติใช้ apply:true ให้ blm ส่งเอง
	if !full {
		type brief struct {
			Name         string   `json:"name"`
			Mode         string   `json:"mode"`
			Folder       string   `json:"folder,omitempty"`
			Bytes        int      `json:"bytes"`
			From         []string `json:"from"`
			TargetExists bool     `json:"targetExists"`
			Warnings     []string `json:"warnings"`
		}
		var briefs []brief
		for _, it := range plan {
			briefs = append(briefs, brief{it.MemorySave.Name, it.MemorySave.Mode, it.MemorySave.Folder, len(it.MemorySave.Content), it.From, it.TargetExists, it.Warnings})
		}
		if briefs == nil {
			briefs = []brief{}
		}
		return map[string]any{"plan": briefs, "parallel": true, "next": "preview only — run blm_sync {apply:true, author, role} to push; pass full:true only if you must push by hand"}, nil
	}
	return map[string]any{"plan": plan, "parallel": true, "next": next}, nil
}

func brief(n blm.Note, err error) map[string]any {
	return map[string]any{"ok": err == nil, "name": n.Name, "target": n.Target, "mode": n.Mode, "folder": n.Folder, "bytes": len(n.Content), "updatedAt": n.UpdatedAt}
}

func (s *Server) agentsRoom() (*blm.Client, error) {
	e, err := blm.LoadAgentsRoomMCP(s.root)
	if err != nil {
		return nil, err
	}
	return blm.Connect(e)
}

func planWarnings(plan []blm.SyncItem) []string {
	var w []string
	for _, it := range plan {
		for _, x := range it.Warnings {
			w = append(w, it.MemorySave.Name+": "+x)
		}
	}
	if w == nil {
		w = []string{}
	}
	return w
}

// ---- JSON-RPC over stdio -----------------------------------------------------

type rpc struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params struct {
		ProtocolVersion string         `json:"protocolVersion"`
		Name            string         `json:"name"`
		Arguments       map[string]any `json:"arguments"`
	} `json:"params"`
}

func (s *Server) Serve(in io.Reader, out io.Writer) {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	enc := json.NewEncoder(out)
	send := func(id json.RawMessage, result any, rpcErr map[string]any) {
		m := map[string]any{"jsonrpc": "2.0", "id": id}
		if rpcErr != nil {
			m["error"] = rpcErr
		} else {
			m["result"] = result
		}
		_ = enc.Encode(m)
	}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var msg rpc
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			send(nil, nil, map[string]any{"code": -32700, "message": "parse error"})
			continue
		}
		if len(msg.ID) == 0 || string(msg.ID) == "null" {
			continue // notifications (initialized, cancelled) — nothing to answer
		}
		switch msg.Method {
		case "initialize":
			pv := msg.Params.ProtocolVersion
			if pv == "" {
				pv = "2024-11-05"
			}
			send(msg.ID, map[string]any{"protocolVersion": pv, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "blm", "version": blm.Version}}, nil)
		case "ping":
			send(msg.ID, map[string]any{}, nil)
		case "tools/list":
			send(msg.ID, map[string]any{"tools": s.tools}, nil)
		case "tools/call":
			args := msg.Params.Arguments
			if args == nil {
				args = map[string]any{}
			}
			res, err := s.Call(msg.Params.Name, args)
			if err != nil {
				send(msg.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": err.Error()}}, "isError": true}, nil)
				continue
			}
			text, _ := json.MarshalIndent(res, "", "  ")
			send(msg.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": string(text)}}}, nil)
		default:
			send(msg.ID, nil, map[string]any{"code": -32601, "message": "method not found: " + msg.Method})
		}
	}
}

// Root โปรเจ็คที่ server ทำงานอยู่: BLM_ROOT หรือ cwd
func Root() string {
	if r := os.Getenv("BLM_ROOT"); r != "" {
		return r
	}
	wd, _ := os.Getwd()
	return wd
}
