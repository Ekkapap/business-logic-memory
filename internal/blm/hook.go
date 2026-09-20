package blm

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Claude Code hooks + statusline สำหรับ SocratiCode — ย้ายมาจาก ~/.claude/helpers/socraticode-{hooks,statusline}.cjs (2026-09-20)
// เพื่อให้ plugin blm ตัวเดียวถือทุกอย่าง · ลอกสัญญา I/O จาก graft: JSON เข้าทาง stdin, ฉีด context ออก stdout เป็น
//   {"hookSpecificOutput":{"hookEventName":"<event>","additionalContext":"..."}}
// ทุกทางล้มเงียบ (stack ปิด / ไม่มี index / error) = ไม่พิมพ์อะไร exit 0 — hook ห้ามพัง session
//
//   blm hook session-start   core brief ของ blm.md (ทุก source รวม compact) + สถานะ index + "ถาม blm_search ก่อน grep"
//   blm hook prompt          core brief เมื่อพ้น debounce (corebrief.go) + prompt ≥ 12 ตัวอักษร → pointer 3 บรรทัด (ไม่ inline โค้ด)
//   blm hook post-edit       หลัง Write/Edit: core brief เมื่อพ้น debounce — งานยาวที่ไม่มี prompt ใหม่ก็ยังได้แก่นกฎกลับมา
//   blm hook grep-nudge      หลัง Grep/Glob: เตือนว่ามี index
// ทุก event ผ่านคำสั่งกลางเดียว (`blm hook <event>`) สถานะ debounce ต่อ session อยู่ในไฟล์ temp (hookState)
//   blm statusline [--top-only]   `socraticode : online · N nodes / M edges · ✓ synced|⟳ indexing` (+ บรรทัด ctx/session)

const hookMinPromptChars = 12

// HookInput ฟิลด์ที่ Claude Code ส่งมาทาง stdin (เท่าที่ใช้)
type HookInput struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	Prompt    string `json:"prompt"`
	// Source ของ SessionStart: startup | resume | clear | compact | fork
	Source    string `json:"source"`
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Pattern  string `json:"pattern"`
		FilePath string `json:"file_path"`
	} `json:"tool_input"`
	Agent struct {
		Name string `json:"name"`
	} `json:"agent"`
	ContextWindow struct {
		UsedPercentage *float64 `json:"used_percentage"`
	} `json:"context_window"`
}

// ReadHookInput อ่าน JSON ของ Claude Code จาก stdin (ว่าง/พัง = struct ว่าง)
func ReadHookInput(r io.Reader) HookInput {
	var in HookInput
	raw, _ := io.ReadAll(r)
	_ = json.Unmarshal(raw, &in)
	return in
}

// hookTrailer — ท้ายทุกข้อความที่ระบบยิงให้ agent เอง (เจ้าของ 2026-09-21): by · related · next เหมือนผลลัพธ์ของ MCP tool
var hookTrailer = map[string]string{
	"session-start": "related: blm, blm_search · next: say in one line that the brief loaded · before touching a topic → blm {query}",
	"prompt":        "related: blm_search, blm_update · next: read a hit → blm_search {get, ids} · review submitted → blm_update {from}",
	"post-edit":     "related: blm · next: the edit touches a topic above → blm {query} before going on",
	"grep-nudge":    "related: blm_search, blm_grep · next: blm_search {query} (Thai/English) instead of Grep/Glob",
}

func hookEmit(w io.Writer, event, text string) {
	raw, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": event, "additionalContext": text}})
	_, _ = w.Write(raw)
}

