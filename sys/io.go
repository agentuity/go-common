package sys

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// CopyFile will copy src to dst
func CopyFile(src, dst string) (int64, error) {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return 0, err
	}

	if !sourceFileStat.Mode().IsRegular() {
		return 0, fmt.Errorf("%s is not a regular file", src)
	}

	source, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer destination.Close()
	nBytes, err := io.Copy(destination, source)
	return nBytes, err
}

// CopyDir will copy all files recursively from src to dst
func CopyDir(src string, dst string) error {
	var err error
	var fds []os.DirEntry
	var srcinfo os.FileInfo

	if srcinfo, err = os.Stat(src); err != nil {
		return fmt.Errorf("error reading %s: %w", src, err)
	}

	if err = os.MkdirAll(dst, srcinfo.Mode()); err != nil {
		return fmt.Errorf("error mkdir %s: %w", dst, err)
	}

	if fds, err = os.ReadDir(src); err != nil {
		return fmt.Errorf("error readdir %s: %w", src, err)
	}
	for _, fd := range fds {
		srcfp := path.Join(src, fd.Name())
		dstfp := path.Join(dst, fd.Name())

		if fd.IsDir() {
			if err = CopyDir(srcfp, dstfp); err != nil {
				return fmt.Errorf("error copying directory from %s to %s: %w", srcfp, dstfp, err)
			}
		} else {
			if _, err = CopyFile(srcfp, dstfp); err != nil {
				return fmt.Errorf("error copying file from %s to %s: %w", srcfp, dstfp, err)
			}
		}
	}
	return nil
}

// Exists returns true if the filename or directory specified by fn exists.
func Exists(fn string) bool {
	if _, err := os.Stat(fn); os.IsNotExist(err) {
		return false
	}
	return true
}

// ListDir will return an array of files recursively walking into sub directories
func ListDir(dir string) ([]string, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	res := make([]string, 0)
	for _, file := range files {
		if file.IsDir() {
			newres, err := ListDir(filepath.Join(dir, file.Name()))
			if err != nil {
				return nil, err
			}
			res = append(res, newres...)
		} else {
			if file.Name() == ".DS_Store" {
				continue
			}
			res = append(res, filepath.Join(dir, file.Name()))
		}
	}
	return res, nil
}

// GzipFile compresses a file using gzip.
func GzipFile(filepath string) error {
	infile, err := os.Open(filepath)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer infile.Close()

	outfile, err := os.Create(filepath + ".gz")
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	defer outfile.Close()

	zr := gzip.NewWriter(outfile)
	defer zr.Close()
	_, err = io.Copy(zr, infile)
	if err != nil {
		return fmt.Errorf("copy: %w", err)
	}

	return nil
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

// JSONEncoder is an encoder that will allow you to encode one or more objects as JSON newline delimited output
type JSONEncoder interface {
	// Encode will encode v as a new line delimited JSON encoded string
	Encode(v any) error
	// Close a stream
	Close() error
	// Count returns the number of records written
	Count() int
}

type JSONDecoder interface {
	Decode(v any) error
	// More returns true if there are more items in the stream
	More() bool
	// Count returns the number of records read
	Count() int
	// Close a stream
	Close() error
}

type ndjsonWriter struct {
	out   *os.File
	gz    *gzip.Writer
	enc   *json.Encoder
	count int
}

var _ JSONEncoder = (*ndjsonWriter)(nil)

func (n *ndjsonWriter) Close() error {
	if n.gz != nil {
		n.gz.Close()
		n.gz = nil
	}
	if n.out != nil {
		n.out.Close()
		n.out = nil
	}
	return nil
}

func (n *ndjsonWriter) Encode(v any) error {
	n.count++
	return n.enc.Encode(v)
}

func (n *ndjsonWriter) Count() int {
	return n.count
}

type ndjsonReader struct {
	in    *os.File
	gr    *gzip.Reader
	dec   *json.Decoder
	count int
}

var _ JSONDecoder = (*ndjsonReader)(nil)

func (n *ndjsonReader) Count() int {
	return n.count
}

func (n *ndjsonReader) Close() error {
	if n.gr != nil {
		n.gr.Close()
		n.gr = nil
	}
	if n.in != nil {
		n.in.Close()
		n.in = nil
	}
	return nil
}

func (n *ndjsonReader) More() bool {
	return n.dec.More()
}

func (n *ndjsonReader) Decode(v any) error {
	if err := n.dec.Decode(v); err != nil {
		return err
	}
	n.count++
	return nil
}

// NewNDJSONEncoder will return a JSONEncoder which allows you to stream json as new line delimited JSON
func NewNDJSONEncoder(fn string) (JSONEncoder, error) {
	out, err := os.Create(fn)
	if err != nil {
		return nil, fmt.Errorf("error opening: %s. %w", fn, err)
	}
	var o io.Writer = out
	var gw *gzip.Writer
	if filepath.Ext(fn) == ".gz" {
		gw = gzip.NewWriter(out)
		o = gw
	}
	jw := json.NewEncoder(o)
	return &ndjsonWriter{
		out: out,
		gz:  gw,
		enc: jw,
	}, nil
}

func NewNDJSONEncoderAppend(fn string) (JSONEncoder, error) {
	out, err := os.OpenFile(fn, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)

	if err != nil {
		return nil, fmt.Errorf("error opening: %s. %w", fn, err)
	}
	var o io.Writer = out
	var gw *gzip.Writer
	if filepath.Ext(fn) == ".gz" {
		gw = gzip.NewWriter(out)
		o = gw
	}
	jw := json.NewEncoder(o)
	return &ndjsonWriter{
		out: out,
		gz:  gw,
		enc: jw,
	}, nil
}

// NewNDJSONDecoder returns a decoder which can be used to read JSON new line delimited files
func NewNDJSONDecoder(fn string) (JSONDecoder, error) {
	in, err := os.Open(fn)
	if err != nil {
		return nil, fmt.Errorf("error opening: %s. %w", fn, err)
	}
	var i io.Reader = in
	var gr *gzip.Reader
	if filepath.Ext(fn) == ".gz" {
		var err error
		gr, err = gzip.NewReader(in)
		if err != nil {
			return nil, fmt.Errorf("gzip: error opening: %s. %w", fn, err)
		}
		i = gr
	}
	je := json.NewDecoder(i)
	return &ndjsonReader{
		in:  in,
		gr:  gr,
		dec: je,
	}, nil
}

func WriteJSON(filename string, v any) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(v); err != nil {
		return err
	}
	return nil
}

