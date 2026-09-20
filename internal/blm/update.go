package blm

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// blm update [topic] / blm_update — "หัวข้อหลักใหม่ควรเป็นอะไร" หลัง /blm_init ผ่านไปสักพัก (เจ้าของ 2026-09-20)
// ไม่ scan ทั้งโปรเจ็คซ้ำ ไม่ใช้ LLM: เทียบ **พื้นที่ที่กฎคุ้มครองแล้ว** (โฟลเดอร์ที่บรรทัด `code:` ของทุก block อ้าง) กับ
// (1) cluster ในกราฟ (`blm_graph`: โฟลเดอร์ชั้นสอง + hub symbol) ที่ไม่มี block ไหนอ้าง = ผู้สมัครหัวข้อใหม่
// (2) ไฟล์ที่ git เห็นว่าเปลี่ยนหลัง updated_at ล่าสุดของกฎ และอยู่นอกพื้นที่นั้น = "เดือนนี้มีอะไรใหม่ที่กฎยังไม่ครอบ"
// ระบุ topic = แนบผลค้น `blm search` (สารบัญ + search-id) ให้เริ่มอ่าน block · ความหมาย/ชื่อหัวข้อยังเป็นของคน (หรือ agent ผ่าน /blm_update)
// CLI กับ MCP ได้รายงานเดียวกัน — เจ้าของรันเองก่อนเพื่อเลือกหัวข้อ แล้วค่อยสั่ง /blm_update "<ชื่อ>"

type UpdateTopic struct {
	Topic  string   `json:"topic"`
	Note   string   `json:"note"` // "" = ยังอยู่ใน blm.md เอง
	Blocks int      `json:"blocks"`
	Oldest string   `json:"oldestUpdatedAt,omitempty"`
	Dirs   []string `json:"dirs,omitempty"` // โฟลเดอร์ (คีย์เดียวกับ cluster) ที่ code: ของหัวข้อนี้อ้าง
}

type UpdateCandidate struct {
	Dir     string   `json:"dir"`
	Files   int      `json:"files"`
	Symbols int      `json:"symbols"`
	Top     []string `json:"topSymbols,omitempty"`
	Changed int      `json:"changedSince,omitempty"` // ไฟล์ในโฟลเดอร์นี้ที่เปลี่ยนหลัง Since
}

type UpdateChanged struct {
	Dir     string   `json:"dir"`
	Covered bool     `json:"covered"`         // มีกฎอ้างโฟลเดอร์นี้อยู่แล้ว (แค่เตือนว่ากฎอาจเก่า)
	Last    string   `json:"last"`            // วันที่ commit ล่าสุดที่แตะโฟลเดอร์นี้ (จาก git)
	Rules   []string `json:"rules,omitempty"` // block กฎ (topic › heading) ที่บรรทัด code: ชี้เข้าโฟลเดอร์นี้ — หาจากไฟล์ main topic blm-*.md (+ blm.md)
	Files   []string `json:"files"`
}

type UpdateReport struct {
	Topic      string            `json:"topic,omitempty"`
	Rules      string            `json:"rules"`
	Topics     []UpdateTopic     `json:"topics"`
	Covered    []string          `json:"coveredDirs"`
	Candidates []UpdateCandidate `json:"candidates"`
	// Since = จุดตัดของส่วน "โค้ดที่แก้หลัง blm init/update ครั้งล่าสุด" (เจ้าของ 2026-09-20: ไม่อิง updated_at ของกฎ):
	// เวลาใน <store>/reviews/last-update.json (จดตอน blm init เสร็จ / รีวิว submit) · ไม่มี = 30 วันย้อนหลัง (SinceFrom บอกที่มา)
	Since     string          `json:"since,omitempty"`
	SinceFrom string          `json:"sinceFrom,omitempty"`
	Changed   []UpdateChanged `json:"changed,omitempty"`
	Search    *SearchResult   `json:"search,omitempty"`
	Notes     []string        `json:"notes,omitempty"`
	Next      string          `json:"next"`
	Terminal  string          `json:"terminal"`
}

var codeLineRe = regexp.MustCompile(`(?m)^code:\s*(.*)$`)

// clusterKey = โฟลเดอร์ชั้นสอง เหมือน clusters() ใน graph.go (src/lib, src/features/x → src/features)
func clusterKey(p string) string {
	parts := strings.Split(strings.Trim(filepath.ToSlash(p), "/"), "/")
	if len(parts) <= 2 {
		return parts[0]
	}
	return strings.Join(parts[:2], "/")
}

