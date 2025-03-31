package env

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestParseEnvFile(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.env")

	content := []string{
		"KEY1=value1",
		"KEY2=\"quoted value\"",
		"KEY3='single quoted value'",
		"# This is a comment",
		"",
		"KEY4=value with spaces",
		"EMPTY=",
	}

	err := os.WriteFile(testFile, []byte(strings.Join(content, "\n")), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	envs, err := ParseEnvFile(testFile)
	if err != nil {
		t.Fatalf("ParseEnvFile() error = %v", err)
	}

	expectedCount := 5
	if len(envs) != expectedCount {
		t.Errorf("ParseEnvFile() returned %d lines, want %d", len(envs), expectedCount)
	}

	for _, env := range envs {
		switch env.Key {
		case "KEY1":
			if env.Val != "value1" {
				t.Errorf("ParseEnvFile() KEY1 = %q, want %q", env.Val, "value1")
			}
		case "KEY2":
			if env.Val != "quoted value" {
				t.Errorf("ParseEnvFile() KEY2 = %q, want %q", env.Val, "quoted value")
			}
		case "KEY3":
			if env.Val != "single quoted value" {
				t.Errorf("ParseEnvFile() KEY3 = %q, want %q", env.Val, "single quoted value")
			}
		case "KEY4":
			if env.Val != "value with spaces" {
				t.Errorf("ParseEnvFile() KEY4 = %q, want %q", env.Val, "value with spaces")
			}
		case "EMPTY":
			if env.Val != "" {
				t.Errorf("ParseEnvFile() EMPTY = %q, want %q", env.Val, "")
			}
		default:
			t.Errorf("ParseEnvFile() unexpected key: %q", env.Key)
		}
	}
}

func TestParseEnvFileNonExistent(t *testing.T) {
	envs, err := ParseEnvFile("/non/existent/file.env")
	if err != nil {
		t.Errorf("ParseEnvFile() with non-existent file should not return error, got %v", err)
	}

	if len(envs) != 0 {
		t.Errorf("ParseEnvFile() with non-existent file should return empty slice, got %d items", len(envs))
	}
}

func TestDequote(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "double quoted",
			input:    `"quoted value"`,
			expected: "quoted value",
		},
		{
			name:     "single quoted",
			input:    `'quoted value'`,
			expected: "quoted value",
		},
		{
			name:     "unquoted",
			input:    `unquoted value`,
			expected: "unquoted value",
		},
		{
			name:     "empty string",
			input:    ``,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dequote(tt.input)
			if result != tt.expected {
				t.Errorf("dequote(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseEnvValue(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{
			name:  "simple value",
			key:   "KEY",
			value: "value",
		},
		{
			name:  "empty value",
			key:   "EMPTY",
			value: "",
		},
		{
			name:  "value with spaces",
			key:   "SPACES",
			value: "value with spaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseEnvValue(tt.key, tt.value)
			if result.Key != tt.key {
				t.Errorf("ParseEnvValue() key = %q, want %q", result.Key, tt.key)
			}
			if result.Val != tt.value {
				t.Errorf("ParseEnvValue() value = %q, want %q", result.Val, tt.value)
			}
		})
	}
}

func TestProcessEnvLine(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected EnvLine
	}{
		{
			name:  "simple value",
			input: "KEY=value",
			expected: EnvLine{
				Key: "KEY",
				Val: "value",
			},
		},
		{
			name:  "quoted value",
			input: "KEY=\"quoted value\"",
			expected: EnvLine{
				Key: "KEY",
				Val: "quoted value",
			},
		},
		{
			name:  "single quoted value",
			input: "KEY='single quoted value'",
			expected: EnvLine{
				Key: "KEY",
				Val: "single quoted value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ProcessEnvLine(tt.input)
			if result.Key != tt.expected.Key {
				t.Errorf("ProcessEnvLine() key = %q, want %q", result.Key, tt.expected.Key)
			}
			if result.Val != tt.expected.Val {
				t.Errorf("ProcessEnvLine() value = %q, want %q", result.Val, tt.expected.Val)
			}
		})
	}
}

