package blm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// GrepHit represents one match found by grep
type GrepHit struct {
	ResultID int    `json:"resultId"`
	Term     string `json:"term"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
	Lines    int    `json:"lines"`
	Chars    int    `json:"chars"`
	Text     string `json:"text"`
}

// GrepResult is the output of a grep operation
type GrepResult struct {
	ID                string    `json:"id,omitempty"`
	File              string    `json:"file,omitempty"`
	RanAt             string    `json:"ranAt,omitempty"`
	CWD               string    `json:"cwd,omitempty"`
	Hits              []GrepHit `json:"hits"`
	SocratiCodeFailed bool      `json:"socraticodeUsed,omitempty"`
}

var (
	allowedExts = map[string]bool{
		".md": true, ".txt": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
		".php": true, ".sql": true, ".sh": true, ".go": true,
	}
	skipDirs = map[string]bool{
		"node_modules": true, ".git": true, ".next": true, "dist": true, "build": true, "vendor": true, ".claude": true,
	}
)

// Grep searches for terms in files with optional SocratiCode semantic search
func Grep(root string, terms []string, filePath, dirPath string, maxLine, maxResult int, exts string, useSC, includeImports bool) (*GrepResult, error) {
	// Parse extension allowlist
	extSet := allowedExts
	if exts != "" {
		extSet = map[string]bool{}
		for _, e := range strings.Split(exts, ",") {
			ext := strings.TrimSpace(e)
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			extSet[ext] = true
		}
	}

	var filesToScan []string

	if filePath != "" {
		// Single file mode
		filesToScan = []string{filePath}
	} else {
		// Directory scan
		scanDir := dirPath
		if scanDir == "" {
			scanDir = "."
		}
		// Resolve to absolute path if relative
		if !filepath.IsAbs(scanDir) {
			scanDir = filepath.Join(root, scanDir)
		}

		// Try SocratiCode first if requested
		if useSC {
			candidates, scErr := scSearch(root, terms)
			if scErr == nil && len(candidates) > 0 {
				// Use SocratiCode candidates
				for _, c := range candidates {
					if extSet[filepath.Ext(c)] {
						filesToScan = append(filesToScan, c)
					}
				}
			} else if scErr != nil {
				// Fall back to regular scan
				collectFiles(scanDir, extSet, &filesToScan)
			}
		} else {
			collectFiles(scanDir, extSet, &filesToScan)
		}
	}

	// Process terms in parallel with worker pool
	hits := grepParallel(filesToScan, terms, maxLine, maxResult, includeImports)

	// Sort by term order, then path, then line
	sort.Slice(hits, func(i, j int) bool {
		ti, tj := termIndex(hits[i].Term, terms), termIndex(hits[j].Term, terms)
		if ti != tj {
			return ti < tj
		}
		if hits[i].Path != hits[j].Path {
			return hits[i].Path < hits[j].Path
		}
		return hits[i].Line < hits[j].Line
	})

	// Convert absolute paths to relative for display and storage
	cwd, _ := os.Getwd()
	for i := range hits {
		if relPath, err := filepath.Rel(cwd, hits[i].Path); err == nil {
			hits[i].Path = relPath
		}
	}

	// Add resultIds and create result
	id := randomBase36()
	for i := range hits {
		hits[i].ResultID = i + 1
	}

	res := &GrepResult{
		ID:    id,
		RanAt: time.Now().Format("2006-01-02T15:04:05Z07:00"),
		CWD:   cwd,
		Hits:  hits,
	}

	// Save to file
	if err := res.SaveResult(root); err != nil {
		// Log but don't fail; still return results
		fmt.Fprintf(os.Stderr, "warning: could not save result file: %v\n", err)
	}

	return res, nil
}

func termIndex(term string, terms []string) int {
	for i, t := range terms {
		if t == term {
			return i
		}
	}
	return len(terms)
}

func collectFiles(root string, extSet map[string]bool, files *[]string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if extSet[filepath.Ext(path)] {
			*files = append(*files, path)
		}
		return nil
	})
}

func grepParallel(files []string, terms []string, maxLine, maxResult int, includeImports bool) []GrepHit {
	type work struct {
		term string
		file string
	}
	// ponytail: worker pool; scale up if needed
	workers := 4
	if len(files) < workers {
		workers = len(files)
	}

	workCh := make(chan work, len(files))
	resCh := make(chan GrepHit, len(files)*len(terms)*maxResult)

	// Start workers
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for wr := range workCh {
				grepFile(wr.file, wr.term, maxLine, maxResult, includeImports, resCh)
			}
		}()
	}

	// Distribute work: each term × each file
	for _, term := range terms {
		for _, file := range files {
			workCh <- work{term, file}
		}
	}
	close(workCh)
	wg.Wait()
	close(resCh)

	// Collect results, respecting maxResult per term
	termHits := map[string]int{}
	var hits []GrepHit
	for h := range resCh {
		if termHits[h.Term] < maxResult {
			hits = append(hits, h)
			termHits[h.Term]++
		}
	}
	return hits
}

// isImportLine checks if a line is an import/export/require statement
func isImportLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}

	// JavaScript/TypeScript imports
	if strings.HasPrefix(trimmed, "import ") || strings.HasPrefix(trimmed, "import{") {
		return true
	}
	// JavaScript/TypeScript exports
	if strings.HasPrefix(trimmed, "export ") && (strings.Contains(trimmed, " from ") || strings.HasPrefix(trimmed, "export *")) {
		return true
	}
	// Node.js require
	if strings.HasPrefix(trimmed, "require(") || strings.Contains(trimmed, "= require(") {
		return true
	}
	// Python imports
	if strings.HasPrefix(trimmed, "import ") || strings.HasPrefix(trimmed, "from ") && strings.Contains(trimmed, " import ") {
		return true
	}

	return false
}

func grepFile(filePath string, term string, maxLine, maxResult int, includeImports bool, out chan GrepHit) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return
	}

	lines := bytes.Split(content, []byte("\n"))
	count := 0

	for lineNum, line := range lines {
		if count >= maxResult {
			break
		}
		if !strings.Contains(strings.ToLower(string(line)), strings.ToLower(term)) {
			continue
		}

		// Skip import/export lines unless includeImports flag is set
		if !includeImports && isImportLine(string(line)) {
			continue
		}

		// Find snippet boundaries (empty lines or file boundaries)
		start := lineNum
		for start > 0 && len(bytes.TrimSpace(lines[start-1])) > 0 {
			start--
		}
		end := lineNum
		for end < len(lines)-1 && len(bytes.TrimSpace(lines[end+1])) > 0 {
			end++
		}

		// Clamp to maxLine/2 window
		if end-start+1 > maxLine {
			mid := lineNum
			halfMax := maxLine / 2
			start = mid - halfMax
			if start < 0 {
				start = 0
			}
			end = start + maxLine - 1
			if end >= len(lines) {
				end = len(lines) - 1
				start = end - maxLine + 1
				if start < 0 {
					start = 0
				}
			}
		}

		// Compute bytes for snippet
		var snippet bytes.Buffer
		for i := start; i <= end; i++ {
			if i > start {
				snippet.WriteString("\n")
			}
			snippet.Write(lines[i])
		}

		out <- GrepHit{
			Term:  term,
			Path:  filePath,
			Line:  lineNum + 1, // 1-indexed for humans
			Start: start + 1,
			End:   end + 1,
			Lines: end - start + 1,
			Chars: snippet.Len(),
			Text:  strings.TrimSpace(string(line)),
		}
		count++
	}
}

// randomBase36 generates an 8-character random base36 ID
func randomBase36() string {
	const chars = "0123456789abcdefghijklmnopqrstuvwxyz"
	rand.Seed(time.Now().UnixNano())
	result := make([]byte, 8)
	for i := range result {
		result[i] = chars[rand.Intn(36)]
	}
	return string(result)
}

// SaveResult saves grep result to a JSON file and returns the file path
func (r *GrepResult) SaveResult(root string) error {
	// ponytail: save location — use blm store if in project, else UserCacheDir/blm/grep
	storeDir := filepath.Join(root, ".agentsroom", "blm", "tmp")
	if _, err := os.Stat(filepath.Join(root, ".agentsroom", "blm")); err != nil {
		// Not a blm project, use cache dir (check BLM_CACHE_DIR env for tests)
		cacheDir := os.Getenv("BLM_CACHE_DIR")
		if cacheDir == "" {
			var err error
			cacheDir, err = os.UserCacheDir()
			if err != nil {
				return fmt.Errorf("cannot determine cache directory: %v", err)
			}
		}
		storeDir = filepath.Join(cacheDir, "blm", "grep")
	}

	if err := os.MkdirAll(storeDir, 0755); err != nil {
		return fmt.Errorf("cannot create result directory: %v", err)
	}

	// Create .gitignore in tmp directory to self-ignore
	gitignorePath := filepath.Join(storeDir, ".gitignore")
	if _, err := os.Stat(gitignorePath); err != nil {
		if err := os.WriteFile(gitignorePath, []byte("*\n"), 0644); err != nil {
			// Ignore write errors for .gitignore; it's optional
		}
	}

	filename := fmt.Sprintf("grep-result-%s.json", r.ID)
	filepath := filepath.Join(storeDir, filename)
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal result: %v", err)
	}

	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("cannot write result file: %v", err)
	}

	r.File = filepath
	return nil
}

// scSearch uses SocratiCode to find candidate files for terms (stub if unreachable)
func scSearch(root string, terms []string) ([]string, error) {
	base := qdrantBase(root)
	if base == "" {
		// No SocratiCode available; return nil to signal fallback
		return nil, fmt.Errorf("socraticode ไม่พร้อม — ใช้การสแกนปกติ")
	}

	// ponytail: stub; semantic search via Qdrant would implement Qdrant search over embeddings
	// TODO: query Qdrant embedding for each term, return candidate file paths
	return nil, fmt.Errorf("socraticode semantic search stub")
}