// codePaths ดึง path จากบรรทัด `code:` ของ block — รูป `[name](path)` (linkPath) หรือ path เปล่า คั่นด้วย , หรือช่องว่าง
func codePaths(text string) []string {
	var out []string
	for _, m := range codeLineRe.FindAllStringSubmatch(text, -1) {
		line := m[1]
		for _, l := range codeLinkRe.FindAllStringSubmatch(line, -1) {
			out = append(out, decodePath(l[2]))
		}
		plain := codeLinkRe.ReplaceAllString(line, " ")
		for _, tok := range strings.FieldsFunc(plain, func(r rune) bool { return r == ',' || r == ' ' || r == '·' }) {
			tok = strings.Trim(tok, "`()<>[]")
			if strings.Contains(tok, "/") || strings.Contains(tok, ".") {
				out = append(out, tok)
			}
		}
	}
	return out
}

var codeLinkRe = regexp.MustCompile(`\[([^\]]*)\]\(([^)\s]+)\)`)

func decodePath(p string) string {
	return strings.NewReplacer("%5B", "[", "%5D", "]", "%20", " ").Replace(p)
}

// UpdateReport — ดูรายละเอียดที่หัวไฟล์ · limit = จำนวนผู้สมัคร/ผลค้นสูงสุด
func (s *Store) UpdateReport(topic string, limit int) (*UpdateReport, error) {
	if limit <= 0 {
		limit = 10
	}
	path := s.RulesPath()
	if path == "" {
		return nil, fmt.Errorf("no blm.md yet — run /blm_init first")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	_, off, body := splitFront(string(raw))
	rep := &UpdateReport{Topic: topic, Rules: s.rel(path)}
	blocks := append(SplitBlocks(body, off, rep.Rules, path), s.topicBlocks(body)...)

	// 1) หัวข้อที่มี + พื้นที่ที่คุ้มครอง
	noteOf := map[string]string{}
	for _, t := range s.topicNotes(body) {
		noteOf[t.Topic] = t.Name
	}
	byTopic := map[string]*UpdateTopic{}
	var order []string
	covered := map[string]bool{}
	rulesOf := map[string][]string{} // โฟลเดอร์ → block ที่อ้าง
	for _, b := range blocks {
		if !b.Rule {
			continue
		}
		t := byTopic[b.Topic]
		if t == nil {
			t = &UpdateTopic{Topic: b.Topic, Note: noteOf[b.Topic]}
			byTopic[b.Topic] = t
			order = append(order, b.Topic)
		}
		t.Blocks++
		if b.UpdatedAt != "" && (t.Oldest == "" || b.UpdatedAt < t.Oldest) {
			t.Oldest = b.UpdatedAt
		}
		for _, p := range codePaths(b.Text) {
			k := clusterKey(p)
			covered[k] = true
			t.Dirs = append(t.Dirs, k)
			rulesOf[k] = append(rulesOf[k], b.Topic+" › "+b.Heading)
		}
	}
	for _, name := range order {
		t := byTopic[name]
		t.Dirs = uniq(t.Dirs)
		sort.Strings(t.Dirs)
		rep.Topics = append(rep.Topics, *t)
	}
	for k := range covered {
		rep.Covered = append(rep.Covered, k)
	}
	sort.Strings(rep.Covered)

	// 2) cluster ที่ไม่มีกฎอ้าง
	g, ok := s.LoadGraph()
	if !ok {
		if g, err = s.BuildGraph(""); err != nil {
			rep.Notes = append(rep.Notes, "graph: "+err.Error())
		}
	}
	// เฉพาะไฟล์โค้ด (รายการเดียวกับ origin ของ blm search): .md/config/css/html/fonts ไม่ใช่ความรู้ของ blm — ไม่โผล่ทั้ง candidates และ changed
	inGraph := map[string]bool{}
	codeFiles := map[string]int{} // cluster → จำนวนไฟล์โค้ด — cluster ที่ไม่มีโค้ดเลย (memory mirror, design/) ไม่ใช่ผู้สมัคร
	for _, n := range g.Nodes {
		if !codeFile(n.Path) {
			continue
		}
		inGraph[n.Path] = true
		codeFiles[clusterKey(n.Path)]++
	}
	if g.BuiltAt != "" {
		rep.Notes = append(rep.Notes, "graph built "+g.BuiltAt+" ("+g.Engine+") — blm graph --rebuild to refresh")
	}
	ignored := s.Ignored()
	if len(ignored) > 0 {
		rep.Notes = append(rep.Notes, fmt.Sprintf("%d dir(s) ignored by the owner in earlier reviews (%s)", len(ignored), s.rel(s.ignoreFile())))
	}
	for _, c := range g.Clusters {
		if covered[c.Dir] || ignored[c.Dir] || codeFiles[c.Dir] == 0 || strings.HasPrefix(c.Dir, ".") {
			continue
		}
		rep.Candidates = append(rep.Candidates, UpdateCandidate{Dir: c.Dir, Files: c.Files, Symbols: c.Symbols, Top: head(c.Top, 3)})
	}
	sort.Slice(rep.Candidates, func(i, j int) bool { return rep.Candidates[i].Symbols > rep.Candidates[j].Symbols })

	// 3) โค้ดที่แก้หลัง init/update ครั้งล่าสุด (เฉพาะไฟล์ที่กราฟรู้จัก = โค้ดที่ index)
	since, from := s.LastUpdate()
	if since == "" {
		since, from = time.Now().AddDate(0, 0, -30).Format("2006-01-02"), "no blm init/update recorded yet — last 30 days"
	}
	if len(inGraph) > 0 {
		rep.Since, rep.SinceFrom = since, from
		cmd := exec.Command("git", "log", "--since="+since, "--name-only", "--date=short", "--pretty=format:@%ad")
		cmd.Dir = s.Root
		if out, err := cmd.Output(); err == nil {
			byDir := map[string][]string{}
			lastOf := map[string]string{}
			seen := map[string]bool{}
			date := ""
			for _, f := range strings.Split(string(out), "\n") {
				f = strings.TrimSpace(f)
				if strings.HasPrefix(f, "@") { // git log ออกใหม่สุดก่อน → วันที่แรกที่เจอโฟลเดอร์ = ล่าสุด
					date = strings.TrimPrefix(f, "@")
					continue
				}
				if f == "" || seen[f] || !inGraph[f] {
					continue
				}
				seen[f] = true
				k := clusterKey(f)
				byDir[k] = append(byDir[k], f)
				if lastOf[k] == "" {
					lastOf[k] = date
				}
			}
			for dir, files := range byDir {
				if ignored[dir] {
					continue
				}
				sort.Strings(files)
				rep.Changed = append(rep.Changed, UpdateChanged{Dir: dir, Covered: covered[dir], Last: lastOf[dir], Rules: uniq(rulesOf[dir]), Files: files})
			}
			sort.Slice(rep.Changed, func(i, j int) bool {
				if rep.Changed[i].Covered != rep.Changed[j].Covered {
					return !rep.Changed[i].Covered
				}
				return len(rep.Changed[i].Files) > len(rep.Changed[j].Files)
			})
			for i := range rep.Candidates {
				rep.Candidates[i].Changed = len(byDir[rep.Candidates[i].Dir])
			}
		} else {
			rep.Notes = append(rep.Notes, "git log unavailable — changed-since section skipped")
		}
	}
	if len(rep.Candidates) > limit {
		rep.Candidates = rep.Candidates[:limit]
	}

	// 4) ระบุหัวข้อ → ผลค้นให้เริ่มอ่าน
	if topic != "" {
		res, err := Search(s.Root, s.Config, SearchOpts{Query: topic, Limit: limit, ExcludeMD: true})
		if err != nil {
			rep.Notes = append(rep.Notes, "search: "+err.Error())
		} else {
			rep.Search = res
		}
		rep.Next = fmt.Sprintf("read the hits (blm search --get <search-id> --id n), then /blm_update %q in Claude Code: meaning → subtopics → blm_create blm-<slug> + a row in the Main Business table", topic)
	} else {
		rep.Next = "pick a candidate, then blm update \"<topic>\" for its hits · /blm_update \"<topic>\" in Claude Code drafts meaning + subtopics for the owner to confirm"
	}
	rep.Terminal = renderUpdate(rep)
	return rep, nil
}

func renderUpdate(r *UpdateReport) string {
	var b strings.Builder
	b.WriteString(Title("blm update — what the rules cover, and what they do not") + "\n")
	b.WriteString(Section(fmt.Sprintf("Topics in %s (%d)", r.Rules, len(r.Topics))) + "\n")
	var rows [][]string
	for _, t := range r.Topics {
		note := t.Note
		if note == "" {
			note = Dim("(in blm.md)")
		}
		rows = append(rows, []string{t.Topic, note, strconv.Itoa(t.Blocks), t.Oldest, strings.Join(t.Dirs, " ")})
	}
	if len(rows) > 0 {
		b.WriteString(Table([]string{"topic", "note", "blocks", "oldest", "dirs covered by code:"}, rows) + "\n")
	}
	b.WriteString(Section(fmt.Sprintf("Candidates — clusters no rule refers to (%d)", len(r.Candidates))) + "\n")
	if len(r.Candidates) == 0 {
		b.WriteString(Dim("none — every cluster in the graph is referenced by some code: line") + "\n")
	} else {
		rows = nil
		for _, c := range r.Candidates {
			ch := ""
			if c.Changed > 0 {
				ch = Yellow(strconv.Itoa(c.Changed))
			}
			rows = append(rows, []string{c.Dir, strconv.Itoa(c.Files), strconv.Itoa(c.Symbols), ch, strings.Join(c.Top, ", ")})
		}
		b.WriteString(Table([]string{"dir", "files", "symbols", "changed", "hub symbols"}, rows) + "\n")
	}
	if r.Since != "" {
		b.WriteString(Section(fmt.Sprintf("Code committed after the last blm init/update (%s · %s) — indexed files only", r.Since, r.SinceFrom)) + "\n")
		b.WriteString(Dim("no rule = no code: line in blm.md / blm-*.md refers to this folder → New topic / Update topic") + "\n")
		if len(r.Changed) == 0 {
			b.WriteString(Dim("nothing") + "\n")
		}
		var coveredRows []UpdateChanged
		for _, c := range r.Changed {
			if c.Covered {
				coveredRows = append(coveredRows, c)
				continue
			}
			b.WriteString(fmt.Sprintf("%s  %d files  last commit %s  %s\n", PadEnd(c.Dir, 28), len(c.Files), c.Last, Yellow("no rule")))
			for _, f := range head(c.Files, 5) {
				b.WriteString("    " + Dim(f) + "\n")
			}
			if len(c.Files) > 5 {
				b.WriteString("    " + Dim(fmt.Sprintf("… +%d", len(c.Files)-5)) + "\n")
			}
		}
		if len(coveredRows) > 0 { // งานของรอบตรวจกฎย่อย ไม่ใช่ของ blm update (กฎหลัก) — แค่บอกว่ามี
			b.WriteString("\n" + Dim(fmt.Sprintf("%d folder(s) changed under existing rules — not for this page; a sub-rule re-check round will pick them up:", len(coveredRows))) + "\n")
			for _, c := range coveredRows {
				b.WriteString("  " + Dim(fmt.Sprintf("%s  %d files  last commit %s  rules: %s", PadEnd(c.Dir, 26), len(c.Files), c.Last, strings.Join(head(c.Rules, 3), " · "))) + "\n")
			}
		}
	}
	if r.Search != nil {
		b.WriteString(Section("Search: "+r.Search.Query) + "\n")
		b.WriteString(r.Search.Terminal + "\n")
	}
	for _, n := range r.Notes {
		b.WriteString(Yellow("note: ") + n + "\n")
	}
	b.WriteString("\n" + Cyan("next: ") + r.Next + "\n")
	return b.String()
}

// last-update.json — เวลาที่ blm init เสร็จ / รีวิว update ถูก submit ครั้งล่าสุด = จุดตัดของ "โค้ดที่แก้หลังจากนั้น"
func (s *Store) lastUpdateFile() string { return filepath.Join(s.Dir, "reviews", "last-update.json") }

// LastUpdate คืน (YYYY-MM-DD, ที่มา) · "" = ยังไม่เคยจด
func (s *Store) LastUpdate() (string, string) {
	raw, err := os.ReadFile(s.lastUpdateFile())
	if err != nil {
		return "", ""
	}
	var m struct {
		At    string `json:"at"`
		Event string `json:"event"`
	}
	if json.Unmarshal(raw, &m) != nil || len(m.At) < 10 || strings.HasPrefix(m.Event, "review submit") { // "review submit" = รุ่นเก่าที่จดเร็วเกิน ไม่นับ
		return "", ""
	}
	return m.At[:10], m.Event + " " + m.At
}

// MarkUpdate จดเวลา (เรียกตอน blm init เสร็จ และตอนรีวิว submit)
func (s *Store) MarkUpdate(event string) error {
	if err := os.MkdirAll(filepath.Dir(s.lastUpdateFile()), 0o755); err != nil {
		return err
	}
	raw, _ := json.Marshal(map[string]string{"at": time.Now().UTC().Format(time.RFC3339), "event": event})
	return os.WriteFile(s.lastUpdateFile(), append(raw, '\n'), 0o644)
}