func TestParseEnvBuffer(t *testing.T) {
	content := []byte(`KEY1=value1
KEY2="quoted value"
KEY3='single quoted value'
# This is a comment

KEY4=value with spaces
EMPTY=`)

	envs, err := ParseEnvBuffer(content)
	if err != nil {
		t.Fatalf("ParseEnvBuffer() error = %v", err)
	}

	expectedCount := 5
	if len(envs) != expectedCount {
		t.Errorf("ParseEnvBuffer() returned %d lines, want %d", len(envs), expectedCount)
	}

	for _, env := range envs {
		switch env.Key {
		case "KEY1":
			if env.Val != "value1" {
				t.Errorf("ParseEnvBuffer() KEY1 = %q, want %q", env.Val, "value1")
			}
		case "KEY2":
			if env.Val != "quoted value" {
				t.Errorf("ParseEnvBuffer() KEY2 = %q, want %q", env.Val, "quoted value")
			}
		case "KEY3":
			if env.Val != "single quoted value" {
				t.Errorf("ParseEnvBuffer() KEY3 = %q, want %q", env.Val, "single quoted value")
			}
		case "KEY4":
			if env.Val != "value with spaces" {
				t.Errorf("ParseEnvBuffer() KEY4 = %q, want %q", env.Val, "value with spaces")
			}
		case "EMPTY":
			if env.Val != "" {
				t.Errorf("ParseEnvBuffer() EMPTY = %q, want %q", env.Val, "")
			}
		default:
			t.Errorf("ParseEnvBuffer() unexpected key: %q", env.Key)
		}
	}
}

