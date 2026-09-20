package blm

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// blm self-update — อัปเดตตัวเอง (เจ้าของ 2026-09-20): binary + plugin ในคำสั่งเดียว ทั้ง CLI (`blm self-update` / `blm --self-update`) และ MCP (`blm_selfupdate`)
//
// binary มี 2 แบบตามที่ติดตั้ง — ดูจากไฟล์ที่กำลังรันอยู่ (os.Executable แล้ว resolve symlink):
//   - dev  : อยู่ใน checkout ของ repo (มี go.mod ของ blm ข้างบน) → `git pull --ff-only` + `go build` ลงที่เดิม (ต้องมี go)
//   - global: ไฟล์จริง (install.sh / make install → ~/.blm/bin/blm; ~/.local/bin/blm เป็น symlink) → ดาวน์โหลด asset ล่าสุดจาก GitHub Releases (blm_<os>_<arch>.tar.gz|zip)
//     เขียนไฟล์ใหม่ข้าง ๆ แล้ว rename ทับ — โปรเซสที่รันอยู่ (รวม MCP ตัวนี้) ยังใช้ inode เดิมต่อจนกว่าจะ reconnect
// plugin: `claude plugin marketplace update blm` → `claude plugin update blm@blm` (skill/commands/.mcp.json ใน ~/.claude/plugins/cache)
// ไม่ retry ไม่วน: ขั้นไหนล้มรายงานแล้วไปขั้นถัดไป ผลรวมบอกว่าอะไรผ่าน/ไม่ผ่าน

const (
	selfRepo       = "Ekkapap/business-logic-memory"
	selfPluginName = "blm"
)

// SelfUpdateOpts — Check = แค่ดูเวอร์ชันล่าสุด ไม่เปลี่ยนอะไร · Binary/Plugin = ทำเฉพาะส่วน (ค่าเริ่มต้นทั้งคู่)
type SelfUpdateOpts struct {
	Check  bool
	Binary bool
	Plugin bool
}

// SelfUpdateResult ผลลัพธ์ — Terminal ให้ CLI/agent อ่าน ที่เหลือเป็น field ให้ agent ตัดสิน
type SelfUpdateResult struct {
	Current       string `json:"current"`
	Latest        string `json:"latest,omitempty"`
	Mode          string `json:"mode"` // dev | global
	BinaryUpdated bool   `json:"binaryUpdated"`
	PluginUpdated bool   `json:"pluginUpdated"`
	Reconnect     bool   `json:"reconnect"`
	Terminal      string `json:"terminal"`
}

