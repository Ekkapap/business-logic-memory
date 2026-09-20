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
	// SocratiCode ที่อยู่ของ Ollama + Qdrant ที่ติดตั้งไว้แล้วบนเครื่องอื่น (`blm tools install socraticode --remote …`)
	// nil = แบบเดิม: บริการอยู่บนเครื่องนี้ที่ 127.0.0.1 · มีค่า = install/status/graph ชี้ไปที่นั่น
	SocratiCode *SocratiCodeConfig `json:"socraticode,omitempty"`
}

// SocratiCodeConfig — ค่าเดียวกับ env ที่ MCP ของ SocratiCode อ่าน (README ของมัน: OLLAMA_URL / QDRANT_URL / EMBEDDING_*)
// เก็บไว้ในโปรเจ็คเพื่อให้ `blm tools install socraticode --remote` รอบถัดไปไม่ต้องพิมพ์ซ้ำ และให้ status/graph รู้ที่อยู่
// prefix ว่างมีความหมาย (bge-m3 ไม่ใช้ prefix) จึงเขียนลง env เสมอเมื่อตั้ง EmbeddingModel — ไม่ใช่ "ไม่ระบุ = ค่าเริ่มต้นของ SocratiCode"
type SocratiCodeConfig struct {
	OllamaURL               string `json:"ollamaUrl"`
	QdrantURL               string `json:"qdrantUrl"`
	EmbeddingModel          string `json:"embeddingModel,omitempty"`
	EmbeddingDimensions     string `json:"embeddingDimensions,omitempty"`
	EmbeddingContextLength  string `json:"embeddingContextLength,omitempty"`
	EmbeddingQueryPrefix    string `json:"embeddingQueryPrefix"`
	EmbeddingDocumentPrefix string `json:"embeddingDocumentPrefix"`
}

// Env — บรรทัด `KEY=value` / `-KEY` สำหรับ ~/.claude/settings.json (รูปเดียวกับที่ tools.sh พิมพ์ `ENV …`)
// ไม่มีโมเดล = ลบ EMBEDDING_* ทิ้งให้ SocratiCode ใช้ค่าเริ่มต้นของ provider (nomic-embed-text 768)
func (s SocratiCodeConfig) Env() []string {
	lines := []string{
		"OLLAMA_MODE=external", "OLLAMA_URL=" + s.OllamaURL,
		"QDRANT_MODE=external", "QDRANT_URL=" + s.QdrantURL,
	}
	if s.EmbeddingModel == "" {
		return append(lines, "-EMBEDDING_MODEL", "-EMBEDDING_DIMENSIONS", "-EMBEDDING_CONTEXT_LENGTH", "-EMBEDDING_QUERY_PREFIX", "-EMBEDDING_DOCUMENT_PREFIX")
	}
	return append(lines,
		"EMBEDDING_MODEL="+s.EmbeddingModel,
		"EMBEDDING_DIMENSIONS="+s.EmbeddingDimensions,
		"EMBEDDING_CONTEXT_LENGTH="+s.EmbeddingContextLength,
		"EMBEDDING_QUERY_PREFIX="+s.EmbeddingQueryPrefix,
		"EMBEDDING_DOCUMENT_PREFIX="+s.EmbeddingDocumentPrefix,
	)
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
