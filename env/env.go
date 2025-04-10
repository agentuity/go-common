package env

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/agentuity/go-common/logger"
	"github.com/agentuity/go-common/telemetry"
	"github.com/spf13/cobra"
)

type EnvLine struct {
	Key string `json:"key"`
	Val string `json:"val"`
}

// ParseEnvFile parses an environment file and returns a list of EnvLine structs.
func ParseEnvFile(filename string) ([]EnvLine, error) {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return []EnvLine{}, nil
	}
	buf, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return ParseEnvBuffer(buf)
}

func dequote(s string) string {
	v := s
	if strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'") {
		v = strings.TrimLeft(v, "'")
		v = strings.TrimRight(v, "'")
	} else if strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`) {
		v = strings.TrimLeft(v, `"`)
		v = strings.TrimRight(v, `"`)
	}
	return v
}

// isLetter returns true if the byte is a letter (A-Z or a-z)
func isLetter(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

// isNumber returns true if the byte is a number (0-9)
func isNumber(c byte) bool {
	return c >= '0' && c <= '9'
}

func ParseEnvValue(key, val string) EnvLine {
	return EnvLine{
		Key: key,
		Val: val,
	}
}

// ProcessEnvLine processes an environment variable line and returns an EnvLine struct with the key, value, and secret flag set.
func ProcessEnvLine(env string) EnvLine {
	tok := strings.SplitN(env, "=", 2)
	if len(tok) < 2 {
		return EnvLine{Key: env, Val: ""}
	}
	key := tok[0]
	val := dequote(tok[1])
	return ParseEnvValue(key, val)
}

type reference struct {
	varName      string
	defaultValue string
	preserve     bool
}

func findClosingBrace(input string, start int) int {
	braceCount := 1
	for i := start; i < len(input); i++ {
		if input[i] == '{' {
			braceCount++
		} else if input[i] == '}' {
			braceCount--
			if braceCount == 0 {
				// Check if this is a valid reference by looking at what's inside
				inner := input[start:i]
				if strings.Contains(inner, "}") {
					// If we find a } inside, this is malformed
					return -1
				}
				return i
			}
		}
	}
	return -1
}

func parseReference(ref string) reference {
	// Remove ${ and }
	inner := ref[2 : len(ref)-1]

	// Split on :- for default value
	parts := strings.SplitN(inner, ":-", 2)

	varName := parts[0]
	defaultValue := ""
	if len(parts) > 1 {
		defaultValue = parts[1]
	}

	return reference{
		varName:      varName,
		defaultValue: defaultValue,
	}
}

func interpolateValue(input string, envMap map[string]string) string {
	if input == "" {
		return input
	}

	// For malformed inputs, return as-is
	if strings.Count(input, "${") != strings.Count(input, "}") {
		return input
	}

	var result strings.Builder
	lastPos := 0

	for i := 0; i < len(input); i++ {
		if i+1 < len(input) && input[i] == '$' && input[i+1] == '{' {
			// Write text before the reference
			result.WriteString(input[lastPos:i])

			// Find closing brace
			end := findClosingBrace(input, i+2)
			if end == -1 {
				// No closing brace - preserve rest of string
				result.WriteString(input[i:])
				return result.String()
			}

			// Extract and parse the reference
			refStr := input[i : end+1]
			ref := parseReference(refStr)

			// Handle empty reference
			if ref.varName == "" {
				result.WriteString("${}")
				i = end
				lastPos = end + 1
				continue
			}

			// Check if this is an OS environment lookup
			if strings.HasPrefix(ref.varName, "env:") {
				// Strip the env: prefix and look up in OS environment
				envKey := strings.TrimPrefix(ref.varName, "env:")
				val := os.Getenv(envKey)
				if val == "" && ref.defaultValue != "" {
					// Use default value if provided and env var is empty
					result.WriteString(ref.defaultValue)
				} else if val == "" {
					// Preserve the original reference if no default and env var is empty
					result.WriteString(refStr)
				} else {
					result.WriteString(val)
				}
			} else {
				// Regular envMap lookup
				val, exists := envMap[ref.varName]
				if !exists || val == "" {
					if ref.defaultValue != "" {
						// Use default value if provided
						result.WriteString(ref.defaultValue)
					} else {
						// Preserve the original reference if no default and variable missing
						result.WriteString(refStr)
					}
				} else {
					result.WriteString(val)
				}
			}

			i = end
			lastPos = end + 1
		}
	}

	// Add remaining text
	result.WriteString(input[lastPos:])
	return result.String()
}

// ParseEnvBuffer parses an environment buffer and returns a list of EnvLine structs.
func ParseEnvBuffer(buf []byte) ([]EnvLine, error) {
	if len(buf) == 0 {
		return make([]EnvLine, 0), nil
	}
	var envs []EnvLine
	var envMap = make(map[string]string)

	// First pass: Build environment map and process interpolation
	lines := strings.Split(string(buf), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		env := ProcessEnvLine(line)
		if env.Key != "" {
			// Interpolate the value using the current environment map
			env.Val = interpolateValue(env.Val, envMap)
			// Update the environment map with the interpolated value
			envMap[env.Key] = env.Val
			envs = append(envs, env)
		}
	}

	// Second pass: Re-interpolate all values with the complete environment map
	for i := range envs {
		envs[i].Val = interpolateValue(envs[i].Val, envMap)
	}

	return envs, nil
}

func mustQuote(val string) bool {
	if strings.Contains(val, `"`) {
		return true
	}
	if strings.Contains(val, "\\n") {
		return true
	}
	return false
}

type callback func(key, val string) string

// EncodeOSEnvFunc encodes an environment variable for use in an OS environment using a custom sprintf function.
func EncodeOSEnvFunc(key, val string, fn callback) string {
	val = strings.ReplaceAll(val, "\n", "\\n")
	val = strings.ReplaceAll(val, "'", "\\'")
	if mustQuote(val) {
		if strings.Contains(val, `"`) {
			val = `'` + val + `'`
		} else {
			val = `"` + val + `"`
		}
	}
	return fn(key, val)
}

// EncodeOSEnv encodes an environment variable for use in an OS environment.
func EncodeOSEnv(key, val string) string {
	return EncodeOSEnvFunc(key, val, func(key, val string) string {
		return fmt.Sprintf(`%s=%s`, key, val)
	})
}

// WriteEnvFile writes an environment file to a file.
func WriteEnvFile(fn string, envs []EnvLine) error {
	of, err := os.Create(fn)
	if err != nil {
		return err
	}
	defer of.Close()
	for _, el := range envs {
		fmt.Fprintln(of, EncodeOSEnv(el.Key, el.Val))
	}
	return of.Close()
}

// FlagOrEnv will try and get a flag from the cobra.Command and if not found, look it up in the environment
// and fallback to defaultValue if non found
func FlagOrEnv(cmd *cobra.Command, flagName string, envName string, defaultValue string) string {
	flagValue, _ := cmd.Flags().GetString(flagName)
	if flagValue != "" {
		return flagValue
	}
	if val, ok := os.LookupEnv(envName); ok {
		return val
	}
	return defaultValue
}

func LogLevel(cmd *cobra.Command) logger.LogLevel {
	level := FlagOrEnv(cmd, "log-level", "AGENTUITY_LOG_LEVEL", "info")
	switch level {
	case "debug", "DEBUG":
		return logger.LevelDebug
	case "warn", "WARN":
		return logger.LevelWarn
	case "error", "ERROR":
		return logger.LevelError
	case "trace", "TRACE":
		return logger.LevelTrace
	}
	return logger.LevelInfo
}

// NewLogger returns a console logger by first checking the cobra.Command log-level flag, then use the
// AGENTUITY_LOG_LEVEL environment value and falling back to the info logger level
func NewLogger(cmd *cobra.Command) logger.Logger {
	log.SetFlags(0)
	level := LogLevel(cmd)
	return logger.NewConsoleLogger(level)
}

// NewTelemetry returns a telemetry context, logger, shutdown function. The cobra flags it expects are:
//
// --no-telemetry (boolean): if set, telemetry will be disabled
//
// --otlp-url (string): the url of the otlp server
//
// --otlp-shared-secret (string): the shared secret for the otlp server
func NewTelemetry(ctx context.Context, cmd *cobra.Command, serviceName string) (context.Context, logger.Logger, func(), error) {
	if noTelemetry, err := cmd.Flags().GetBool("no-telemetry"); err == nil && noTelemetry {
		return ctx, NewLogger(cmd), func() {}, nil
	}
	otlpURL := FlagOrEnv(cmd, "otlp-url", "AGENTUITY_OTLP_URL", "https://otlp.agentuity.cloud")
	otlpSharedSecret := FlagOrEnv(cmd, "otlp-shared-secret", "AGENTUITY_OTLP_SHARED_SECRET", "")

	if otlpURL == "" {
		return nil, nil, nil, fmt.Errorf("otlp-url or AGENTUITY_OTLP_URL are required and --no-telemetry was not set")
	}
	if otlpSharedSecret == "" {
		return nil, nil, nil, fmt.Errorf("otlp-shared-secret or AGENTUITY_OTLP_SHARED_SECRET are required and --no-telemetry was not set")
	}

	telemetryCtx, logger, shutdown, err := telemetry.New(ctx, serviceName, otlpSharedSecret, otlpURL, NewLogger(cmd))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error creating telemetry: %w", err)
	}
	return telemetryCtx, logger, shutdown, nil
}
