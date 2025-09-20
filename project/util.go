package project

import (
	"path/filepath"
	"regexp"

	"github.com/agentuity/go-common/sys"
)

var safeNameTransformer = regexp.MustCompile(`[^a-zA-Z0-9_-]`)
var safePythonNameTransformer = regexp.MustCompile(`[^a-zA-Z0-9_]`)
var beginsWithNumber = regexp.MustCompile(`^[0-9]+`)
var removeStartingDashes = regexp.MustCompile(`^[-]+`)
var removeEndingDashes = regexp.MustCompile(`[-]+$`)

// SafeProjectFilename returns a safe filename for a project.
func SafeProjectFilename(name string, isPython bool) string {
	if isPython {
		if beginsWithNumber.MatchString(name) {
			name = beginsWithNumber.ReplaceAllString(name, "")
		}
		name = safePythonNameTransformer.ReplaceAllString(name, "_")
		if removeStartingDashes.MatchString(name) {
			name = removeStartingDashes.ReplaceAllString(name, "")
		}
		if removeEndingDashes.MatchString(name) {
			name = removeEndingDashes.ReplaceAllString(name, "")
		}
		return name
	}
	return safeNameTransformer.ReplaceAllString(name, "-")
}

// GetProjectFilename returns the filename for a project.
func GetProjectFilename(dir string) string {
	return filepath.Join(dir, "agentuity.yaml")
}

// ProjectExists returns true if a project exists in the given directory.
func ProjectExists(dir string) bool {
	fn := GetProjectFilename(dir)
	return sys.Exists(fn)
}
