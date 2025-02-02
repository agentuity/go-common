package sys

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTarGz(t *testing.T) {

	baseDir := t.TempDir()

	dir := filepath.Join(baseDir, "test")

	os.WriteFile(filepath.Join(dir, "foo"), []byte("bar"), 0644)
	os.Mkdir(filepath.Join(dir, "nested"), 0755)
	os.WriteFile(filepath.Join(dir, "nested", "foo2"), []byte("bar2"), 0644)

	tarball, err := os.Create(filepath.Join(baseDir, "test.tar.gz"))
	assert.NoError(t, err)
	assert.NoError(t, TarGz(dir, tarball))
	tarball.Close()

}

func TestUnzip(t *testing.T) {
	baseDir := t.TempDir()
	zf, err := os.Create(filepath.Join(baseDir, "foobar.zip"))
	assert.NoError(t, err)
	zw := zip.NewWriter(zf)
	w, err := zw.Create("foo/foo.txt")
	assert.NoError(t, err)
	w.Write([]byte("bar"))
	zw.Close()
	zf.Close()
	assert.NoError(t, Unzip(filepath.Join(baseDir, "foobar.zip"), baseDir, true))
	assert.True(t, Exists(filepath.Join(baseDir, "foo.txt")))
	os.Remove(filepath.Join(baseDir, "foo.txt"))
	assert.NoError(t, Unzip(filepath.Join(baseDir, "foobar.zip"), baseDir, false))
	assert.False(t, Exists(filepath.Join(baseDir, "foo.txt")))
	assert.True(t, Exists(filepath.Join(baseDir, "foo", "foo.txt")))
}
