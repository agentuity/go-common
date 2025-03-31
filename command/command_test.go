package command

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentuity/go-common/logger"
)

func TestParseLastLines(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.log")

	content := []string{
		"line 1",
		"line 2",
		"line 3",
		"line 4",
		"line 5",
	}

	err := os.WriteFile(testFile, []byte(strings.Join(content, "\n")), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tests := []struct {
		name     string
		lines    int
		expected string
	}{
		{
			name:     "get all lines",
			lines:    5,
			expected: strings.Join(content, "\n"),
		},
		{
			name:     "get last 3 lines",
			lines:    3,
			expected: strings.Join(content[2:], "\n"),
		},
		{
			name:     "get more lines than exist",
			lines:    10,
			expected: strings.Join(content, "\n"),
		},
		{
			name:     "get 0 lines",
			lines:    0,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseLastLines(testFile, tt.lines)
			if err != nil {
				t.Errorf("parseLastLines() error = %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("parseLastLines() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestParseLastLinesWithNonExistentFile(t *testing.T) {
	_, err := parseLastLines("/non/existent/file.txt", 5)
	if err == nil {
		t.Error("parseLastLines() with non-existent file should return error")
	}
}

func TestLooksLikeJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "object JSON",
			input:    `{"key": "value"}`,
			expected: true,
		},
		{
			name:     "array JSON",
			input:    `[1, 2, 3]`,
			expected: true,
		},
		{
			name:     "JSON with whitespace",
			input:    `  {"key": "value"}`,
			expected: true,
		},
		{
			name:     "not JSON",
			input:    `not json`,
			expected: false,
		},
		{
			name:     "empty string",
			input:    ``,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := looksLikeJSON(tt.input)
			if result != tt.expected {
				t.Errorf("looksLikeJSON(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatCmd(t *testing.T) {
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	os.Args = []string{"/path/to/executable"}

	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name:     "simple args",
			args:     []string{"arg1", "arg2", "arg3"},
			expected: "/path/to/executable arg1 arg2 arg3\n",
		},
		{
			name:     "args with JSON",
			args:     []string{"arg1", `{"key": "value"}`, "arg3"},
			expected: "/path/to/executable arg1 '{\"key\": \"value\"}' arg3\n",
		},
		{
			name:     "args with JSON array",
			args:     []string{"arg1", `[1, 2, 3]`, "arg3"},
			expected: "/path/to/executable arg1 '[1, 2, 3]' arg3\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatCmd(tt.args)
			if result != tt.expected {
				t.Errorf("formatCmd() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGetExecutable(t *testing.T) {
	executable := GetExecutable()
	if executable == "" {
		t.Error("GetExecutable() returned empty string")
	}
}

func TestForkResultString(t *testing.T) {
	result := &ForkResult{
		Duration:      time.Second * 5,
		LogFileBundle: "/path/to/logs.tar.gz",
	}

	str := result.String()
	if !strings.Contains(str, "Duration: 5s") {
		t.Errorf("ForkResult.String() = %q, should contain duration", str)
	}

	if !strings.Contains(str, "/path/to/logs.tar.gz") {
		t.Errorf("ForkResult.String() = %q, should contain log file path", str)
	}
}

func TestForkWithMinimalArgs(t *testing.T) {
	args := ForkArgs{
		Command: "version", // A command that should exist and complete quickly
		Log:     logger.NewConsoleLogger(logger.LevelInfo),
		Context: context.Background(),
	}

	_, err := Fork(args)
	if err != nil {
		t.Logf("Fork returned error: %v", err)
	}
}

func TestForkWithCustomDir(t *testing.T) {
	tempDir := t.TempDir()

	args := ForkArgs{
		Command:  "version",
		Log:      logger.NewConsoleLogger(logger.LevelInfo),
		Context:  context.Background(),
		Dir:      tempDir,
		SaveLogs: true,
	}

	_, err := Fork(args)
	if err != nil {
		t.Logf("Fork returned error: %v", err)
	}

	files, err := os.ReadDir(tempDir)
	if err != nil {
		t.Logf("Error reading directory: %v", err)
	}

	for _, file := range files {
		t.Logf("Found file: %s", file.Name())
	}
}
