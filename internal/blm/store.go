package blm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Note หนึ่งไฟล์ = หนึ่งโน้ต: frontmatter (target/mode/folder/description/tags) + body markdown
// Target คือชื่อโน้ตปลายทาง (ค่าเริ่ม = Name) Mode คือวิธี apply ตอน sync
type Note struct {
	Name        string   `json:"name"`
	Target      string   `json:"target"`
	Mode        string   `json:"mode"` // append | replace
	Folder      string   `json:"folder,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	UpdatedAt   string   `json:"updatedAt"`
	Content     string   `json:"content"`
}

// Input ค่าที่ agent ส่งมา — ช่องว่างหมายถึง "คงของเดิม"
type Input struct {
	Name, Content, Target, Mode, Folder, Description string
	Tags                                             []string
	// HasContent แยก "ไม่ส่ง content" จาก "ส่ง content ว่าง"
	HasContent bool
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

type Store struct {
	Dir string
	// MirrorDir ของ backend (AgentsRoom) — ใช้เดาว่า target มีอยู่แล้วไหม/อยู่โฟลเดอร์ไหน · "" = ไม่มี mirror
	MirrorDir string
	// Root ของโปรเจ็ค ใช้ทำ path relative ในผลลัพธ์
	Root string
}

func Open(root string, c Config) *Store {
	s := &Store{Dir: filepath.Join(root, c.Store), Root: root}
	if c.Mirror != "" {
		s.MirrorDir = filepath.Join(root, c.Mirror)
	}
	return s // ไม่สร้างโฟลเดอร์ตรงนี้ — คำสั่งอ่าน (status) ต้องไม่ทิ้ง dir เปล่าไว้ สร้างตอนเขียนครั้งแรกแทน
}

// HasMirror — มี backend ที่ถือ "ของจริง" แยกจาก store ไหม
func (s *Store) HasMirror() bool {
	if s.MirrorDir == "" {
		return false
	}
	fi, err := os.Stat(s.MirrorDir)
	return err == nil && fi.IsDir()
}

func (s *Store) rel(p string) string {
	if r, err := filepath.Rel(s.Root, p); err == nil {
		return filepath.ToSlash(r)
	}
	return p
}

func (s *Store) file(name string) (string, error) {
	if !nameRe.MatchString(name) {
		return "", fmt.Errorf("note name must be lowercase kebab-case: %q", name)
	}
	return filepath.Join(s.Dir, name+".md"), nil
}

func (s *Store) Has(name string) bool {
	p, err := s.file(name)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

func (s *Store) Get(name string) (Note, error) {
	p, err := s.file(name)
	if err != nil {
		return Note{}, err
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return Note{}, fmt.Errorf("no such note %q", name)
	}
	return parseNote(name, string(raw)), nil
}

func (s *Store) List() []Note {
	entries, _ := os.ReadDir(s.Dir)
	var notes []Note
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if n, err := s.Get(strings.TrimSuffix(e.Name(), ".md")); err == nil {
			notes = append(notes, n)
		}
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].UpdatedAt < notes[j].UpdatedAt })
	return notes
}

// snapshot สำเนาไฟล์เดิมไป history/ ก่อนทุกการแก้/ลบ (เจ้าของสั่ง 2026-09-08)
// ชื่อ `<name>-[<action>]-YYYYMMDD-HHmmss.md` เก็บทั้งไฟล์ เพราะไฟล์เล็กและ diff ย้อนหลังต้องมีบริบทครบ
func (s *Store) snapshot(name, action string) {
	p, err := s.file(name)
	if err != nil {
		return
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return
	}
	dir := filepath.Join(s.Dir, "history")
	_ = os.MkdirAll(dir, 0o755)
	stamp := time.Now().Format("20060102-150405")
	_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-[%s]-%s.md", name, action, stamp)), raw, 0o644)
}

// Save upsert ทั้งโน้ต (เหมือน memory_save mode replace)
func (s *Store) Save(in Input) (Note, error) {
	return s.save(in, "update")
}

func (s *Store) save(in Input, action string) (Note, error) {
	p, err := s.file(in.Name)
	if err != nil {
		return Note{}, err
	}
	prev, hadPrev := Note{}, false
	if s.Has(in.Name) {
		prev, _ = s.Get(in.Name)
		hadPrev = true
		s.snapshot(in.Name, action)
	}
	_ = os.MkdirAll(s.Dir, 0o755)
	pick := func(a, b, c string) string {
		if a != "" {
			return a
		}
		if b != "" {
			return b
		}
		return c
	}
	n := Note{
		Name:        in.Name,
		Target:      pick(in.Target, prev.Target, in.Name),
		Mode:        pick(in.Mode, prev.Mode, "append"),
		Folder:      pick(in.Folder, prev.Folder, ""),
		Description: pick(in.Description, prev.Description, ""),
		Tags:        in.Tags,
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
		Content:     prev.Content,
	}
	if n.Tags == nil && hadPrev {
		n.Tags = prev.Tags
	}
	if in.HasContent {
		n.Content = in.Content
	}
	if n.Mode != "replace" {
		n.Mode = "append"
	}
	return n, os.WriteFile(p, []byte(serializeNote(n)), 0o644)
}

// Update ต่อท้าย body (เหมือน memory_save mode append) — ต้องมีโน้ตอยู่ก่อน
func (s *Store) Update(in Input) (Note, error) {
	prev, err := s.Get(in.Name)
	if err != nil {
		return Note{}, err
	}
	add := strings.TrimSpace(in.Content)
	if strings.TrimSpace(prev.Content) != "" {
		in.Content = strings.TrimRight(prev.Content, "\n") + "\n\n" + add + "\n"
	} else {
		in.Content = add + "\n"
	}
	in.HasContent = true
	return s.save(in, "append")
}

// Patch แทนที่ข้อความตรงตัว ต้องเจอพอดีหนึ่งครั้ง
func (s *Store) Patch(name, find, replace string) (Note, error) {
	prev, err := s.Get(name)
	if err != nil {
		return Note{}, err
	}
	if find == "" {
		return Note{}, fmt.Errorf("patch %q: find is empty", name)
	}
	if hits := strings.Count(prev.Content, find); hits != 1 {
		return Note{}, fmt.Errorf("patch %q: find matched %d times, must match exactly once", name, hits)
	}
	return s.save(Input{Name: name, Content: strings.Replace(prev.Content, find, replace, 1), HasContent: true}, "patch")
}

func (s *Store) Delete(name string) error {
	p, err := s.file(name)
	if err != nil {
		return err
	}
	if !s.Has(name) {
		return fmt.Errorf("no such note %q", name)
	}
	s.snapshot(name, "delete")
	return os.Remove(p)
}

// Archive ย้ายไฟล์ที่ sync แล้วไป .synced/ (เก็บหลักฐานไว้ ไม่ทิ้ง)
func (s *Store) Archive(name string) (string, error) {
	p, err := s.file(name)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(s.Dir, ".synced")
	_ = os.MkdirAll(dest, 0o755)
	to := filepath.Join(dest, time.Now().UTC().Format("2006-01-02T15-04-05Z")+"-"+name+".md")
	return s.rel(to), os.Rename(p, to)
}

// FindTargetFolder หา target ใน mirror: คืน folder (เช่น "features", "global/conventions") หรือ "" ถ้าไม่มี
func (s *Store) FindTargetFolder(target string) (string, bool) {
	if !s.HasMirror() {
		return "", false
	}
	var found string
	_ = filepath.WalkDir(s.MirrorDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && p != s.MirrorDir {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == target+".md" {
			rel, _ := filepath.Rel(s.MirrorDir, filepath.Dir(p))
			if rel == "." || rel == "" {
				rel = "features"
			}
			found = filepath.ToSlash(rel)
		}
		return nil
	})
	return found, found != ""
}

// ---- sync plan -------------------------------------------------------------------

// SyncItem หนึ่งรายการ = argument ของ memory_save ที่พร้อมส่ง (หรือคำสั่ง shell สำหรับ custom)
type SyncItem struct {
	MemorySave   SaveArgs `json:"memory_save"`
	From         []string `json:"from"`
	TargetExists bool     `json:"targetExists"`
	Warnings     []string `json:"warnings"`
	Command      string   `json:"command,omitempty"`
}

type SaveArgs struct {
	Name        string   `json:"name"`
	Mode        string   `json:"mode"`
	Content     string   `json:"content"`
	Folder      string   `json:"folder,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// PlanSync รวมโน้ตที่มี target เดียวกันเป็น memory_save ครั้งเดียว เติม folder จาก mirror
// และเตือนเมื่อ append ไปหาโน้ตที่ยังไม่มี (AgentsRoom ปฏิเสธ append กับโน้ตที่ไม่มี — ต้อง replace + description)
func (s *Store) PlanSync(notes []Note) []SyncItem {
	var order []string
	groups := map[string][]Note{}
	for _, n := range notes {
		if _, ok := groups[n.Target]; !ok {
			order = append(order, n.Target)
		}
		groups[n.Target] = append(groups[n.Target], n)
	}
	var plan []SyncItem
	for _, target := range order {
		g := groups[target]
		mirrorFolder, exists := s.FindTargetFolder(target)
		item := SyncItem{TargetExists: exists, Warnings: []string{}}
		mode, modes := "append", map[string]bool{}
		var parts, from []string
		var desc, folder string
		tagSet, tags := map[string]bool{}, []string{}
		for _, n := range g {
			modes[n.Mode] = true
			parts = append(parts, strings.TrimSpace(n.Content))
			from = append(from, n.Name)
			if n.Description != "" {
				desc = n.Description
			}
			if n.Folder != "" {
				folder = n.Folder
			}
			for _, t := range n.Tags {
				if !tagSet[t] {
					tagSet[t] = true
					tags = append(tags, t)
				}
			}
		}
		if modes["replace"] {
			mode = "replace"
		}
		if len(modes) > 1 {
			item.Warnings = append(item.Warnings, "notes for this target mix append and replace — using replace; review the content before sending")
		}
		if !exists && mode == "append" {
			mode = "replace"
			item.Warnings = append(item.Warnings, fmt.Sprintf("%q not found in mirror — switched to replace (creates it); description required", target))
		}
		if mode == "replace" && desc == "" {
			item.Warnings = append(item.Warnings, "mode replace requires a description (retrieval cue \"Contains …\")")
		}
		if folder == "" {
			folder = mirrorFolder
		}
		item.MemorySave = SaveArgs{Name: target, Mode: mode, Content: strings.Join(parts, "\n\n"), Folder: folder, Description: desc, Tags: tags}
		if len(tags) == 0 {
			item.MemorySave.Tags = nil
		}
		item.From = from
		plan = append(plan, item)
	}
	return plan
}

// ---- file format -------------------------------------------------------------

func serializeNote(n Note) string {
	meta := []string{"target: " + n.Target, "mode: " + n.Mode, "updatedAt: " + n.UpdatedAt}
	if n.Folder != "" {
		meta = append(meta, "folder: "+n.Folder)
	}
	if n.Description != "" {
		d, _ := json.Marshal(n.Description)
		meta = append(meta, "description: "+string(d))
	}
	if len(n.Tags) > 0 {
		t, _ := json.Marshal(n.Tags)
		meta = append(meta, "tags: "+string(t))
	}
	return "---\n" + strings.Join(meta, "\n") + "\n---\n\n" + strings.TrimRight(n.Content, "\n") + "\n"
}

var frontRe = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n?`)

// splitFront คืน (frontmatter body, จำนวนบรรทัดของ frontmatter ในไฟล์จริง, body)
func splitFront(raw string) (map[string]string, int, string) {
	meta := map[string]string{}
	m := frontRe.FindStringSubmatchIndex(raw)
	if m == nil {
		return meta, 0, raw
	}
	for _, line := range strings.Split(raw[m[2]:m[3]], "\n") {
		if i := strings.Index(line, ":"); i > 0 {
			meta[strings.TrimSpace(line[:i])] = strings.TrimSpace(line[i+1:])
		}
	}
	head := raw[:m[1]]
	body := strings.TrimPrefix(raw[m[1]:], "\n")
	// เลขบรรทัดของ block ต้องนับรวม frontmatter ที่ตัดทิ้ง ไม่งั้นลิงก์ path:line ชี้เพี้ยน
	offset := strings.Count(raw[:len(raw)-len(body)], "\n")
	_ = head
	return meta, offset, body
}

func parseNote(name, raw string) Note {
	meta, _, body := splitFront(raw)
	n := Note{Name: name, Target: meta["target"], Mode: meta["mode"], Folder: meta["folder"], UpdatedAt: meta["updatedAt"], Content: body}
	if n.Target == "" {
		n.Target = name
	}
	if n.Mode != "replace" {
		n.Mode = "append"
	}
	if d := meta["description"]; d != "" {
		if json.Unmarshal([]byte(d), &n.Description) != nil {
			n.Description = d
		}
	}
	// mirror ของ AgentsRoom เขียน folder เป็น "global / conventions" (quoted + เว้นวรรค) → ให้ได้ค่า canonical
	if f := meta["folder"]; f != "" {
		var q string
		if json.Unmarshal([]byte(f), &q) == nil {
			f = q
		}
		n.Folder = strings.ReplaceAll(f, " ", "")
	}
	if t := meta["tags"]; t != "" {
		_ = json.Unmarshal([]byte(t), &n.Tags)
	}
	return n
}

// Edit แก้บางบรรทัดของโน้ตปลายทางแบบ local (เจ้าของ 2026-09-09: เนื้อโน้ตต้องไม่ผ่าน context ของ agent):
// อ่านต้นทาง = ร่างใน temp ชื่อเดียวกัน (ถ้ามี แก้ต่อจากครั้งก่อน) ไม่งั้นโน้ตใน mirror → แทน find→replace (ต้องพบพอดี 1)
// → เก็บทั้งก้อนเป็นร่าง mode replace ใน temp พร้อม folder/description/tags เดิม · คืนแค่บริบท 3 บรรทัดรอบจุดแก้
func (s *Store) Edit(target, find, replace string) (Note, string, error) {
	if find == "" {
		return Note{}, "", fmt.Errorf("edit %q: find is empty", target)
	}
	var src Note
	if s.Has(target) {
		src, _ = s.Get(target)
		if src.Mode != "replace" {
			return Note{}, "", fmt.Errorf("edit %q: temp note exists with mode append (a new section, not the whole note) — blm_patch it instead", target)
		}
	} else {
		folder, ok := s.FindTargetFolder(target)
		if !ok {
			return Note{}, "", fmt.Errorf("edit %q: not found in mirror or temp — use blm_save to create it", target)
		}
		raw, err := os.ReadFile(filepath.Join(s.MirrorDir, folder, target+".md"))
		if err != nil {
			return Note{}, "", err
		}
		src = parseNote(target, string(raw))
		src.Target, src.Mode = target, "replace"
		if src.Folder == "" {
			src.Folder = folder
		}
	}
	if hits := strings.Count(src.Content, find); hits != 1 {
		return Note{}, "", fmt.Errorf("edit %q: find matched %d times, must match exactly once", target, hits)
	}
	content := strings.Replace(src.Content, find, replace, 1)
	n, err := s.save(Input{Name: target, Target: target, Mode: "replace", Folder: src.Folder, Description: src.Description, Tags: src.Tags, Content: content, HasContent: true}, "patch")
	if err != nil {
		return Note{}, "", err
	}
	return n, contextAround(content, replace, 3), nil
}

// contextAround บรรทัดรอบข้อความ (สำหรับให้ agent เห็นว่าแก้ถูกที่ โดยไม่ต้องอ่านทั้งโน้ต)
func contextAround(content, needle string, lines int) string {
	idx := strings.Index(content, needle)
	if idx < 0 {
		return ""
	}
	all := strings.Split(content, "\n")
	line := strings.Count(content[:idx], "\n")
	from, to := line-lines, line+strings.Count(needle, "\n")+lines
	if from < 0 {
		from = 0
	}
	if to >= len(all) {
		to = len(all) - 1
	}
	var out []string
	for i := from; i <= to; i++ {
		out = append(out, fmt.Sprintf("%4d  %s", i+1, all[i]))
	}
	return strings.Join(out, "\n")
}