// Hook รัน event หนึ่งตัว · root = โฟลเดอร์โปรเจ็ค (CLAUDE_PROJECT_DIR → cwd ของ hook)
func Hook(event string, in HookInput, root string, w io.Writer) error {
	c := Load(root)
	switch event {
	case "session-start":
		// แก่นกฎก่อนเสมอ — โดยเฉพาะ source=compact ที่บริบทเดิมเพิ่งหาย
		var parts []string
		if core := coreBriefIfDue(root, c, in.SessionID, true); core != "" {
			if in.Source == "compact" {
				core = "[blm] context was just compacted — re-anchoring on the project's business logic.\n" + core
			}
			// เจ้าของ 2026-09-21: บอกให้รู้ว่า brief มาถึงแล้ว — เฉพาะ session-start (new session / compact) ไม่ใช่ทุก prompt
			core += "\n[blm] Tell the owner in one line at the start of your next reply that the blm core brief was loaded" +
				map[bool]string{true: " after compaction", false: " for this new session"}[in.Source == "compact"] + " (topics above) — do not repeat the list."
			parts = append(parts, core)
		}
		if s := scStats(root, c); s.Online {
			state := "index: none for this project (run codebase_index)"
			if s.Meta != nil {
				state = fmt.Sprintf("index: %d files · %d symbols / %d call edges", s.Meta.FileCount, s.Meta.SymbolCount, s.Meta.EdgeCount)
			}
			parts = append(parts, "[socraticode] "+state+". Ask the index before grepping: blm_search {query} (Thai or English; lang:\"typescript\" drops .md), "+
				"codebase_impact <file> before changing a symbol, codebase_symbol for one definition.")
		}
		if len(parts) > 0 {
			hookEmit(w, "SessionStart", strings.Join(parts, "\n\n")+"\n— by blm hook session-start · "+hookTrailer["session-start"])
		}
	case "prompt":
		var parts []string
		// ก่อนลงมือ: แก่นกฎกลับมาเมื่อพ้น debounce (ไม่ใช่ทุก prompt)
		if core := coreBriefIfDue(root, c, in.SessionID, false); core != "" {
			parts = append(parts, core)
		}
		// เจ้าของ submit หน้ารีวิว (blm update --html) แล้ว agent ยังไม่อ่าน — นี่คือสัญญาณเดียวที่ข้ามจากเบราว์เซอร์มาถึง agent
		for _, r := range Open(root, c).PendingReviews() {
			parts = append(parts, fmt.Sprintf("[blm] review submitted by the owner (round %d): %s — read it now with blm_update {from:%q} and answer with a proposal before anything else", r.Round-1, r.File, r.File))
		}
		p := strings.TrimSpace(in.Prompt)
		if len([]rune(p)) >= hookMinPromptChars {
			st := readHookState(in.SessionID)
			if p != st.LastQuery { // prompt เดิมซ้ำ (เช่น retry) ไม่ค้นซ้ำ
				if res, err := Search(root, c, SearchOpts{Query: p, Limit: 3, Brief: true}); err == nil && len(res.Hits) > 0 {
					var lines []string
					for i, h := range res.Hits {
						lines = append(lines, fmt.Sprintf(" %d. %s:L%d-L%d (%.2f)", i+1, h.Path, h.StartLine, h.EndLine, h.Score))
					}
					parts = append(parts, "[socraticode] starting points for this prompt (RRF, 1.0 = top of both semantic and keyword):\n"+
						strings.Join(lines, "\n")+"\nRead the span if it fits; otherwise blm_search with a narrower query.")
				}
				st = readHookState(in.SessionID) // coreBriefIfDue อาจเขียนไปแล้ว
				st.LastQuery = p
				writeHookState(in.SessionID, st)
			}
		}
		if len(parts) > 0 {
			hookEmit(w, "UserPromptSubmit", strings.Join(parts, "\n\n")+"\n— by blm hook prompt · "+hookTrailer["prompt"])
		}
	case "post-edit":
		// ระหว่างแก้ไฟล์รัว ๆ: ฉีดเฉพาะเมื่อพ้น debounce — ไม่ใช่ทุกครั้ง
		if core := coreBriefIfDue(root, c, in.SessionID, false); core != "" {
			hookEmit(w, "PostToolUse", core+"\n— by blm hook post-edit · "+hookTrailer["post-edit"])
		}
	case "grep-nudge":
		s := scStats(root, c)
		if !s.Online || s.Meta == nil {
			return nil
		}
		msg := "[socraticode] this repo is indexed — for \"where is / how does\" questions prefer blm_search over " + in.ToolName
		if p := in.ToolInput.Pattern; p != "" {
			if r := []rune(p); len(r) > 60 {
				p = string(r[:60])
			}
			msg += fmt.Sprintf(" (you just searched %q)", p)
		}
		hookEmit(w, "PostToolUse", msg+".\n— by blm hook grep-nudge · "+hookTrailer["grep-nudge"])
	default:
		return fmt.Errorf("unknown hook event %q (session-start | prompt | post-edit | grep-nudge)", event)
	}
	return nil
}

