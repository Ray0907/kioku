package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func updateArchive(t *testing.T, bin string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct{ name, body string }{{"kioku", bin}, {"LICENSE", "mit"}} {
		tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(f.body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// updateServer serves a release; sum overrides the published checksum when set.
func updateServer(t *testing.T, tag, bin, sum string) *httptest.Server {
	t.Helper()
	arc := updateArchive(t, bin)
	name := fmt.Sprintf("kioku_%s_darwin_arm64.tar.gz", tag)
	if sum == "" {
		h := sha256.Sum256(arc)
		sum = hex.EncodeToString(h[:])
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, `{"tag_name":%q}`, tag) })
	mux.HandleFunc("/dl/"+tag+"/SHA256SUMS", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n%s  other.tar.gz\n", sum, name, strings.Repeat("0", 64))
	})
	mux.HandleFunc("/dl/"+tag+"/"+name, func(w http.ResponseWriter, r *http.Request) { w.Write(arc) })
	srv := httptest.NewServer(mux)
	oldAPI, oldDL := updateAPI, updateDownload
	updateAPI, updateDownload = srv.URL+"/api", srv.URL+"/dl"
	t.Cleanup(func() { srv.Close(); updateAPI, updateDownload = oldAPI, oldDL })
	return srv
}

func updateExe(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "kioku")
	if err := os.WriteFile(p, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func runUpd(current, exe, goos string, check bool) (string, error) {
	var out bytes.Buffer
	err := doUpdate(http.DefaultClient, current, exe, goos, "arm64", check, &out)
	return out.String(), err
}

func content(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUpdateReplacesBinary(t *testing.T) {
	updateServer(t, "v0.5.0", "new-binary", "")
	for _, cur := range []string{"v0.4.1", "dev", "v0.4.1-dirty", "v0.3.0-1-g4ad9f63"} {
		exe := updateExe(t)
		out, err := runUpd(cur, exe, "darwin", false)
		if err != nil || content(t, exe) != "new-binary" || !strings.Contains(out, "v0.5.0") {
			t.Fatalf("%s: err=%v content=%q out=%q", cur, err, content(t, exe), out)
		}
		if st, _ := os.Stat(exe); st.Mode().Perm() != 0o755 {
			t.Fatalf("%s: mode %v", cur, st.Mode())
		}
		if left, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".kioku-update-*")); len(left) != 0 {
			t.Fatalf("temp file left behind: %v", left)
		}
	}
}

func TestUpdateUpToDateLeavesBinary(t *testing.T) {
	updateServer(t, "v0.5.0", "new-binary", "")
	for _, cur := range []string{"v0.5.0", "v0.6.0"} {
		exe := updateExe(t)
		out, err := runUpd(cur, exe, "darwin", false)
		if err != nil || content(t, exe) != "old" || !strings.Contains(out, "up to date") {
			t.Fatalf("%s: err=%v content=%q out=%q", cur, err, content(t, exe), out)
		}
	}
}

func TestUpdateCheckDoesNotWrite(t *testing.T) {
	updateServer(t, "v0.5.0", "new-binary", "")
	exe := updateExe(t)
	out, err := runUpd("v0.4.1", exe, "darwin", true)
	if err != nil || content(t, exe) != "old" || !strings.Contains(out, "v0.5.0") {
		t.Fatalf("err=%v content=%q out=%q", err, content(t, exe), out)
	}
}

func TestUpdateRejectsBadChecksum(t *testing.T) {
	updateServer(t, "v0.5.0", "new-binary", strings.Repeat("a", 64))
	exe := updateExe(t)
	if _, err := runUpd("v0.4.1", exe, "darwin", false); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("want checksum error, got %v", err)
	}
	if content(t, exe) != "old" {
		t.Fatal("binary replaced despite a bad checksum")
	}
}

func TestUpdateUnsupportedPlatform(t *testing.T) {
	updateServer(t, "v0.5.0", "new-binary", "")
	exe := updateExe(t)
	if _, err := runUpd("v0.4.1", exe, "plan9", false); err == nil || !strings.Contains(err.Error(), "plan9") {
		t.Fatalf("want a no-asset error naming the platform, got %v", err)
	}
	if content(t, exe) != "old" {
		t.Fatal("binary changed")
	}
}

func TestUpdateUnwritableDirKeepsBinary(t *testing.T) {
	updateServer(t, "v0.5.0", "new-binary", "")
	exe := updateExe(t)
	dir := filepath.Dir(exe)
	os.Chmod(dir, 0o555)
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	if _, err := runUpd("v0.4.1", exe, "darwin", false); err == nil {
		t.Fatal("expected an error for an unwritable directory")
	}
	if content(t, exe) != "old" {
		t.Fatal("binary changed")
	}
}

func TestUpdateServerErrors(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	oldAPI := updateAPI
	updateAPI = srv.URL
	defer func() { updateAPI = oldAPI }()
	exe := updateExe(t)
	if _, err := runUpd("v0.4.1", exe, "darwin", false); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("want a 404 error, got %v", err)
	}
	updateServer(t, "latest-and-greatest", "x", "")
	if _, err := runUpd("v0.4.1", exe, "darwin", false); err == nil || !strings.Contains(err.Error(), "tag") {
		t.Fatalf("want an unexpected-tag error, got %v", err)
	}
	if content(t, exe) != "old" {
		t.Fatal("binary changed")
	}
}

func TestRunUpdateArguments(t *testing.T) {
	if err := runUpdate([]string{"--now"}); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("want usage error, got %v", err)
	}
}
