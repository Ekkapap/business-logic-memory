package blm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// สถิติ (เจ้าของสั่ง 2026-09-08: เก็บไว้ benchmark คล้าย `rtk gain`) — jsonl บรรทัดละเหตุการณ์
// event: read {query, blocks, trigger} · check {query, passed, notPassed, unknown, fixedByMirror, trigger} · lookup {query} · override {topic, note}

func (s *Store) statsFile() string { return filepath.Join(s.Dir, "stats.jsonl") }

func (s *Store) RecordStat(ev map[string]any) map[string]any {
	ev["at"] = time.Now().UTC().Format(time.RFC3339)
	_ = os.MkdirAll(s.Dir, 0o755)
	f, err := os.OpenFile(s.statsFile(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return ev
	}
	defer f.Close()
	raw, _ := json.Marshal(ev)
	_, _ = f.Write(append(raw, '\n'))
	return ev
}

type DayStat struct {
	Day       string `json:"day"`
	Reads     int    `json:"reads"`
	Checks    int    `json:"checks"`
	NotPassed int    `json:"notPassed"`
	Lookups   int    `json:"lookups"`
	Overrides int    `json:"overrides"`
}

type HotTopic struct {
	Topic     string `json:"topic"`
	NotPassed int    `json:"notPassed"`
}

type Gain struct {
	Since         string     `json:"since,omitempty"`
	Reads         int        `json:"reads"`
	ReadsByUser   int        `json:"readsByUser"`
	ReadsByAgent  int        `json:"readsByAgent"`
	Checks        int        `json:"checks"`
	Passed        int        `json:"passed"`
	NotPassed     int        `json:"notPassed"`
	Unknown       int        `json:"unknown"`
	FixedByMirror int        `json:"fixedByMirror"` // agent เข้าใจผิดแล้วไฟล์กฎพากลับมาถูกเอง
	Overrides     int        `json:"overrides"`     // เจ้าของต้องเปลี่ยนกฎ
	Lookups       int        `json:"lookups"`       // เจ้าของจำไม่ได้ ให้ agent ค้น
	HotTopics     []HotTopic `json:"hotTopics"`
	ByDay         []DayStat  `json:"byDay"`
}

func (s *Store) GainSummary() Gain {
	g := Gain{HotTopics: []HotTopic{}, ByDay: []DayStat{}}
	f, err := os.Open(s.statsFile())
	if err != nil {
		return g
	}
	defer f.Close()
	hot := map[string]int{}
	days := map[string]*DayStat{}
	num := func(m map[string]any, k string) int {
		if v, ok := m[k].(float64); ok {
			return int(v)
		}
		return 0
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var r map[string]any
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			continue
		}
		at, _ := r["at"].(string)
		if g.Since == "" {
			g.Since = at
		}
		day := at
		if len(day) > 10 {
			day = day[:10]
		}
		d := days[day]
		if d == nil {
			d = &DayStat{Day: day}
			days[day] = d
		}
		q, _ := r["query"].(string)
		switch r["event"] {
		case "read":
			g.Reads++
			d.Reads++
			if r["trigger"] == "user" {
				g.ReadsByUser++
			} else {
				g.ReadsByAgent++
			}
		case "check":
			g.Checks++
			d.Checks++
			g.Passed += num(r, "passed")
			np := num(r, "notPassed")
			g.NotPassed += np
			d.NotPassed += np
			g.Unknown += num(r, "unknown")
			g.FixedByMirror += num(r, "fixedByMirror")
			if np > 0 {
				if q == "" {
					q = "(whole file)"
				}
				hot[q] += np
			}
		case "fixed":
			g.FixedByMirror += num(r, "count")
		case "lookup":
			g.Lookups++
			d.Lookups++
		case "override":
			g.Overrides++
			d.Overrides++
			t, _ := r["topic"].(string)
			hot[t]++
		}
	}
	for t, n := range hot {
		g.HotTopics = append(g.HotTopics, HotTopic{t, n})
	}
	sort.Slice(g.HotTopics, func(i, j int) bool { return g.HotTopics[i].NotPassed > g.HotTopics[j].NotPassed })
	if len(g.HotTopics) > 10 {
		g.HotTopics = g.HotTopics[:10]
	}
	for _, d := range days {
		g.ByDay = append(g.ByDay, *d)
	}
	sort.Slice(g.ByDay, func(i, j int) bool { return g.ByDay[i].Day < g.ByDay[j].Day })
	return g
}

// RenderGain section สถิติแบบ `rtk gain` — ใช้ต่อท้าย status
func RenderGain(g Gain) string {
	head := "Stats"
	if g.Since != "" {
		head += "  (since " + g.Since[:10] + ")"
	}
	b := &strings.Builder{}
	b.WriteString(Section(head) + "\n")
	b.WriteString(KV([][2]string{
		{"Rules reads", fmt.Sprintf("%d  (user %d · agent %d)", g.Reads, g.ReadsByUser, g.ReadsByAgent)},
		{"Check rounds", fmt.Sprintf("%d  → %s %d · %s %d · UNKNOWN %d", g.Checks, Green("PASSED"), g.Passed, Red("NOT PASSED"), g.NotPassed, g.Unknown)},
		{"Fixed by rules file", fmt.Sprintf("%d  (agent was wrong, corrected itself from blm.md)", g.FixedByMirror)},
		{"Owner overrides", fmt.Sprintf("%d  (owner had to change a rule)", g.Overrides)},
		{"Owner lookups", fmt.Sprintf("%d  (owner asked, agent searched the rules)", g.Lookups)},
	}) + "\n")
	if len(g.HotTopics) > 0 {
		var rows [][]string
		for i, t := range g.HotTopics {
			rows = append(rows, []string{fmt.Sprintf("%d.", i+1), t.Topic, fmt.Sprintf("%d", t.NotPassed)})
		}
		b.WriteString(Section("Most Missed Topics") + "\n" + Table([]string{"#", "Topic", "Missed"}, rows) + "\n")
	}
	if len(g.ByDay) > 0 {
		var rows [][]string
		for _, d := range g.ByDay {
			rows = append(rows, []string{d.Day, fmt.Sprintf("%d", d.Reads), fmt.Sprintf("%d", d.Checks), fmt.Sprintf("%d", d.NotPassed), fmt.Sprintf("%d", d.Lookups), fmt.Sprintf("%d", d.Overrides)})
		}
		b.WriteString(Section("By Day") + "\n" + Table([]string{"Day", "Reads", "Checks", "Not passed", "Lookups", "Overrides"}, rows) + "\n")
	}
	return b.String()
}
