package blm

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"html/template"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Review — หน้า HTML ให้เจ้าของตัดสินใจเรื่องหัวข้อหลักใหม่ (เจ้าของออกแบบ 2026-09-20):
// ไฟล์เดียว `<store>/reviews/review-<id>.json` เป็นสถานะร่วมของเจ้าของและ agent — เจ้าของกดปุ่มในหน้า HTML (ติ๊ก ref, New topic,
// comment / agree / draft ทุกจุด) แล้ว submit · agent อ่านไฟล์เดิม ใส่ข้อเสนอ (ชื่อ/ความหมายหัวข้อหลัก, หัวข้อย่อย+ความหมาย) ลงไฟล์เดิม
// ทุก event เรียกฟังก์ชันเดียว SaveReview = เขียน JSON + gen HTML ใหม่ (`blm update --html --from <json>`)
//
// HTML ต้องมีตัวรับปุ่ม (ไฟล์ static เขียนไฟล์ไม่ได้) → server เล็ก ๆ บน 127.0.0.1 (พอร์ตคงที่ต่อโปรเจ็ค) รับ POST /r/<id>/submit
// CLI รัน server รอจน submit แล้วจบ · ใน `blm mcp` server อยู่กับ process · agent รู้ว่ามี submit จาก hook prompt (reviewPending) หรือ blm_update {from}
//
// action ต่อจุด: "" (ยังไม่ตัดสิน) · COMMENT (มีข้อความให้ agent แก้) · AGREE (ตกลง) · DRAFT (บันทึกค่าล่าสุดแต่ยังไม่จบ — `blm update draft` ตามเก็บ)
// ครบทุกจุด AGREE = หัวข้อผ่าน agent สร้าง blm-<slug> + แถวในตาราง

const (
	ActionComment = "COMMENT"
	ActionAgree   = "AGREE"
	ActionDraft   = "DRAFT"
)

// ReviewPoint จุดที่ตัดสินได้หนึ่งจุด (ชื่อ/ความหมาย) — Text = ข้อเสนอล่าสุด · History = ข้อเสนอก่อนหน้าพร้อม action/comment ที่ได้รับ
type ReviewPoint struct {
	Text    string        `json:"text"`
	Action  string        `json:"action,omitempty"`
	Comment string        `json:"comment,omitempty"` // รุ่นก่อน: ช่องข้อความเดียว (ยังอ่านได้ใน history)
	History []ReviewPoint `json:"history,omitempty"`
	// Chat — comment ของจุดนี้เป็นแชตแบบเดียวกับแถว (เจ้าของ 2026-09-21: เจ้าของไม่เสียบริบท agent ตอบ realtime ไม่รอ submit)
	// id ของแชต = "<topic>:<key>" (key = name · desc · sub:<sid>:name|desc) ใช้ใน proposal.answers · ข้อความจากเจ้าของ = action COMMENT ค้าง
	Chat    []ChatMsg `json:"chat,omitempty"`
	AskDone bool      `json:"askDone,omitempty"`
	Summary *ChatMsg  `json:"summary,omitempty"`
	// Submission เท่านั้น (เหมือน ReviewItem): ข้อความใหม่ / index ของบทสรุป
	Ask        string `json:"ask,omitempty"`
	SummaryIdx *int   `json:"summaryIdx,omitempty"`
}

func (p ReviewPoint) chatPending() bool {
	return len(p.Chat) > 0 && p.Chat[len(p.Chat)-1].By == "owner" && !p.AskDone
}

// pointRef — จุดตาม id ของแชต "<topic>:name|desc" หรือ "<topic>:sub:<sid>:name|desc" (nil = ไม่พบ)
func pointRef(r *Review, id string) *ReviewPoint {
	parts := strings.Split(id, ":")
	if len(parts) < 2 {
		return nil
	}
	for i := range r.Topics {
		t := &r.Topics[i]
		if t.ID != parts[0] {
			continue
		}
		switch {
		case len(parts) == 2 && parts[1] == "name":
			return &t.Name
		case len(parts) == 2 && parts[1] == "desc":
			return &t.Desc
		case len(parts) == 4 && parts[1] == "sub":
			for j := range t.Subs {
				if t.Subs[j].ID == parts[2] {
					if parts[3] == "name" {
						return &t.Subs[j].Name
					}
					return &t.Subs[j].Desc
				}
			}
		}
	}
	return nil
}

type ReviewSub struct {
	ID   string      `json:"id"`
	Refs []string    `json:"refs,omitempty"` // item id ที่เจ้าของติ๊ก (c2, g1 …) และ/หรือไฟล์ที่ agent ใช้อ้าง — ให้เจ้าของเห็นว่าข้อนี้มาจาก ref ไหน
	Name ReviewPoint `json:"name"`
	Desc ReviewPoint `json:"desc"`
}

type ReviewTopic struct {
	ID       string      `json:"id"`
	Refs     []string    `json:"refs,omitempty"`     // item ids ที่ติ๊ก ref = หลักฐานประกอบ
	Related  []string    `json:"related,omitempty"`  // item ids (t…) ที่ติ๊ก relate = หัวข้อที่มีอยู่ที่เกี่ยวข้อง
	Existing string      `json:"existing,omitempty"` // ชื่อหัวข้อที่มีอยู่ (label) เมื่อเป็นการเอาไปรวม (target) — เขียนจริงด้วย blm_append เข้าโน้ตนั้น
	Name     ReviewPoint `json:"name"`
	Desc     ReviewPoint `json:"desc"`
	Subs     []ReviewSub `json:"subs,omitempty"`
	Status   string      `json:"status"`         // new (เจ้าของตั้ง รอ agent เสนอ) · extend (รวมเข้าหัวข้อเดิม รอ agent เสนอ) · proposed · agreed · draft · created
	Note     string      `json:"note,omitempty"` // "บอก agent" ของเจ้าของตอนสร้าง / note ของ agent ตอนเสนอ
}

// ReviewItem ข้อมูลประกอบหนึ่งแถว (จากรายงาน update: topic ที่มี / candidate / changed / hit) — แถวเดียวติ๊กได้หลายคอลัมน์ (เจ้าของ 2026-09-20):
//
//	ref    = ใช้เป็นหลักฐานประกอบแถวหัวข้อที่กำลังสร้าง (ทุกชนิด · ติ๊กแถว t = หัวข้อที่มีอยู่ที่เกี่ยวข้อง → Related ของแถวนั้น)
//	target = ปลายทางเมื่อจะ "เอาไปรวม" กับหัวข้อที่มีอยู่ (แถว t, เลือกได้อันเดียว) → หัวข้อในรีวิวเป็น status extend
//	ignore = ไม่ใช่ความรู้ของโปรเจ็ค (config, ไฟล์เครื่องมือ) — จำไว้ใน <store>/reviews/ignore.json แล้ว blm update รอบหน้าไม่เสนออีก
//
// ทิศทางเดียว (เจ้าของ 2026-09-20): C หลายตัว → T เดียว ต่อการกด "Update topic" หนึ่งครั้ง = หนึ่งแถวหัวข้อ กดซ้ำได้จนทุก C ถูกจัด
// (คอลัมน์ "used in" บอกว่าแถวนี้ไปอยู่หัวข้อไหนแล้ว) แล้ว submit รอบเดียว
type ReviewItem struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	Ref    bool   `json:"ref,omitempty"`
	Target bool   `json:"target,omitempty"`
	Ignore bool   `json:"ignore,omitempty"`
	// Link — แถว t ที่เคยผ่านรีวิวแล้ว: URL หน้าหัวข้อของรีวิวนั้น (ล่าสุด) ให้คลิกไปดูหัวข้อย่อยที่เคยเสนอ/ตกลง (เจ้าของ 2026-09-20)
	Link string `json:"link,omitempty"`
	// UsedIn — แถวนี้ถูกใช้เป็น ref ในรีวิวอื่นแล้ว ("→ Custom VPN (draft, f6e4d323)") — แถว mock-gateway/packaging ที่ยังไม่มีกฎเพราะข้อย่อยยัง DRAFT
	// ต้องไม่ขึ้น "ยังไม่ได้จัด" ซ้ำ (เจ้าของ 2026-09-20)
	UsedIn []string `json:"usedIn,omitempty"`
	// Gone — แถวนี้ไม่อยู่ในรายงานรอบล่าสุดแล้ว (โฟลเดอร์ถูกกฎคุ้มครองแล้ว / ไม่เปลี่ยนอีก) แต่ยังมีหัวข้ออ้างถึงหรือมีแชท จึงเก็บไว้
	// ให้กดดูได้ ไม่โชว์ในตารางหลัก (เจ้าของ 2026-09-20: "chat ใน C ตัวเก่าที่ซ่อนไปแล้ว ทำไงจะมีช่องทางกดดูได้")
	Gone bool `json:"gone,omitempty"`
	// Chat — แชทต่อแถว (เจ้าของ 2026-09-20: modal เหมือนหน้าแชท หน้าหลักโชว์แค่ "chat N") · เจ้าของส่งผ่าน Submission.Ask (ข้อความใหม่)
	// agent ตอบผ่าน proposal.answers → ต่อท้ายเป็น by:agent · รอ agent = ข้อความล่าสุดเป็นของเจ้าของ · AskDone = เจ้าของปิดแชท
	Chat    []ChatMsg `json:"chat,omitempty"`
	AskDone bool      `json:"askDone,omitempty"`
	// Summary — ข้อความที่ถูก "mark เป็นบทสรุป" (เจ้าของ 2026-09-20: ก่อนปิดแชทต้องมีบทสรุป ใครพูด/ตกลงอะไร) · ปิดแชทได้เมื่อมีบทสรุป
	Summary *ChatMsg `json:"summary,omitempty"`
	// Ask/Answer รุ่นก่อน (ย้ายเข้า Chat ตอนโหลด) · Ask ยังใช้เป็น "ข้อความใหม่" ใน Submission
	Ask    string `json:"ask,omitempty"`
	Answer string `json:"answer,omitempty"`
	// SummaryIdx — ใน Submission: index ของข้อความใน Chat ที่เจ้าของกด mark เป็นบทสรุป (nil = ไม่เปลี่ยน)
	SummaryIdx *int `json:"summaryIdx,omitempty"`
}

type ChatMsg struct {
	At   string `json:"at"`
	By   string `json:"by"` // owner | agent
	Text string `json:"text"`
}

// chatThread — ส่วนของแชทที่ agent ต้องเห็นเพื่อตอบ (เจ้าของ 2026-09-20: compact): ★ บทสรุป (ถ้ามี) + เฉพาะข้อความใหม่หลังคำตอบล่าสุดของ agent
// ย้อนดูทั้งหมด → blm_review {get, select:"items[id=…].chat"}
func chatThread(chat []ChatMsg, summary *ChatMsg) string {
	var lines []string
	if summary != nil {
		lines = append(lines, "★ "+summary.Text)
	}
	start := 0
	for i, m := range chat {
		if m.By == "agent" {
			start = i + 1
		}
	}
	for _, m := range chat[start:] {
		lines = append(lines, m.By+": "+m.Text)
	}
	return strings.Join(lines, "\n")
}

// chatPending — แถวนี้รอ agent ตอบไหม
func (it ReviewItem) chatPending() bool {
	return len(it.Chat) > 0 && it.Chat[len(it.Chat)-1].By == "owner" && !it.AskDone
}

// migrateChat — Ask/Answer รุ่นก่อน → Chat
func migrateChat(r *Review) {
	for i := range r.Items {
		it := &r.Items[i]
		if len(it.Chat) == 0 && it.Ask != "" {
			it.Chat = append(it.Chat, ChatMsg{At: r.UpdatedAt, By: "owner", Text: it.Ask})
			if it.Answer != "" {
				it.Chat = append(it.Chat, ChatMsg{At: r.UpdatedAt, By: "agent", Text: it.Answer})
			}
		}
		it.Ask, it.Answer = "", ""
	}
}

type ReviewLog struct {
	At    string `json:"at"`
	By    string `json:"by"` // owner | agent
	Event string `json:"event"`
}

type Review struct {
	ID        string        `json:"id"`
	Kind      string        `json:"kind"` // update
	Query     string        `json:"query,omitempty"`
	CreatedAt string        `json:"createdAt"`
	UpdatedAt string        `json:"updatedAt"`
	Round     int           `json:"round"`
	Status    string        `json:"status"` // open (รอเจ้าของ) · submitted (รอ agent) · done
	ReadAt    string        `json:"readAt,omitempty"`
	URL       string        `json:"url,omitempty"`
	File      string        `json:"file,omitempty"`
	HTML      string        `json:"html,omitempty"`
	SinceNote string        `json:"sinceNote,omitempty"` // คำอธิบายจุดตัดของกลุ่ม changed
	Ignored   []string      `json:"ignored,omitempty"`   // รายการใน reviews/ignore.json ตอน render (แสดงให้เลิก ignore ได้)
	Items     []ReviewItem  `json:"items"`
	Topics    []ReviewTopic `json:"topics"`
	Log       []ReviewLog   `json:"log"`
}

// Proposal — สิ่งที่ agent ส่งกลับเข้าไฟล์: ต่อหัวข้อ (id เดิม หรือใหม่) ชื่อ ความหมาย หัวข้อย่อย
type Proposal struct {
	Topics  []ProposalTopic  `json:"topics"`
	Answers []ProposalAnswer `json:"answers,omitempty"` // คำตอบต่อ item.ask
}
type ProposalAnswer struct {
	ID      string `json:"id"`
	Answer  string `json:"answer"`
	Summary bool   `json:"summary,omitempty"` // ข้อความนี้คือบทสรุปของแชท (agent สรุปให้ก่อนปิด)
}

// ProposalTopic — บางส่วนได้ (เจ้าของ 2026-09-21: ไม่ต้อง get all แล้ว replace all): id เดิม + เฉพาะฟิลด์ที่เปลี่ยน —
// name/desc ว่าง = คงเดิม · subs ไม่ส่ง = คงทั้งหมด · ส่ง subs = แก้/เพิ่มเฉพาะ id ที่ส่ง (ที่ไม่ส่งยังอยู่) · ตัดหัวข้อย่อย = drop:[id]
type ProposalTopic struct {
	ID   string        `json:"id,omitempty"`
	Name string        `json:"name,omitempty"`
	Desc string        `json:"desc,omitempty"`
	Subs []ProposalSub `json:"subs,omitempty"`
	Drop []string      `json:"drop,omitempty"`
	Note string        `json:"note,omitempty"`
}
type ProposalSub struct {
	ID   string   `json:"id,omitempty"`
	Refs []string `json:"refs,omitempty"` // item ids จาก refs ของหัวข้อ + path ไฟล์ที่อ่านจริง
	Name string   `json:"name"`
	Desc string   `json:"desc"`
}

// Submission — สิ่งที่หน้า HTML POST มา (เฉพาะช่องที่เจ้าของแก้ได้)
type Submission struct {
	Items  []ReviewItem  `json:"items"`
	Topics []ReviewTopic `json:"topics"`
}

func (s *Store) reviewsDir() string { return filepath.Join(s.Dir, "reviews") }

func newReviewID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func nowISO() string { return time.Now().UTC().Format(time.RFC3339) }

