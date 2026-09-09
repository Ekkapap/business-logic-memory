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
	// Base = updatedAt ของโน้ตปลายทาง (จาก mirror) ตอนที่ร่าง replace นี้ถูกสร้าง — ใช้ตรวจว่า cloud เปลี่ยนไปหลังจากนั้นไหม
	Base    string `json:"base,omitempty"`
	Content string `json:"content"`
	// Dirty = เนื้อหาต่างจาก .base (สำเนาที่ดึงมาครั้งล่าสุด) — โน้ตที่ไม่มี base ถือว่า dirty (ร่างใหม่)
	Dirty bool `json:"dirty"`
}

// Input ค่าที่ agent ส่งมา — ช่องว่างหมายถึง "คงของเดิม"
type Input struct {
	Name, Content, Target, Mode, Folder, Description, Base string
	Tags                                                   []string
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
			n.Dirty = s.IsDirty(n)
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
		Base:        pick(in.Base, prev.Base, ""),
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
// Create สร้างโน้ตใหม่ใน blm/ เท่านั้น — มีอยู่แล้ว = ปฏิเสธ (เจ้าของ 2026-09-09: ชื่อต้องตรง action · save เคยทับ blm-plugin ทั้งก้อน)
func (s *Store) Create(in Input) (Note, error) {
	if s.Has(in.Name) {
		return Note{}, fmt.Errorf("create %q: already exists in blm/ — blm_append to add, blm_patch to change lines, blm_replace to overwrite on purpose", in.Name)
	}
	return s.save(in, "create")
}

// ReplaceCheck = ข้อมูลให้ agent คิดอีกรอบก่อนทับทั้งโน้ต (เจ้าของ 2026-09-09: ต่างกันมากทั้งขนาดและเนื้อหา → ต้อง confirm)
type ReplaceCheck struct {
	NeedsConfirm bool   `json:"needsConfirm"`
	OldBytes     int    `json:"oldBytes"`
	NewBytes     int    `json:"newBytes"`
	OldLines     int    `json:"oldLines"`
	NewLines     int    `json:"newLines"`
	ChangedLines int    `json:"changedLines"`
	Removed      string `json:"removed,omitempty"` // บรรทัดแรก ๆ ที่จะหาย (ให้เห็นว่ากำลังทิ้งอะไร)
	Reason       string `json:"reason,omitempty"`
}

// Replace ทับทั้งโน้ตโดยตั้งใจ — ต้องมีอยู่แล้ว (ของเดิมลง history/)
// confirm=false: ถ้าเนื้อหาใหม่ต่างจากเดิมมาก (ขนาดต่าง >30% หรือบรรทัดเปลี่ยน >30%) ไม่เขียน คืน ReplaceCheck ให้ agent ยืนยันด้วย confirm=true
func (s *Store) Replace(in Input, confirm bool) (Note, *ReplaceCheck, error) {
	if !s.Has(in.Name) {
		return Note{}, nil, fmt.Errorf("replace %q: not in blm/ — blm_create it first", in.Name)
	}
	if !in.HasContent {
		return Note{}, nil, fmt.Errorf("replace %q: content is required", in.Name)
	}
	prev, _ := s.Get(in.Name)
	chk := replaceCheck(prev.Content, in.Content)
	if chk.NeedsConfirm && !confirm {
		return Note{}, &chk, nil
	}
	n, err := s.save(in, "replace")
	return n, &chk, err
}

func replaceCheck(oldC, newC string) ReplaceCheck {
	c := ReplaceCheck{OldBytes: len(oldC), NewBytes: len(newC), OldLines: len(strings.Split(strings.TrimSpace(oldC), "\n")), NewLines: len(strings.Split(strings.TrimSpace(newC), "\n"))}
	diff, _ := LineDiff(oldC, newC)
	var removed []string
	gone := 0 // บรรทัดเดิมที่จะหาย — ตัวชี้ว่ากำลังทิ้งอะไร (บรรทัดที่เพิ่มไม่นับ)
	for _, l := range strings.Split(diff, "\n") {
		if strings.HasPrefix(l, "- ") {
			gone++
			if len(removed) < 5 {
				removed = append(removed, strings.TrimPrefix(l, "- "))
			}
		}
	}
	c.ChangedLines = gone
	c.Removed = strings.Join(removed, "\n")
	sizeDelta := c.NewBytes - c.OldBytes
	if sizeDelta < 0 {
		sizeDelta = -sizeDelta
	}
	base := c.OldBytes
	if base == 0 {
		base = 1
	}
	switch {
	case sizeDelta*100/base > 30:
		c.NeedsConfirm, c.Reason = true, fmt.Sprintf("size changes by %d%% (%d → %d bytes)", sizeDelta*100/base, c.OldBytes, c.NewBytes)
	case c.OldLines >= 5 && gone*100/c.OldLines > 30: // โน้ตสั้นมากใช้เกณฑ์ขนาดอย่างเดียว ไม่งั้นแก้คำเดียวก็ 100%
		c.NeedsConfirm, c.Reason = true, fmt.Sprintf("%d of %d existing lines disappear", gone, c.OldLines)
	}
	return c
}

// Append ต่อท้ายโน้ต · ยังไม่อยู่ใน blm/ แต่มีบน mirror → ดึงมาก่อน (Checkout) แล้วต่อ
func (s *Store) Append(in Input) (Note, error) {
	if !s.Has(in.Name) {
		if _, err := s.Checkout(in.Name); err != nil {
			return Note{}, fmt.Errorf("append %q: not in blm/ and not in mirror — blm_create it", in.Name)
		}
	}
	return s.Update(in)
}

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
// Patch ค้นหา/แทนที่ (พบพอดี 1) · ยังไม่อยู่ใน blm/ แต่มีบน mirror → ดึงมาก่อน (เดิมคือ Edit)
func (s *Store) Patch(name, find, replace string) (Note, error) {
	if !s.Has(name) {
		if _, err := s.Checkout(name); err != nil {
			return Note{}, fmt.Errorf("patch %q: not in blm/ and not in mirror — blm_create it", name)
		}
	}
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
	// Base = updatedAt ของ cloud ตอนสร้างร่าง (โน้ตที่ checkout จาก mirror) ใช้ตรวจ conflict ก่อน push
	Base string `json:"base,omitempty"`
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
		// สำเนา local ที่ยังตรงกับ .base (ดึงมาแล้วไม่ได้แก้) ไม่มีอะไรจะส่ง — ตัดออกจากกลุ่ม ไม่งั้น blm.md ถูก push ทุกรอบ
		var dirty []Note
		for _, n := range g {
			if n.Base == "" || s.IsDirty(n) {
				dirty = append(dirty, n)
			}
		}
		if len(dirty) == 0 {
			continue
		}
		g = dirty
		mirrorFolder, exists := s.FindTargetFolder(target)
		item := SyncItem{TargetExists: exists, Warnings: []string{}}
		mode, modes := "append", map[string]bool{}
		var parts, from []string
		var desc, folder, base string
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
			if n.Base != "" {
				base = n.Base
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
		item.Base = base
		plan = append(plan, item)
	}
	return plan
}

// ---- file format -------------------------------------------------------------

func serializeNote(n Note) string {
	meta := []string{"target: " + n.Target, "mode: " + n.Mode, "updatedAt: " + n.UpdatedAt}
	if n.Base != "" {
		meta = append(meta, "base: "+n.Base)
	}
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
	// mirror ของ AgentsRoom เขียนค่าใน quote ("2026-09-02T00:00:00Z") ของ blm เองไม่มี quote — ถอดให้เท่ากันก่อนใช้เทียบเวลา
	unq := func(v string) string {
		var q string
		if json.Unmarshal([]byte(v), &q) == nil {
			return q
		}
		return v
	}
	n := Note{Name: name, Target: unq(meta["target"]), Mode: meta["mode"], Folder: meta["folder"], UpdatedAt: unq(meta["updatedAt"]), Base: unq(meta["base"]), Content: body}
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

// Checkout ดึงโน้ตจาก mirror มาเป็นสำเนา local ใน store (mode replace, Base = updatedAt ของ mirror, .base/<name>.md = เนื้อหาตอนดึง)
// concept เจ้าของ 2026-09-09: blm คือ local memory — แก้ที่ store ก่อน แล้ว sync ขึ้น backend · mirror มีไว้เทียบ ไม่ใช่ที่ทำงาน
func (s *Store) Checkout(target string) (Note, error) {
	folder, ok := s.FindTargetFolder(target)
	if !ok {
		return Note{}, fmt.Errorf("%q not found in mirror", target)
	}
	mp := filepath.Join(s.MirrorDir, folder, target+".md")
	raw, err := os.ReadFile(mp)
	if err != nil {
		return Note{}, err
	}
	src := parseNote(target, string(raw))
	src.Target, src.Mode = target, "replace"
	if src.Folder == "" {
		src.Folder = folder
	}
	src.Base = mirrorStamp(src, mp)
	_ = os.MkdirAll(filepath.Join(s.Dir, ".base"), 0o755)
	_ = os.WriteFile(filepath.Join(s.Dir, ".base", target+".md"), []byte(src.Content), 0o644)
	n, err := s.save(Input{Name: target, Target: target, Mode: "replace", Folder: src.Folder, Description: src.Description, Tags: src.Tags, Content: src.Content, HasContent: true, Base: src.Base}, "checkout")
	if err == nil && target == RulesNote {
		// blm.md: ทุกคำที่อ้างไฟล์เป็นลิงก์เสมอ (linkPath · ไม่นับเป็นการแก้ → .base ตามไปด้วย)
		if linked := s.linkPaths(n.Content); linked != n.Content {
			_ = os.WriteFile(filepath.Join(s.Dir, ".base", target+".md"), []byte(linked), 0o644)
			n, err = s.save(Input{Name: target, Content: linked, HasContent: true}, "link")
		}
	}
	return n, err
}

// mirrorStamp = updatedAt ของโน้ตใน mirror · ไม่มี frontmatter (backend อื่น/ไฟล์เขียนมือ) → mtime ของไฟล์ ให้ Base ไม่ว่างเสมอ
func mirrorStamp(n Note, path string) string {
	if n.UpdatedAt != "" {
		return n.UpdatedAt
	}
	if fi, err := os.Stat(path); err == nil {
		return fi.ModTime().UTC().Format(time.RFC3339)
	}
	return "0"
}

// IsDirty — เนื้อหาต่างจากสำเนา .base ไหม (ไม่มี .base = ร่างใหม่ = dirty)
func (s *Store) IsDirty(n Note) bool {
	base, err := os.ReadFile(filepath.Join(s.Dir, ".base", n.Name+".md"))
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(base)) != strings.TrimSpace(n.Content)
}

// Rebase หลัง push สำเร็จ: สำเนา local ยังอยู่ (ไม่ archive) แต่ .base และ Base เลื่อนมาที่สถานะที่ backend รับแล้ว
func (s *Store) Rebase(name, updatedAt string) error {
	n, err := s.Get(name)
	if err != nil {
		return err
	}
	_ = os.MkdirAll(filepath.Join(s.Dir, ".base"), 0o755)
	if err := os.WriteFile(filepath.Join(s.Dir, ".base", name+".md"), []byte(n.Content), 0o644); err != nil {
		return err
	}
	_, err = s.save(Input{Name: name, Base: updatedAt}, "synced")
	return err
}

// PullResult ผลการดึง mirror → store หลัง backend refresh
type PullResult struct {
	Refreshed []string `json:"refreshed"` // สำเนาสะอาด ที่ cloud ใหม่กว่า → เขียนทับ
	Conflicts []string `json:"conflicts"` // แก้ local ค้างอยู่ และ cloud ก็เปลี่ยน → ไม่แตะ ให้ blm_diff/blm_merge
	UpToDate  []string `json:"upToDate"`
}

// Pull เทียบทุกสำเนา local (มี Base) กับ mirror · cloud ใหม่กว่าและ local สะอาด → ดึงทับ · local dirty → รายงาน conflict ไม่ทับ
func (s *Store) Pull() PullResult {
	var r PullResult
	for _, n := range s.List() {
		if n.Base == "" {
			continue
		}
		folder, ok := s.FindTargetFolder(n.Target)
		if !ok {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.MirrorDir, folder, n.Target+".md"))
		if err != nil {
			continue
		}
		cloud := parseNote(n.Target, string(raw))
		if mirrorStamp(cloud, filepath.Join(s.MirrorDir, folder, n.Target+".md")) <= n.Base {
			r.UpToDate = append(r.UpToDate, n.Name)
			continue
		}
		if n.Dirty {
			r.Conflicts = append(r.Conflicts, n.Name)
			continue
		}
		if _, err := s.Checkout(n.Target); err == nil {
			r.Refreshed = append(r.Refreshed, n.Name)
		}
	}
	return r
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

// History รายชื่อไฟล์ใน history/ ของโน้ต (ใหม่สุดก่อน) — ไว้เลือกกู้ด้วย Restore
func (s *Store) History(name string) []string {
	entries, _ := os.ReadDir(filepath.Join(s.Dir, "history"))
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), name+"-[") && strings.HasSuffix(e.Name(), ".md") {
			out = append(out, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out
}

// Restore กู้โน้ตใน blm/ จากไฟล์ใน history/ (เจ้าของสั่ง 2026-09-09 หลังคำสั่ง save เดิมทับ blm-plugin ทั้งก้อน)
// ของปัจจุบันถูกเก็บลง history ก่อนเสมอ (action restore) จึงย้อนกลับได้อีก · historyFile รับทั้งชื่อไฟล์และ path
func (s *Store) Restore(historyFile, name string) (Note, error) {
	if historyFile == "" || name == "" {
		return Note{}, fmt.Errorf("restore: history file and note name are required")
	}
	p := filepath.Join(s.Dir, "history", filepath.Base(historyFile))
	raw, err := os.ReadFile(p)
	if err != nil {
		return Note{}, fmt.Errorf("restore: %v (blm restore <history-file> <note> — list with blm restore <note>)", err)
	}
	dest, err := s.file(name)
	if err != nil {
		return Note{}, err
	}
	if s.Has(name) {
		s.snapshot(name, "restore")
	}
	n := parseNote(name, string(raw))
	n.Name = name
	if n.Target == "" {
		n.Target = name
	}
	if n.Mode != "replace" {
		n.Mode = "append"
	}
	n.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_ = os.MkdirAll(s.Dir, 0o755)
	return n, os.WriteFile(dest, []byte(serializeNote(n)), 0o644)
}
