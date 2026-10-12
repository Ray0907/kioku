package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var (
	updateAPI      = "https://api.github.com/repos/Ray0907/kioku/releases/latest"
	updateDownload = "https://github.com/Ray0907/kioku/releases/download"
)

const updateMaxBytes = 64 << 20

var semverRe = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

// semver parses an exact vX.Y.Z. Dev, dirty and git-describe versions do not
// parse, so they are never mistaken for a release.
func semver(v string) (n [3]int, ok bool) {
	m := semverRe.FindStringSubmatch(v)
	if m == nil {
		return n, false
	}
	for i := range n {
		n[i], _ = strconv.Atoi(m[i+1])
	}
	return n, true
}

func runUpdate(args []string) error {
	check := false
	for _, a := range args {
		if a != "--check" {
			return fmt.Errorf("usage: kioku update [--check]")
		}
		check = true
	}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return err
	}
	return doUpdate(&http.Client{Timeout: 60 * time.Second}, version, exe, runtime.GOOS, runtime.GOARCH, check, output)
}

func doUpdate(c *http.Client, current, exe, goos, goarch string, check bool, w io.Writer) error {
	body, err := updateGet(c, updateAPI, 1<<20)
	if err != nil {
		return err
	}
	var rel struct {
		Tag string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &rel); err != nil {
		return fmt.Errorf("release info: %w", err)
	}
	latest, ok := semver(rel.Tag)
	if !ok {
		return fmt.Errorf("unexpected release tag %q", rel.Tag)
	}
	if cur, known := semver(current); known && cur[0]*1e6+cur[1]*1e3+cur[2] >= latest[0]*1e6+latest[1]*1e3+latest[2] {
		fmt.Fprintf(w, "kioku %s is up to date\n", current)
		return nil
	}
	if check {
		fmt.Fprintf(w, "kioku %s · latest %s · run `kioku update` to install it\n", current, rel.Tag)
		return nil
	}

	name := fmt.Sprintf("kioku_%s_%s_%s.tar.gz", rel.Tag, goos, goarch)
	base := updateDownload + "/" + rel.Tag + "/"
	sums, err := updateGet(c, base+"SHA256SUMS", 1<<20)
	if err != nil {
		return err
	}
	want := ""
	for _, line := range strings.Split(string(sums), "\n") {
		if f := strings.Fields(line); len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			want = f[0]
		}
	}
	if want == "" {
		return fmt.Errorf("release %s has no build for %s/%s", rel.Tag, goos, goarch)
	}
	arc, err := updateGet(c, base+name, updateMaxBytes)
	if err != nil {
		return err
	}
	if got := sha256.Sum256(arc); hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("checksum mismatch for %s; nothing was changed", name)
	}
	bin, err := updateExtract(arc)
	if err != nil {
		return err
	}

	// A temp file in the same directory makes the final rename atomic, and a
	// failure at any earlier step leaves the running binary untouched.
	tmp, err := os.CreateTemp(filepath.Dir(exe), ".kioku-update-*")
	if err != nil {
		return fmt.Errorf("cannot write next to %s: %w", exe, err)
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(bin); err == nil {
		err = tmp.Chmod(0o755)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), exe)
	}
	if err != nil {
		return fmt.Errorf("replace %s: %w", exe, err)
	}
	fmt.Fprintf(w, "kioku %s → %s\n", current, rel.Tag)
	return nil
}

func updateGet(c *http.Client, url string, max int64) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "kioku/"+version)
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("GET %s: larger than %d bytes", url, max)
	}
	return b, nil
}

func updateExtract(arc []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(arc))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("archive has no kioku binary")
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && path.Base(h.Name) == "kioku" {
			b, err := io.ReadAll(io.LimitReader(tr, updateMaxBytes))
			return b, err
		}
	}
}