// NewReviewFromUpdate — รอบ 1: ข้อมูลประกอบทั้งหมดจากรายงาน update เป็น item ให้ติ๊ก
func NewReviewFromUpdate(rep *UpdateReport) *Review {
	r := &Review{ID: newReviewID(), Kind: "update", Query: rep.Topic, CreatedAt: nowISO(), Round: 1, Status: "open", Items: []ReviewItem{}, Topics: []ReviewTopic{}, Log: []ReviewLog{}}
	r.UpdatedAt = r.CreatedAt
	for i, t := range rep.Topics {
		note := t.Note
		if note == "" {
			note = "in blm.md"
		}
		r.Items = append(r.Items, ReviewItem{ID: fmt.Sprintf("t%d", i+1), Kind: "topic", Label: t.Topic, Detail: fmt.Sprintf("%s · %d blocks · oldest %s · %s", note, t.Blocks, t.Oldest, strings.Join(t.Dirs, " "))})
	}
	// ตารางเดียว "โฟลเดอร์โค้ดที่ยังไม่มีกฎ" = union ของ cluster ที่ไม่มีกฎอ้าง (C) กับโฟลเดอร์ที่ git แก้หลัง update ล่าสุดและไม่มีกฎ (G)
	// (เจ้าของ 2026-09-20: "C มัน no rule มันควร union ใน G") — แถวที่มี commit ใหม่ขึ้นก่อน พร้อมวันแก้ + รายชื่อไฟล์ครบ · โฟลเดอร์ที่มีกฎย่อยครอบไม่อยู่ที่นี่ (รอบกฎย่อย)
	type row struct {
		dir   string
		cand  *UpdateCandidate
		chg   *UpdateChanged
		order int
	}
	rows := map[string]*row{}
	var keys []string
	for i := range rep.Candidates {
		c := &rep.Candidates[i]
		rows[c.Dir] = &row{dir: c.Dir, cand: c, order: 1000 + i}
		keys = append(keys, c.Dir)
	}
	for i := range rep.Changed {
		c := &rep.Changed[i]
		if c.Covered {
			continue
		}
		if rw, ok := rows[c.Dir]; ok {
			rw.chg, rw.order = c, i
		} else {
			rows[c.Dir] = &row{dir: c.Dir, chg: c, order: i}
			keys = append(keys, c.Dir)
		}
	}
	sort.SliceStable(keys, func(a, b int) bool { return rows[keys[a]].order < rows[keys[b]].order })
	for i, k := range keys {
		rw := rows[k]
		// บรรทัดแรกบอกว่าแถวนี้มาจากอะไร (เจ้าของ 2026-09-20: เอาชื่อกลุ่มเดิมมาใส่ในรายละเอียดแทนหัวตาราง)
		var why []string
		if rw.chg != nil {
			why = append(why, fmt.Sprintf("โค้ดที่ถูกแก้หลัง blm init/update ครั้งล่าสุด (แก้ล่าสุด %s · %d ไฟล์)", rw.chg.Last, len(rw.chg.Files)))
		}
		if rw.cand != nil {
			why = append(why, "โฟลเดอร์โค้ดที่ยังไม่มีกฎข้อไหนอ้างถึง")
		}
		var parts []string
		if rw.cand != nil {
			parts = append(parts, fmt.Sprintf("%d files · %d symbols", rw.cand.Files, rw.cand.Symbols))
			if len(rw.cand.Top) > 0 {
				parts = append(parts, strings.Join(head(rw.cand.Top, 3), ", "))
			}
		}
		parts = append(parts, "→ New topic / Update topic")
		d := strings.Join(why, " + ") + "\n" + strings.Join(parts, " · ")
		if rw.chg != nil {
			d += "\n" + strings.Join(rw.chg.Files, "\n")
		}
		r.Items = append(r.Items, ReviewItem{ID: fmt.Sprintf("c%d", i+1), Kind: "candidate", Label: k, Detail: d})
	}
	if rep.Since != "" {
		r.SinceNote = fmt.Sprintf("โค้ดที่ commit หลัง blm init/update ครั้งล่าสุด (%s · %s)", rep.Since, rep.SinceFrom)
	}
	if rep.Search != nil {
		for _, h := range rep.Search.Hits {
			d := fmt.Sprintf("score %.2f", h.Score)
			if h.Trust != nil {
				d += " · " + trustCell(h.Trust)
			}
			if h.Preview != "" {
				d += " · " + h.Preview
			}
			r.Items = append(r.Items, ReviewItem{ID: fmt.Sprintf("h%d", h.ID), Kind: "hit", Label: fmt.Sprintf("%s:L%d-L%d", h.Path, h.StartLine, h.EndLine), Detail: d})
		}
	}
	return r
}

// reviewStores — store ที่รีวิวไฟล์นี้อยู่ (ตั้งตอน Save/Load) เพื่อให้ render อ่าน ignore.json ได้โดยไม่ต้องส่ง store ไปทุกที่
var (
	reviewStoresMu sync.Mutex
	reviewStores   = map[string]*Store{}
)

func bindReviewStore(r *Review, s *Store) {
	reviewStoresMu.Lock()
	reviewStores[r.ID] = s
	reviewStoresMu.Unlock()
}

func reviewStoreOf(r *Review) *Store {
	reviewStoresMu.Lock()
	defer reviewStoresMu.Unlock()
	return reviewStores[r.ID]
}

// RefreshItems — สร้างรายการข้อมูลประกอบใหม่จากรายงาน update ปัจจุบัน แล้วคงติ๊ก (ref/target/ignore) ของแถวเดิมไว้ (จับคู่ด้วย kind+label)
// id ของแถวอาจเลื่อน → refs ของหัวข้อที่สร้างแล้วถูกแปลงตาม (เก็บเป็น id เดิมของแถวที่ label เดียวกัน)
func (s *Store) RefreshItems(r *Review) {
	rep, err := s.UpdateReport(r.Query, 10)
	if err != nil {
		return
	}
	s.RefreshItemsFrom(r, rep)
}

// RefreshItemsFrom — เหมือน RefreshItems แต่ใช้รายงานที่คำนวณไว้แล้ว
func (s *Store) RefreshItemsFrom(r *Review, rep *UpdateReport) {
	// id ของแถวคงที่ตลอดอายุรีวิว (เจ้าของ 2026-09-20: refs ของหัวข้อชี้ "c3" แล้วรอบถัดไป c3 กลายเป็นโฟลเดอร์อื่น): แถวเดิม (kind+label
	// เดิม) ใช้ id เดิม · แถวใหม่ได้เลขถัดจากสูงสุดของ prefix นั้น · แถวที่หายจากรายงานแต่มีหัวข้ออ้าง/มีแชท เก็บไว้เป็น Gone
	fresh := NewReviewFromUpdate(rep).Items
	oldByKey := map[string]ReviewItem{}
	maxID := map[byte]int{}
	for _, it := range r.Items {
		oldByKey[it.Kind+"|"+it.Label] = it
		if n, err := strconv.Atoi(it.ID[1:]); err == nil && n > maxID[it.ID[0]] {
			maxID[it.ID[0]] = n
		}
	}
	seen := map[string]bool{}
	for i := range fresh {
		key := fresh[i].Kind + "|" + fresh[i].Label
		seen[key] = true
		if old, ok := oldByKey[key]; ok {
			fresh[i].ID = old.ID
			fresh[i].Ref, fresh[i].Target, fresh[i].Ignore = old.Ref, old.Target, old.Ignore
			fresh[i].Chat, fresh[i].AskDone, fresh[i].Summary = old.Chat, old.AskDone, old.Summary
			continue
		}
		p := fresh[i].ID[0]
		maxID[p]++
		fresh[i].ID = fmt.Sprintf("%c%d", p, maxID[p])
	}
	referenced := map[string]bool{}
	for _, t := range r.Topics {
		for _, id := range append(append([]string{}, t.Refs...), t.Related...) {
			referenced[id] = true
		}
		for _, sd := range t.Subs {
			for _, id := range sd.Refs {
				referenced[id] = true
			}
		}
	}
	for _, it := range r.Items {
		if !seen[it.Kind+"|"+it.Label] && (referenced[it.ID] || len(it.Chat) > 0) {
			it.Gone, it.Ref, it.Target = true, false, false
			fresh = append(fresh, it)
		}
	}
	r.Items = fresh
	if rep.Since != "" {
		r.SinceNote = fmt.Sprintf("โค้ดที่ commit หลัง blm init/update ครั้งล่าสุด (%s · %s)", rep.Since, rep.SinceFrom)
	}
}

// linkReviewedTopics — แถว t ที่มีหน้าหัวข้อในรีวิวใด ๆ (รวมที่ done) → Link ไปหน้าล่าสุด (ListReviews ใหม่สุดก่อน)
func (s *Store) linkReviewedTopics(r *Review) {
	base := "" // ใช้ server ปัจจุบัน ไม่ใช่ URL ที่จำไว้ในรีวิวเก่า (พอร์ตอาจเปลี่ยนหลัง reconnect)
	if i := strings.Index(r.URL, "/r/"); i > 0 {
		base = r.URL[:i]
	}
	found := map[string]string{}
	for _, o := range s.ListReviews() {
		for _, t := range o.Topics {
			key := t.Existing
			if key == "" {
				key = t.Name.Text
			}
			if _, ok := found[key]; !ok && base != "" && (t.Status == "created" || t.Status == "agreed" || t.Status == "proposed" || t.Status == "draft") {
				found[key] = base + "/r/" + o.ID + "/t/" + t.ID + "?from=" + r.ID // breadcrumb กลับมาหน้าหลักที่คลิกมา ไม่ใช่หน้าหลักของรีวิวเก่า
			}
		}
	}
	// แถว C/G ที่ถูกใช้เป็น ref ของหัวข้อในรีวิวอื่น (รวม done) → UsedIn (บอกสถานะหัวข้อ) ไม่ขึ้น "ยังไม่ได้จัด" ซ้ำ
	usedBy := map[string][]string{}
	for _, o := range s.ListReviews() {
		if o.ID == r.ID {
			continue
		}
		labelOf := map[string]string{}
		for _, it := range o.Items {
			labelOf[it.ID] = it.Label
		}
		for _, t := range o.Topics {
			name := t.Existing
			if name == "" {
				name = t.Name.Text
			}
			for _, ref := range t.Refs {
				if lbl := labelOf[ref]; lbl != "" {
					usedBy[lbl] = append(usedBy[lbl], fmt.Sprintf("→ %s (%s, %s)", name, t.Status, o.ID))
				}
			}
		}
	}
	for i := range r.Items {
		if r.Items[i].Kind == "topic" {
			r.Items[i].Link = found[r.Items[i].Label]
		} else {
			r.Items[i].UsedIn = uniq(usedBy[r.Items[i].Label])
		}
	}
}

// SaveReview — จุดเดียวที่เขียนไฟล์: JSON + HTML คู่กันเสมอ (`blm update --html --from`)
func (s *Store) SaveReview(r *Review) error {
	bindReviewStore(r, s)
	s.linkReviewedTopics(r)
	if err := os.MkdirAll(s.reviewsDir(), 0o755); err != nil {
		return err
	}
	r.UpdatedAt = nowISO()
	jsonPath := filepath.Join(s.reviewsDir(), "review-"+r.ID+".json")
	htmlPath := filepath.Join(s.reviewsDir(), "review-"+r.ID+".html")
	r.File, r.HTML = s.rel(jsonPath), s.rel(htmlPath)
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	html, err := RenderReviewHTML(r)
	if err != nil {
		return err
	}
	if err := os.WriteFile(htmlPath, []byte(html), 0o644); err != nil {
		return err
	}
	if r.Kind == "update" { // ไฟล์คงที่ต่อหน้า: reviews/update.html = รีวิวที่ทำอยู่
		_ = os.WriteFile(filepath.Join(s.reviewsDir(), "update.html"), []byte(html), 0o644)
	}
	liveBroadcast(r.ID, r.UpdatedAt, "update")
	// ignore มีผลกับรอบหน้าเมื่อ submit เท่านั้น (เจ้าของ 2026-09-20: ติ๊กพลาดแล้วโฟลเดอร์หายทันที) — ระหว่างนั้นอยู่แค่ในไฟล์รีวิว
	if r.Status == "submitted" {
		return s.saveIgnored(r)
	}
	return nil
}

// ignore.json — โฟลเดอร์/ไฟล์ที่เจ้าของติ๊ก ignore ในรีวิวใด ๆ · blm update ตัดออกจาก candidates/changed ทุกรอบถัดไป
func (s *Store) ignoreFile() string { return filepath.Join(s.reviewsDir(), "ignore.json") }

func (s *Store) Ignored() map[string]bool {
	out := map[string]bool{}
	raw, err := os.ReadFile(s.ignoreFile())
	if err != nil {
		return out
	}
	var list []string
	_ = json.Unmarshal(raw, &list)
	for _, l := range list {
		out[l] = true
	}
	return out
}

// Unignore เอาโฟลเดอร์ออกจาก ignore.json (ปุ่ม "เลิก ignore" ในหน้า — แถวที่ถูกกรองไปแล้วไม่มีช่องให้ติ๊กออก)
func (s *Store) Unignore(label string) error {
	set := s.Ignored()
	if !set[label] {
		return nil
	}
	delete(set, label)
	list := make([]string, 0, len(set))
	for k := range set {
		list = append(list, k)
	}
	sort.Strings(list)
	raw, _ := json.MarshalIndent(list, "", "  ")
	return os.WriteFile(s.ignoreFile(), append(raw, '\n'), 0o644)
}

func (s *Store) saveIgnored(r *Review) error {
	set := s.Ignored()
	changed := false
	for _, it := range r.Items {
		if it.Kind != "candidate" && it.Kind != "changed" {
			continue
		}
		if it.Ignore && !set[it.Label] { // เพิ่มอย่างเดียว — เลิก ignore ใช้ปุ่ม "เลิก ignore" (Unignore)
			set[it.Label], changed = true, true
		}
	}
	if !changed {
		return nil
	}
	list := make([]string, 0, len(set))
	for k := range set {
		list = append(list, k)
	}
	sort.Strings(list)
	raw, _ := json.MarshalIndent(list, "", "  ")
	return os.WriteFile(s.ignoreFile(), append(raw, '\n'), 0o644)
}

// ActiveReview — รีวิว update ที่ยังไม่จบ (ใหม่สุด) · alias "update" ใช้แทน id ได้ทุกที่ (URL /r/update, reviews/update.html, --from update)
// (เจ้าของ 2026-09-20: "html เดียวต่อหน้า แล้ว reuse" — id ข้างในยังเป็นของรอบนั้น เพื่อให้ลิงก์หน้าหัวข้อของรอบเก่าเปิดได้)
func (s *Store) ActiveReview() *Review {
	var fallback *Review
	for _, r := range s.ListReviews() {
		if r.Kind != "update" {
			continue
		}
		if r.Status != "done" { // ไฟล์รุ่นก่อนที่เคยถูกปิดเป็น done = archive เปิดดูได้แต่ไม่ใช่หน้าหลัก
			return r
		}
		if fallback == nil {
			fallback = r
		}
	}
	return fallback
}

// LoadReview — รับ path (relative จาก root ได้), ชื่อไฟล์, id หรือ alias "update"
func (s *Store) LoadReview(ref string) (*Review, error) {
	if ref == "update" || ref == "reviews/update.html" || strings.HasSuffix(ref, "/reviews/update.html") {
		if r := s.ActiveReview(); r != nil {
			return r, nil
		}
		return nil, fmt.Errorf("no open update review — blm update --html to start one")
	}
	cands := []string{ref, filepath.Join(s.Root, ref), filepath.Join(s.reviewsDir(), ref), filepath.Join(s.reviewsDir(), "review-"+ref+".json"), filepath.Join(s.reviewsDir(), ref+".json")}
	for _, p := range cands {
		raw, err := os.ReadFile(p)
		if err != nil || !strings.HasSuffix(p, ".json") {
			continue
		}
		var r Review
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("review %s: %w", p, err)
		}
		if r.ID == "" {
			return nil, fmt.Errorf("review %s: not a review file", p)
		}
		migrateChat(&r)
		bindReviewStore(&r, s)
		return &r, nil
	}
	return nil, fmt.Errorf("review %q not found under %s", ref, s.rel(s.reviewsDir()))
}

