package blm

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// replaceBinary: tar.gz จาก "release" ปลอม → ไฟล์ถูกแทนแบบ atomic เนื้อหาใหม่ mode 755
func TestReplaceBinaryFromTarGz(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("#!/bin/sh\necho new\n")
	_ = tw.WriteHeader(&tar.Header{Name: "blm", Mode: 0o755, Size: int64(len(body))})
	_, _ = tw.Write(body)
	_ = tw.Close()
	_ = gz.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(buf.Bytes()) }))
	defer srv.Close()

	exe := filepath.Join(t.TempDir(), "blm")
	_ = os.WriteFile(exe, []byte("old"), 0o755)
	if err := replaceBinary(exe, srv.URL+"/blm_darwin_arm64.tar.gz"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != string(body) {
		t.Fatalf("binary not replaced: %q", got)
	}
	if fi, _ := os.Stat(exe); fi.Mode()&0o111 == 0 {
		t.Fatal("not executable")
	}
	if _, err := os.Stat(exe + ".new"); err == nil {
		t.Fatal("temp file left behind")
	}
	// archive ไม่มี blm → error ไฟล์เดิมอยู่
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var b bytes.Buffer
		g := gzip.NewWriter(&b)
		_ = tar.NewWriter(g).Close()
		_ = g.Close()
		_, _ = w.Write(b.Bytes())
	}))
	defer empty.Close()
	if err := replaceBinary(exe, empty.URL+"/x.tar.gz"); err == nil {
		t.Fatal("expected error for archive without blm")
	}
	if got, _ := os.ReadFile(exe); string(got) != string(body) {
		t.Fatal("binary must be untouched after a failed replace")
	}
}

func TestSelfLocationDetectsDevCheckout(t *testing.T) {
	exe, repo := selfLocation()
	// test binary อยู่ใน build cache ไม่ใช่ checkout → global · แต่ exe ต้องเป็น path จริง
	if exe == "" {
		t.Fatal("exe empty")
	}
	if repo != "" && !filepath.IsAbs(repo) {
		t.Fatalf("repo dir must be absolute: %s", repo)
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{{"2.0.8", "2.0.7", 1}, {"2.0.7", "2.0.7-13-gdb58b6d-dirty", 0}, {"v2.0.7", "2.1.0", -1}, {"3.0.0", "2.9.9", 1}, {"2.0.7", "dev", 1}}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}
