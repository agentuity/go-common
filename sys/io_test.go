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

func TestUnzipWithBracketFilename(t *testing.T) {
	baseDir := t.TempDir()
	zipPath := filepath.Join(baseDir, "test.zip")
	
	// Create a zip file with a filename containing [[...mdxPath]]
	zf, err := os.Create(zipPath)
	assert.NoError(t, err)
	defer zf.Close()
	
	zw := zip.NewWriter(zf)
	defer zw.Close()
	
	// Create a file with problematic filename
	filename := "pages/[[...mdxPath]]/index.js"
	w, err := zw.Create(filename)
	assert.NoError(t, err)
	_, err = w.Write([]byte("console.log('test');"))
	assert.NoError(t, err)
	
	// Close the zip writer to finalize the zip
	err = zw.Close()
	assert.NoError(t, err)
	zf.Close()
	
	// Test extraction
	extractDir := filepath.Join(baseDir, "extracted")
	err = os.MkdirAll(extractDir, 0755)
	assert.NoError(t, err)
	
	// This should now work with the fix
	err = Unzip(zipPath, extractDir, false)
	assert.NoError(t, err, "Unzip should succeed with [[...mdxPath]] filename")
	
	// Verify the file was extracted correctly
	expectedPath := filepath.Join(extractDir, "pages", "[[...mdxPath]]", "index.js")
	assert.True(t, Exists(expectedPath), "File should exist at: %s", expectedPath)
}

func TestUnzipDirectoryTraversalProtection(t *testing.T) {
	baseDir := t.TempDir()
	zipPath := filepath.Join(baseDir, "malicious.zip")
	
	// Create a zip file with directory traversal attempts
	zf, err := os.Create(zipPath)
	assert.NoError(t, err)
	defer zf.Close()
	
	zw := zip.NewWriter(zf)
	defer zw.Close()
	
	// Test various directory traversal patterns
	maliciousFilenames := []string{
		"../../../etc/passwd",
		"../../malicious.txt",
		"./../../evil.sh",
		"subdir/../../../etc/hosts",
		"..",
		"valid/../../etc/shadow",
	}
	
	for _, filename := range maliciousFilenames {
		w, err := zw.Create(filename)
		assert.NoError(t, err)
		_, err = w.Write([]byte("malicious content"))
		assert.NoError(t, err)
	}
	
	// Close the zip writer
	err = zw.Close()
	assert.NoError(t, err)
	zf.Close()
	
	// Test extraction - should fail
	extractDir := filepath.Join(baseDir, "extracted")
	err = os.MkdirAll(extractDir, 0755)
	assert.NoError(t, err)
	
	err = Unzip(zipPath, extractDir, false)
	assert.Error(t, err, "Unzip should fail with directory traversal filenames")
	assert.Contains(t, err.Error(), "invalid file path", "Error should mention invalid file path")
}