// ListReviews — ทุกรีวิว ใหม่สุดก่อน
func (s *Store) ListReviews() []*Review {
	entries, _ := os.ReadDir(s.reviewsDir())
	var out []*Review
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if r, err := s.LoadReview(filepath.Join(s.reviewsDir(), e.Name())); err == nil {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out
}

// ApplySubmission — เจ้าของกด submit รวม: บันทึกสถานะ + เลื่อนรอบ + status=submitted (จุดเดียวที่ทำให้ agent ถูกเรียก)
func ApplySubmission(r *Review, sub Submission) {
	ApplyOwnerState(r, sub)
	r.Round++
	r.Status = "submitted"
	r.ReadAt = ""
	r.Log = append(r.Log, ReviewLog{At: nowISO(), By: "owner", Event: fmt.Sprintf("submit round %d", r.Round-1)})
}

// ApplyOwnerState — ทุกคลิกในหน้า (ติ๊ก ref, New topic, COMMENT+บันทึก, AGREE, DRAFT) ลงไฟล์ทันที (เจ้าของ 2026-09-20)
// แต่ status หลัก/รอบไม่เปลี่ยนและไม่เรียก agent จนกว่าจะ submit รวม · ข้อความข้อเสนอ (Text) ของ agent ไม่ถูกแก้จากหน้า HTML
func ApplyOwnerState(r *Review, sub Submission) {
	flags := map[string]ReviewItem{}
	for _, it := range sub.Items {
		flags[it.ID] = it
	}
	for i := range r.Items {
		f := flags[r.Items[i].ID]
		r.Items[i].Ref, r.Items[i].Target, r.Items[i].Ignore = f.Ref, f.Target, f.Ignore
		if q := strings.TrimSpace(f.Ask); q != "" { // ข้อความใหม่ของเจ้าของในแชทแถวนี้
			r.Items[i].Chat = append(r.Items[i].Chat, ChatMsg{At: nowISO(), By: "owner", Text: q})
			r.Items[i].AskDone = false
		}
		if f.SummaryIdx != nil && *f.SummaryIdx >= 0 && *f.SummaryIdx < len(r.Items[i].Chat) {
			m := r.Items[i].Chat[*f.SummaryIdx]
			r.Items[i].Summary = &m
		}
		if f.AskDone && r.Items[i].Summary != nil { // ปิดแชทได้เมื่อมีบทสรุปแล้ว
			r.Items[i].AskDone = true
		}
	}
	byID := map[string]int{}
	for i, t := range r.Topics {
		byID[t.ID] = i
	}
	takeReview := func(dst *ReviewPoint, src ReviewPoint) {
		dst.Action, dst.Comment = strings.ToUpper(strings.TrimSpace(src.Action)), strings.TrimSpace(src.Comment)
		if dst.Comment != "" && dst.Action == "" {
			dst.Action = ActionComment
		}
		if q := strings.TrimSpace(src.Ask); q != "" { // comment = ข้อความใหม่ในแชตของจุดนี้ → COMMENT ค้างจน agent ตอบ/แก้
			dst.Chat = append(dst.Chat, ChatMsg{At: nowISO(), By: "owner", Text: q})
			dst.AskDone, dst.Action = false, ActionComment
		}
		if src.SummaryIdx != nil && *src.SummaryIdx >= 0 && *src.SummaryIdx < len(dst.Chat) {
			m := dst.Chat[*src.SummaryIdx]
			dst.Summary = &m
		}
		if src.AskDone && dst.Summary != nil {
			dst.AskDone = true
		}
	}
	kindOf := map[string]string{}
	for _, it := range r.Items {
		kindOf[it.ID] = it.Kind
	}
	relatedOf := func(refs []string) []string { // ref ที่เป็นแถว t = หัวข้อที่มีอยู่ที่เกี่ยวข้อง
		var out []string
		for _, id := range refs {
			if kindOf[id] == "topic" {
				out = append(out, id)
			}
		}
		return out
	}
	for _, t := range sub.Topics {
		i, ok := byID[t.ID]
		if !ok { // แถวใหม่จากปุ่ม Update topic
			nt := ReviewTopic{ID: t.ID, Refs: t.Refs, Related: relatedOf(t.Refs), Existing: strings.TrimSpace(t.Existing), Note: strings.TrimSpace(t.Note), Name: ReviewPoint{Text: strings.TrimSpace(t.Name.Text)}, Status: "new"}
			if nt.Existing != "" {
				nt.Status = "extend"
				if nt.Name.Text == "" {
					nt.Name.Text = nt.Existing
				}
			}
			if nt.ID == "" || byID[nt.ID] != 0 {
				nt.ID = "n" + newReviewID()[:4]
			}
			if nt.Name.Text == "" {
				continue
			}
			r.Topics = append(r.Topics, nt)
			byID[nt.ID] = len(r.Topics) - 1
			continue
		}
		cur := &r.Topics[i]
		if len(t.Refs) > 0 {
			cur.Refs = t.Refs
			cur.Related = relatedOf(t.Refs)
		}
		if cur.Status == "new" || cur.Status == "extend" { // ยังไม่มีข้อเสนอ — เจ้าของแก้ note ได้
			cur.Note = strings.TrimSpace(t.Note)
		}
		takeReview(&cur.Name, t.Name)
		takeReview(&cur.Desc, t.Desc)
		subs := map[string]int{}
		for j, sd := range cur.Subs {
			subs[sd.ID] = j
		}
		for _, sd := range t.Subs {
			if j, ok := subs[sd.ID]; ok {
				takeReview(&cur.Subs[j].Name, sd.Name)
				takeReview(&cur.Subs[j].Desc, sd.Desc)
			}
		}
		cur.Status = topicStatus(*cur)
	}
}

// topicStatus — agreed เมื่อทุกจุด AGREE · draft เมื่อมี DRAFT และไม่มี COMMENT ค้าง · ไม่งั้น proposed/new ตามเดิม
func topicStatus(t ReviewTopic) string {
	if t.Status == "new" || t.Status == "extend" || t.Status == "created" {
		return t.Status
	}
	points := []ReviewPoint{t.Name, t.Desc}
	for _, s := range t.Subs {
		points = append(points, s.Name, s.Desc)
	}
	agreed, draft := true, false
	for _, p := range points {
		switch p.Action {
		case ActionAgree:
		case ActionDraft:
			agreed, draft = false, true
		default:
			agreed = false
			if p.Action == ActionComment {
				return "proposed"
			}
		}
	}
	if agreed {
		return "agreed"
	}
	if draft {
		return "draft"
	}
	return "proposed"
}

// ApplyProposal — agent ใส่ข้อเสนอ: ข้อความเปลี่ยน → ของเดิม (พร้อม action/comment ที่ได้รับ) ลง history แล้วล้าง action · ไม่เปลี่ยน = คง action เดิม (AGREE อยู่ต่อ)
func ApplyProposal(r *Review, p Proposal) error {
	set := func(dst *ReviewPoint, text string) {
		text = strings.TrimSpace(text)
		if text == "" || text == dst.Text {
			return
		}
		if dst.Text != "" {
			dst.History = append(dst.History, ReviewPoint{Text: dst.Text, Action: dst.Action, Comment: dst.Comment})
		}
		dst.Text, dst.Action, dst.Comment = text, "", ""
	}
	for _, a := range p.Answers {
		if strings.TrimSpace(a.Answer) == "" {
			continue
		}
		m := ChatMsg{At: nowISO(), By: "agent", Text: strings.TrimSpace(a.Answer)}
		for i := range r.Items {
			if r.Items[i].ID == a.ID {
				r.Items[i].Chat = append(r.Items[i].Chat, m)
				if a.Summary {
					r.Items[i].Summary = &m
				}
			}
		}
		if pt := pointRef(r, a.ID); pt != nil { // แชตของจุด (comment) — ตอบแล้วยังเป็น COMMENT จนเจ้าของกด agree หรือข้อความจุดถูกแก้
			pt.Chat = append(pt.Chat, m)
			if a.Summary {
				pt.Summary = &m
			}
		}
	}
	byID := map[string]int{}
	for i, t := range r.Topics {
		byID[t.ID] = i
	}
	for _, pt := range p.Topics {
		i, ok := byID[pt.ID]
		if !ok {
			if strings.TrimSpace(pt.Name) == "" {
				return fmt.Errorf("proposal: topic name required for a new topic")
			}
			r.Topics = append(r.Topics, ReviewTopic{ID: "p" + newReviewID()[:4], Status: "proposed"})
			i = len(r.Topics) - 1
			byID[r.Topics[i].ID] = i
		}
		cur := &r.Topics[i]
		set(&cur.Name, pt.Name)
		set(&cur.Desc, pt.Desc)
		if pt.Note != "" {
			cur.Note = pt.Note
		}
		subs := map[string]int{}
		for j, sd := range cur.Subs {
			subs[sd.ID] = j
		}
		for _, ps := range pt.Subs { // เฉพาะที่ส่งมา: id เดิม = แก้ · ไม่มี id = เพิ่ม · ที่ไม่ส่งคงอยู่
			if j, ok := subs[ps.ID]; ok {
				set(&cur.Subs[j].Name, ps.Name)
				set(&cur.Subs[j].Desc, ps.Desc)
				if len(ps.Refs) > 0 {
					cur.Subs[j].Refs = ps.Refs
				}
				continue
			}
			cur.Subs = append(cur.Subs, ReviewSub{ID: fmt.Sprintf("%s-s%d-%s", cur.ID, len(cur.Subs)+1, newReviewID()[:2]), Refs: ps.Refs, Name: ReviewPoint{Text: strings.TrimSpace(ps.Name)}, Desc: ReviewPoint{Text: strings.TrimSpace(ps.Desc)}})
		}
		if len(pt.Drop) > 0 { // ตัดหัวข้อย่อยต้องบอกชัด
			var left []ReviewSub
			for _, sd := range cur.Subs {
				if !slices.Contains(pt.Drop, sd.ID) {
					left = append(left, sd)
				}
			}
			cur.Subs = left
		}
		if cur.Status == "new" || cur.Status == "extend" {
			cur.Status = "proposed"
		}
		cur.Status = topicStatus(*cur)
		if strings.HasPrefix(strings.ToLower(pt.Note), "created") { // agent เขียนโน้ตจริงแล้ว
			cur.Status = "created"
		}
	}
	r.Status = "open"
	// รีวิว update ไม่มีวัน "done" (เจ้าของ 2026-09-20: หน้าหลักยังไม่จบ ทำไมสร้างไฟล์ใหม่) — หัวข้อจบทีละแถว (created) และหายจากรายการ
	// ส่วนหน้า/ไฟล์เดิมใช้ต่อไปเรื่อย ๆ · จุดตัด "โค้ดที่แก้หลัง update ล่าสุด" เลื่อนทุกครั้งที่มีหัวข้อถูกเขียน
	for _, t := range r.Topics {
		if t.Status == "created" && strings.HasPrefix(strings.ToLower(t.Note), "created") {
			if st := reviewStoreOf(r); st != nil {
				_ = st.MarkUpdate("topic created " + t.ID + " (" + r.ID + ")")
			}
		}
	}
	r.Log = append(r.Log, ReviewLog{At: nowISO(), By: "agent", Event: fmt.Sprintf("propose round %d", r.Round)})
	return nil
}

// MarkRead — agent อ่านแล้ว (hook เลิกเตือน)
func (s *Store) MarkRead(r *Review) { r.ReadAt = nowISO() }

// PendingReviews — submit แล้ว agent ยังไม่อ่าน (hook prompt ใช้)
func (s *Store) PendingReviews() []*Review {
	var out []*Review
	for _, r := range s.ListReviews() {
		if r.Status == "submitted" && r.ReadAt == "" {
			out = append(out, r)
		}
	}
	return out
}

// DraftPoint จุดที่ยัง DRAFT (blm update draft)
type DraftPoint struct {
	Review string `json:"review"`
	Topic  string `json:"topic"`
	Where  string `json:"where"` // name · desc · sub <name> name/desc
	Text   string `json:"text"`
	Note   string `json:"note,omitempty"`
}

// Drafts — ทุกจุดที่ค้าง DRAFT ในทุกรีวิว (topic ว่าง = ทั้งหมด)
func (s *Store) Drafts(topic string) []DraftPoint {
	var out []DraftPoint
	match := func(name string) bool {
		return topic == "" || strings.Contains(strings.ToLower(name), strings.ToLower(topic))
	}
	for _, r := range s.ListReviews() {
		for _, t := range r.Topics {
			if !match(t.Name.Text) {
				continue
			}
			add := func(where string, p ReviewPoint) {
				if p.Action == ActionDraft {
					out = append(out, DraftPoint{Review: r.File, Topic: t.Name.Text, Where: where, Text: p.Text, Note: p.Comment})
				}
			}
			add("name", t.Name)
			add("desc", t.Desc)
			for _, sd := range t.Subs {
				add("sub "+sd.Name.Text+" · name", sd.Name)
				add("sub "+sd.Name.Text+" · desc", sd.Desc)
			}
		}
	}
	return out
}

// ---- server ----

var (
	reviewSrvMu sync.Mutex
	reviewSrvs  = map[string]*reviewServer{} // root → server (process หนึ่งรับได้หลายโปรเจ็ค — test รันหลาย root ในโปรเซสเดียว)
)

type reviewServer struct {
	store    *Store
	base     string
	submitCh chan string // id ที่เพิ่ง submit (CLI รอ)
}

// reviewPort พอร์ตคงที่ต่อโปรเจ็ค (41000–41999) เพื่อให้ URL ในไฟล์ JSON ใช้ซ้ำได้หลัง restart
func reviewPort(root string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(root))
	return 41000 + int(h.Sum32()%1000)
}

// StartReviewServer — server เดียวต่อ process · คืน base URL (http://127.0.0.1:<port>)
func (s *Store) StartReviewServer() (string, error) {
	reviewSrvMu.Lock()
	defer reviewSrvMu.Unlock()
	if srv := reviewSrvs[s.Root]; srv != nil {
		return srv.base, nil
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", reviewPort(s.Root)))
	if err != nil {
		if ln, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			return "", err
		}
	}
	srv := &reviewServer{store: s, base: "http://" + ln.Addr().String(), submitCh: make(chan string, 16)}
	mux := http.NewServeMux()
	mux.HandleFunc("/r/", srv.handle)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<h3>blm review</h3><ul>")
		for _, r := range s.ListReviews() {
			fmt.Fprintf(w, `<li><a href="/r/%s">%s</a> — round %d · %s · %s</li>`, r.ID, template.HTMLEscapeString(reviewTitle(r)), r.Round, r.Status, r.UpdatedAt)
		}
		fmt.Fprint(w, "</ul>")
	})
	go func() { _ = http.Serve(ln, mux) }()
	reviewSrvs[s.Root] = srv
	return srv.base, nil
}

func reviewTitle(r *Review) string {
	if r.Query != "" {
		return "blm update " + r.Query
	}
	return "blm update"
}