// SelfUpdate ทำตาม opts · out = stream ของคำสั่งย่อย (nil = เงียบ)
func SelfUpdate(opts SelfUpdateOpts, out io.Writer) (*SelfUpdateResult, error) {
	if !opts.Binary && !opts.Plugin {
		opts.Binary, opts.Plugin = true, true
	}
	r := &SelfUpdateResult{Current: Version}
	exe, repoDir := selfLocation()
	r.Mode = "global"
	if repoDir != "" {
		r.Mode = "dev"
	}
	var b strings.Builder
	b.WriteString(Title("blm  " + Dim("self-update · current "+Version+" · "+r.Mode+" install at "+exe)))

	if opts.Binary {
		b.WriteString("\n" + Section("Binary"))
		switch {
		case repoDir != "":
			// dev: git pull + build ลงไฟล์เดิม
			if opts.Check {
				if outp, err := runIn(repoDir, nil, "git", "fetch", "--quiet"); err != nil {
					b.WriteString("\n  " + Red("✘") + " git fetch: " + firstLine(outp, err))
				} else if behind, _ := runIn(repoDir, nil, "git", "rev-list", "--count", "HEAD..@{u}"); strings.TrimSpace(behind) != "0" {
					b.WriteString("\n  " + Yellow("⟳") + " " + strings.TrimSpace(behind) + " commit(s) behind origin — run: blm self-update")
				} else {
					b.WriteString("\n  " + Green("✔") + " checkout is up to date with origin")
				}
			} else if outp, err := runIn(repoDir, out, "git", "pull", "--ff-only"); err != nil {
				b.WriteString("\n  " + Red("✘") + " git pull --ff-only: " + firstLine(outp, err) + Dim("  (commit or stash local changes, then retry)"))
			} else {
				ver := strings.TrimSpace(mustOut(repoDir, "git", "describe", "--tags", "--always", "--dirty"))
				ld := "-X github.com/Ekkapap/business-logic-memory/internal/blm.Version=" + strings.TrimPrefix(ver, "v")
				if outp, err := runIn(repoDir, out, "go", "build", "-ldflags", ld, "-o", exe, "./cmd/blm"); err != nil {
					b.WriteString("\n  " + Red("✘") + " go build: " + firstLine(outp, err))
				} else {
					r.Latest = strings.TrimPrefix(ver, "v")
					r.BinaryUpdated = r.Latest != Version
					b.WriteString("\n  " + Green("✔") + " built " + r.Latest + Dim(" → "+exe))
					r.Reconnect = r.Reconnect || r.BinaryUpdated
				}
			}
		default:
			latest, url, err := latestRelease()
			if err != nil {
				b.WriteString("\n  " + Red("✘") + " GitHub release lookup: " + err.Error())
				break
			}
			r.Latest = latest
			switch cmp := compareVersions(latest, Version); {
			case cmp == 0:
				b.WriteString("\n  " + Green("✔") + " already latest " + Dim(latest))
			case cmp < 0:
				b.WriteString("\n  " + Green("✔") + " " + Version + " is ahead of the latest release " + Dim(latest+" — nothing to do"))
			case opts.Check:
				b.WriteString("\n  " + Yellow("⟳") + " " + Version + " → " + latest + " available — run: blm self-update")
			default:
				if err := replaceBinary(exe, url); err != nil {
					b.WriteString("\n  " + Red("✘") + " download/replace: " + err.Error())
				} else {
					r.BinaryUpdated, r.Reconnect = true, true
					b.WriteString("\n  " + Green("✔") + " " + Version + " → " + latest + Dim("  ("+exe+" replaced)"))
				}
			}
		}
	}

	if opts.Plugin {
		b.WriteString("\n" + Section("Plugin  "+Dim(selfPluginName+"@"+selfPluginName)))
		if _, err := exec.LookPath("claude"); err != nil {
			b.WriteString("\n  " + Yellow("note:") + " `claude` not on PATH — update inside Claude Code: /plugin → " + selfPluginName)
		} else {
			before := pluginVersionDir(selfPluginName)
			if opts.Check {
				b.WriteString("\n  " + Dim("installed") + " " + orDash(before) + Dim("  (run blm self-update to refresh from the marketplace)"))
			} else {
				if outp, err := runIn("", out, "claude", "plugin", "marketplace", "update", selfPluginName); err != nil {
					b.WriteString("\n  " + Red("✘") + " marketplace update: " + firstLine(outp, err))
				} else {
					b.WriteString("\n  " + Green("✔") + " marketplace refreshed")
				}
				if outp, err := runIn("", out, "claude", "plugin", "update", selfPluginName+"@"+selfPluginName); err != nil {
					b.WriteString("\n  " + Red("✘") + " plugin update: " + firstLine(outp, err))
				} else if after := pluginVersionDir(selfPluginName); after != before {
					r.PluginUpdated, r.Reconnect = true, true
					b.WriteString("\n  " + Green("✔") + " plugin " + orDash(before) + " → " + after)
				} else {
					b.WriteString("\n  " + Green("✔") + " plugin already latest " + Dim(orDash(after)))
				}
			}
		}
	}

	if r.Reconnect {
		b.WriteString("\n" + Section("Next") + "\n  " + "/mcp reconnect plugin:blm:blm " + Dim("(the running MCP still uses the old binary until then)"))
	}
	r.Terminal = b.String()
	return r, nil
}

