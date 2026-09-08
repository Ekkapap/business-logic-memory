// Package blm — business-logic-memory: โน้ตชั่วคราวของ agent ระหว่าง session + ไฟล์กฎธุรกิจ blm.md หนึ่งเดียว
//
// ทำไมมี: memory_save ของ AgentsRoom กลาง session ช้า (เกิน 2 นาทีก็มี) เจ้าของสั่ง 2026-09-08 ให้จดลงไฟล์ในเครื่องก่อน
// แล้ว sync เข้าปลายทางครั้งเดียวตอนท้าย — หรือไม่ sync เลยเมื่อไม่มีปลายทาง (backend none: ไฟล์คือของจริง)
package blm

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Backend บอกว่า "ของจริง" อยู่ไหนและ sync ทำอะไร
//   - none      : store คือของจริง ไม่มี sync (blm_sync ไม่ถูกประกาศ)             store .claude/blm
//   - agentsroom: store เป็นร่าง ของจริงอยู่ AgentsRoom (mirror .agentsroom/memory) push = memory_save
//   - obsidian  : store เป็นโฟลเดอร์ปกติในห้อง vault (Obsidian ไม่แสดงโฟลเดอร์จุด) ไม่มี sync
//   - custom    : store + คำสั่ง shell ที่ agent เรียนรู้จาก `<cli> --help` ตอน init  push/pull = รัน template
type Backend string

const (
	BackendNone       Backend = "none"
	BackendAgentsRoom Backend = "agentsroom"
	BackendObsidian   Backend = "obsidian"
	BackendCustom     Backend = "custom"
)

// Config เขียนครั้งเดียวตอน `blm init …` ที่ <project>/.claude/blm.json — MCP แค่อ่าน
type Config struct {
	Backend Backend `json:"backend"`
	// โฟลเดอร์ store relative จาก root โปรเจ็ค
	Store string `json:"store"`
	// mirror ของ backend (agentsroom) relative จาก root — ว่าง = อ่านไฟล์กฎจาก store ตรง ๆ
	Mirror string `json:"mirror,omitempty"`
	// custom: template คำสั่ง push/pull ใช้ {name} {file} {folder} {description}
	PushCommand string `json:"pushCommand,omitempty"`
	PullCommand string `json:"pullCommand,omitempty"`
	// Tools เครื่องมือข้างเคียงที่เลือกตอน init (socraticode|obsidian|graphify) — status/tools แสดงเฉพาะชุดนี้
	Tools []string `json:"tools,omitempty"`
}

const (
	ConfigFile = ".claude/blm.json"
	// RulesNote ชื่อโน้ต/ไฟล์กฎธุรกิจ (blm.md) — source of truth เดียว เจ้าของเปลี่ยนเท่านั้น
	RulesNote = "blm"
)

var Presets = map[Backend]Config{
	BackendNone:       {Backend: BackendNone, Store: ".claude/blm"},
	BackendAgentsRoom: {Backend: BackendAgentsRoom, Store: ".agentsroom/blm", Mirror: ".agentsroom/memory"},
	BackendObsidian:   {Backend: BackendObsidian, Store: "blm"},
}

// Load อ่าน config ของโปรเจ็ค · ไม่มี/พัง = เดาให้: มี .agentsroom/memory → agentsroom ไม่งั้น none
// (โปรเจ็คที่ไม่เคย init ยังใช้ได้ server ไม่ล้ม)
func Load(root string) Config {
	if raw, err := os.ReadFile(filepath.Join(root, ConfigFile)); err == nil {
		var c Config
		if json.Unmarshal(raw, &c) == nil && c.Backend != "" && c.Store != "" {
			return c
		}
	}
	if fi, err := os.Stat(filepath.Join(root, Presets[BackendAgentsRoom].Mirror)); err == nil && fi.IsDir() {
		return Presets[BackendAgentsRoom]
	}
	return Presets[BackendNone]
}

// HasSync — backend นี้มีปลายทางให้ sync ไหม (none/obsidian ไม่มี → ไม่ประกาศ blm_sync เลย)
func (c Config) HasSync() bool {
	return c.Backend == BackendAgentsRoom || c.Backend == BackendCustom
}

func (c Config) Save(root string) error {
	file := filepath.Join(root, ConfigFile)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	raw, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(file, append(raw, '\n'), 0o644)
}