func (srv *reviewServer) handle(w http.ResponseWriter, req *http.Request) {
	rest := strings.TrimPrefix(req.URL.Path, "/r/")
	id, action, _ := strings.Cut(rest, "/")
	if id == "update" && action == "" && req.Method == http.MethodGet { // URL คงที่ → หน้าที่ทำอยู่ (redirect ให้ BASE/SSE เป็น id จริง)
		if r := srv.store.ActiveReview(); r != nil {
			http.Redirect(w, req, srv.base+"/r/"+r.ID, http.StatusFound)
			return
		}
	}
	r, err := srv.store.LoadReview(id)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	topicID, back := "", ""
	if strings.HasPrefix(action, "t/") { // หน้าหัวข้อ (เจ้าของ 2026-09-20: หัวข้อย่อยที่เสนอเป็นหน้าใหม่ ไม่ปนหน้าหลัก)
		topicID, action = strings.TrimPrefix(action, "t/"), ""
		if from := req.URL.Query().Get("from"); from != "" && from != r.ID {
			if o, err := srv.store.LoadReview(from); err == nil { // เปิดจากรีวิวอื่น (แถว t ที่เคยรีวิว) → breadcrumb กลับไปที่นั่น
				back = srv.base + "/r/" + o.ID
			}
		}
	}
	switch {
	case action == "" && req.Method == http.MethodGet:
		html, err := renderReviewHTMLBack(r, topicID, back)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	case action == "state" && req.Method == http.MethodGet: // หน้าโหลดสถานะล่าสุด (SSE บอกว่าเปลี่ยน)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"review": r, "live": liveWaiting(r.ID), "typing": liveIsTyping(r.ID)})
	case action == "events" && req.Method == http.MethodGet: // SSE
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		ch, unsub := liveSubscribe(r.ID)
		defer unsub()
		send := func(ev reviewEvent) {
			raw, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", raw)
			fl.Flush()
		}
		send(reviewEvent{ID: r.ID, UpdatedAt: r.UpdatedAt, Kind: "hello", Live: liveWaiting(r.ID), Typing: liveIsTyping(r.ID), Version: buildID()})
		hb := time.NewTicker(20 * time.Second)
		defer hb.Stop()
		for {
			select {
			case ev := <-ch:
				ev.Live, ev.Typing = liveWaiting(r.ID), liveIsTyping(r.ID)
				send(ev)
			case <-hb.C:
				fmt.Fprint(w, ": hb\n\n")
				fl.Flush()
			case <-req.Context().Done():
				return
			}
		}
	case action == "file" && req.Method == http.MethodGet: // เปิดไฟล์โปรเจ็คที่ agent อ้างในแชท (อ่านอย่างเดียว, เฉพาะใต้ root, ไม่ใช่ .env)
		rel := filepath.ToSlash(filepath.Clean(req.URL.Query().Get("p")))
		if rel == "" || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "..") || strings.Contains(rel, "/../") || strings.HasPrefix(filepath.Base(rel), ".env") {
			http.Error(w, "bad path", 400)
			return
		}
		raw, err := os.ReadFile(filepath.Join(srv.store.Root, filepath.FromSlash(rel)))
		if err != nil {
			http.Error(w, "not found: "+rel, 404)
			return
		}
		if len(raw) > 400*1024 {
			raw = append(raw[:400*1024], []byte("\n… (truncated)")...)
		}
		ct := "text/plain; charset=utf-8"
		if req.URL.Query().Get("raw") == "1" { // .html ที่ agent อ้าง → เปิดแท็บใหม่แสดงผลจริง (เจ้าของ 2026-09-20)
			switch strings.ToLower(filepath.Ext(rel)) {
			case ".html", ".htm":
				ct = "text/html; charset=utf-8"
			case ".svg":
				ct = "image/svg+xml"
			case ".png":
				ct = "image/png"
			case ".jpg", ".jpeg":
				ct = "image/jpeg"
			}
		}
		w.Header().Set("Content-Type", ct)
		_, _ = w.Write(raw)
	case action == "wait" && req.Method == http.MethodGet: // long-poll ผ่าน HTTP ให้ CLI (`blm update --wait`) ที่รันเป็น background ของ Claude Code
		sec := 240
		if v, err := strconv.Atoi(req.URL.Query().Get("timeout")); err == nil && v > 0 {
			sec = v
		}
		res, err := srv.store.WaitReview(r.ID, time.Duration(sec)*time.Second)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)
	case action == "live" && req.Method == http.MethodPost: // เจ้าของกด "จบ live" → agent ที่รออยู่ตื่นแล้วเลิกวน
		liveBroadcast(r.ID, r.UpdatedAt, "stop")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	case action == "unignore" && req.Method == http.MethodPost:
		var in struct {
			Label  string   `json:"label"`
			Labels []string `json:"labels"`
		}
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil || (in.Label == "" && len(in.Labels) == 0) {
			http.Error(w, "label(s) required", 400)
			return
		}
		if in.Label != "" {
			in.Labels = append(in.Labels, in.Label)
		}
		for _, l := range in.Labels {
			if err := srv.store.Unignore(l); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			for i := range r.Items {
				if r.Items[i].Label == l {
					r.Items[i].Ignore = false
				}
			}
		}
		srv.store.RefreshItems(r) // แถวที่เคยถูกกรองกลับมาในหน้าเดิมทันที (เจ้าของ 2026-09-20: ไม่ต้องรัน --html ใหม่)
		_ = srv.store.SaveReview(r)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "reload": true})
	case (action == "submit" || action == "save") && req.Method == http.MethodPost:
		if r.Status == "submitted" {
			http.Error(w, "already submitted — waiting for the agent's next round", 409)
			return
		}
		var sub Submission
		if err := json.NewDecoder(req.Body).Decode(&sub); err != nil {
			http.Error(w, "bad json: "+err.Error(), 400)
			return
		}
		if action == "save" {
			ApplyOwnerState(r, sub)
		} else {
			// submit โดยยังไม่มีแถวหัวข้อ = ลืมกด New topic / Update topic (เจ้าของทำจริง 2026-09-20 รอบแรก) → ไม่ปิดหน้า บอกให้กดก่อน
			hasAsk := false // 1 ข้อความแชทก็ submit ได้ และ submit พร้อม New/Update topic ได้ (เจ้าของ 2026-09-20)
			for _, it := range sub.Items {
				if strings.TrimSpace(it.Ask) != "" {
					hasAsk = true
				}
			}
			for _, it := range r.Items {
				if it.chatPending() {
					hasAsk = true
				}
			}
			if len(sub.Topics) == 0 && !hasAsk {
				ApplyOwnerState(r, sub)
				_ = srv.store.SaveReview(r)
				http.Error(w, "ยังไม่มีแถวหัวข้อหรือคำถาม — ติ๊ก ref แล้วกด New topic / Update topic หรือกด ask ที่แถว ก่อน submit (ที่ติ๊กไว้บันทึกแล้ว)", 422)
				return
			}
			ApplySubmission(r, sub)
		}
		r.URL = srv.base + "/r/" + r.ID
		if err := srv.store.SaveReview(r); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if action == "save" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "saved": r.UpdatedAt, "file": r.File})
			return
		}
		select {
		case srv.submitCh <- r.ID:
		default:
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "round": r.Round, "file": r.File, "next": "the agent reads " + r.File + " (blm_update {from}) and answers in the next round"})
	default:
		http.Error(w, "not found", 404)
	}
}

// WaitSubmit — CLI: รอจนเจ้าของ submit รีวิว id (timeout 0 = ไม่จำกัด)
func (s *Store) WaitSubmit(id string, timeout time.Duration) bool {
	reviewSrvMu.Lock()
	srv := reviewSrvs[s.Root]
	reviewSrvMu.Unlock()
	if srv == nil {
		return false
	}
	var t <-chan time.Time
	if timeout > 0 {
		t = time.After(timeout)
	}
	for {
		select {
		case got := <-srv.submitCh:
			if got == id {
				return true
			}
		case <-t:
			return false
		}
	}
}

// OpenBrowser — เปิด URL ด้วยเบราว์เซอร์ของเครื่อง: ลอง `open`/xdg-open ก่อน ไม่สำเร็จ → เรียก Chrome ตรง ๆ (เจ้าของ 2026-09-21:
// fallback อยู่ใน blm ไม่ใช่ให้ agent ไปกด extension เอง) · MCP ใช้ผ่าน blm_update {html, open} เพราะ `open` จาก Bash ของ agent
// โดน sandbox ตัด LaunchServices · คืน error เมื่อทุกทางล้ม (ผู้เรียกบอก URL ให้เจ้าของเปิดเอง)
func OpenBrowser(url string) error {
	var tries [][]string
	switch runtime.GOOS {
	case "darwin":
		tries = [][]string{{"open", url}, {"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", url}, {"open", "-a", "Safari", url}}
	case "windows":
		tries = [][]string{{"rundll32", "url.dll,FileProtocolHandler", url}, {"cmd", "/c", "start", "", url}}
	default:
		tries = [][]string{{"xdg-open", url}, {"google-chrome", url}, {"chromium", url}, {"firefox", url}}
	}
	var last error
	for _, t := range tries {
		c := exec.Command(t[0], t[1:]...)
		if runtime.GOOS == "darwin" && strings.HasSuffix(t[0], "Google Chrome") {
			if err := c.Start(); err == nil { // ไบนารีของ Chrome ไม่ return จนปิดหน้าต่าง → แค่ start
				return nil
			} else {
				last = err
				continue
			}
		}
		if out, err := c.CombinedOutput(); err == nil {
			return nil
		} else {
			last = fmt.Errorf("%s: %v %s", t[0], err, strings.TrimSpace(string(out)))
		}
	}
	return last
}

// OpenReview — สร้าง/โหลด แล้วให้มี server + URL · คืน review ที่บันทึกแล้ว
func (s *Store) OpenReview(r *Review) (*Review, error) {
	base, err := s.StartReviewServer()
	if err != nil {
		return nil, err
	}
	r.URL = base + "/r/" + r.ID
	return r, s.SaveReview(r)
}

// ---- html ----

// RenderReviewHTML — หน้าหลัก (ไฟล์ .html ที่บันทึก) · หน้าหัวข้อ = renderReviewHTML(r, topicID) เสิร์ฟสดจาก server
func RenderReviewHTML(r *Review) (string, error) { return renderReviewHTML(r, "") }

func renderReviewHTML(r *Review, topicID string) (string, error) {
	return renderReviewHTMLBack(r, topicID, "")
}

