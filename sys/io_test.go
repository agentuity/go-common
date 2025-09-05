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

func TestUnzipWithMultipleDots(t *testing.T) {
	baseDir := t.TempDir()
	zipPath := filepath.Join(baseDir, "test.zip")

	// Create a zip file with filenames containing multiple dots
	zf, err := os.Create(zipPath)
	assert.NoError(t, err)
	defer zf.Close()

	zw := zip.NewWriter(zf)
	defer zw.Close()

	// Create files with various dot patterns that should be allowed
	// Note: pure dot sequences (3+ dots) as path components are now blocked for security
	testFiles := []string{
		"file..txt",         // two dots in middle (allowed)
		"test.json",         // single dot (allowed)
		"some..thing.json",  // two dots in middle (allowed)
		"folder/file..html", // two dots in subfolder (allowed)
		"config..backup",    // two dots (allowed)
		"test..test..txt",   // multiple two-dot sequences (allowed)
	}

	for _, filename := range testFiles {
		w, err := zw.Create(filename)
		assert.NoError(t, err)
		_, err = w.Write([]byte("test content"))
		assert.NoError(t, err)
	}

	// Close the zip writer
	err = zw.Close()
	assert.NoError(t, err)
	zf.Close()

	// Test extraction - should succeed
	extractDir := filepath.Join(baseDir, "extracted")
	err = os.MkdirAll(extractDir, 0755)
	assert.NoError(t, err)

	err = Unzip(zipPath, extractDir, false)
	assert.NoError(t, err, "Unzip should succeed with multiple dot filenames")

	// Verify all files were extracted correctly
	for _, filename := range testFiles {
		expectedPath := filepath.Join(extractDir, filename)
		assert.True(t, Exists(expectedPath), "File should exist at: %s", expectedPath)
	}
}

func TestUnzipZipSlipComprehensive(t *testing.T) {
	testCases := []struct {
		name        string
		filenames   []string
		shouldError bool
		description string
	}{
		{
			name: "RelativeTraversalAttacks",
			filenames: []string{
				"../../../etc/passwd",
				"../../malicious.txt",
				"./../../evil.sh",
				"subdir/../../../etc/hosts",
				"../../../../../../../etc/shadow",
				"dir1/dir2/../../../../../../../tmp/backdoor",
				"legitimate/../../../system32/drivers/etc/hosts",
				"data/../../../../../../var/log/auth.log",
			},
			shouldError: true,
			description: "Classic relative path traversal using ../ sequences",
		},
		{
			name: "AbsolutePathAttacks",
			filenames: []string{
				"/etc/passwd",
				"/tmp/malicious.txt",
				"/var/www/html/shell.php",
				"/home/user/.ssh/authorized_keys",
				"/root/.bashrc",
				"/etc/hosts",
				"/usr/bin/backdoor",
			},
			shouldError: true,
			description: "Absolute Unix/Linux paths attempting to write outside destination",
		},
		{
			name: "WindowsPathAttacks",
			filenames: []string{
				"C:\\Windows\\System32\\drivers\\etc\\hosts",
				"C:\\temp\\malicious.exe",
				"D:\\backdoor.bat",
				"\\\\server\\share\\file.txt",
				"C:/Windows/System32/cmd.exe",
				"..\\..\\..\\Windows\\System32\\calc.exe",
				"subdir\\..\\..\\..\\..\\Windows\\notepad.exe",
			},
			shouldError: true,
			description: "Windows-specific paths including drive letters and UNC paths",
		},
		{
			name: "MixedSeparatorAttacks",
			filenames: []string{
				"../../../etc\\passwd",
				"..\\..\\..\\etc/shadow",
				"dir/../..\\..\\system32/file.txt",
				"subdir\\../../../etc\\hosts",
				"valid/../../..\\tmp\\shell.php",
			},
			shouldError: true,
			description: "Mixed forward and backslash separators to bypass basic filtering",
		},
		{
			name: "EdgeCaseAttacks",
			filenames: []string{
				"..",
				"...",
				"....",
				".....",
				"..../malicious.txt",
				"dir/..../backdoor.sh",
				".../file.txt",
				"....\\file.exe",
				"valid/../../../...",
				"test/../../../../....",
			},
			shouldError: true,
			description: "Edge cases with multiple dots and unusual path components",
		},
		{
			name: "NormalizedVsRawPaths",
			filenames: []string{
				"./subdir/../../../etc/passwd",
				"dir1/./dir2/../../../tmp/file",
				"./././../../../var/log/file",
				"subdir/./../../../../../../etc/shadow",
				"test/./../../../../../../../root/.ssh/id_rsa",
				"dir/.//../../../../../../tmp/backdoor",
			},
			shouldError: true,
			description: "Paths that normalize to traversals using ./ components",
		},
		{
			name: "ComplexNestedAttacks",
			filenames: []string{
				"legitdir/subdir/../../../../../../../etc/passwd",
				"data/uploads/../../../../../../var/www/html/shell.php",
				"app/config/../../../../../../home/user/.bashrc",
				"logs/2023/../../../../../../tmp/malicious.sh",
				"backup/old/../../../../../../etc/crontab",
				"cache/temp/../../../../../../usr/bin/evil",
			},
			shouldError: true,
			description: "Complex nested paths that traverse out after legitimate subdirectories",
		},
		{
			name: "CanonicalizedAttacks",
			filenames: []string{
				"/tmp/../../../etc/passwd",
				"/var/log/../../../etc/shadow",
				"C:\\temp\\..\\..\\Windows\\System32\\hosts",
				"/home/user/../../../etc/hosts",
				"/usr/local/../../../tmp/backdoor",
			},
			shouldError: true,
			description: "Absolute paths with traversal components that canonicalize outside destination",
		},
		{
			name: "LegitimateFiles",
			filenames: []string{
				"README.txt",
				"config/app.json",
				"data/users.csv",
				"images/logo.png",
				"scripts/deploy.sh",
				"docs/manual.pdf",
				"lib/utils.js",
				"test/unit_tests.py",
				"logs/application.log",
				"backup/db_backup.sql",
			},
			shouldError: false,
			description: "Legitimate files that should extract successfully",
		},
		{
			name: "LegitimateWithDots",
			filenames: []string{
				"file.txt",
				"config.json",
				"image.png",
				"script.sh",
				"app.config.xml",
				"test..backup.sql",
				"file..old.txt",
				"some..thing.json",
				"config..backup.ini",
				"data...archive.zip",
				"routes/[[...mdxPath]]/index.js",
			},
			shouldError: false,
			description: "Legitimate files containing dots in filenames (not path components)",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			baseDir := t.TempDir()
			zipPath := filepath.Join(baseDir, "test.zip")

			// Create zip file with test filenames
			zf, err := os.Create(zipPath)
			assert.NoError(t, err)
			defer zf.Close()

			zw := zip.NewWriter(zf)

			for _, filename := range tc.filenames {
				w, err := zw.Create(filename)
				assert.NoError(t, err)
				_, err = w.Write([]byte("test content: " + filename))
				assert.NoError(t, err)
			}

			err = zw.Close()
			assert.NoError(t, err)
			zf.Close()

			// Test extraction
			extractDir := filepath.Join(baseDir, "extracted")
			err = os.MkdirAll(extractDir, 0755)
			assert.NoError(t, err)

			err = Unzip(zipPath, extractDir, false)

			if tc.shouldError {
				assert.Error(t, err, "Test case '%s': %s - should fail", tc.name, tc.description)
				if err != nil {
					assert.Contains(t, err.Error(), "invalid file path",
						"Test case '%s': Error should mention invalid file path", tc.name)
				}
			} else {
				assert.NoError(t, err, "Test case '%s': %s - should succeed", tc.name, tc.description)

				// Verify legitimate files were extracted correctly
				for _, filename := range tc.filenames {
					expectedPath := filepath.Join(extractDir, filename)
					assert.True(t, Exists(expectedPath),
						"Test case '%s': File should exist at: %s", tc.name, expectedPath)
				}
			}
		})
	}
}

