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
	Raw string `json:"-"` // the raw line from the file
}

// EnvLineComment extends EnvLine to include an optional comment
type EnvLineComment struct {
	EnvLine
	Comment string `json:"comment,omitempty"`
}

// ParseEnvFile parses an environment file and returns a list of EnvLine structs.
func ParseEnvFile(filename string, opts ...WithParseEnvOptions) ([]EnvLine, error) {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return nil, nil
	}
	buf, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return ParseEnvBuffer(buf, opts...)
}

// ParseEnvFileWithComments parses an environment file and returns a list of EnvLineComment structs.
func ParseEnvFileWithComments(filename string) ([]EnvLineComment, error) {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return nil, nil
	}
	buf, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return ParseEnvLinesWithComments(buf)
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

func ParseEnvValue(key, val string) EnvLine {
	return EnvLine{
		Key: key,
		Val: val,
		Raw: val,
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

type debugLogger interface {
	Logf(format string, args ...interface{})
}

type noopLogger struct{}

func (n noopLogger) Logf(format string, args ...interface{}) {}

var emptyLogger = noopLogger{}

func interpolateValue(input string, envMap map[string]string) string {
	return interpolateValueWithLogger(input, envMap, emptyLogger)
}

func interpolateValueWithLogger(input string, envMap map[string]string, logger debugLogger) string {
	logger.Logf("Interpolating value: %s", input)
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

			// Extract the reference
			refStr := input[i : end+1]
			innerContent := refStr[2 : len(refStr)-1]

			logger.Logf("Processing reference: %s (inner: %s)", refStr, innerContent)

			// First interpolate any nested references in the inner content
			resolvedInner := interpolateValueWithLogger(innerContent, envMap, logger)
			logger.Logf("Resolved inner content: %s -> %s", innerContent, resolvedInner)

			// If the inner content was resolved to something different, we need to
			// resolve the new reference
			if resolvedInner != innerContent {
				newRef := "${" + resolvedInner + "}"
				logger.Logf("Resolving new reference: %s", newRef)
				finalValue := interpolateValueWithLogger(newRef, envMap, logger)
				logger.Logf("Final value after nested resolution: %s", finalValue)
				result.WriteString(finalValue)
				i = end
				lastPos = end + 1
				continue
			}

			// Now parse the reference with resolved inner content
			ref := parseReference("${" + resolvedInner + "}")
			logger.Logf("Parsed reference: %+v", ref)

			// Handle empty reference
			if ref.varName == "" {
				result.WriteString("${}")
				i = end
				lastPos = end + 1
				continue
			}

			var finalValue string
			// Check if this is an OS environment lookup
			if strings.HasPrefix(ref.varName, "env:") {
				// Strip the env: prefix and look up in OS environment
				envKey := strings.TrimPrefix(ref.varName, "env:")
				val := os.Getenv(envKey)
				logger.Logf("Looking up OS env %s -> %s", envKey, val)
				if val == "" && ref.defaultValue != "" {
					finalValue = ref.defaultValue
					logger.Logf("Using default value: %s", finalValue)
				} else if val == "" {
					finalValue = refStr
					logger.Logf("No value found, preserving reference: %s", finalValue)
				} else {
					finalValue = val
					logger.Logf("Using OS env value: %s", finalValue)
				}
			} else {
				// Regular envMap lookup
				val, exists := envMap[ref.varName]
				logger.Logf("Looking up envMap %s -> %v (%v)", ref.varName, val, exists)
				if !exists || val == "" {
					if ref.defaultValue != "" {
						finalValue = ref.defaultValue
						logger.Logf("Using default value: %s", finalValue)
					} else {
						finalValue = refStr
						logger.Logf("No value found, preserving reference: %s", finalValue)
					}
				} else {
					finalValue = val
					logger.Logf("Using envMap value: %s", finalValue)
				}
			}

			// If we got a value and it contains references, resolve them too
			if finalValue != refStr {
				logger.Logf("Recursively resolving: %s", finalValue)
				finalValue = interpolateValueWithLogger(finalValue, envMap, logger)
			}
			logger.Logf("Final value: %s", finalValue)
			result.WriteString(finalValue)

			i = end
			lastPos = end + 1
		}
	}

	// Add remaining text
	result.WriteString(input[lastPos:])
	finalResult := result.String()
	logger.Logf("Final result: %s", finalResult)
	return finalResult
}

type parseEnvOptions struct {
	Interpolate bool
}

type WithParseEnvOptions func(opts *parseEnvOptions)