// renderReviewHTMLBack — หน้าเดียว: state ฝังเป็น JSON แล้ว JS วาด (ใช้ซ้ำทุก event, ไม่มี asset ข้างนอก) · topicID ว่าง = หน้าหลัก · back = URL ที่ breadcrumb กลับไป ("" = หน้าหลักของรีวิวนี้)
func renderReviewHTMLBack(r *Review, topicID, back string) (string, error) {
	r.Ignored = nil
	if r.File != "" {
		if s := reviewStoreOf(r); s != nil {
			for k := range s.Ignored() {
				r.Ignored = append(r.Ignored, k)
			}
			sort.Strings(r.Ignored)
		}
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if back == "" {
		back = r.URL
	}
	err = reviewTmpl.Execute(&b, map[string]any{"Title": reviewTitle(r), "State": template.JS(raw), "TopicID": topicID, "Base": r.URL, "Back": back, "Version": buildID(), "Submit": r.URL + "/submit", "Save": r.URL + "/save", "Unignore": r.URL + "/unignore"})
	return b.String(), err
}

var reviewTmpl = template.Must(template.New("review").Parse(reviewHTML))

const reviewHTML = `<!doctype html>
<html lang="th"><head><meta charset="utf-8"><title>{{.Title}}</title>
<style>
body{font:14px/1.5 -apple-system,Segoe UI,Roboto,sans-serif;margin:0;background:#f6f7f9;color:#1f2328}
header{background:#1f6f43;color:#fff;padding:14px 24px}header h1{margin:0;font-size:18px}header .meta{opacity:.85;font-size:12px}
main{max-width:1100px;margin:0 auto;padding:16px 24px 120px}
h2{font-size:15px;margin:28px 0 8px;border-bottom:2px solid #d0d7de;padding-bottom:4px}
table{width:100%;border-collapse:collapse;background:#fff;font-size:13px}td,th{padding:6px 8px;border-bottom:1px solid #e6e8eb;vertical-align:top;text-align:left}
th{background:#eef1f4;font-weight:600}td.k{white-space:nowrap;color:#57606a}td.d{color:#57606a;font-size:12px;white-space:pre-line}
.card{background:#fff;border:1px solid #d0d7de;border-radius:8px;padding:12px 16px;margin:12px 0}
.card .st{float:right;font-size:11px;padding:2px 8px;border-radius:10px;background:#eef1f4}
.st.agreed{background:#dafbe1;color:#116329}.st.draft{background:#fff8c5;color:#7d4e00}.st.proposed,.st.new,.st.extend{background:#ddf4ff;color:#0550ae}.st.created{background:#e7e7e7}
.pt{display:grid;grid-template-columns:110px 1fr 190px;gap:8px;padding:8px 0;border-top:1px dashed #e6e8eb;align-items:start}
.pt .lbl{color:#57606a;font-size:12px;padding-top:4px}.pt .txt{white-space:pre-wrap}.pt .txt.empty{color:#8c959f;font-style:italic}
.btns button{font-size:12px;padding:3px 8px;margin-left:4px;border:1px solid #d0d7de;border-radius:6px;background:#fff;cursor:pointer}
.btns button.on.COMMENT{background:#ddf4ff;border-color:#54aeff}.btns button.on.AGREE{background:#dafbe1;border-color:#4ac26b}.btns button.on.DRAFT{background:#fff8c5;border-color:#d4a72c}
textarea{width:100%;box-sizing:border-box;margin-top:6px;font:13px inherit;padding:6px;border:1px solid #d0d7de;border-radius:6px;min-height:56px}
.hist{font-size:11px;color:#8c959f;margin-top:4px}.hist span{display:block}
.refs{font-size:12px;color:#57606a}.refs code{background:#eef1f4;padding:1px 5px;border-radius:4px;margin-right:4px}
.newtopic{display:flex;gap:8px;align-items:center;margin:10px 0}.newtopic input{flex:1;padding:6px 8px;border:1px solid #d0d7de;border-radius:6px;font:inherit}.newtopic button{white-space:nowrap}button.ghost:disabled{opacity:.45;cursor:default}.newtopic input:disabled{background:#f6f7f9;color:#8c959f}
button.primary{background:#1f6f43;color:#fff;border:0;border-radius:6px;padding:8px 16px;font:inherit;cursor:pointer}button.primary:disabled{opacity:.5;cursor:default}
button.ghost{background:#fff;border:1px solid #d0d7de;border-radius:6px;padding:6px 12px;font:inherit;cursor:pointer}
footer{position:fixed;bottom:0;left:0;right:0;background:#fff;border-top:1px solid #d0d7de;padding:12px 24px;display:flex;gap:12px;align-items:center}#live{font-size:12px}
#chatModal{position:fixed;inset:0;background:#0006;display:flex;align-items:center;justify-content:center;z-index:50}
.cbox{background:#fff;width:min(720px,94vw);height:min(70vh,640px);border-radius:10px;display:flex;flex-direction:column;box-shadow:0 12px 40px #0004}
.chead{display:flex;align-items:center;gap:8px;padding:10px 14px;border-bottom:1px solid #e6e8eb}
.cmsgs{flex:1;overflow:auto;padding:12px 14px;display:flex;flex-direction:column;gap:8px;background:#f6f7f9}
.cm{max-width:85%;padding:8px 10px;border-radius:10px;white-space:pre-wrap;font-size:13px}.cm .who{font-size:11px;color:#8c959f;margin-bottom:2px}
.cm.owner{align-self:flex-end;background:#ddf4ff}.cm.agent{align-self:flex-start;background:#fff;border:1px solid #e6e8eb}
.cm .ctools{margin-left:8px;opacity:0;transition:opacity .15s}.cm:hover .ctools{opacity:1}.cm .ctools button{border:0;background:none;cursor:pointer;font-size:12px;color:#57606a;padding:0 3px}.cm .ctools button:hover{color:#0550ae}
.cinput{display:flex;gap:8px;padding:10px 14px;border-top:1px solid #e6e8eb}.cinput textarea{flex:1;margin:0;min-height:44px}
details.code{margin:4px 0;border:1px solid #d0d7de;border-radius:6px;background:#f6f8fa}details.code summary{cursor:pointer;padding:4px 8px;list-style:none;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}details.code summary .more{color:#8c959f;font-size:11px}details.code[open] summary .more{display:none}details.code pre{margin:0;padding:8px;border-top:1px solid #d0d7de;overflow:auto;font-size:12px}
#fileModal{position:fixed;inset:0;background:#0006;display:flex;align-items:center;justify-content:center;z-index:60}.fbody{flex:1;overflow:auto;margin:0;padding:8px 0;font-size:12px;line-height:1.45}.fl{padding:0 12px;white-space:pre}.fl.hit{background:#fff8c5}.fl .ln{display:inline-block;width:44px;color:#8c959f;text-align:right;margin-right:12px;user-select:none}
.spin{display:inline-block;width:11px;height:11px;border:2px solid #0550ae;border-right-color:transparent;border-radius:50%;animation:spin .8s linear infinite;vertical-align:-1px}@keyframes spin{to{transform:rotate(360deg)}}
.typing{color:#0550ae}.typing span{animation:blink 1.2s infinite}.typing span:nth-child(2){animation-delay:.2s}.typing span:nth-child(3){animation-delay:.4s}@keyframes blink{0%,80%,100%{opacity:.2}40%{opacity:1}}
footer .msg{color:#57606a;font-size:13px}#msg.err{color:#cf222e}#msg.ok{color:#116329}
.count{font-size:12px;color:#57606a;margin-left:8px}
</style></head><body>
<header><h1 id="title"></h1><div class="meta" id="meta"></div></header>
<main>
<section id="refsec"><h2 id="refhead" style="cursor:pointer;user-select:none"><span id="reftoggle">▾</span> ข้อมูลประกอบ <span class="count" id="checkedCount"></span></h2>
<div id="refbody">
<p class="refs">ทิศทางเดียว: ติ๊ก <b>ref</b> หลายแถว (C ที่จะจัด + แถว T ที่เกี่ยวข้อง) → พิมพ์ชื่อใหม่แล้วกด <b>New topic</b> หรือเลือก <b>target</b> หนึ่งอันแล้วกด <b>Update topic</b> (รวมเข้าหัวข้อเดิม) = หนึ่งแถวด้านล่าง · กดซ้ำจน "ยังไม่ได้จัด" เหลือ 0 แล้วค่อย submit · <b>ignore</b> = ไม่ใช่ความรู้ของโปรเจ็ค ไม่เสนออีก · <b>used in</b> = แถวนี้ไปอยู่หัวข้อไหนแล้ว</p>
<div id="items"></div>
<div class="newtopic"><input id="newName" placeholder="ชื่อหัวข้อหลักใหม่ (สำหรับ New topic)"><button class="primary" id="newBtn">New topic</button><button class="ghost" id="updBtn" disabled>Update topic</button></div>
<textarea id="newNote" placeholder="บอก agent: บริบท/กติกาที่คุณรู้ เช่น ติ๊ก is_vpn ใน service = ใช้ผ่าน VPN เท่านั้น, auth วิ่งผ่าน auth_guard"></textarea>
</div>
</section>
<section><h2>หัวข้อหลัก</h2><div id="topics"></div></section>
</main>
<footer><button class="primary" id="submit"></button><span class="msg" id="msg"></span><span style="margin-left:auto" id="live"></span><button class="ghost" id="stopLive" style="display:none">จบ live</button></footer>
<script>
const STATE = {{.State}};
const SUBMIT = {{.Submit}};
const SAVE = {{.Save}};
const UNIGNORE = {{.Unignore}};
const TOPIC = {{.TopicID}};   // '' = หน้าหลัก (ข้อมูลประกอบ + รายการหัวข้อ) · id = หน้าหัวข้อ (ข้อเสนอ + comment/agree/draft)
const BASE = {{.Base}};
const BACK = {{.Back}};
const VERSION = {{.Version}};
// live: SSE บอกว่าไฟล์เปลี่ยน → โหลด /state แล้ววาดใหม่ (ถ้ากำลังพิมพ์ค้างอยู่ รอจนกดบันทึกก่อน) · แสดงว่ามี agent รออยู่ไหม / กำลังพิมพ์ไหม
let agentLive = false, agentTyping = false, pendingRefresh = false;
function editing(){ const ct = document.getElementById('chatText'); return openComments.size || openAsks.size || ($('#newName') && $('#newName').value) || ($('#newNote') && $('#newNote').value) || (ct && ct.value.trim()); }
async function refreshState(){
  if (editing()) { pendingRefresh = true; return; }
  const res = await fetch(BASE + '/state'); if (!res.ok) return;
  const j = await res.json(); Object.assign(STATE, j.review); agentLive = j.live; agentTyping = j.typing; pendingRefresh = false;
  if (!(STATE.topics||[]).some(x => x.status !== 'created')) refOpen = true;
  render(); setLive(agentLive, agentTyping); renderChat();
}
function setLive(on, typing){
  agentLive = on; agentTyping = !!typing; const el = $('#live'); if (!el) return;
  el.innerHTML = typing ? '<span class="typing"><span class="spin"></span> agent กำลังทำงาน<span>.</span><span>.</span><span>.</span></span>' : on ? '<span style="color:#116329">🟢 agent กำลังฟังอยู่ — บันทึก ask/comment/agree แล้วตอบทันที</span>' : '<span style="color:#8c959f">⚪ ไม่มี agent รอ — submit แล้ว hook จะแจ้งเมื่อคุยครั้งถัดไป</span>';
  $('#stopLive').style.display = on ? '' : 'none';
  if (chatOpen) renderChat();
  if (typing !== lastTyping || on !== lastLive) { lastTyping = typing; lastLive = on; if (!editing()) render(); }
}
let lastTyping = false, lastLive = false, redirected = false;
const openedCreated = !!(TOPIC && ((STATE.topics||[]).find(x => x.id === TOPIC) || {}).status === 'created');
try {
  const es = new EventSource(BASE + '/events');
  es.onmessage = e => {
    const ev = JSON.parse(e.data);
    if (ev.kind === 'hello' && ev.version && ev.version !== VERSION && !editing()) { location.reload(); return; } // server เป็น build ใหม่ (reconnect แล้ว) → หน้าใหม่เอง
    // agent gen หน้าใหม่ (blm_update {html:true}) = ข้อมูลเปลี่ยน ไม่ใช่โค้ดหน้าเปลี่ยน → ดึง state มา render ในที่ ไม่ reload ทั้งหน้า
    // (เจ้าของ 2026-09-21: reload ทำให้ทุก UI รีเซ็ต) · reload ทั้งหน้าเหลือกรณีเดียว: build ใหม่ (hello.version ต่างกัน)
    if (ev.kind === 'reload') { refreshState(); return; }
    setLive(!!ev.live, !!ev.typing); if (ev.updatedAt && ev.updatedAt !== STATE.updatedAt && ev.kind !== 'presence') refreshState();
  };
} catch (e) {}
document.getElementById('stopLive').onclick = async () => { await fetch(BASE + '/live', {method:'POST', headers:{'Content-Type':'application/json'}, body:'{"on":false}'}); setLive(false, false); };
const KINDS = {topic:'หัวข้อที่มีอยู่แล้ว', candidate:'โฟลเดอร์โค้ดที่ยังไม่มีกฎ', changed:'โค้ดที่ถูกแก้หลัง blm init/update ครั้งล่าสุด', hit:'ผลค้น'};
const $ = s => document.querySelector(s);
const esc = s => String(s ?? '').replace(/[&<>"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
const openComments = new Set();
const openAsks = new Set();
let chatOpen = null; // item id ที่ modal แชทเปิดอยู่
// ข้อความแชท → HTML: fenced code (3 backticks) เป็น block ย่อเหลือบรรทัดแรก คลิกกาง/ย่อ · path ไฟล์ (มี / และนามสกุล, หรือ [x](path)) เป็นลิงก์เปิด modal ไฟล์ (เจ้าของ 2026-09-20)
function chatHTML(text){
  const FENCE = String.fromCharCode(96).repeat(3); // สาม backtick — เขียนแบบนี้เพราะหน้าอยู่ใน raw string ของ Go
  const parts = String(text).split(FENCE);
  return parts.map((seg, i) => {
    if (i % 2 === 1) { // code block
      const lines = seg.replace(/^[a-z0-9_-]*\n/i, '').replace(/\n$/, '');
      const first = lines.split('\n')[0];
      return '<details class="code"><summary><code>' + esc(first) + '</code><span class="more"> … ' + lines.split('\n').length + ' บรรทัด (คลิกกาง)</span></summary><pre>' + esc(lines) + '</pre></details>';
    }
    let h = esc(seg);
    h = h.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (m, t, p) => fileLink(p, t));
    h = h.replace(new RegExp('(^|[\\s("\'' + String.fromCharCode(96) + '])((?:[A-Za-z0-9_.-]+\\/)+[A-Za-z0-9_.-]+\\.[A-Za-z0-9]+)(?::L?(\\d+)(?:-L?\\d+)?)?', 'g'), (m, pre, p, ln) => pre + fileLink(p, p + (ln ? ':' + ln : ''), ln));
    return h.replace(/\n/g, '<br>');
  }).join('');
}
function fileLink(p, label, line){
  if (/^https?:/.test(p)) return '<a href="' + p + '" target="_blank">' + esc(label) + '</a>';
  if (/\.(html?|svg|png|jpe?g)$/i.test(p)) return '<a href="' + BASE + '/file?raw=1&p=' + encodeURIComponent(p) + '" target="_blank">' + esc(label) + ' ↗</a>'; // แสดงผลจริงในแท็บใหม่
  return '<a href="#" data-file="' + esc(p) + '"' + (line ? ' data-line="' + line + '"' : '') + '>' + esc(label) + '</a>';
}
async function openFile(p, line){
  let f = document.getElementById('fileModal'); if (!f) { f = document.createElement('div'); f.id = 'fileModal'; document.body.appendChild(f); }
  f.innerHTML = '<div class="cbox" style="width:min(1000px,96vw);height:min(85vh,900px)"><div class="chead"><b>' + esc(p) + '</b><span style="margin-left:auto"></span><button class="ghost" style="font-size:11px" id="fileCopy">copy path</button> <button class="ghost" style="font-size:11px" id="fileClose">✕</button></div><pre class="fbody" id="fbody">loading…</pre></div>';
  f.onclick = e => { if (e.target === f) f.remove(); };
  f.querySelector('#fileClose').onclick = () => f.remove();
  f.querySelector('#fileCopy').onclick = () => navigator.clipboard.writeText(p);
  const res = await fetch(BASE + '/file?p=' + encodeURIComponent(p));
  const body = f.querySelector('#fbody');
  if (!res.ok) { body.textContent = await res.text(); return; }
  const txt = await res.text(); const ls = txt.split('\n'); const target = line ? +line : 0;
  body.innerHTML = ls.map((l, i) => '<div class="fl' + (target && i + 1 === target ? ' hit' : '') + '" id="L' + (i+1) + '"><span class="ln">' + (i+1) + '</span>' + esc(l) + '</div>').join('');
  if (target) { const el = body.querySelector('#L' + target); if (el) el.scrollIntoView({block:'center'}); }
}
function chatPending(it){ const c = it.chat||[]; return c.length && c[c.length-1].by === 'owner' && !it.askDone; }
// แชตเปิดได้สองที่ (เจ้าของ 2026-09-21): แถวข้อมูล (id = c2, t1 …) และจุดของหัวข้อ (id = "<topic>:<key>") — modal เดียวกัน object เดียวกัน
function chatTarget(id){
  if (!id.includes(':')) { const it = (STATE.items||[]).find(i => i.id === id); return it ? {it, title: id + ' ' + it.label} : null; }
  const tid = id.slice(0, id.indexOf(':')), key = id.slice(id.indexOf(':') + 1);
  const tp = (STATE.topics||[]).find(x => x.id === tid); if (!tp) return null;
  const it = getPoint(tid, key); if (!it) return null;
  return {it, title: (tp.name.text || tid) + ' › ' + key.replace(/^sub:/, 'sub ').replace(/:/g, ' ') + ' — ' + (it.text || '').slice(0, 60)};
}
// ปุ่มเปิดแชต + สถานะ (รอตอบ / ปิดแล้ว / ⚠ ขาดบทสรุป / ★ บทสรุป) — ใช้ทั้งแถวและจุด
function chatBtn(it, id, word){
  const n = (it.chat||[]).length, pend = chatPending(it);
  const noSum = n && !it.summary; // มีบทสนทนาแต่ยังไม่มีบทสรุป (เจ้าของ 2026-09-20: ปิดเบราว์เซอร์ไปก็ต้องเห็นว่าค้าง)
  return '<button class="ghost" style="font-size:11px;padding:1px 6px' + (pend?';border-color:#54aeff;background:#ddf4ff':'') + '" data-chat="' + id + '">💬 ' + (n ? 'chat ' + (n > 20 ? '20+' : n) : word) + (pend ? ' · รอตอบ' : (it.askDone && n ? ' · ปิดแล้ว' : '')) + '</button>' + (noSum ? ' <span style="color:#cf222e;font-size:11px" title="ยังไม่ได้ mark ★ บทสรุป">⚠ ขาดบทสรุป</span>' : '') + (it.summary ? ' <span class="refs" title="บทสรุปของแชท">★ ' + esc(it.summary.text.split('\n')[0].slice(0,140)) + '</span>' : '');
}
function openChat(id){ chatOpen = id; renderChat(); }
function renderChat(){
  let m = document.getElementById('chatModal');
  if (!chatOpen) { if (m) m.remove(); return; }
  const tg = chatTarget(chatOpen); if (!tg) { chatOpen = null; return; }
  const it = tg.it;
  const draft = m ? (m.querySelector('textarea') || {}).value || '' : '';
  if (!m) { m = document.createElement('div'); m.id = 'chatModal'; document.body.appendChild(m); }
  // ฟองซ้าย = agent · ฟองขวา = เจ้าของ · ชื่อ + วันเวลา · ปุ่ม copy (เอาไปสร้าง main topic) และ reply (อ้างข้อความนี้ในคำตอบ)
  const msgs = (it.chat||[]).map((x, i) => '<div class="cm ' + x.by + '"><div class="who">' + (x.by === 'owner' ? 'คุณ' : 'agent') + ' · ' + esc(x.at.replace('T',' ').slice(0,16)) + '<span class="ctools"><button title="copy" data-ccopy="' + i + '">⧉</button><button title="reply" data-creply="' + i + '">↩</button><button title="mark เป็นบทสรุป" data-csum="' + i + '"' + (it.summary && it.summary.at === x.at && it.summary.text === x.text ? ' style="color:#d4a72c"' : '') + '>★</button></span></div><div class="txt">' + chatHTML(x.text) + '</div></div>').join('');
  const typing = agentTyping && chatPending(it) ? '<div class="cm agent"><div class="who">agent</div><div class="txt typing">กำลังพิมพ์<span>.</span><span>.</span><span>.</span></div></div>' : (chatPending(it) ? '<div class="refs">' + (agentLive ? 'agent ได้รับแล้ว รอคำตอบ' : 'รอ agent (ไม่มี agent รอ — submit แล้วจะได้รับผ่าน hook)') + '</div>' : '');
  m.innerHTML = '<div class="cbox"><div class="chead"><b>💬 ' + esc(tg.title) + '</b><span style="margin-left:auto"></span>' + ((it.chat||[]).length && !it.askDone ? '<button class="ghost" style="font-size:11px" id="chatDone"' + (it.summary ? '' : ' disabled title="mark ★ ข้อความที่เป็นบทสรุปก่อนปิด"') + '>ปิดแชท' + (it.summary ? '' : ' (ต้องมีบทสรุป ★)') + '</button> ' : '') + '<button class="ghost" style="font-size:11px" id="chatClose">✕</button><span id="sumWarn" style="display:none;color:#cf222e;font-size:11px;margin-left:8px">กด ★ ที่ข้อความที่เป็นบทสรุปก่อน ถึงจะออกได้</span></div><div class="cmsgs" id="cmsgs">' + (msgs || '<div class="refs">ยังไม่มีข้อความ — ถาม/สั่งแก้/อธิบายให้ agent เกี่ยวกับ' + (chatOpen.includes(':') ? 'จุดนี้' : 'แถวนี้') + '</div>') + typing + '</div><div class="cinput"><textarea id="chatText" placeholder="พิมพ์ถาม agent… (Enter = ส่ง, Shift+Enter = ขึ้นบรรทัด)">' + esc(draft) + '</textarea><button class="primary" id="chatSend">ส่ง</button></div></div>';
  const box = m.querySelector('#cmsgs'); box.scrollTop = box.scrollHeight;
  const canLeave = () => !((it.chat||[]).length) || !!it.summary; // ไม่มีบทสรุป = ออกไม่ได้ (เจ้าของ 2026-09-20)
  const leave = () => { if (!canLeave()) { const w = m.querySelector('#sumWarn'); if (w) { w.style.display = ''; setTimeout(() => w.style.display = 'none', 2500); } return; } chatOpen = null; renderChat(); };
  m.querySelector('#chatClose').onclick = leave;
  m.querySelectorAll('a[data-file]').forEach(a => a.onclick = e => { e.preventDefault(); openFile(a.dataset.file, a.dataset.line); });
  const done = m.querySelector('#chatDone'); if (done) done.onclick = () => { it.askDone = true; chatOpen = null; renderChat(); render(); save(); };
  const ta = m.querySelector('#chatText');
  m.querySelectorAll('button[data-ccopy]').forEach(b => b.onclick = () => { const t = it.chat[+b.dataset.ccopy].text; navigator.clipboard.writeText(t).then(() => { b.textContent = '✓'; setTimeout(() => b.textContent = '⧉', 1200); }); });
  m.querySelectorAll('button[data-csum]').forEach(b => b.onclick = () => { it.summary = it.chat[+b.dataset.csum]; it.summaryIdx = +b.dataset.csum; renderChat(); render(); save().then(() => { it.summaryIdx = undefined; }); });
  m.querySelectorAll('button[data-creply]').forEach(b => b.onclick = () => { const x = it.chat[+b.dataset.creply]; ta.value = '> ' + x.text.split('\n').join('\n> ') + '\n\n' + ta.value; ta.focus(); ta.selectionStart = ta.selectionEnd = ta.value.length; });
  const send = () => { const q = ta.value.trim(); if (!q) return; it.ask = q; ta.value = ''; it.chat = (it.chat||[]).concat([{at: new Date().toISOString(), by: 'owner', text: q}]); it.askDone = false; render(); renderChat(); save().then(() => { it.ask = ''; }); };
  m.querySelector('#chatSend').onclick = send;
  ta.onkeydown = e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(); } };
  m.onclick = e => { if (e.target === m) leave(); };
}
let ignOpen = false, ignSel = new Set();
// ส่วนข้อมูลประกอบย่อไว้เมื่อมีแถวหัวข้อแล้ว (เจ้าของ 2026-09-20: หน้าหลักโชว์รายการหัวข้อที่เป็นลิงก์ ส่วนบนเป็น accordion) · คลิกหัวข้อเพื่อกาง
let refOpen = !((STATE.topics||[]).some(x => x.status !== 'created'));
document.getElementById('refhead').onclick = () => { refOpen = !refOpen; syncRef(); };
function syncRef(){ const body = document.getElementById('refbody'); if (body) { body.style.display = refOpen ? '' : 'none'; document.getElementById('reftoggle').textContent = refOpen ? '▾' : '▸'; } }
function render(){
  const cur = TOPIC ? (STATE.topics||[]).find(x => x.id === TOPIC) : null;
  $('#title').innerHTML = TOPIC ? '<a href="' + BACK + '" style="color:#fff;opacity:.8">' + esc({{.Title}}) + '</a> › ' + esc(cur ? cur.name.text : TOPIC) : esc({{.Title}});
  $('#meta').textContent = 'round ' + STATE.round + ' · ' + STATE.status + ' · ' + STATE.file + ' · updated ' + STATE.updatedAt;
  $('#refsec').style.display = TOPIC ? 'none' : '';
  syncRef();
  if (TOPIC && !cur) { $('#topics').innerHTML = '<p class="refs">ไม่พบหัวข้อ ' + esc(TOPIC) + '</p>'; return; }
  // หัวข้อนี้จบแล้ว (agent เขียนลงโน้ตแล้ว) → บอกแล้วพากลับหน้าหลัก (เจ้าของ 2026-09-20)
  // พากลับเฉพาะเมื่อหัวข้อ "เพิ่งจบ" ระหว่างที่ดูอยู่ — เปิดหน้าหัวข้อที่ created ไปแล้ว (ลิงก์จากแถว t) ต้องอยู่ดูได้ (เจ้าของ 2026-09-21: โดนเด้งกลับหน้าหลัก)
  if (TOPIC && cur.status === 'created' && !redirected && !openedCreated) { redirected = true; $('#msg').className = 'msg ok'; $('#msg').innerHTML = '<span class="spin"></span> ✓ ' + esc(cur.note || 'เขียนลงโน้ตแล้ว') + ' — กลับหน้าหลักใน 3 วิ'; setTimeout(() => { location.href = BACK; }, 3000); }
  const groups = {};
  (STATE.items||[]).forEach(it => { if (!it.gone) (groups[it.kind] ||= []).push(it); }); // gone = หายจากรายงานแล้ว เปิดได้จากชิป ref ในหน้าหัวข้อ
  let h = '';
  for (const k of Object.keys(KINDS)) {
    if (!groups[k]) continue;
    const isT = k === 'topic';
    if (k === 'candidate' && STATE.sinceNote) h += '<p class="refs">' + esc(STATE.sinceNote) + '</p>';
    // คอลัมน์ตำแหน่งเดียวกันทุกตาราง (เจ้าของกด ignore พลาดเพราะช่องที่สองของตาราง T = target แต่ของ C/G = ignore) · ignore อยู่ขวาสุด
    h += '<table><tr><th style="width:44px">ref</th><th style="width:52px">target</th><th style="width:40px">id</th><th>' + KINDS[k] + '</th><th style="width:220px">used in</th><th>รายละเอียด</th><th style="width:56px">ignore</th></tr>';
    for (const it of groups[k]) {
      const cb = (f) => '<input type="' + (f==='target'?'radio':'checkbox') + '" name="' + (f==='target'?'target':'') + '" data-id="' + it.id + '" data-f="' + f + '"' + (it[f]?' checked':'') + '>';
      const used = usedIn(it.id);
      const label = it.link ? '<a href="' + esc(it.link) + '" title="เปิดหน้ารีวิวหัวข้อย่อยล่าสุดของหัวข้อนี้">' + esc(it.label) + '</a>' : esc(it.label);
      // แชทต่อแถว (เจ้าของ 2026-09-20): ปุ่ม chat (N) เปิด modal เหมือนหน้าแชท หน้าหลักโชว์แค่สถานะ
      const ask = '<div style="margin-top:4px">' + chatBtn(it, it.id, 'ask') + '</div>';
      h += '<tr><td>' + cb('ref') + '</td><td>' + (isT ? cb('target') : '') + '</td><td class="k">' + it.id + '</td><td' + (it.ignore?' style="text-decoration:line-through;color:#8c959f"':'') + '>' + label + '</td><td class="d">' + (used.length ? used.map(u => '<code>' + esc(u) + '</code>').join(' ') : (isT||it.ignore ? '' : '<span style="color:#d4a72c">ยังไม่ได้จัด</span>')) + '</td><td class="d">' + esc(it.detail) + ask + '</td><td style="text-align:center">' + (isT ? '' : cb('ignore')) + '</td></tr>';
    }
    h += '</table>';
  }
  if ((STATE.ignored||[]).length) { // accordion ย่อไว้ (เจ้าของ 2026-09-20: โตถึง 50–100 ได้) · 3 คอลัมน์ติ๊กเลือก · check all / uncheck all → un-ignore ที่ติ๊ก
    h += '<div style="margin-top:12px"><h2 style="margin:8px 0;cursor:pointer;user-select:none" id="ignhead"><span>' + (ignOpen?'▾':'▸') + '</span> ignored (' + STATE.ignored.length + ') <span class="count">ไม่ถูกเสนออีกจนกว่าจะเลิก ignore</span></h2>';
    h += '<div id="ignbody" style="display:' + (ignOpen?'':'none') + '"><div style="margin-bottom:6px"><button class="ghost" style="font-size:12px" data-igncheck="1">check all</button> <button class="ghost" style="font-size:12px" data-igncheck="0">uncheck all</button> <button class="ghost" style="font-size:12px" data-unignoresel="1">un-ignore ที่ติ๊ก</button></div>';
    h += '<div style="display:grid;grid-template-columns:repeat(3,1fr);gap:4px 12px;font-size:12px">' + STATE.ignored.map(x => '<label><input type="checkbox" data-ignsel="' + esc(x) + '"' + (ignSel.has(x)?' checked':'') + '> <code>' + esc(x) + '</code></label>').join('') + '</div></div></div>';
  }
  $('#items').innerHTML = h || '<p class="refs">ไม่มีข้อมูลประกอบในรีวิวนี้</p>';
  $('#items').querySelectorAll('button[data-chat]').forEach(b => b.onclick = () => openChat(b.dataset.chat));
  if (chatOpen) renderChat();
  const ignhead = document.getElementById('ignhead'); if (ignhead) ignhead.onclick = () => { ignOpen = !ignOpen; render(); };
  $('#items').querySelectorAll('input[data-ignsel]').forEach(cb => cb.onchange = () => { if (cb.checked) ignSel.add(cb.dataset.ignsel); else ignSel.delete(cb.dataset.ignsel); });
  $('#items').querySelectorAll('button[data-igncheck]').forEach(b => b.onclick = () => { ignSel = b.dataset.igncheck === '1' ? new Set(STATE.ignored) : new Set(); render(); });
  $('#items').querySelectorAll('button[data-unignoresel]').forEach(b => b.onclick = async () => {
    const labels = [...ignSel]; if (!labels.length) { $('#msg').className='msg err'; $('#msg').textContent = 'ติ๊กรายการที่จะเลิก ignore ก่อน'; return; }
    const res = await fetch(UNIGNORE, {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({labels})});
    if (res.ok) { location.reload(); }
    else { $('#msg').className='msg err'; $('#msg').textContent = 'เลิก ignore ไม่สำเร็จ: ' + (await res.text()); }
  });
  $('#items').querySelectorAll('input[data-f]').forEach(cb => {
    if (cb.dataset.f === 'target') cb.onclick = () => { // radio: คลิกอันที่เลือกอยู่ = ยกเลิก target
      const it = STATE.items.find(i => i.id === cb.dataset.id); const was = it.target;
      STATE.items.forEach(i => i.target = false); it.target = !was; render(); save();
    };
    else cb.onchange = () => { const it = STATE.items.find(i => i.id === cb.dataset.id); it[cb.dataset.f] = cb.checked; render(); save(); };
  });
  countChecked();
  let t = '';
  // หน้าหลักโชว์เฉพาะแถวที่ยังทำอยู่ — หัวข้อที่เขียนลงโน้ตแล้ว (created) หายจากรายการ ดูได้จากลิงก์ที่แถว t (เจ้าของ 2026-09-20)
  const active = (STATE.topics||[]).filter(x => x.status !== 'created');
  for (const tp of (TOPIC ? [cur] : active)) {
    t += '<div class="card"><span class="st ' + tp.status + '">' + tp.status + '</span>';
    if (!TOPIC) { // หน้าหลัก: ชื่อเป็นลิงก์ไปหน้าหัวข้อ (ข้อเสนอล่าสุดอยู่ที่นั่น)
      const n = (tp.subs||[]).length;
      // สถานะรอ agent (เจ้าของ 2026-09-20): spinner หมุน = agent ได้รับและกำลังทำ · ไม่หมุน = ยังไม่มีใครรับ (มาถามในแชทได้)
      const waitTxt = tp.status === 'created' ? '· <span style="color:#116329">✓ ' + esc(tp.note || 'เขียนลงโน้ตแล้ว') + '</span>' : n ? '· ' + n + ' หัวข้อย่อย' + (tp.status === 'agreed' ? ' · <span style="color:#116329">agree ครบ</span>' + (agentTyping ? ' <span class="spin"></span> agent กำลังเขียนลงโน้ต…' : '') : '') : (agentTyping ? '· <span class="spin"></span> agent กำลังเสนอ…' : (agentLive ? '· agent ได้รับแล้ว รอเสนอ' : '· รอ agent เสนอ <span class="refs">(ยังไม่มี agent รับงาน)</span>'));
      t += '<div style="padding:6px 0"><a href="' + BASE + '/t/' + tp.id + '" style="font-size:15px;font-weight:600">' + esc(tp.name.text) + '</a> <span class="refs">' + waitTxt + (tp.desc && tp.desc.text ? ' · ' + esc(tp.desc.text.slice(0,120)) : '') + '</span></div>';
      const lbl = id => { const it = (STATE.items||[]).find(i => i.id === id); return it ? id + ' ' + it.label : id; };
      t += '<div class="refs">' + (tp.existing ? 'รวมเข้า <b>' + esc(tp.existing) + '</b> · ' : '') + 'ref: ' + (tp.refs||[]).map(refChip).join(' ') + ((tp.status === 'new' || tp.status === 'extend') ? ' <button class="ghost" style="font-size:11px" data-rm="' + tp.id + '">ลบแถว</button>' : '') + '</div>';
      if (tp.note) t += '<div class="refs">note: ' + esc(tp.note) + '</div>';
      t += '</div>';
      continue;
    }
    t += point(tp, 'name', 'ชื่อหัวข้อหลัก', tp.name, true);
    t += point(tp, 'desc', 'ความหมาย', tp.desc, tp.status !== 'new');
    const lbl = id => { const it = (STATE.items||[]).find(i => i.id === id); return it ? id + ' ' + it.label : id; };
    t += '<div class="refs">' + (tp.existing ? 'รวมเข้า <b>' + esc(tp.existing) + '</b> · ' : '') + 'ref: ' + (tp.refs||[]).map(refChip).join(' ') + ((tp.status === 'new' || tp.status === 'extend') ? ' <button class="ghost" style="font-size:11px" data-rm="' + tp.id + '">ลบแถว</button>' : '') + '</div>';
    if (tp.note) t += '<div class="refs">note: ' + esc(tp.note) + '</div>';
    for (const s of tp.subs||[]) {
      t += '<div style="margin-left:24px;border-left:3px solid #eef1f4;padding-left:12px">';
      t += point(tp, 'sub:' + s.id + ':name', 'หัวข้อย่อย', s.name, true);
      t += point(tp, 'sub:' + s.id + ':desc', 'ความหมาย', s.desc, true);
      if (s.refs && s.refs.length) t += '<div class="refs" style="padding:4px 0 8px 118px">ref: ' + s.refs.map(refChip).join(' ') + '</div>';
      t += '</div>';
    }
    t += '</div>';
  }
  $('#topics').innerHTML = t || '<p class="refs">ยังไม่มีแถวที่ทำอยู่ — ติ๊ก ref ด้านบนแล้วกด New topic / Update topic' + ((STATE.topics||[]).length ? ' · หัวข้อที่เสร็จแล้วดูได้จากลิงก์ที่แถว "หัวข้อที่มีอยู่แล้ว"' : '') + '</p>';
  $('#topics').querySelectorAll('button[data-act]').forEach(b => b.onclick = () => act(b.dataset.tp, b.dataset.pt, b.dataset.act));
  $('#topics').querySelectorAll('textarea[data-pt]').forEach(ta => ta.oninput = () => { getPoint(ta.dataset.tp, ta.dataset.pt).comment = ta.value; });
  $('#topics').querySelectorAll('button[data-rm]').forEach(b => b.onclick = () => { STATE.topics = STATE.topics.filter(x => x.id !== b.dataset.rm); render(); save(); });
  $('#topics').querySelectorAll('button[data-chat]').forEach(b => b.onclick = () => openChat(b.dataset.chat));
  const anyNew = (STATE.topics||[]).some(x => x.status === 'new' || x.status === 'extend');
  // ป้ายปุ่ม submit ตามสิ่งที่อยู่ในรายการจริง: สร้างใหม่ / อัปเดตหัวข้อเดิม / ทั้งคู่ / รอบรีวิวข้อเสนอ (เจ้าของ 2026-09-20: "ผมไม่ได้ create ผม update")
  const nNew = (STATE.topics||[]).filter(x => x.status === 'new').length, nUpd = (STATE.topics||[]).filter(x => x.status === 'extend').length, nAsk = (STATE.items||[]).filter(i => chatPending(i)).length;
  const parts = [nNew ? nNew + ' new topic' : '', nUpd ? nUpd + ' update topic' : '', nAsk ? nAsk + ' ask' : ''].filter(Boolean);
  $('#submit').textContent = parts.length ? 'Submit: ' + parts.join(' + ') + ' → agent' : 'Submit review → agent';
  const pendingAsk = (STATE.items||[]).some(i => chatPending(i));
  const noRows = !(STATE.topics||[]).length && !pendingAsk;
  const pending = !TOPIC && (STATE.items||[]).some(i => i.ref || i.target);
  if (TOPIC) $('#submit').textContent = 'Submit review → agent';
  $('#submit').disabled = STATE.status === 'submitted' || STATE.status === 'done' || noRows;
  if (STATE.status === 'done') { $('#msg').className = 'msg ok'; $('#msg').textContent = '✓ รีวิวนี้จบแล้ว — ทุกหัวข้อถูกเขียนลงโน้ตแล้ว · หัวข้อใหม่: blm update --html'; }
  else if (STATE.status === 'submitted') $('#msg').textContent = 'submit แล้ว — รอ agent อ่าน ' + STATE.file + ' และเสนอรอบถัดไป';
  else if (noRows) { $('#msg').className = 'msg'; $('#msg').textContent = 'submit ได้เมื่อมีแถวหัวข้อหรือคำถาม — ติ๊ก ref แล้วกด New topic / Update topic หรือกด ask ที่แถว'; }
  else if (pending) { $('#msg').className = 'msg'; $('#msg').textContent = 'มี ref/target ที่ติ๊กค้าง ยังไม่ได้กด New topic / Update topic'; }
}
function usedIn(id){
  const it = (STATE.items||[]).find(i => i.id === id);
  const local = (STATE.topics||[]).filter(tp => (tp.refs||[]).includes(id) || (tp.existing && it && it.kind === 'topic' && it.label === tp.existing)).map(tp => tp.existing ? '→ ' + tp.existing : tp.name.text);
  return local.concat((it && it.usedIn) || []);
}
function countChecked(){
  const its = STATE.items||[]; const ref = its.filter(i => i.ref).length, tg = its.find(i => i.target);
  const left = its.filter(i => i.kind !== 'topic' && !i.ignore && !usedIn(i.id).length).length;
  $('#checkedCount').textContent = 'ยังไม่ได้จัด ' + left + (ref||tg ? ' · ติ๊ก ref ' + ref + (tg ? ' → target ' + tg.label : '') : '');
  $('#checkedCount').style.color = left ? '#d4a72c' : '#116329';
  $('#updBtn').disabled = !tg;
  $('#updBtn').textContent = tg ? 'Update topic → ' + tg.label : 'Update topic (เลือก target ก่อน)';
  // มี target = โหมดรวมเข้าหัวข้อเดิม: ชื่อไม่ต้องใส่ New topic ปิด · ยกเลิกได้ด้วยการติ๊ก target ออก
  $('#newBtn').disabled = !!tg; $('#newName').disabled = !!tg;
  $('#newName').placeholder = tg ? 'รวมเข้า ' + tg.label + ' — ไม่ต้องใส่ชื่อ พิมพ์รายละเอียดในช่องล่าง (ติ๊ก target ออกเพื่อกลับไปสร้างใหม่)' : 'ชื่อหัวข้อหลักใหม่ (สำหรับ New topic)';
}
function getPoint(tpId, key){
  const tp = STATE.topics.find(x => x.id === tpId);
  if (key === 'name') return tp.name; if (key === 'desc') return tp.desc;
  const [, sid, f] = key.split(':'); const s = tp.subs.find(x => x.id === sid); return s[f];
}
// ชิป ref: แถวที่มีแชต (ask history) ต่อท้ายด้วยปุ่มเปิดแชต — ไม่มีแชตก็แค่ชื่อ (เจ้าของ 2026-09-20: แถว C ที่หายจากรายงานแล้วยังเปิดดูแชตได้จากตรงนี้)
function refChip(r){
  const it = (STATE.items||[]).find(i => i.id === r);
  const n = it ? (it.chat||[]).length : 0;
  return '<code>' + esc(it ? r + ' ' + it.label : r) + '</code>' + (n ? ' <button class="ghost" style="font-size:11px;padding:0 5px" data-chat="' + esc(r) + '" title="ประวัติแชตของแถวนี้">💬 ' + n + '</button>' : '');
}
function point(tp, key, label, p, reviewable){
  p = p || {text:''};
  const id = tp.id + ':' + key;
  let h = '<div class="pt"><div class="lbl">' + label + '</div><div><div class="txt' + (p.text?'':' empty') + '">' + (esc(p.text) || (agentTyping ? '<span class="spin"></span> agent กำลังเสนอ…' : '(รอ agent เสนอ)')) + '</div>';
  if (p.history && p.history.length) h += '<div class="hist">' + p.history.map(x => '<span>เดิม: ' + esc(x.text) + (x.comment ? ' — comment: ' + esc(x.comment) : '') + '</span>').join('') + '</div>';
  // comment = แชตของจุดนี้ (เจ้าของ 2026-09-21: เหมือน ask ของแถว agent ตอบ realtime) · ช่อง comment รุ่นก่อนเหลือแค่แสดง
  if (p.comment) h += '<div class="hist"><span>comment เดิม: ' + esc(p.comment) + '</span></div>';
  if (p.text || (p.chat||[]).length) h += '<div style="margin-top:4px">' + chatBtn(p, id, 'comment') + '</div>';
  h += '</div><div class="btns">';
  if (reviewable && p.text) for (const a of ['COMMENT','AGREE','DRAFT']) h += '<button class="' + (p.action===a?'on ':'') + a + '" data-tp="' + tp.id + '" data-pt="' + key + '" data-act="' + a + '">' + a.toLowerCase() + '</button>';
  return h + '</div></div>';
}
function act(tpId, key, a){
  const p = getPoint(tpId, key);
  if (a === 'COMMENT') { openChat(tpId + ':' + key); return; } // comment = เปิดแชตของจุดนี้
  p.action = p.action === a ? '' : a;
  render(); save();
}
function points(tp){ return [tp.name, tp.desc].concat((tp.subs||[]).flatMap(s => [s.name, s.desc])).filter(Boolean); }
function body(){
  const tps = TOPIC ? (STATE.topics||[]).filter(x => x.id === TOPIC) : (STATE.topics||[]);
  const b = {items: (STATE.items||[]).map(i => ({id:i.id, ref:!!i.ref, target:!!i.target, ignore:!!i.ignore, ask:i.ask||'', askDone:!!i.askDone, summaryIdx: (typeof i.summaryIdx === 'number') ? i.summaryIdx : undefined})), topics: JSON.parse(JSON.stringify(tps))};
  (STATE.items||[]).forEach(i => { i.ask = ''; }); tps.forEach(tp => points(tp).forEach(p => { p.ask = ''; p.summaryIdx = undefined; })); // ส่งครั้งเดียว
  return b;
}
async function save(){
  $('#msg').className = 'msg'; $('#msg').textContent = 'saving…';
  const snapshot = body();
  try {
    const res = await fetch(SAVE, {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify(snapshot)});
    const j = await res.json().catch(() => ({})); if (!res.ok) throw new Error(j.error || await res.text().catch(()=>res.statusText));
    $('#msg').className = 'msg ok'; $('#msg').textContent = 'saved ' + j.saved + ' → ' + j.file + (agentLive ? ' · agent ได้รับแล้ว' : ' (status ยังไม่เปลี่ยนจนกว่าจะ submit)');
    STATE.updatedAt = j.saved; if (pendingRefresh) refreshState();
  } catch (e) { $('#msg').className = 'msg err'; $('#msg').textContent = 'บันทึกไม่สำเร็จ: ' + e.message + ' — server ปิดอยู่? รัน: blm update --html --from ' + STATE.file; }
}
function addRow(update){
  const its = STATE.items||[]; const tg = update ? its.find(i => i.target) : null;
  if (update && !tg) return;
  const name = update ? tg.label : $('#newName').value.trim(); if (!name) { $('#newName').focus(); return; }
  const refs = its.filter(i => i.ref).map(i => i.id);
  if (!refs.length) { $('#msg').className = 'msg err'; $('#msg').textContent = 'ติ๊ก ref อย่างน้อยหนึ่งแถวก่อน (C ที่จะจัดเข้าหัวข้อนี้)'; return; }
  STATE.topics.push({id: 'n' + Math.random().toString(36).slice(2,6), refs, existing: update ? tg.label : '', note: $('#newNote').value.trim(), name:{text:name}, desc:{text:''}, subs:[], status: update ? 'extend' : 'new'});
  its.forEach(i => { i.ref = false; i.target = false; }); $('#newName').value = ''; $('#newNote').value = ''; refOpen = false; render(); save();
}
$('#newBtn').onclick = () => addRow(false);
$('#updBtn').onclick = () => addRow(true);
$('#submit').onclick = async () => {
  $('#msg').className = 'msg'; $('#msg').textContent = 'submitting…';
  try {
    const res = await fetch(SUBMIT, {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify(body())});
    if (!res.ok) throw new Error((await res.text()).trim() || res.statusText);
    const j = await res.json(); if (!j.ok) throw new Error(j.error || res.statusText);
    $('#msg').className = 'msg ok'; $('#msg').textContent = 'saved round ' + (j.round-1) + ' → ' + j.file + ' · ' + j.next; $('#submit').disabled = true; STATE.status = 'submitted';
  } catch (e) { $('#msg').className = 'msg err'; $('#msg').textContent = 'submit ไม่สำเร็จ: ' + e.message + ' — server ปิดอยู่? รัน: blm update --html --from ' + STATE.file; }
};
render();
</script></body></html>`

// ---- entry points ที่ CLI/MCP ใช้ร่วม ----

// ReviewOpenResult ผลของการเปิดหน้ารีวิว
type ReviewOpenResult struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	File     string `json:"file"`
	HTML     string `json:"html"`
	Round    int    `json:"round"`
	Status   string `json:"status"`
	Next     string `json:"next"`
	Terminal string `json:"terminal"`
}