func TestUnzipZipSlipSpecialCases(t *testing.T) {
	t.Run("EmptyDestination", func(t *testing.T) {
		baseDir := t.TempDir()
		zipPath := filepath.Join(baseDir, "test.zip")

		// Create a simple zip
		zf, err := os.Create(zipPath)
		assert.NoError(t, err)
		defer zf.Close()

		zw := zip.NewWriter(zf)
		w, err := zw.Create("../test.txt")
		assert.NoError(t, err)
		w.Write([]byte("test"))
		zw.Close()
		zf.Close()

		// Test with empty destination
		err = Unzip(zipPath, "", false)
		assert.Error(t, err, "Should fail with empty destination")
	})

	t.Run("NonExistentDestination", func(t *testing.T) {
		baseDir := t.TempDir()
		zipPath := filepath.Join(baseDir, "test.zip")

		// Create a simple zip
		zf, err := os.Create(zipPath)
		assert.NoError(t, err)
		defer zf.Close()

		zw := zip.NewWriter(zf)
		w, err := zw.Create("../test.txt")
		assert.NoError(t, err)
		w.Write([]byte("test"))
		zw.Close()
		zf.Close()

		// Test with non-existent destination
		nonExistentDir := filepath.Join(baseDir, "does", "not", "exist")
		err = Unzip(zipPath, nonExistentDir, false)
		assert.Error(t, err, "Should fail with non-existent destination or protect against traversal")
	})

	t.Run("SymlinkDestination", func(t *testing.T) {
		baseDir := t.TempDir()
		zipPath := filepath.Join(baseDir, "test.zip")
		actualDir := filepath.Join(baseDir, "actual")
		symlinkDir := filepath.Join(baseDir, "symlink")

		// Create actual destination and symlink
		err := os.MkdirAll(actualDir, 0755)
		assert.NoError(t, err)

		// Skip if symlinks not supported (Windows without dev mode)
		err = os.Symlink(actualDir, symlinkDir)
		if err != nil {
			t.Skip("Symlinks not supported on this system")
		}

		// Create malicious zip
		zf, err := os.Create(zipPath)
		assert.NoError(t, err)
		defer zf.Close()

		zw := zip.NewWriter(zf)
		w, err := zw.Create("../malicious.txt")
		assert.NoError(t, err)
		w.Write([]byte("malicious content"))
		zw.Close()
		zf.Close()

		// Test extraction to symlinked destination
		err = Unzip(zipPath, symlinkDir, false)
		assert.Error(t, err, "Should protect against traversal even with symlinked destination")
	})
}
