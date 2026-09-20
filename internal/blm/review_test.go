package blm

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReviewLoopOwnerAndAgent(t *testing.T) {
	root := t.TempDir()
	c := Config{Backend: BackendNone, Store: ".claude/blm"}
	_ = os.MkdirAll(filepath.Join(root, c.Store), 0o755)
	s := Open(root, c)
	rep := &UpdateReport{Topic: "line webhook", Candidates: []UpdateCandidate{{Dir: "src/lib/line", Files: 3, Symbols: 9}}, Changed: []UpdateChanged{{Dir: "src/app", Files: []string{"a.ts"}}}}
	open, err := s.OpenUpdateReview(rep)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(open.URL, "http://127.0.0.1:") || !exists(filepath.Join(root, open.File)) || !exists(filepath.Join(root, open.HTML)) {
		t.Fatalf("open: %+v", open)
	}
	post := func(path string, body any) (int, map[string]any) {
		raw, _ := json.Marshal(body)
		res, err := http.Post(open.URL+path, "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var m map[string]any
		_ = json.NewDecoder(res.Body).Decode(&m)
		return res.StatusCode, m
	}
	// เจ้าของติ๊ก + New topic แล้ว "save" (ทุกคลิกลงไฟล์ สถานะไม่เปลี่ยน)
	// ตารางเดียว: c1 = src/app (มี commit ใหม่ ขึ้นก่อน), c2 = src/lib/line
	code, _ := post("/save", Submission{Items: []ReviewItem{{ID: "c2", Ref: true}, {ID: "c1", Ignore: true}}, Topics: []ReviewTopic{{ID: "n1", Refs: []string{"c2"}, Note: "webhook ต้องตรวจ signature", Name: ReviewPoint{Text: "LINE Webhook"}, Status: "new"}}})
	r, _ := s.LoadReview(open.ID)
	if code != 200 || r.Status != "open" || r.Round != 1 || len(r.Topics) != 1 || !r.Items[1].Ref || !r.Items[0].Ignore || s.Ignored()["src/app"] || r.Topics[0].Note == "" || len(s.PendingReviews()) != 0 { // ignore ยังไม่มีผลก่อน submit
		t.Fatalf("save must persist without changing status: %d %+v", code, r)
	}
	// submit รวม → submitted, hook เห็น
	code, _ = post("/submit", Submission{Items: []ReviewItem{{ID: "c2", Ref: true}, {ID: "c1", Ignore: true}}, Topics: r.Topics})
	r, _ = s.LoadReview(open.ID)
	if code != 200 || r.Status != "submitted" || r.Round != 2 || len(s.PendingReviews()) != 1 || !s.Ignored()["src/app"] {
		t.Fatalf("submit: %d %+v", code, r)
	}
	if err := s.Unignore("src/app"); err != nil || s.Ignored()["src/app"] {
		t.Fatalf("unignore: %v %v", err, s.Ignored())
	}
	if code, _ = post("/save", Submission{}); code != 409 {
		t.Fatalf("save after submit must be refused, got %d", code)
	}
	var buf bytes.Buffer
	_ = Hook("prompt", HookInput{SessionID: "x", Prompt: "hi"}, root, &buf)
	if !strings.Contains(buf.String(), "review submitted by the owner") {
		t.Fatalf("hook must announce the submit: %s", buf.String())
	}
	// agent อ่าน + เสนอ
	if rr0, _ := s.ReviewRound(open.File, nil); len(rr0.Todo) != 1 || !strings.Contains(rr0.Todo[0], "signature") {
		t.Fatalf("todo must carry the owner's note: %+v", rr0.Todo)
	}
	rr, err := s.ReviewRound(open.File, map[string]any{"topics": []map[string]any{{"id": "n1", "name": "LINE Webhook", "desc": "รับ event จาก LINE", "subs": []map[string]any{{"name": "join", "desc": "เมื่อบอทถูกเชิญ"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	r, _ = s.LoadReview(open.ID)
	if r.Status != "open" || r.ReadAt == "" || len(s.PendingReviews()) != 0 || r.Topics[0].Status != "proposed" || len(r.Topics[0].Subs) != 1 || !strings.Contains(rr.Next, "saved (round") {
		t.Fatalf("proposal: %+v %+v", r.Topics[0], rr)
	}
	// เจ้าของ: agree ชื่อ+ความหมาย, comment sub name, draft sub desc → submit
	tp := r.Topics[0]
	tp.Name.Action, tp.Desc.Action = "AGREE", "AGREE"
	tp.Subs[0].Name.Comment = "เรียก member-joined ดีกว่า"
	tp.Subs[0].Desc.Action = "DRAFT"
	post("/submit", Submission{Topics: []ReviewTopic{tp}})
	rr, _ = s.ReviewRound(open.File, nil)
	if len(rr.Todo) != 1 || !strings.Contains(rr.Todo[0], "member-joined") || rr.Review.Topics[0].Status != "proposed" {
		t.Fatalf("todo: %+v status=%s", rr.Todo, rr.Review.Topics[0].Status)
	}
	if d := s.Drafts("webhook"); len(d) != 1 || d[0].Where != "sub join · desc" {
		t.Fatalf("drafts: %+v", d)
	}
	// agent แก้ sub name (ของเดิมลง history) และคงชื่อ/ความหมายเดิม → AGREE อยู่ต่อ
	sub := rr.Review.Topics[0].Subs[0]
	rr, _ = s.ReviewRound(open.File, map[string]any{"topics": []map[string]any{{"id": "n1", "name": "LINE Webhook", "desc": "รับ event จาก LINE", "subs": []map[string]any{{"id": sub.ID, "name": "member-joined", "desc": sub.Desc.Text}}}}})
	tp = rr.Review.Topics[0]
	if tp.Name.Action != "AGREE" || tp.Subs[0].Name.Action != "" || len(tp.Subs[0].Name.History) != 1 || tp.Subs[0].Name.History[0].Comment == "" || tp.Subs[0].Desc.Action != "DRAFT" {
		t.Fatalf("revise: %+v", tp)
	}
	// ครบ AGREE
	tp.Subs[0].Name.Action, tp.Subs[0].Desc.Action = "AGREE", "AGREE"
	post("/submit", Submission{Topics: []ReviewTopic{tp}})
	rr, _ = s.ReviewRound(open.File, nil)
	if rr.Review.Topics[0].Status != "agreed" || !strings.Contains(rr.Next, "every point is AGREE") {
		t.Fatalf("agreed: %s %s", rr.Review.Topics[0].Status, rr.Next)
	}
	html, _ := os.ReadFile(filepath.Join(root, open.HTML))
	if !strings.Contains(string(html), "member-joined") || !strings.Contains(string(html), "/save") {
		t.Fatal("html not regenerated from the json")
	}
}

func TestReviewExtendExistingTopicAndIgnore(t *testing.T) {
	root := t.TempDir()
	c := Config{Backend: BackendNone, Store: ".claude/blm"}
	_ = os.MkdirAll(filepath.Join(root, c.Store), 0o755)
	s := Open(root, c)
	rep := &UpdateReport{Topics: []UpdateTopic{{Topic: "Custom VPN", Note: "blm-custom-vpn"}}, Candidates: []UpdateCandidate{{Dir: "wireguard/tools"}, {Dir: "postcss.config.mjs"}}}
	r := NewReviewFromUpdate(rep)
	ApplyOwnerState(r, Submission{Items: []ReviewItem{{ID: "t1", Target: true, Ref: true}, {ID: "c1", Ref: true}, {ID: "c2", Ignore: true}}, Topics: []ReviewTopic{{ID: "n2", Existing: "Custom VPN", Refs: []string{"c1", "t1"}}}})
	r.Status = "submitted" // ignore มีผลเมื่อ submit
	if err := s.SaveReview(r); err != nil {
		t.Fatal(err)
	}
	if r.Topics[0].Status != "extend" || r.Topics[0].Name.Text != "Custom VPN" || strings.Join(r.Topics[0].Related, ",") != "t1" || !s.Ignored()["postcss.config.mjs"] {
		t.Fatalf("extend/ignore: %+v %v", r.Topics[0], s.Ignored())
	}
	rr, _ := s.ReviewRound(reviewFile(r), nil)
	if len(rr.Todo) != 1 || !strings.Contains(rr.Todo[0], "extends existing \"Custom VPN\"") {
		t.Fatalf("todo: %+v", rr.Todo)
	}
	// blm update รอบหน้าไม่เสนอ postcss อีก (ต้องมี blm.md + graph)
	_ = os.WriteFile(filepath.Join(root, c.Store, "blm.md"), []byte("---\ntarget: blm\n---\n\n# X\n\n## Y\n- r\nmemory: -\ncode: src/x.ts\n"), 0o644)
	raw, _ := json.Marshal(Graph{Nodes: []GraphNode{{Path: "src/x.ts"}, {Path: "postcss.config.mjs"}, {Path: "wireguard/tools/a.sh"}}, Clusters: []GraphCluster{{Dir: "src"}, {Dir: "postcss.config.mjs", Files: 1}, {Dir: "wireguard/tools", Files: 1}}})
	_ = os.WriteFile(filepath.Join(root, c.Store, "graph.json"), raw, 0o644)
	up, err := s.UpdateReport("", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(up.Candidates) != 1 || up.Candidates[0].Dir != "wireguard/tools" {
		t.Fatalf("ignored dir must be dropped: %+v", up.Candidates)
	}
}

func reviewFile(r *Review) string { return r.File }

func TestOpenUpdateReviewReusesOpenReview(t *testing.T) {
	root := t.TempDir()
	c := Config{Backend: BackendNone, Store: ".claude/blm"}
	_ = os.MkdirAll(filepath.Join(root, c.Store), 0o755)
	_ = os.WriteFile(filepath.Join(root, c.Store, "blm.md"), []byte("---\ntarget: blm\n---\n\n# X\n\n## Y\n- r\nmemory: -\ncode: src/x.ts\n"), 0o644)
	s := Open(root, c)
	first, err := s.OpenUpdateReview(&UpdateReport{Candidates: []UpdateCandidate{{Dir: "a"}}})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := s.LoadReview(first.ID)
	ApplyOwnerState(r, Submission{Items: []ReviewItem{{ID: "c1", Ref: true}}, Topics: []ReviewTopic{{ID: "n1", Refs: []string{"c1"}, Name: ReviewPoint{Text: "A"}}}})
	_ = s.SaveReview(r)
	second, _ := s.OpenUpdateReview(&UpdateReport{Candidates: []UpdateCandidate{{Dir: "a"}, {Dir: "b"}}})
	if second.ID != first.ID {
		t.Fatalf("must reuse the open review, got %s vs %s", second.ID, first.ID)
	}
	r, _ = s.LoadReview(first.ID)
	if len(r.Topics) != 1 || len(r.Items) < 2 || !r.Items[0].Ref {
		t.Fatalf("rows/ticks must survive the refresh: %+v", r)
	}
	// หัวข้อ created → รีวิวยังใช้ต่อ (ไม่มี done) และจุดตัด last-update เลื่อน
	rr, _ := s.ReviewRound(first.ID, map[string]any{"topics": []map[string]any{{"id": "n1", "name": "A", "desc": "d", "subs": []map[string]any{}, "note": "created blm-a"}}})
	if rr.Review.Status == "done" || rr.Review.Topics[0].Status != "created" {
		t.Fatalf("status: %s %s", rr.Review.Status, rr.Review.Topics[0].Status)
	}
	if at, from := s.LastUpdate(); at == "" || !strings.Contains(from, "topic created") {
		t.Fatalf("marker: %q %q", at, from)
	}
	third, _ := s.OpenUpdateReview(&UpdateReport{Candidates: []UpdateCandidate{{Dir: "a"}}})
	if third.ID != first.ID {
		t.Fatal("the update review must be reused forever")
	}
}

func TestReviewedTopicRowsLinkToTheirTopicPage(t *testing.T) {
	root := t.TempDir()
	c := Config{Backend: BackendNone, Store: ".claude/blm"}
	_ = os.MkdirAll(filepath.Join(root, c.Store), 0o755)
	s := Open(root, c)
	// รีวิวเก่าที่ done มีหัวข้อ extend ของ "VPN"
	old, _ := s.OpenReview(NewReviewFromUpdate(&UpdateReport{Topics: []UpdateTopic{{Topic: "VPN"}}}))
	ApplyOwnerState(old, Submission{Topics: []ReviewTopic{{ID: "n1", Existing: "VPN", Refs: []string{"t1"}}}})
	old.Topics[0].Status, old.Status = "created", "done"
	_ = s.SaveReview(old)
	// รีวิวใหม่: แถว t1 ต้องลิงก์ไปหน้าหัวข้อของรีวิวเก่า ด้วย base ของ server ปัจจุบัน
	nr, _ := s.OpenReview(NewReviewFromUpdate(&UpdateReport{Topics: []UpdateTopic{{Topic: "VPN"}}}))
	want := strings.TrimSuffix(nr.URL, "/r/"+nr.ID) + "/r/" + old.ID + "/t/n1?from=" + nr.ID
	if nr.Items[0].Link != want {
		t.Fatalf("link = %q want %q", nr.Items[0].Link, want)
	}
}

func TestChatPerRowRoundTrip(t *testing.T) {
	root := t.TempDir()
	c := Config{Backend: BackendNone, Store: ".claude/blm"}
	_ = os.MkdirAll(filepath.Join(root, c.Store), 0o755)
	s := Open(root, c)
	r, _ := s.OpenReview(NewReviewFromUpdate(&UpdateReport{Candidates: []UpdateCandidate{{Dir: "src/x"}}}))
	ApplySubmission(r, Submission{Items: []ReviewItem{{ID: "c1", Ask: "โฟลเดอร์นี้ทำอะไร"}}})
	_ = s.SaveReview(r)
	rr, _ := s.ReviewRound(r.File, nil)
	if len(rr.Todo) != 1 || !strings.Contains(rr.Todo[0], "chat on c1") || !strings.Contains(rr.Todo[0], "owner: โฟลเดอร์นี้ทำอะไร") {
		t.Fatalf("todo: %+v", rr.Todo)
	}
	rr, err := s.ReviewRound(r.File, map[string]any{"answers": []map[string]any{{"id": "c1", "answer": "จัดการ X"}}})
	if err != nil || len(rr.Review.Items[0].Chat) != 2 || rr.Review.Items[0].Chat[1].By != "agent" || rr.Review.Items[0].chatPending() {
		t.Fatalf("answer: %v %+v", err, rr.Review.Items[0].Chat)
	}
	// ถามต่อ → รอ agent อีก · ปิดแชท → ไม่รอ
	ApplyOwnerState(rr.Review, Submission{Items: []ReviewItem{{ID: "c1", Ask: "แล้ว Y ล่ะ"}}})
	if len(rr.Review.Items[0].Chat) != 3 || !rr.Review.Items[0].chatPending() {
		t.Fatalf("follow-up: %+v", rr.Review.Items[0].Chat)
	}
	// ปิดแชทไม่ได้จนกว่าจะมีบทสรุป · mark ★ แล้วปิดได้
	ApplyOwnerState(rr.Review, Submission{Items: []ReviewItem{{ID: "c1", AskDone: true}}})
	if rr.Review.Items[0].AskDone {
		t.Fatal("close without summary must be refused")
	}
	one := 1
	ApplyOwnerState(rr.Review, Submission{Items: []ReviewItem{{ID: "c1", SummaryIdx: &one, AskDone: true}}})
	if rr.Review.Items[0].Summary == nil || rr.Review.Items[0].Summary.Text != "จัดการ X" || !rr.Review.Items[0].AskDone || rr.Review.Items[0].chatPending() || len(rr.Review.Items[0].Chat) != 3 {
		t.Fatalf("summary/close: %+v", rr.Review.Items[0])
	}
	// ไฟล์รุ่นก่อน (ask/answer) ถูกย้ายเป็น chat ตอนโหลด
	old := &Review{ID: "x", Items: []ReviewItem{{ID: "c1", Ask: "q", Answer: "a"}}}
	migrateChat(old)
	if len(old.Items[0].Chat) != 2 || old.Items[0].Ask != "" {
		t.Fatalf("migrate: %+v", old.Items[0])
	}
}

func TestRowsUsedInOtherReviewsAreNotUnassigned(t *testing.T) {
	root := t.TempDir()
	c := Config{Backend: BackendNone, Store: ".claude/blm"}
	_ = os.MkdirAll(filepath.Join(root, c.Store), 0o755)
	s := Open(root, c)
	old, _ := s.OpenReview(NewReviewFromUpdate(&UpdateReport{Topics: []UpdateTopic{{Topic: "VPN"}}, Candidates: []UpdateCandidate{{Dir: "wireguard/mock"}}}))
	ApplyOwnerState(old, Submission{Topics: []ReviewTopic{{ID: "n1", Existing: "VPN", Refs: []string{"c1"}}}})
	old.Topics[0].Status, old.Status = "created", "done"
	_ = s.SaveReview(old)
	nr, _ := s.OpenReview(NewReviewFromUpdate(&UpdateReport{Topics: []UpdateTopic{{Topic: "VPN"}}, Candidates: []UpdateCandidate{{Dir: "wireguard/mock"}}}))
	if len(nr.Items[1].UsedIn) != 1 || !strings.HasPrefix(nr.Items[1].UsedIn[0], "→ VPN (created, "+old.ID) {
		t.Fatalf("usedIn: %+v", nr.Items[1])
	}
}

func TestCandidatesAndChangedAreOneTable(t *testing.T) {
	r := NewReviewFromUpdate(&UpdateReport{Candidates: []UpdateCandidate{{Dir: "a", Symbols: 9, Top: []string{"f"}}, {Dir: "b"}}, Changed: []UpdateChanged{{Dir: "a", Last: "2026-09-01", Files: []string{"a/x.ts"}}, {Dir: "z", Last: "2026-09-02", Files: []string{"z/y.ts"}}, {Dir: "cov", Covered: true, Files: []string{"cov/c.ts"}}}})
	var got []string
	for _, it := range r.Items {
		if it.Kind == "changed" {
			t.Fatal("no separate changed rows any more")
		}
		if it.Kind == "candidate" {
			got = append(got, it.ID+":"+it.Label)
		}
	}
	if strings.Join(got, " ") != "c1:a c2:z c3:b" {
		t.Fatalf("rows: %v", got)
	}
	if d := r.Items[0].Detail; !strings.HasPrefix(d, "โค้ดที่ถูกแก้หลัง blm init/update ครั้งล่าสุด (แก้ล่าสุด 2026-09-01") || !strings.Contains(d, "+ โฟลเดอร์โค้ดที่ยังไม่มีกฎข้อไหนอ้างถึง") || !strings.Contains(d, "9 symbols · f") || !strings.Contains(d, "a/x.ts") {
		t.Fatalf("merged detail: %s", d)
	}
}

func TestLiveWaitWakesOnChatAndStop(t *testing.T) {
	root := t.TempDir()
	c := Config{Backend: BackendNone, Store: ".claude/blm"}
	_ = os.MkdirAll(filepath.Join(root, c.Store), 0o755)
	s := Open(root, c)
	r, _ := s.OpenReview(NewReviewFromUpdate(&UpdateReport{Candidates: []UpdateCandidate{{Dir: "src/x"}}}))
	post := func(path string, body any) int {
		raw, _ := json.Marshal(body)
		res, err := http.Post(r.URL+path, "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	type out struct {
		res *WaitReviewResult
		err error
	}
	done := make(chan out, 1)
	go func() { w, err := s.WaitReview(r.ID, 5*time.Second); done <- out{w, err} }()
	time.Sleep(150 * time.Millisecond)
	if !liveWaiting(r.ID) {
		t.Fatal("agent must be marked as waiting")
	}
	// เจ้าของส่งข้อความในแชท (save ธรรมดา ไม่ต้อง submit) → wait ตื่น
	post("/save", Submission{Items: []ReviewItem{{ID: "c1", Ask: "ทำอะไร"}}})
	o := <-done
	if o.err != nil || o.res.Timeout || o.res.Stopped || len(o.res.Todo) != 1 || !liveIsTyping(r.ID) {
		t.Fatalf("wake: %+v typing=%v", o.res, liveIsTyping(r.ID))
	}
	// agent ตอบ → typing ดับ
	if _, err := s.ReviewRound(r.ID, map[string]any{"answers": []map[string]any{{"id": "c1", "answer": "ตอบ"}}}); err != nil || liveIsTyping(r.ID) {
		t.Fatalf("typing must clear after the answer: %v", err)
	}
	// รอบสอง: ไม่มีงาน → กด "จบ live" → stopped
	go func() { w, err := s.WaitReview(r.ID, 5*time.Second); done <- out{w, err} }()
	time.Sleep(150 * time.Millisecond)
	post("/live", map[string]any{"on": false})
	o = <-done
	if o.err != nil || !o.res.Stopped {
		t.Fatalf("stop: %+v", o.res)
	}
	// timeout
	w, _ := s.WaitReview(r.ID, 200*time.Millisecond)
	if !w.Timeout {
		t.Fatal("expected timeout")
	}
}

// comment ของจุด = แชตแบบเดียวกับแถว (เจ้าของ 2026-09-21): ข้อความเจ้าของ → COMMENT ค้าง + todo · agent ตอบใน answers ด้วย id "<topic>:<key>"
func TestPointCommentIsChat(t *testing.T) {
	root := t.TempDir()
	c := Config{Backend: BackendNone, Store: ".claude/blm"}
	_ = os.MkdirAll(filepath.Join(root, c.Store), 0o755)
	s := Open(root, c)
	r, _ := s.OpenReview(NewReviewFromUpdate(&UpdateReport{Candidates: []UpdateCandidate{{Dir: "src/x"}}}))
	r.Topics = []ReviewTopic{{ID: "n1", Status: "proposed", Name: ReviewPoint{Text: "X"}, Desc: ReviewPoint{Text: "ทำ X"}, Subs: []ReviewSub{{ID: "s1", Name: ReviewPoint{Text: "S"}, Desc: ReviewPoint{Text: "ทำ S"}}}}}
	_ = s.SaveReview(r)
	// เจ้าของ comment ที่ desc ของหัวข้อย่อย + agree ที่ชื่อ → todo มีแค่แชตนั้น (agree/draft ไม่ trigger)
	ApplyOwnerState(r, Submission{Topics: []ReviewTopic{{ID: "n1", Name: ReviewPoint{Action: "AGREE"}, Subs: []ReviewSub{{ID: "s1", Desc: ReviewPoint{Ask: "ยังไม่ครอบคลุม Y"}}}}}})
	sd := &r.Topics[0].Subs[0].Desc
	if sd.Action != ActionComment || !sd.chatPending() || r.Topics[0].Name.Action != ActionAgree || r.Topics[0].Status != "proposed" {
		t.Fatalf("owner comment: %+v status=%s", sd, r.Topics[0].Status)
	}
	if todo := reviewTodo(r); len(todo) != 1 || todo[0] != "comment n1#1" {
		t.Fatalf("todo: %v", todo)
	}
	_ = s.SaveReview(r)
	rr, _ := s.ReviewRound(r.File, nil)
	if len(rr.Todo) != 1 || !strings.Contains(rr.Todo[0], `id:"n1:sub:s1:desc"`) || !strings.Contains(rr.Todo[0], "owner: ยังไม่ครอบคลุม Y") {
		t.Fatalf("round todo: %+v", rr.Todo)
	}
	// agent ตอบในแชตของจุด → ไม่ค้าง · ข้อความจุดยังเดิม action ยัง COMMENT จนเจ้าของ agree
	rr, err := s.ReviewRound(r.File, map[string]any{"answers": []map[string]any{{"id": "n1:sub:s1:desc", "answer": "เพิ่ม Y ให้แล้ว", "summary": true}}})
	sd = &rr.Review.Topics[0].Subs[0].Desc
	if err != nil || len(sd.Chat) != 2 || sd.chatPending() || sd.Summary == nil || len(reviewTodo(rr.Review)) != 0 {
		t.Fatalf("answer: %v %+v todo=%v", err, sd, reviewTodo(rr.Review))
	}
	if pointRef(rr.Review, "n1:name").Text != "X" || pointRef(rr.Review, "n1:sub:zz:name") != nil || pointRef(rr.Review, "bad") != nil {
		t.Fatal("pointRef")
	}
}

func TestSelectJSON(t *testing.T) {
	raw := []byte(`{"topics":[{"id":"a","status":"new","subs":[{"id":"s1"},{"id":"s2"}]},{"id":"b","status":"created"}],"items":[{"id":"c1","kind":"candidate"},{"id":"c2","kind":"candidate"}]}`)
	get := func(sel string) string {
		v, err := SelectRaw(raw, sel)
		if err != nil {
			return "ERR " + err.Error()
		}
		b, _ := json.Marshal(v)
		return string(b)
	}
	for sel, want := range map[string]string{
		"topics[0].subs[-1].id": `"s2"`,
		"topics[id=b].status":   `"created"`,
		"topics.status":         `["new","created"]`,
		"items[kind=candidate]": `[{"id":"c1","kind":"candidate"},{"id":"c2","kind":"candidate"}]`,
		"items[kind=x]":         `[]`,
		"topics[9]":             "ERR",
		"nope":                  "null",
	} {
		if got := get(sel); got != want && !(want == "ERR" && strings.HasPrefix(got, "ERR")) {
			t.Fatalf("%s: got %s want %s", sel, got, want)
		}
	}
}

// id ของแถวคงที่ข้ามการ refresh · แชทตามแถวไป · แถวที่หายจากรายงานแต่ถูกอ้าง/มีแชท เก็บเป็น gone (เจ้าของ 2026-09-20)
func TestRefreshKeepsIdsChatAndGoneRows(t *testing.T) {
	root := t.TempDir()
	c := Config{Backend: BackendNone, Store: ".claude/blm"}
	_ = os.MkdirAll(filepath.Join(root, c.Store), 0o755)
	s := Open(root, c)
	r := NewReviewFromUpdate(&UpdateReport{Candidates: []UpdateCandidate{{Dir: "db/scripts"}, {Dir: "src/hooks"}}})
	ApplyOwnerState(r, Submission{Items: []ReviewItem{{ID: "c1", Ask: "โฟลเดอร์นี้คืออะไร"}}})
	r.Topics = []ReviewTopic{{ID: "n1", Refs: []string{"c1"}, Status: "created", Name: ReviewPoint{Text: "X"}}}
	// รอบใหม่: db/scripts หายไป (มีกฎแล้ว) · มี public/x เพิ่ม
	s.RefreshItemsFrom(r, &UpdateReport{Candidates: []UpdateCandidate{{Dir: "src/hooks"}, {Dir: "public/x"}}})
	by := map[string]ReviewItem{}
	for _, it := range r.Items {
		by[it.ID] = it
	}
	if by["c2"].Label != "src/hooks" || by["c3"].Label != "public/x" || by["c1"].Label != "db/scripts" || !by["c1"].Gone || len(by["c1"].Chat) != 1 {
		t.Fatalf("items: %+v", r.Items)
	}
	if r.Topics[0].Refs[0] != "c1" {
		t.Fatalf("refs must stay: %v", r.Topics[0].Refs)
	}
	// แถวที่หายและไม่มีใครอ้าง/ไม่มีแชท = ทิ้ง
	s.RefreshItemsFrom(r, &UpdateReport{Candidates: []UpdateCandidate{{Dir: "src/hooks"}}})
	for _, it := range r.Items {
		if it.Label == "public/x" {
			t.Fatal("unreferenced gone row must be dropped")
		}
	}
}