func openResult(r *Review, next string) *ReviewOpenResult {
	res := &ReviewOpenResult{ID: r.ID, URL: r.URL, File: r.File, HTML: r.HTML, Round: r.Round, Status: r.Status, Next: next}
	stable := ""
	if i := strings.Index(r.URL, "/r/"); i > 0 && r.Kind == "update" {
		stable = r.URL[:i] + "/r/update"
	}
	res.Terminal = Title("blm review") + "\n" + KV([][2]string{{"URL", res.URL + Dim("   (stable: "+stable+")")}, {"JSON", res.File}, {"HTML", res.HTML + Dim("   (stable: reviews/update.html)")}, {"Round", fmt.Sprint(res.Round)}, {"Status", res.Status}}) + "\n" + Cyan("next: ") + next + "\n"
	return res
}

// OpenUpdateReview — `blm update --html` / blm_update {html:true}: ใช้รีวิวเดิมที่ยังไม่ done (เจ้าของ 2026-09-20: "รอบต่อไป วันอื่น ๆ ไม่ต้องสร้างไฟล์ใหม่")
// → ดึงข้อมูลประกอบใหม่ทับ (ติ๊ก/หัวข้อ/ข้อเสนอเดิมคง) · ไม่มี = สร้างรอบ 1 ใหม่
func (s *Store) OpenUpdateReview(rep *UpdateReport) (*ReviewOpenResult, error) {
	all := s.ListReviews() // ใหม่สุดก่อน
	for i, old := range all {
		if old.Kind != "update" || old.Status == "done" || old.Query != rep.Topic {
			continue
		}
		// รีวิวเปล่าที่เก่ากว่า (เปิดแล้วไม่ได้ทำอะไร — เหลือค้างจากรอบทดสอบ) ลบทิ้ง ไม่งั้นกองเป็นสิบไฟล์
		for _, o := range all[i+1:] {
			if o.Kind == "update" && o.Status == "open" && len(o.Topics) == 0 {
				_ = os.Remove(filepath.Join(s.Root, o.File))
				_ = os.Remove(filepath.Join(s.Root, o.HTML))
			}
		}
		s.RefreshItemsFrom(old, rep)
		r, err := s.OpenReview(old)
		if err != nil {
			return nil, err
		}
		liveBroadcast(r.ID, r.UpdatedAt, "reload") // agent สั่ง regenerate (blm_update {html:true}) → หน้าที่เปิดอยู่โหลดใหม่
		return openResult(r, "continuing review "+r.ID+" (items refreshed, your rows kept) · owner: open the URL · agent: blm_update {from:\""+r.File+"\"} after the submit"), nil
	}
	r, err := s.OpenReview(NewReviewFromUpdate(rep))
	if err != nil {
		return nil, err
	}
	return openResult(r, "owner: open the URL, tick refs, New topic (many), then 'blm topic create' · agent: wait for the submit, then blm_update {from:\""+r.File+"\"}"), nil
}