func TestMustQuote(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "contains double quote",
			input:    `value with "quotes"`,
			expected: true,
		},
		{
			name:     "contains newline",
			input:    `value with \n newline`,
			expected: true,
		},
		{
			name:     "simple value",
			input:    `simple value`,
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
			result := mustQuote(tt.input)
			if result != tt.expected {
				t.Errorf("mustQuote(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestEncodeOSEnv(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		expected string
	}{
		{
			name:     "simple value",
			key:      "KEY",
			value:    "value",
			expected: "KEY=value",
		},
		{
			name:     "value with spaces",
			key:      "KEY",
			value:    "value with spaces",
			expected: "KEY=value with spaces",
		},
		{
			name:     "value with double quotes",
			key:      "KEY",
			value:    `value with "quotes"`,
			expected: `KEY='value with "quotes"'`,
		},
		{
			name:     "value with newlines",
			key:      "KEY",
			value:    "value with\nnewline",
			expected: `KEY="value with\nnewline"`,
		},
		{
			name:     "value with single quotes",
			key:      "KEY",
			value:    "value with 'quotes'",
			expected: `KEY=value with \'quotes\'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EncodeOSEnv(tt.key, tt.value)
			if result != tt.expected {
				t.Errorf("EncodeOSEnv(%q, %q) = %q, want %q", tt.key, tt.value, result, tt.expected)
			}
		})
	}
}

func TestWriteEnvFile(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "output.env")

	envs := []EnvLine{
		{Key: "KEY1", Val: "value1"},
		{Key: "KEY2", Val: "value with spaces"},
		{Key: "KEY3", Val: "value with \"quotes\""},
		{Key: "KEY4", Val: "value with 'quotes'"},
		{Key: "KEY5", Val: "value with\nnewline"},
	}

	err := WriteEnvFile(testFile, envs)
	if err != nil {
		t.Fatalf("WriteEnvFile() error = %v", err)
	}

	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}

	lines := strings.Split(string(content), "\n")
	if len(lines) != len(envs)+1 {
		t.Errorf("WriteEnvFile() wrote %d lines, want %d", len(lines), len(envs)+1)
	}

	parsedEnvs, err := ParseEnvFile(testFile)
	if err != nil {
		t.Fatalf("ParseEnvFile() error = %v", err)
	}

	if len(parsedEnvs) != len(envs) {
		t.Errorf("WriteEnvFile() + ParseEnvFile() returned %d lines, want %d", len(parsedEnvs), len(envs))
	}

	envMap := make(map[string]string)
	for _, env := range parsedEnvs {
		envMap[env.Key] = env.Val
	}

	for _, env := range envs {
		if val, ok := envMap[env.Key]; !ok {
			t.Errorf("WriteEnvFile() + ParseEnvFile() missing key %q", env.Key)
		} else if val != env.Val {
			t.Errorf("WriteEnvFile() + ParseEnvFile() key %q = %q, want %q", env.Key, val, env.Val)
		}
	}
}

func TestFlagOrEnv(t *testing.T) {
	cmd := &cobra.Command{
		Use: "test",
	}
	cmd.Flags().String("flag", "", "test flag")

	tests := []struct {
		name         string
		flagValue    string
		envValue     string
		defaultValue string
		expected     string
	}{
		{
			name:         "flag value",
			flagValue:    "flag-value",
			envValue:     "env-value",
			defaultValue: "default-value",
			expected:     "flag-value",
		},
		{
			name:         "env value",
			flagValue:    "",
			envValue:     "env-value",
			defaultValue: "default-value",
			expected:     "env-value",
		},
		{
			name:         "default value",
			flagValue:    "",
			envValue:     "",
			defaultValue: "default-value",
			expected:     "default-value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.flagValue != "" {
				cmd.Flags().Set("flag", tt.flagValue)
			} else {
				cmd.Flags().Set("flag", "")
			}

			if tt.envValue != "" {
				os.Setenv("TEST_ENV", tt.envValue)
			} else {
				os.Unsetenv("TEST_ENV")
			}

			result := FlagOrEnv(cmd, "flag", "TEST_ENV", tt.defaultValue)
			if result != tt.expected {
				t.Errorf("FlagOrEnv() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestLogLevel(t *testing.T) {
	cmd := &cobra.Command{
		Use: "test",
	}
	cmd.Flags().String("log-level", "", "log level")

	tests := []struct {
		name      string
		flagValue string
		envValue  string
		expected  string
	}{
		{
			name:      "debug level",
			flagValue: "debug",
			expected:  "debug",
		},
		{
			name:      "warn level",
			flagValue: "warn",
			expected:  "warn",
		},
		{
			name:      "error level",
			flagValue: "error",
			expected:  "error",
		},
		{
			name:      "trace level",
			flagValue: "trace",
			expected:  "trace",
		},
		{
			name:      "info level (default)",
			flagValue: "",
			expected:  "info",
		},
		{
			name:      "unknown level",
			flagValue: "unknown",
			expected:  "info", // Falls back to info
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.flagValue != "" {
				cmd.Flags().Set("log-level", tt.flagValue)
			} else {
				cmd.Flags().Set("log-level", "")
			}

			os.Unsetenv("AGENTUITY_LOG_LEVEL")

			level := LogLevel(cmd)

			var levelStr string
			switch level {
			case 0:
				levelStr = "debug"
			case 1:
				levelStr = "info"
			case 2:
				levelStr = "warn"
			case 3:
				levelStr = "error"
			case 4:
				levelStr = "trace"
			default:
				levelStr = "unknown"
			}

			if tt.expected == "info" && levelStr != "info" {
				t.Errorf("LogLevel() = %q, want %q", levelStr, tt.expected)
			} else if tt.expected == "debug" && levelStr != "debug" {
				t.Errorf("LogLevel() = %q, want %q", levelStr, tt.expected)
			} else if tt.expected == "warn" && levelStr != "warn" {
				t.Errorf("LogLevel() = %q, want %q", levelStr, tt.expected)
			} else if tt.expected == "error" && levelStr != "error" {
				t.Errorf("LogLevel() = %q, want %q", levelStr, tt.expected)
			} else if tt.expected == "trace" && levelStr != "trace" {
				t.Errorf("LogLevel() = %q, want %q", levelStr, tt.expected)
			}
		})
	}
}

func TestNewLogger(t *testing.T) {
	cmd := &cobra.Command{
		Use: "test",
	}
	cmd.Flags().String("log-level", "", "log level")

	logLevels := []string{"debug", "info", "warn", "error", "trace"}

	for _, level := range logLevels {
		t.Run(level, func(t *testing.T) {
			cmd.Flags().Set("log-level", level)
			logger := NewLogger(cmd)

			if logger == nil {
				t.Errorf("NewLogger() returned nil")
			}
		})
	}
}

func TestNewTelemetry(t *testing.T) {
	cmd := &cobra.Command{
		Use: "test",
	}
	cmd.Flags().Bool("no-telemetry", false, "disable telemetry")
	cmd.Flags().String("otlp-url", "", "otlp url")
	cmd.Flags().String("otlp-shared-secret", "", "otlp shared secret")

	t.Run("telemetry disabled", func(t *testing.T) {
		cmd.Flags().Set("no-telemetry", "true")

		ctx := context.Background()
		telemetryCtx, logger, shutdown, err := NewTelemetry(ctx, cmd, "test-service")

		if err != nil {
			t.Errorf("NewTelemetry() error = %v", err)
		}

		if telemetryCtx == nil {
			t.Errorf("NewTelemetry() returned nil context")
		}

		if logger == nil {
			t.Errorf("NewTelemetry() returned nil logger")
		}

		if shutdown == nil {
			t.Errorf("NewTelemetry() returned nil shutdown function")
		} else {
			shutdown()
		}
	})

	t.Run("missing URL", func(t *testing.T) {
		cmd.Flags().Set("no-telemetry", "false")
		cmd.Flags().Set("otlp-url", "")
		cmd.Flags().Set("otlp-shared-secret", "secret")

		ctx := context.Background()
		_, _, _, err := NewTelemetry(ctx, cmd, "test-service")

		if err == nil {
			t.Errorf("NewTelemetry() should return error with missing URL")
		}
	})

	t.Run("missing shared secret", func(t *testing.T) {
		cmd.Flags().Set("no-telemetry", "false")
		cmd.Flags().Set("otlp-url", "https://example.com")
		cmd.Flags().Set("otlp-shared-secret", "")

		ctx := context.Background()
		_, _, _, err := NewTelemetry(ctx, cmd, "test-service")

		if err == nil {
			t.Errorf("NewTelemetry() should return error with missing shared secret")
		}
	})
}