// ---- statusline -----------------------------------------------------------------

// symgraphMeta = payload.meta ของ point เดียวใน <id>_symgraph_meta (socraticode code-graph.ts:608)
type symgraphMeta struct {
	SymbolCount int   `json:"symbolCount"`
	EdgeCount   int   `json:"edgeCount"`
	FileCount   int   `json:"fileCount"`
	BuiltAt     int64 `json:"builtAt"` // ms
}

type scStatsCache struct {
	At     int64         `json:"at"`
	Online bool          `json:"online"`
	Meta   *symgraphMeta `json:"meta"`
}

const scStatsTTL = 10 * time.Second

// scStats — online? + meta ของ graph · แคช 10 วิใน temp เพราะ statusline ถูกเรียกทุก tick ของ UI (round-trip ไป server ไม่ควรโดนทุกครั้ง)
// meta ล่าสุดเก็บไว้แม้ offline ให้แถบบอกได้ว่า index เมื่อไร
func scStats(root string, c Config) scStatsCache {
	pid := SocratiCodeProjectID(root)
	file := filepath.Join(os.TempDir(), "blm-socraticode-stats-"+pid+".json")
	var cached scStatsCache
	if raw, err := os.ReadFile(file); err == nil && json.Unmarshal(raw, &cached) == nil && time.Since(time.UnixMilli(cached.At)) < scStatsTTL {
		return cached
	}
	e := scEndpointFor(root)
	next := scStatsCache{At: time.Now().UnixMilli(), Meta: cached.Meta}
	next.Online = httpUp(e.Ollama+"/api/tags") && httpUp(e.Qdrant+"/collections")
	var out struct {
		Result struct {
			Points []struct {
				Payload struct {
					Meta symgraphMeta `json:"meta"`
				} `json:"payload"`
			} `json:"points"`
		} `json:"result"`
	}
	if err := qdrantPost(e.Qdrant, "/collections/"+e.CollectionPrefix+pid+"_symgraph_meta/points/scroll", map[string]any{"limit": 1, "with_payload": true, "with_vector": false}, &out); err == nil && len(out.Result.Points) > 0 {
		m := out.Result.Points[0].Payload.Meta
		next.Meta = &m
	}
	if raw, err := json.Marshal(next); err == nil {
		_ = os.WriteFile(file, raw, 0o644)
	}
	return next
}

