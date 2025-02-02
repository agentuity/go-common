package env

import (
	"log"
	"os"

	"github.com/agentuity/go-common/logger"
	"github.com/spf13/cobra"
)

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

// NewLogger returns a console logger by first checking the cobra.Command log-level flag, then use the
// AGENTUITY_LOG_LEVEL environment value and falling back to the info logger level
func NewLogger(cmd *cobra.Command) logger.Logger {
	log.SetFlags(0)
	level := FlagOrEnv(cmd, "log-level", "AGENTUITY_LOG_LEVEL", "info")
	switch level {
	case "debug", "DEBUG":
		return logger.NewConsoleLogger(logger.LevelDebug)
	case "warn", "WARN":
		return logger.NewConsoleLogger(logger.LevelWarn)
	case "error", "ERROR":
		return logger.NewConsoleLogger(logger.LevelError)
	case "trace", "TRACE":
		return logger.NewConsoleLogger(logger.LevelTrace)
	case "info", "INFO":
	default:
	}
	return logger.NewConsoleLogger(logger.LevelInfo)
}