// ReopenReview — `blm update --html --from <json>`: ไฟล์เดิม → gen HTML ใหม่ + server (ไม่แตะสถานะ)
func (s *Store) ReopenReview(ref string) (*ReviewOpenResult, error) {
	r, err := s.LoadReview(ref)
	if err != nil {
		return nil, err
	}
	if r, err = s.OpenReview(r); err != nil {
		return nil, err
	}
	return openResult(r, "owner: continue at the URL · agent: blm_update {from} when it says submitted"), nil
}

// ReviewRoundResult — สิ่งที่ agent ได้จาก blm_update {from[, proposal]}
type ReviewRoundResult struct {
	Review *Review  `json:"review"`
	Next   string   `json:"next"`
	Todo   []string `json:"todo,omitempty"` // จุดที่เจ้าของ COMMENT / หัวข้อ new ที่รอเสนอ
}

// ReviewRound — agent อ่านไฟล์ (mark read) และถ้ามี proposal ก็เขียนลงไฟล์เดิม + gen HTML
func (s *Store) ReviewRound(ref string, proposal any) (*ReviewRoundResult, error) {
	r, err := s.LoadReview(ref)
	if err != nil {
		return nil, err
	}
	if proposal != nil {
		raw, _ := json.Marshal(proposal)
		var p Proposal
		if err := json.Unmarshal(raw, &p); err != nil || (len(p.Topics) == 0 && len(p.Answers) == 0) {
			return nil, fmt.Errorf("proposal must be {topics:[{id?, name, desc, subs:[{id?, name, desc, refs}]}], answers:[{id, answer}]}")
		}
		if err := ApplyProposal(r, p); err != nil {
			return nil, err
		}
		setTyping(r.ID, false) // ตอบแล้ว
	}
	s.MarkRead(r)
	reopened := false
	hasAsk := false
	for _, it := range r.Items {
		if len(it.Chat) > 0 {
			hasAsk = true
		}
	}
	if r.Status == "submitted" && len(r.Topics) == 0 && !hasAsk { // ไฟล์จากรุ่นก่อนที่ยอมให้ submit เปล่า → เปิดหน้าให้เจ้าของทำต่อ
		r.Status, r.Round, reopened = "open", max(1, r.Round-1), true
	}
	if _, err := s.OpenReview(r); err != nil { // มี server เสมอเมื่อ agent เขียน เพื่อให้ URL ในไฟล์ใช้ได้
		return nil, err
	}
	res := &ReviewRoundResult{Review: r}
	if reopened {
		res.Next = "the owner submitted without any topic row (ref/target ticks are kept) — page reopened at " + r.URL + ": ask them to press New topic / Update topic, then submit again"
		return res, nil
	}
	for _, it := range r.Items {
		if it.chatPending() {
			res.Todo = append(res.Todo, fmt.Sprintf("chat on %s %q — reply with blm_update {from, proposal:{answers:[{id:%q, answer}]}} (read the files/rules behind that row first)\n%s", it.ID, it.Label, it.ID, chatThread(it.Chat, it.Summary)))
		}
	}
	allAgreed := len(r.Topics) > 0
	for _, t := range r.Topics {
		switch t.Status {
		case "new":
			res.Todo = append(res.Todo, fmt.Sprintf("topic %s %q: propose desc + subtopics from refs %v · related topics %v (read their notes first; say which one each subtopic touches) · owner note: %q", t.ID, t.Name.Text, t.Refs, t.Related, t.Note))
			allAgreed = false
		case "extend":
			res.Todo = append(res.Todo, fmt.Sprintf("topic %s extends existing %q: read its note (blm {query}) then propose ONLY the subtopics to add from refs %v · related %v · owner note: %q · on agreement blm_append into that note, do not create a new one", t.ID, t.Existing, t.Refs, t.Related, t.Note))
			allAgreed = false
		case "agreed", "created":
		default:
			allAgreed = false
			pts := map[string]ReviewPoint{"name": t.Name, "desc": t.Desc}
			for _, sd := range t.Subs {
				pts["sub "+sd.ID+" name"], pts["sub "+sd.ID+" desc"] = sd.Name, sd.Desc
			}
			keys := make([]string, 0, len(pts))
			for k := range pts {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				p := pts[k]
				if p.chatPending() {
					id := t.ID + ":" + strings.NewReplacer("sub ", "sub:", " name", ":name", " desc", ":desc").Replace(k)
					res.Todo = append(res.Todo, fmt.Sprintf("comment on topic %s · %s %q — reply in that chat with blm_update {from, proposal:{answers:[{id:%q, answer}]}} and, when the text should change, send the revised topic in the same proposal\n%s", t.ID, k, p.Text, id, chatThread(p.Chat, p.Summary)))
				} else if p.Action == ActionComment && p.Comment != "" && len(p.Chat) == 0 {
					res.Todo = append(res.Todo, fmt.Sprintf("topic %s · %s: owner says %q — revise", t.ID, k, p.Comment))
				}
			}
		}
	}
	switch {
	case proposal != nil:
		res.Next = "saved (round " + fmt.Sprint(r.Round) + ") — the page updated itself; keep listening: blm update --wait --from update (background) or blm_update {from, wait:true}"
	case allAgreed:
		res.Next = "every point is AGREE — create the topic notes: blm_create blm-<slug> (folder = topicFolder) + a row in the Main Business table, then set status created via proposal note"
	case len(res.Todo) == 0 && r.Status == "submitted":
		res.Next = "submitted with no comment and no new topic — points still open are DRAFT: ask the owner or leave them for blm update draft"
	default:
		res.Next = "answer the todo list with blm_update {from, proposal}"
	}
	return res, nil
}

