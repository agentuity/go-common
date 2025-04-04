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
		content  string
		expected []EnvLine
		wantErr  bool
	}{
		{
			name:     "empty buffer",
			content:  "",
			expected: []EnvLine{},
			wantErr:  false,
		},
		{
			name: "valid env content",
			content: `KEY1=value1
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
			got, err := ParseEnvBuffer([]byte(tt.content))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseEnvBuffer() error = %v, wantErr %v", err, tt.wantErr)
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