// selfLocation — ไฟล์จริงของ binary ที่รัน และ repo dir ถ้ามันอยู่ใน checkout ของ blm (dev install)
func selfLocation() (exe, repoDir string) {
	exe, err := os.Executable()
	if err != nil {
		return "blm", ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	for dir := filepath.Dir(exe); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if raw, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil && strings.Contains(string(raw), "module github.com/"+selfRepo) {
			if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
				return exe, dir
			}
		}
	}
	return exe, ""
}

func runIn(dir string, out io.Writer, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	raw, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(raw))
	if out != nil && text != "" {
		fmt.Fprintln(out, Dim("$ "+name+" "+strings.Join(args, " ")))
		fmt.Fprintln(out, text)
	}
	return text, err
}

func mustOut(dir, name string, args ...string) string {
	s, _ := runIn(dir, nil, name, args...)
	return s
}

func firstLine(out string, err error) string {
	if l := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0]); l != "" {
		return l
	}
	return err.Error()
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func pluginVersionDir(name string) string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	return latestDir(filepath.Join(home, ".claude", "plugins", "cache", name, name))
}

// latestRelease — tag ล่าสุด + URL ของ asset สำหรับ OS/arch นี้ (ชื่อตาม scripts/release.sh)
func latestRelease() (version, url string, err error) {
	cl := http.Client{Timeout: 15 * time.Second}
	resp, err := cl.Get("https://api.github.com/repos/" + selfRepo + "/releases/latest")
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("github → %d", resp.StatusCode)
	}
	var rel struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", "", err
	}
	want := fmt.Sprintf("blm_%s_%s.", runtime.GOOS, runtime.GOARCH)
	for _, a := range rel.Assets {
		if strings.HasPrefix(a.Name, want) {
			return strings.TrimPrefix(rel.TagName, "v"), a.URL, nil
		}
	}
	return strings.TrimPrefix(rel.TagName, "v"), "", fmt.Errorf("release %s has no asset for %s/%s", rel.TagName, runtime.GOOS, runtime.GOARCH)
}

// replaceBinary — โหลด archive → เอา blm ออกมา → เขียน exe.new ข้าง ๆ → rename ทับ (atomic บน POSIX; Windows rename ทับไฟล์ที่กำลังรันไม่ได้ → บอกให้ปิดก่อน)
func replaceBinary(exe, url string) error {
	if url == "" {
		return fmt.Errorf("no download URL")
	}
	cl := http.Client{Timeout: 2 * time.Minute}
	resp, err := cl.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download → %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var bin []byte
	if strings.HasSuffix(url, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			return err
		}
		for _, f := range zr.File {
			if strings.HasPrefix(f.Name, "blm") {
				rc, err := f.Open()
				if err != nil {
					return err
				}
				bin, err = io.ReadAll(rc)
				rc.Close()
				if err != nil {
					return err
				}
			}
		}
	} else {
		gz, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return err
		}
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if filepath.Base(h.Name) == "blm" {
				if bin, err = io.ReadAll(tr); err != nil {
					return err
				}
			}
		}
	}
	if len(bin) == 0 {
		return fmt.Errorf("archive has no blm binary")
	}
	tmp := exe + ".new"
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp)
		if runtime.GOOS == "windows" {
			return fmt.Errorf("%v — Windows cannot replace a running exe: quit Claude Code, then run blm self-update again", err)
		}
		return err
	}
	return nil
}

// compareVersions เทียบ a กับ b ที่ major.minor.patch (ตัด suffix หลัง '-' เช่น 2.0.7-13-gdb58 → 2.0.7) · >0 = a ใหม่กว่า
// build ระหว่าง tag (2.0.7-13) นับเท่ากับ 2.0.7 — release เดียวกันไม่ต้องโหลดทับ และไม่ downgrade
func compareVersions(a, b string) int {
	parse := func(v string) [3]int {
		v = strings.TrimPrefix(strings.SplitN(v, "-", 2)[0], "v")
		var n [3]int
		for i, p := range strings.SplitN(v, ".", 3) {
			fmt.Sscanf(p, "%d", &n[i])
		}
		return n
	}
	x, y := parse(a), parse(b)
	for i := range x {
		if x[i] != y[i] {
			if x[i] > y[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}
