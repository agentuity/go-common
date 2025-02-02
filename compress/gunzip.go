package compress

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Gunzip will unzip data and return buffer inline
func Gunzip(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var resB bytes.Buffer
	_, err = resB.ReadFrom(r)
	if err != nil {
		return nil, err
	}

	return append([]byte(nil), resB.Bytes()...), nil
}

func TarGz(srcDir string, outfile *os.File) error {
	zr := gzip.NewWriter(outfile)
	tw := tar.NewWriter(zr)

	baseDir := filepath.Base(srcDir)
	// walk through every file in the folder
	filepath.Walk(srcDir, func(file string, fi os.FileInfo, _ error) error {
		// generate tar header
		header, err := tar.FileInfoHeader(fi, file)
		if err != nil {
			return err
		}

		header.Name = baseDir + strings.Replace(filepath.ToSlash(file), srcDir, "", -1)

		// write header
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		// if not a dir, write file content
		if !fi.IsDir() {
			data, err := os.Open(file)
			if err != nil {
				return err
			}
			if _, err := io.Copy(tw, data); err != nil {
				return err
			}
		}
		return nil
	})

	// produce tar
	if err := tw.Close(); err != nil {
		return err
	}
	// produce gzip
	if err := zr.Close(); err != nil {
		return err
	}

	return nil
}

// TarGzipDir will tar and gzip a directory and return the path to the file. You must delete the file when done.
func TarGzipDir(srcDir string) (string, error) {
	tmpfn, err := os.CreateTemp("", "*.tar.gz")
	if err != nil {
		return "", fmt.Errorf("tmp: %w", err)
	}
	defer tmpfn.Close()

	if err := TarGz(srcDir, tmpfn); err != nil {
		return "", err
	}
	return tmpfn.Name(), nil
}
