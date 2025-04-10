package env

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agentuity/go-common/logger"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestParseEnvFile(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.env")

	tests := []struct {
		name     string
		content  string
		expected []EnvLine
		wantErr  bool
	}{
		{
			name:     "empty file",
			content:  "",
			expected: []EnvLine{},
			wantErr:  false,
		},
		{
			name: "valid env file",
			content: `
KEY1=value1
KEY2="value2"
KEY3='value3'
# This is a comment
KEY4=value with spaces
`,
			expected: []EnvLine{
				{Key: "KEY1", Val: "value1"},
				{Key: "KEY2", Val: "value2"},
				{Key: "KEY3", Val: "value3"},
				{Key: "KEY4", Val: "value with spaces"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := os.WriteFile(tmpFile, []byte(tt.content), 0644)
			assert.NoError(t, err)

			got, err := ParseEnvFile(tmpFile)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseEnvFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			assert.Equal(t, len(tt.expected), len(got))

			for i, expected := range tt.expected {
				if i < len(got) {
					assert.Equal(t, expected.Key, got[i].Key)
					assert.Equal(t, expected.Val, got[i].Val)
				}
			}
		})
	}

	t.Run("non-existent file", func(t *testing.T) {
		got, err := ParseEnvFile(filepath.Join(tmpDir, "nonexistent.env"))
		assert.NoError(t, err)
		assert.Empty(t, got)
	})
}

