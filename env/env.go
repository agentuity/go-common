package env

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/agentuity/go-common/logger"
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

func ParseEnvValue(key, val string) EnvLine {
	return EnvLine{
		Key: key,
		Val: val,
	}
}

// ProcessEnvLine processes an environment variable line and returns an EnvLine struct with the key, value, and secret flag set.
func ProcessEnvLine(env string) EnvLine {
	tok := strings.SplitN(env, "=", 2)
	key := tok[0]
	val := dequote(tok[1])
	return ParseEnvValue(key, val)
}

// ParseEnvBuffer parses an environment file from a buffer and returns a list of EnvLine structs.
func ParseEnvBuffer(buf []byte) ([]EnvLine, error) {
	var envs []EnvLine
	if len(buf) > 0 {
		lines := strings.Split(string(buf), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || line[0] == '#' || !strings.Contains(line, "=") {
				continue
			}
			envs = append(envs, ProcessEnvLine(line))
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