// WithInterpolate sets the Interpolate flag to true or false
func WithInterpolate(interpolate bool) WithParseEnvOptions {
	return func(opts *parseEnvOptions) {
		opts.Interpolate = interpolate
	}
}

// InterpolateEnvLines interpolates the values of a list of EnvLine structs using the current environment.
func InterpolateEnvLines(envs []EnvLine) []EnvLine {
	// Create a copy of the input slice to avoid modifying the original
	result := make([]EnvLine, 0)

	// Build environment map from input and OS environment
	envMap := make(map[string]string)
	for _, env := range envs {
		if env.Raw != "" {
			envMap[env.Key] = env.Raw
		} else {
			envMap[env.Key] = env.Val
		}
	}
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = strings.Join(parts[1:], "=")
		}
	}

	// Process each environment variable
	for _, env := range envs {
		val := env.Raw
		if val == "" {
			val = env.Val
		}
		result = append(result, EnvLine{Key: env.Key, Val: interpolateValue(val, envMap), Raw: env.Raw})
	}

	return result
}

// ParseEnvBuffer parses an environment buffer and returns a list of EnvLine structs.
func ParseEnvBuffer(buf []byte, opts ...WithParseEnvOptions) ([]EnvLine, error) {
	if len(buf) == 0 {
		return make([]EnvLine, 0), nil
	}
	config := parseEnvOptions{
		Interpolate: true,
	}
	for _, opt := range opts {
		opt(&config)
	}
	var envs []EnvLine
	var envMap = make(map[string]string)
	var envRaw = make(map[string]string)

	// First pass: Build environment map and process interpolation
	lines := strings.Split(string(buf), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		env := ProcessEnvLine(line)
		if env.Key != "" {
			envRaw[env.Key] = env.Raw
			if config.Interpolate {
				// Interpolate the value using the current environment map
				env.Val = interpolateValue(env.Val, envMap)
			}
			// Update the environment map with the interpolated value
			envMap[env.Key] = env.Val
			envs = append(envs, env)
		}
	}

	// Second pass: Re-interpolate all values with the complete environment map
	if config.Interpolate {
		for i := range envs {
			envs[i].Raw = envRaw[envs[i].Key]
			envs[i].Val = interpolateValue(envs[i].Val, envMap)
		}
	} else {
		for i := range envs {
			envs[i].Raw = envRaw[envs[i].Key]
		}
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
		if el.Raw != "" {
			fmt.Fprintln(of, EncodeOSEnv(el.Key, el.Raw))
		} else {
			fmt.Fprintln(of, EncodeOSEnv(el.Key, el.Val))
		}
	}
	return of.Close()
}

// FlagOrEnv will try and get a flag from the cobra.Command and if not found, look it up in the environment
// and fallback to defaultValue if non found
func FlagOrEnv(cmd *cobra.Command, flagName string, envName string, defaultValue string) string {
	if cmd.Flags().Changed(flagName) {
		flagValue, _ := cmd.Flags().GetString(flagName)
		if flagValue != "" {
			return flagValue
		}
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
	otlpURL := FlagOrEnv(cmd, "otlp-url", "AGENTUITY_OTLP_URL", "https://otel.agentuity.cloud")
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

// ParseEnvLinesWithComments parses an environment buffer and returns a list of EnvLineComment structs.
// Comments preceding environment variables (starting with #) are associated with the following variable.
func ParseEnvLinesWithComments(buf []byte) ([]EnvLineComment, error) {
	if len(buf) == 0 {
		return make([]EnvLineComment, 0), nil
	}
	var envs []EnvLineComment
	var envMap = make(map[string]string)
	var lastComment string

	lines := strings.Split(string(buf), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			lastComment = "" // Reset comment on empty line
			continue
		}

		if strings.HasPrefix(line, "#") {
			// Store comment without the # prefix and trimmed
			lastComment = strings.TrimSpace(strings.TrimPrefix(line, "#"))
			continue
		}

		env := ProcessEnvLine(line)
		if env.Key != "" {
			// Interpolate the value using the current environment map
			env.Val = interpolateValue(env.Val, envMap)
			// Update the environment map with the interpolated value
			envMap[env.Key] = env.Val

			// Create EnvLineComment with the last seen comment
			envs = append(envs, EnvLineComment{
				EnvLine: env,
				Comment: lastComment,
			})

			lastComment = "" // Reset comment after using it
		}
	}

	// Second pass: Re-interpolate all values with the complete environment map
	for i := range envs {
		envs[i].Val = interpolateValue(envs[i].Val, envMap)
	}

	return envs, nil
}