func TestParseEnvBuffer(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []EnvLine
	}{
		{
			name:     "empty buffer",
			input:    "",
			expected: []EnvLine{},
		},
		{
			name:  "valid env content",
			input: "KEY=value\nOTHER=test",
			expected: []EnvLine{
				{Key: "KEY", Val: "value"},
				{Key: "OTHER", Val: "test"},
			},
		},
		{
			name:  "basic variable interpolation",
			input: "FOO=bar\nBAR=${FOO}",
			expected: []EnvLine{
				{Key: "FOO", Val: "bar"},
				{Key: "BAR", Val: "bar"},
			},
		},
		{
			name:  "default values",
			input: "FOO=${MISSING:-default}\nBAR=${FOO:-backup}",
			expected: []EnvLine{
				{Key: "FOO", Val: "default"},
				{Key: "BAR", Val: "default"},
			},
		},
		{
			name:  "multiple interpolation",
			input: "A=1\nB=${A}/2\nC=${A}/${B}",
			expected: []EnvLine{
				{Key: "A", Val: "1"},
				{Key: "B", Val: "1/2"},
				{Key: "C", Val: "1/1/2"},
			},
		},
		{
			name:  "empty references",
			input: "EMPTY=${}\nFOO=bar\nBAR=${FOO}",
			expected: []EnvLine{
				{Key: "EMPTY", Val: "${}"},
				{Key: "FOO", Val: "bar"},
				{Key: "BAR", Val: "bar"},
			},
		},
		{
			name:  "nested reference",
			input: "FOO=bar\nBAR=${FOO}foo",
			expected: []EnvLine{
				{Key: "FOO", Val: "bar"},
				{Key: "BAR", Val: "barfoo"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEnvBuffer([]byte(tt.input))
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestParseEnvLinesWithComments(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []EnvLineComment
	}{
		{
			name:     "empty buffer",
			input:    "",
			expected: []EnvLineComment{},
		},
		{
			name:  "single comment with variable",
			input: "# This is a test comment\nTEST_KEY=value",
			expected: []EnvLineComment{
				{
					EnvLine: EnvLine{Key: "TEST_KEY", Val: "value"},
					Comment: "This is a test comment",
				},
			},
		},
		{
			name: "multiple comments and variables",
			input: `# Database settings
DB_HOST=localhost
DB_PORT=5432

# API configuration
API_KEY=secret
API_URL=https://api.example.com`,
			expected: []EnvLineComment{
				{
					EnvLine: EnvLine{Key: "DB_HOST", Val: "localhost"},
					Comment: "Database settings",
				},
				{
					EnvLine: EnvLine{Key: "DB_PORT", Val: "5432"},
					Comment: "",
				},
				{
					EnvLine: EnvLine{Key: "API_KEY", Val: "secret"},
					Comment: "API configuration",
				},
				{
					EnvLine: EnvLine{Key: "API_URL", Val: "https://api.example.com"},
					Comment: "",
				},
			},
		},
		{
			name: "comment reset on empty line",
			input: `# First comment
KEY1=value1

# Second comment
KEY2=value2`,
			expected: []EnvLineComment{
				{
					EnvLine: EnvLine{Key: "KEY1", Val: "value1"},
					Comment: "First comment",
				},
				{
					EnvLine: EnvLine{Key: "KEY2", Val: "value2"},
					Comment: "Second comment",
				},
			},
		},
		{
			name: "variable interpolation with comments",
			input: `# Base URL setting
BASE_URL=https://example.com
# API endpoint configuration
API_ENDPOINT=${BASE_URL}/api`,
			expected: []EnvLineComment{
				{
					EnvLine: EnvLine{Key: "BASE_URL", Val: "https://example.com"},
					Comment: "Base URL setting",
				},
				{
					EnvLine: EnvLine{Key: "API_ENDPOINT", Val: "https://example.com/api"},
					Comment: "API endpoint configuration",
				},
			},
		},
		{
			name: "multiple comment lines",
			input: `# First comment line
# Second comment line
# Third comment line
MULTI_COMMENT=value`,
			expected: []EnvLineComment{
				{
					EnvLine: EnvLine{Key: "MULTI_COMMENT", Val: "value"},
					Comment: "Third comment line",
				},
			},
		},
		{
			name: "comments with special characters",
			input: `# Comment with "quotes" and 'apostrophes'
SPECIAL=value
# Comment with # hash # symbols #
HASH=value`,
			expected: []EnvLineComment{
				{
					EnvLine: EnvLine{Key: "SPECIAL", Val: "value"},
					Comment: `Comment with "quotes" and 'apostrophes'`,
				},
				{
					EnvLine: EnvLine{Key: "HASH", Val: "value"},
					Comment: "Comment with # hash # symbols #",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEnvLinesWithComments([]byte(tt.input))
			assert.NoError(t, err)
			assert.Equal(t, len(tt.expected), len(got), "number of entries should match")

			for i, expected := range tt.expected {
				assert.Equal(t, expected.Key, got[i].Key, "Key should match for entry %d", i)
				assert.Equal(t, expected.Val, got[i].Val, "Value should match for entry %d", i)
				assert.Equal(t, expected.Comment, got[i].Comment, "Comment should match for entry %d", i)
			}
		})
	}
}

func TestProcessEnvLine(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		expected EnvLine
	}{
		{
			name:     "simple key value",
			env:      "KEY=value",
			expected: EnvLine{Key: "KEY", Val: "value"},
		},
		{
			name:     "quoted value",
			env:      "KEY=\"value\"",
			expected: EnvLine{Key: "KEY", Val: "value"},
		},
		{
			name:     "single quoted value",
			env:      "KEY='value'",
			expected: EnvLine{Key: "KEY", Val: "value"},
		},
		{
			name:     "value with spaces",
			env:      "KEY=value with spaces",
			expected: EnvLine{Key: "KEY", Val: "value with spaces"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProcessEnvLine(tt.env)
			assert.Equal(t, tt.expected.Key, got.Key)
			assert.Equal(t, tt.expected.Val, got.Val)
		})
	}
}

func TestDequote(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no quotes",
			input:    "value",
			expected: "value",
		},
		{
			name:     "double quotes",
			input:    "\"value\"",
			expected: "value",
		},
		{
			name:     "single quotes",
			input:    "'value'",
			expected: "value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dequote(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestMustQuote(t *testing.T) {
	tests := []struct {
		name     string
		val      string
		expected bool
	}{
		{
			name:     "no special chars",
			val:      "value",
			expected: false,
		},
		{
			name:     "contains double quote",
			val:      "value\"quote",
			expected: true,
		},
		{
			name:     "contains newline",
			val:      "value\\nwith\\nnewline",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mustQuote(tt.val)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestEncodeOSEnv(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		val      string
		expected string
	}{
		{
			name:     "simple value",
			key:      "KEY",
			val:      "value",
			expected: "KEY=value",
		},
		{
			name:     "value with newline",
			key:      "KEY",
			val:      "value\nwith\nnewline",
			expected: "KEY=\"value\\nwith\\nnewline\"",
		},
		{
			name:     "value with single quote",
			key:      "KEY",
			val:      "value'with'quote",
			expected: "KEY=value\\'with\\'quote",
		},
		{
			name:     "value with double quote",
			key:      "KEY",
			val:      "value\"with\"quote",
			expected: "KEY='value\"with\"quote'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EncodeOSEnv(tt.key, tt.val)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestWriteEnvFile(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "out.env")

	envs := []EnvLine{
		{Key: "KEY1", Val: "value1"},
		{Key: "KEY2", Val: "value2"},
		{Key: "KEY3", Val: "value with spaces"},
		{Key: "KEY4", Val: "value\nwith\nnewline"},
	}

	err := WriteEnvFile(tmpFile, envs)
	assert.NoError(t, err)

	content, err := os.ReadFile(tmpFile)
	assert.NoError(t, err)

	gotEnvs, err := ParseEnvBuffer(content)
	assert.NoError(t, err)

	assert.Equal(t, len(envs), len(gotEnvs))
	for i, env := range envs {
		assert.Equal(t, env.Key, gotEnvs[i].Key)
	}
}

func TestFlagOrEnv(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("test-flag", "", "Test flag")

	cmd.Flags().Set("test-flag", "flag-value")
	assert.Equal(t, "flag-value", FlagOrEnv(cmd, "test-flag", "TEST_ENV", "default"))

	cmd.Flags().Set("test-flag", "")
	os.Setenv("TEST_ENV", "env-value")
	defer os.Unsetenv("TEST_ENV")
	assert.Equal(t, "env-value", FlagOrEnv(cmd, "test-flag", "TEST_ENV", "default"))

	os.Unsetenv("TEST_ENV")
	assert.Equal(t, "default", FlagOrEnv(cmd, "test-flag", "TEST_ENV", "default"))
}

func TestLogLevel(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("log-level", "", "Log level")

	testCases := []struct {
		name      string
		flagValue string
		envValue  string
		expected  logger.LogLevel
	}{
		{"debug level via flag", "debug", "", logger.LevelDebug},
		{"debug level via env", "", "DEBUG", logger.LevelDebug},
		{"warn level via flag", "warn", "", logger.LevelWarn},
		{"warn level via env", "", "WARN", logger.LevelWarn},
		{"error level via flag", "error", "", logger.LevelError},
		{"error level via env", "", "ERROR", logger.LevelError},
		{"trace level via flag", "trace", "", logger.LevelTrace},
		{"trace level via env", "", "TRACE", logger.LevelTrace},
		{"default level", "", "", logger.LevelInfo},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd.Flags().Set("log-level", "")
			os.Unsetenv("AGENTUITY_LOG_LEVEL")

			if tc.flagValue != "" {
				cmd.Flags().Set("log-level", tc.flagValue)
			}
			if tc.envValue != "" {
				os.Setenv("AGENTUITY_LOG_LEVEL", tc.envValue)
				defer os.Unsetenv("AGENTUITY_LOG_LEVEL")
			}

			level := LogLevel(cmd)
			assert.Equal(t, tc.expected, level)
		})
	}
}

type testLogger struct {
	t      *testing.T
	prefix string
}

func (l testLogger) Logf(format string, args ...interface{}) {
	l.t.Logf(l.prefix+format, args...)
}

func TestInterpolateValue(t *testing.T) {
	// Set up some OS environment variables for testing
	os.Setenv("TEST_OS_VAR", "os_value")
	os.Setenv("TEST_OS_VAR2", "os_value2")
	os.Setenv("USER", "jhaynie")
	defer func() {
		os.Unsetenv("TEST_OS_VAR")
		os.Unsetenv("TEST_OS_VAR2")
		os.Unsetenv("USER")
	}()

	tests := []struct {
		name     string
		val      string
		envMap   map[string]string
		expected string
	}{
		{
			name:     "no interpolation needed",
			val:      "simple value",
			envMap:   map[string]string{},
			expected: "simple value",
		},
		{
			name:     "basic interpolation",
			val:      "prefix ${FOO} suffix",
			envMap:   map[string]string{"FOO": "bar"},
			expected: "prefix bar suffix",
		},
		{
			name:     "missing variable",
			val:      "prefix ${FOO} suffix",
			envMap:   map[string]string{},
			expected: "prefix ${FOO} suffix",
		},
		{
			name:     "empty variable reference",
			val:      "prefix ${} suffix",
			envMap:   map[string]string{},
			expected: "prefix ${} suffix",
		},
		{
			name:     "default value when missing",
			val:      "prefix ${FOO:-default} suffix",
			envMap:   map[string]string{},
			expected: "prefix default suffix",
		},
		{
			name:     "default value not used when exists",
			val:      "prefix ${FOO:-default} suffix",
			envMap:   map[string]string{"FOO": "bar"},
			expected: "prefix bar suffix",
		},
		{
			name:     "multiple interpolations",
			val:      "${FOO}/${BAR}",
			envMap:   map[string]string{"FOO": "foo", "BAR": "bar"},
			expected: "foo/bar",
		},
		{
			name:     "adjacent variables",
			val:      "${FOO}${BAR}",
			envMap:   map[string]string{"FOO": "foo", "BAR": "bar"},
			expected: "foobar",
		},
		// New test cases for env: prefix
		{
			name:     "basic OS env lookup",
			val:      "prefix ${env:TEST_OS_VAR} suffix",
			envMap:   map[string]string{},
			expected: "prefix os_value suffix",
		},
		{
			name:     "OS env with default when exists",
			val:      "prefix ${env:TEST_OS_VAR:-default} suffix",
			envMap:   map[string]string{},
			expected: "prefix os_value suffix",
		},
		{
			name:     "OS env with default when missing",
			val:      "prefix ${env:MISSING_VAR:-default} suffix",
			envMap:   map[string]string{},
			expected: "prefix default suffix",
		},
		{
			name:     "OS env missing without default",
			val:      "prefix ${env:MISSING_VAR} suffix",
			envMap:   map[string]string{},
			expected: "prefix ${env:MISSING_VAR} suffix",
		},
		{
			name:     "mix of env and regular vars",
			val:      "${FOO}/${env:TEST_OS_VAR}/${BAR}",
			envMap:   map[string]string{"FOO": "foo", "BAR": "bar"},
			expected: "foo/os_value/bar",
		},
		{
			name:     "adjacent OS env vars",
			val:      "${env:TEST_OS_VAR}${env:TEST_OS_VAR2}",
			envMap:   map[string]string{},
			expected: "os_valueos_value2",
		},
		{
			name:     "nested interpolation with env prefix",
			val:      "${DATABASE_URL_${env:USER}}",
			envMap:   map[string]string{"DATABASE_URL_jhaynie": "foo"},
			expected: "foo",
		},
		{
			name:     "full database url example",
			val:      "DATABASE_URL=${DATABASE_URL_${env:USER}}",
			envMap:   map[string]string{"DATABASE_URL_jhaynie": "foo"},
			expected: "DATABASE_URL=foo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Input: %s", tt.val)
			t.Logf("EnvMap: %v", tt.envMap)
			logger := testLogger{t: t, prefix: "DEBUG: "}
			got := interpolateValueWithLogger(tt.val, tt.envMap, logger)
			t.Logf("Got: %s", got)
			assert.Equal(t, tt.expected, got)
		})
	}
}
