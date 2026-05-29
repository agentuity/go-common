//go:build !windows

package sys

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestUnzipClosesEachEntry(t *testing.T) {
	baseDir := t.TempDir()
	zipPath := filepath.Join(baseDir, "many-files.zip")

	files := make(map[string]string)
	for i := 0; i < 64; i++ {
		files[fmt.Sprintf("node_modules/pkg_%02d/index.js", i)] = fmt.Sprintf("module.exports = %d\n", i)
	}
	writeUnzipTestArchive(t, zipPath, files)

	openFDs, err := countOpenFDs()
	if err != nil {
		t.Skipf("cannot count open file descriptors: %v", err)
	}

	var original syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &original); err != nil {
		t.Skipf("cannot read RLIMIT_NOFILE: %v", err)
	}

	target := uint64(openFDs + 32)
	if target < 64 {
		target = 64
	}
	if original.Cur <= target || original.Max < target {
		t.Skipf("RLIMIT_NOFILE is too low for deterministic test: open=%d cur=%d max=%d target=%d", openFDs, original.Cur, original.Max, target)
	}

	limited := original
	limited.Cur = target
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limited); err != nil {
		t.Skipf("cannot lower RLIMIT_NOFILE: %v", err)
	}
	t.Cleanup(func() {
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &original); err != nil {
			t.Logf("failed to restore RLIMIT_NOFILE: %v", err)
		}
	})

	extractDir := filepath.Join(baseDir, "out")
	if err := Unzip(zipPath, extractDir, false); err != nil {
		t.Fatalf("Unzip returned error under low RLIMIT_NOFILE: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(extractDir, "node_modules/pkg_63/index.js"))
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(got) != "module.exports = 63\n" {
		t.Fatalf("extracted content = %q", got)
	}
}

func writeUnzipTestArchive(t *testing.T, path string, files map[string]string) {
	t.Helper()

	out, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer out.Close()

	zw := zip.NewWriter(out)
	for name, contents := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %q: %v", name, err)
		}
		if _, err := w.Write([]byte(contents)); err != nil {
			t.Fatalf("write zip entry %q: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
}

func countOpenFDs() (int, error) {
	for _, dir := range []string{"/proc/self/fd", "/dev/fd"} {
		entries, err := os.ReadDir(dir)
		if err == nil {
			return len(entries), nil
		}
	}
	return 0, os.ErrNotExist
}