// DraftsResult — `blm update draft [topic]`
type DraftsResult struct {
	Drafts   []DraftPoint `json:"drafts"`
	Terminal string       `json:"terminal"`
}

func (s *Store) DraftsResult(topic string) (*DraftsResult, error) {
	d := s.Drafts(topic)
	var b strings.Builder
	b.WriteString(Title(fmt.Sprintf("blm update draft — %d point(s) still open", len(d))) + "\n")
	var rows [][]string
	for _, p := range d {
		rows = append(rows, []string{p.Topic, p.Where, PadEnd(strings.SplitN(p.Text, "\n", 2)[0], 60), p.Note, p.Review})
	}
	if len(rows) > 0 {
		b.WriteString(Table([]string{"topic", "where", "text", "note", "review"}, rows) + "\n")
		b.WriteString(Cyan("next: ") + "blm update --html --from <review> to continue in the browser · /blm_update \"<topic>\" for the agent\n")
	} else {
		b.WriteString(Dim("nothing drafted") + "\n")
	}
	return &DraftsResult{Drafts: d, Terminal: b.String()}, nil
}

// ---- live mode (เจ้าของ 2026-09-20: โต้ตอบเหมือนแชท) ----
//
// ฝั่ง agent: blm_update {from, wait:true} บล็อกรอจนมี "งานใหม่" ในรีวิว (ask/comment/agree/แถวใหม่/submit) หรือเจ้าของกด "จบ live" แล้วคืน
// ReviewRoundResult พร้อม todo — agent ตอบ (proposal/answers) แล้วเรียก wait อีก วนอยู่ในเทิร์นเดียว · timeout คืนเปล่าให้เรียกซ้ำ
// ฝั่งหน้า: SSE /r/<id>/events ส่ง updatedAt ทุกครั้งที่ไฟล์เปลี่ยน + สถานะว่ามี agent รออยู่ไหม → หน้าโหลด /r/<id>/state แล้ววาดใหม่
// ทุกการเขียนไฟล์รีวิว (SaveReview) = broadcast หนึ่งครั้ง ไม่มี state แยก

type reviewEvent struct {
	ID        string `json:"id"`
	UpdatedAt string `json:"updatedAt"`
	Kind      string `json:"kind"`              // update · stop · presence · hello
	Live      bool   `json:"live"`              // มี agent รออยู่
	Typing    bool   `json:"typing"`            // agent รับงานไปแล้วกำลังตอบ (เจ้าของ 2026-09-20: ไอคอนกำลังพิมพ์ จนกว่าคำตอบจะมา)
	Version   string `json:"version,omitempty"` // build ของ server — หน้าเทียบกับของตัวเอง ต่างกัน = reload (หลัง reconnect ได้หน้าใหม่เอง)
}

var (
	liveMu     sync.Mutex
	liveSubs   = map[string][]chan reviewEvent{} // review id → subscribers (SSE + wait)
	liveWait   = map[string]int{}                // review id → จำนวน agent ที่กำลัง wait
	liveTyping = map[string]bool{}               // review id → agent ได้งานไปแล้ว ยังไม่ตอบ
)

func setTyping(id string, on bool) {
	liveMu.Lock()
	liveTyping[id] = on
	liveMu.Unlock()
}

func liveSubscribe(id string) (chan reviewEvent, func()) {
	ch := make(chan reviewEvent, 8)
	liveMu.Lock()
	liveSubs[id] = append(liveSubs[id], ch)
	liveMu.Unlock()
	return ch, func() {
		liveMu.Lock()
		subs := liveSubs[id]
		for i, c := range subs {
			if c == ch {
				liveSubs[id] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		liveMu.Unlock()
	}
}

func liveBroadcast(id, updatedAt, kind string) {
	liveMu.Lock()
	ev := reviewEvent{ID: id, UpdatedAt: updatedAt, Kind: kind, Live: liveWait[id] > 0, Typing: liveTyping[id]}
	subs := append([]chan reviewEvent{}, liveSubs[id]...)
	liveMu.Unlock()
	for _, c := range subs {
		select {
		case c <- ev:
		default:
		}
	}
}

func liveWaiting(id string) bool {
	liveMu.Lock()
	defer liveMu.Unlock()
	return liveWait[id] > 0
}

func liveIsTyping(id string) bool {
	liveMu.Lock()
	defer liveMu.Unlock()
	return liveTyping[id]
}

// reviewTodo — งานที่รอ agent ในรีวิวนี้ (ใช้ตัดสินว่า wait ควรตื่นไหม): ask ที่ยังไม่ตอบ, จุด COMMENT, หัวข้อ new/extend, หรือ submit แล้ว
func reviewTodo(r *Review) []string {
	var todo []string
	for _, it := range r.Items {
		if it.chatPending() {
			todo = append(todo, fmt.Sprintf("chat %s#%d", it.ID, len(it.Chat)))
		}
	}
	for _, t := range r.Topics {
		if t.Status == "new" || t.Status == "extend" {
			todo = append(todo, "topic "+t.ID)
			continue
		}
		pts := []ReviewPoint{t.Name, t.Desc}
		for _, sd := range t.Subs {
			pts = append(pts, sd.Name, sd.Desc)
		}
		for _, p := range pts {
			if p.chatPending() || (p.Action == ActionComment && p.Comment != "" && len(p.Chat) == 0) {
				todo = append(todo, fmt.Sprintf("comment %s#%d", t.ID, len(p.Chat)))
			}
		}
	}
	if r.Status == "submitted" {
		todo = append(todo, "submitted")
	}
	return todo
}

// WaitReviewResult — ผลของ blm_update {wait:true}
type WaitReviewResult struct {
	*ReviewRoundResult
	Stopped bool   `json:"stopped"` // เจ้าของกด "จบ live"
	Timeout bool   `json:"timeout"` // ไม่มีอะไรเกิดขึ้นใน timeout — เรียกซ้ำได้
	Waited  string `json:"waited"`
}

// WaitReview — บล็อกจนกว่า todo ของรีวิวจะเปลี่ยน (มีงานใหม่) หรือ stop หรือ timeout · ตอนเริ่มถ้ามี todo ค้างอยู่แล้วคืนทันที
func (s *Store) WaitReview(ref string, timeout time.Duration) (*WaitReviewResult, error) {
	r, err := s.LoadReview(ref)
	if err != nil {
		return nil, err
	}
	if _, err := s.StartReviewServer(); err != nil {
		return nil, err
	}
	start := time.Now()
	baseline := strings.Join(reviewTodo(r), ",")
	if baseline != "" && !strings.HasSuffix(r.ReadAt, r.UpdatedAt) { // งานค้างที่ยังไม่ได้อ่าน → ไม่ต้องรอ
		rr, err := s.ReviewRound(ref, nil)
		if err != nil {
			return nil, err
		}
		setTyping(r.ID, true)
		liveBroadcast(r.ID, "", "presence")
		return &WaitReviewResult{ReviewRoundResult: rr, Waited: "0s"}, nil
	}
	ch, unsub := liveSubscribe(r.ID)
	defer unsub()
	liveMu.Lock()
	liveWait[r.ID]++
	liveMu.Unlock()
	defer func() {
		liveMu.Lock()
		liveWait[r.ID]--
		liveMu.Unlock()
		liveBroadcast(r.ID, "", "presence")
	}()
	liveBroadcast(r.ID, r.UpdatedAt, "presence") // บอกหน้าว่า agent มาแล้ว
	t := time.After(timeout)
	for {
		select {
		case ev := <-ch:
			if ev.Kind == "presence" {
				continue
			}
			cur, err := s.LoadReview(r.ID)
			if err != nil {
				return nil, err
			}
			if ev.Kind == "stop" {
				rr, _ := s.ReviewRound(r.ID, nil)
				return &WaitReviewResult{ReviewRoundResult: rr, Stopped: true, Waited: time.Since(start).Round(time.Second).String()}, nil
			}
			if now := strings.Join(reviewTodo(cur), ","); now != "" && now != baseline {
				rr, err := s.ReviewRound(r.ID, nil)
				if err != nil {
					return nil, err
				}
				setTyping(r.ID, true) // agent ได้งานแล้ว — หน้าโชว์ "กำลังพิมพ์" จนกว่าจะมี proposal/answers เขียนกลับ
				liveBroadcast(r.ID, "", "presence")
				return &WaitReviewResult{ReviewRoundResult: rr, Waited: time.Since(start).Round(time.Second).String()}, nil
			}
		case <-t:
			rr := &ReviewRoundResult{Review: r, Next: "nothing new — call blm_update {from, wait:true} again, or stop when the owner says so"}
			return &WaitReviewResult{ReviewRoundResult: rr, Timeout: true, Waited: timeout.String()}, nil
		}
	}
}

// WaitViaServer — CLI: ถ้ามี server ของ MCP รันอยู่ (URL ในไฟล์รีวิว) ต่อ long-poll ที่นั่นเพื่อแชร์ event เดียวกับ agent ·
// ไม่มี = รอในโปรเซสนี้เอง (เปิด server ของตัวเอง) · ใช้กับ `blm update --wait` ที่ Claude Code รันเป็น background แล้วเรียก agent กลับเมื่อจบ
func (s *Store) WaitViaServer(ref string, timeout time.Duration) (*WaitReviewResult, error) {
	r, err := s.LoadReview(ref)
	if err != nil {
		return nil, err
	}
	if r.URL != "" {
		cl := &http.Client{Timeout: timeout + 15*time.Second}
		resp, err := cl.Get(fmt.Sprintf("%s/wait?timeout=%d", r.URL, int(timeout.Seconds())))
		if err == nil {
			defer resp.Body.Close()
			var out WaitReviewResult
			if resp.StatusCode == 200 && json.NewDecoder(resp.Body).Decode(&out) == nil {
				return &out, nil
			}
		}
	}
	// server ของ MCP หายไป (reconnect/ปิด) — ห้ามเปิด server เองในโปรเซส CLI: มันจะยึด port ประจำไว้ แล้ว MCP ตัวใหม่ต้องหลบไป port สุ่ม
	// (เกิดจริง 2026-09-21: หน้าเปิดที่ :55820 แทน :41327) → คืน timeout พร้อมบอกให้เปิดใหม่ผ่าน MCP
	return &WaitReviewResult{ReviewRoundResult: &ReviewRoundResult{Review: r, Next: "no review server at " + r.URL + " (blm mcp restarted?) — run blm_update {html:true, open:true} from the agent, then start this waiter again"}, Timeout: true, Waited: "0s"}, nil
}

// RenderWait — ข้อความสำหรับ terminal/agent เมื่อ wait คืน
func RenderWait(w *WaitReviewResult) string {
	var b strings.Builder
	switch {
	case w.Stopped:
		b.WriteString(Yellow("live stopped by the owner") + "\n")
	case w.Timeout:
		b.WriteString(Dim("nothing new in "+w.Waited+" — run blm update --wait again to keep listening") + "\n")
	default:
		b.WriteString(Green("review activity") + " after " + w.Waited + "\n")
	}
	if w.ReviewRoundResult != nil {
		if w.Review != nil {
			b.WriteString("file: " + w.Review.File + " · round " + fmt.Sprint(w.Review.Round) + " · " + w.Review.Status + "\n")
		}
		for _, t := range w.Todo {
			b.WriteString("- " + t + "\n")
		}
		if w.Next != "" {
			b.WriteString(Cyan("next: ") + w.Next + "\n")
		}
	}
	b.WriteString(Dim("— by blm update --wait · related: blm_update, blm_review · next: blm_update {from:\"update\"} → do the todo → run blm update --wait --from update again in the background") + "\n")
	return b.String()
}

// buildID — Version + mtime ของไบนารี: dev build ทุกตัวมี Version เดียวกัน (…-dirty) จึงต้องใช้เวลาไฟล์แยกว่า build ใหม่หรือยัง
func buildID() string {
	exe, err := os.Executable()
	if err != nil {
		return Version
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	fi, err := os.Stat(exe)
	if err != nil {
		return Version
	}
	return Version + "@" + fi.ModTime().UTC().Format("20060102T150405")
}