func WriteJSONLAppend(filename string, v []interface{}) error {
	enc, err := NewNDJSONEncoderAppend(filename)
	if err != nil {
		return err
	}
	defer enc.Close()
	for _, item := range v {
		if err := enc.Encode(item); err != nil {
			return err
		}
	}
	return nil
}

type ProcessDirWithDecoderCallback func(decoder JSONDecoder, filename string) error

// ProcessDirWithDecoder will process all the JSON files in directory and call the callback
func ProcessDirWithDecoder(dir string, callback ProcessDirWithDecoderCallback) error {
	files, err := ListDir(dir)
	if err != nil {
		return err
	}
	for _, file := range files {
		filename := file
		if filepath.Ext(filename) == ".json" || filepath.Ext(filename) == ".gz" {
			dec, err := NewNDJSONDecoder(filename)
			if err != nil {
				return err
			}
			defer dec.Close()
			if err := callback(dec, filename); err != nil {
				return err
			}
			dec.Close()
		}
	}
	return nil
}

// Unzip a file to a directory
func Unzip(src, dest string, flatten bool) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		// Check for directory traversal attacks by examining path components
		// Split the path and check each component
		pathParts := strings.Split(filepath.ToSlash(f.Name), "/")
		for _, part := range pathParts {
			// Block path components that could be used for directory traversal
			// ".." is obvious, but also block pure dot sequences (3+ dots) that might be interpreted as ".."
			if part == ".." || (len(part) >= 3 && strings.Trim(part, ".") == "") {
				return fmt.Errorf("invalid file path: %s", f.Name)
			}
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()

		if flatten {
			f.Name = filepath.Base(f.Name)
		}

		// Validate file name before path construction to catch absolute paths and other attacks
		if filepath.IsAbs(f.Name) {
			return fmt.Errorf("invalid file path, absolute path not allowed: %s", f.Name)
		}

		// Check for Windows-style absolute paths and UNC paths (platform-independent)
		if len(f.Name) >= 3 && f.Name[1] == ':' && (f.Name[2] == '\\' || f.Name[2] == '/') {
			return fmt.Errorf("invalid file path, Windows drive path not allowed: %s", f.Name)
		}
		if strings.HasPrefix(f.Name, "\\\\") {
			return fmt.Errorf("invalid file path, UNC path not allowed: %s", f.Name)
		}

		fpath := filepath.Join(dest, f.Name)

		// Canonicalize paths and validate against directory traversal
		destRoot := filepath.Clean(dest)
		if !strings.HasSuffix(destRoot, string(os.PathSeparator)) {
			destRoot += string(os.PathSeparator)
		}
		cleanedFpath := filepath.Clean(fpath)

		// Ensure the cleaned file path is within the destination directory
		if !strings.HasPrefix(cleanedFpath+string(os.PathSeparator), destRoot) {
			return fmt.Errorf("invalid file path, outside destination directory: %s", f.Name)
		}

		if f.FileInfo().IsDir() && !flatten {
			os.MkdirAll(fpath, os.ModePerm)
		} else {
			var fdir string
			if lastIndex := strings.LastIndex(fpath, string(os.PathSeparator)); lastIndex > -1 {
				fdir = fpath[:lastIndex]
			}

			err = os.MkdirAll(fdir, os.ModePerm)
			if err != nil {
				return err
			}
			f, err := os.OpenFile(
				fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return err
			}
			defer f.Close()

			_, err = io.Copy(f, rc)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
