package compress

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGunzip(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		want    []byte
		wantErr bool
	}{
		{
			name:    "empty input",
			data:    []byte{},
			want:    []byte{},
			wantErr: true,
		},
		{
			name:    "valid gzip data",
			data:    []byte{31, 139, 8, 0, 0, 0, 0, 0, 0, 255, 242, 72, 205, 201, 201, 87, 8, 207, 47, 202, 73, 1, 4, 0, 0, 255, 255, 86, 177, 23, 74, 11, 0, 0, 0},
			want:    []byte("Hello World"),
			wantErr: false,
		},
		{
			name:    "invalid gzip data",
			data:    []byte{1, 2, 3, 4},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Gunzip(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("Gunzip() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !bytes.Equal(got, tt.want) {
				t.Errorf("Gunzip() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTarGz(t *testing.T) {
	tmpDir := t.TempDir()
	testDir := filepath.Join(tmpDir, "testdir")
	err := os.MkdirAll(testDir, 0755)
	assert.NoError(t, err)

	testFile1 := filepath.Join(testDir, "test1.txt")
	err = os.WriteFile(testFile1, []byte("test content 1"), 0644)
	assert.NoError(t, err)

	testFile2 := filepath.Join(testDir, "test2.txt")
	err = os.WriteFile(testFile2, []byte("test content 2"), 0644)
	assert.NoError(t, err)

	nestedDir := filepath.Join(testDir, "nested")
	err = os.MkdirAll(nestedDir, 0755)
	assert.NoError(t, err)

	testFile3 := filepath.Join(nestedDir, "test3.txt")
	err = os.WriteFile(testFile3, []byte("test content 3"), 0644)
	assert.NoError(t, err)

	outFile, err := os.CreateTemp(tmpDir, "test-*.tar.gz")
	assert.NoError(t, err)
	defer outFile.Close()

	err = TarGz(testDir, outFile)
	assert.NoError(t, err)

	_, err = outFile.Seek(0, 0)
	assert.NoError(t, err)

	gr, err := gzip.NewReader(outFile)
	assert.NoError(t, err)
	defer gr.Close()

	tr := tar.NewReader(gr)

	fileCount := 0
	dirCount := 0

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		assert.NoError(t, err)

		if header.FileInfo().IsDir() {
			dirCount++
		} else {
			fileCount++
			if header.Name == "testdir/test1.txt" {
				buf := new(bytes.Buffer)
				_, err = io.Copy(buf, tr)
				assert.NoError(t, err)
				assert.Equal(t, "test content 1", buf.String())
			}
		}
	}

	assert.Equal(t, 2, dirCount, "Should have 2 directories (testdir and nested)")
	assert.Equal(t, 3, fileCount, "Should have 3 files")
}

func TestTarGzipDir(t *testing.T) {
	tmpDir := t.TempDir()
	testDir := filepath.Join(tmpDir, "testdir")
	err := os.MkdirAll(testDir, 0755)
	assert.NoError(t, err)

	testFile := filepath.Join(testDir, "test.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0644)
	assert.NoError(t, err)

	tarGzPath, err := TarGzipDir(testDir)
	assert.NoError(t, err)
	defer os.Remove(tarGzPath)

	_, err = os.Stat(tarGzPath)
	assert.NoError(t, err)

	f, err := os.Open(tarGzPath)
	assert.NoError(t, err)
	defer f.Close()

	gr, err := gzip.NewReader(f)
	assert.NoError(t, err)
	defer gr.Close()

	tr := tar.NewReader(gr)

	header, err := tr.Next()
	assert.NoError(t, err)
	assert.Contains(t, header.Name, "testdir")
}