// newestDirtyMtime — mtime ใหม่สุดของไฟล์ที่ git เห็นว่าเปลี่ยน **และ socraticode index จริง**: ให้ git ประเมิน .socraticodeignore
// (syntax เดียวกับ gitignore รวม negation) ผ่าน core.excludesFile — ไฟล์ที่ index ไม่เห็น (.claude/*, store ของ blm) ไม่ทำให้ watcher rebuild
// จึงต้องไม่นับเป็น pending (บั๊กวันแรก 2026-09-20: แก้ .claude/settings.json แล้วแถบค้าง pending ตลอด)
func newestDirtyMtime(root string) int64 {
	git := func(input string, args ...string) (string, int) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if input != "" {
			cmd.Stdin = strings.NewReader(input)
		}
		out, err := cmd.Output()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			code = -1
		}
		return string(out), code
	}
	out, code := git("", "status", "--porcelain", "--untracked-files=all")
	if code != 0 {
		return 0
	}
	var files []string
	for _, l := range strings.Split(out, "\n") {
		if len(l) > 3 {
			f := l[3:]
			if i := strings.LastIndex(f, " -> "); i >= 0 {
				f = f[i+4:]
			}
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return 0
	}
	ignored := map[string]bool{}
	if ig := filepath.Join(root, ".socraticodeignore"); exists(ig) {
		out, code := git(strings.Join(files, "\n"), "-c", "core.excludesFile="+ig, "check-ignore", "--no-index", "--stdin")
		if code == 0 { // 1 = ไม่มีไฟล์ไหนถูก ignore
			for _, f := range strings.Split(out, "\n") {
				if f != "" {
					ignored[f] = true
				}
			}
		}
	}
	var newest int64
	for _, f := range files {
		if ignored[f] {
			continue
		}
		if fi, err := os.Stat(filepath.Join(root, f)); err == nil && fi.ModTime().UnixMilli() > newest {
			newest = fi.ModTime().UnixMilli()
		}
	}
	return newest
}

// ANSI โทนเดียวกับ graft (แยกจาก Color ของ status เพราะ statusline ไม่ใช่ TTY แต่ Claude Code render ANSI ให้)
var slC = struct{ blue, green, amber, muted, text func(string) string }{
	blue:  func(s string) string { return "\x1b[38;2;84;111;255m" + s + "\x1b[0m" },
	green: func(s string) string { return "\x1b[38;2;80;200;120m" + s + "\x1b[0m" },
	amber: func(s string) string { return "\x1b[38;2;224;165;68m" + s + "\x1b[0m" },
	muted: func(s string) string { return "\x1b[38;5;244m" + s + "\x1b[0m" },
	text:  func(s string) string { return "\x1b[38;5;251m" + s + "\x1b[0m" },
}

// Statusline — บรรทัดบน: สถานะ index · บรรทัดล่าง (ไม่ใช่ topOnly): ctx% / agent / session
// topOnly ใช้เมื่อถูก include จาก statusline หลักของผู้ใช้ (~/.claude/statusline-command.sh) ที่แสดง ctx อยู่แล้ว
func Statusline(in HookInput, root string, topOnly bool) string {
	c := Load(root)
	s := scStats(root, c)
	sep := slC.muted(" · ")
	top := []string{slC.blue("socraticode") + slC.muted(" : ") + map[bool]string{true: slC.green("online"), false: "offline"}[s.Online]}
	if s.Meta != nil {
		top = append(top, slC.text(fmt.Sprintf("%d nodes / %d edges", s.Meta.SymbolCount, s.Meta.EdgeCount)))
		switch {
		case !s.Online:
			top = append(top, slC.muted("indexed "+time.UnixMilli(s.Meta.BuiltAt).Format("02/01 15:04")))
		case newestDirtyMtime(root) > s.Meta.BuiltAt:
			top = append(top, slC.amber("⟳ indexing"))
		default:
			top = append(top, slC.green("✓ synced"))
		}
	} else if s.Online {
		top = append(top, slC.amber("not indexed"))
	}
	lines := []string{strings.Join(top, sep)}
	if !topOnly {
		var bottom []string
		if p := in.ContextWindow.UsedPercentage; p != nil {
			bottom = append(bottom, slC.text(fmt.Sprintf("ctx %d%%", int(*p+0.5))))
		}
		if in.Agent.Name != "" {
			bottom = append(bottom, slC.muted("agent: ")+slC.text(in.Agent.Name))
		}
		if id := in.SessionID; id != "" {
			if r := []rune(id); len(r) > 8 {
				id = string(r[:8])
			}
			bottom = append(bottom, slC.muted("session: ")+slC.text(id))
		}
		if len(bottom) > 0 {
			lines = append(lines, slC.muted("▸ ")+strings.Join(bottom, sep))
		}
	}
	return strings.Join(lines, "\n")
}
