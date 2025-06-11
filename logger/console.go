package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
)

const isWindows = runtime.GOOS == "windows"

var noColor = os.Getenv("TERM") == "dumb" ||
	(!isatty.IsTerminal(os.Stdout.Fd()) && !isatty.IsCygwinTerminal(os.Stdout.Fd()))

func color(val string) string {
	if isWindows || noColor {
		return ""
	}
	return val
}

const (
	Reset       = "\033[0m"
	Black       = "\033[1;30m"
	Red         = "\033[31m"
	Green       = "\033[32m"
	Yellow      = "\033[33m"
	Blue        = "\033[34m"
	Magenta     = "\033[35m"
	Cyan        = "\033[36m"
	White       = "\033[37m"
	BlueBold    = "\033[34;1m"
	MagentaBold = "\033[35;1m"
	RedBold     = "\033[31;1m"
	YellowBold  = "\033[33;1m"
	WhiteBold   = "\033[37;1m"
	CyanBold    = "\033[36;1m"
	Gray        = "\033[1;90m" // This is actually "bright black" but Robin said that's a oximoron.
	Purple      = "\u001b[38;5;200m"
)

type consoleLogger struct {
	prefixes          []string
	metadata          map[string]interface{}
	traceLevelColor   string
	traceMessageColor string
	debugLevelColor   string
	debugMessageColor string
	infoLevelColor    string
	infoMessageColor  string
	warnLevelColor    string
	warnMessageColor  string
	errorLevelColor   string
	errorMessageColor string
	sink              Sink
	logLevel          LogLevel
	sinkLogLevel      LogLevel
	child             Logger
}

var _ Logger = (*consoleLogger)(nil)

func (c *consoleLogger) WithContext(ctx context.Context) Logger {
	clone := c.Clone()
	if clone.child != nil {
		clone.child = clone.child.WithContext(ctx)
	}
	return clone
}

func (c *consoleLogger) Default(val string, def string) string {
	if val == "" {
		return def
	}
	return val
}

// WithPrefix will return a new logger with a prefix prepended to the message
func (c *consoleLogger) WithPrefix(prefix string) Logger {
	prefixes := make([]string, len(c.prefixes))
	copy(prefixes, c.prefixes)
	if !slices.Contains(prefixes, prefix) {
		prefixes = append(prefixes, prefix)
	}
	l := c.Clone()
	l.prefixes = prefixes
	if l.child != nil {
		l.child = l.child.WithPrefix(prefix)
	}
	return l
}

var isCI = os.Getenv("CI") != ""

func (c *consoleLogger) Clone() *consoleLogger {
	prefixes := make([]string, len(c.prefixes))
	copy(prefixes, c.prefixes)
	metadata := make(map[string]interface{})
	for k, v := range c.metadata {
		metadata[k] = v
	}
	var tracecolor = Gray
	if isCI {
		tracecolor = Purple
	}
	return &consoleLogger{
		metadata:          metadata,
		prefixes:          prefixes,
		traceLevelColor:   c.Default(c.traceLevelColor, CyanBold),
		traceMessageColor: c.Default(c.traceMessageColor, tracecolor),
		debugLevelColor:   c.Default(c.debugLevelColor, BlueBold),
		debugMessageColor: c.Default(c.debugMessageColor, Green),
		infoLevelColor:    c.Default(c.infoLevelColor, YellowBold),
		infoMessageColor:  c.Default(c.infoMessageColor, WhiteBold),
		warnLevelColor:    c.Default(c.infoMessageColor, MagentaBold),
		warnMessageColor:  c.Default(c.warnMessageColor, Magenta),
		errorLevelColor:   c.Default(c.errorMessageColor, RedBold),
		errorMessageColor: c.Default(c.errorMessageColor, Red),
		sink:              c.sink,
		logLevel:          c.logLevel,
		sinkLogLevel:      c.sinkLogLevel,
		child:             c.child,
	}
}

func (c *consoleLogger) SetSink(sink Sink, level LogLevel) {
	c.sink = sink
	c.sinkLogLevel = level
	if c.child != nil {
		if child, ok := c.child.(SinkLogger); ok {
			child.SetSink(sink, level)
		}
	}
}

func (c *consoleLogger) With(metadata map[string]interface{}) Logger {
	clone := c.Clone()
	for k, v := range metadata {
		clone.metadata[k] = v
	}
	if clone.child != nil {
		clone.child = clone.child.With(metadata)
	}
	return clone
}

func (c *consoleLogger) Log(level LogLevel, levelColor string, messageColor string, levelString string, msg string, args ...interface{}) {
	if level < c.logLevel && level < c.sinkLogLevel {
		return
	}
	_msg := fmt.Sprintf(msg, args...)
	var prefix string
	var suffix string
	if len(c.prefixes) > 0 {
		prefix = color(Purple) + strings.Join(c.prefixes, " ") + color(Reset) + " "
	}
	if c.metadata != nil {
		buf, _ := json.Marshal(c.metadata)
		_buf := string(buf)
		if _buf != "{}" {
			if isCI {
				suffix = " " + color(MagentaBold) + _buf + color(Reset)
			} else {
				suffix = " " + color(Gray) + _buf + color(Reset)
			}
		}
	}
	var levelSuffix string
	if len(levelString) < 5 {
		levelSuffix = strings.Repeat(" ", 5-len(levelString))
	}
	levelText := color(levelColor) + fmt.Sprintf("[%s]%s", levelString, levelSuffix) + color(Reset)
	message := color(messageColor) + _msg + color(Reset)
	out := fmt.Sprintf("%s %s%s%s", levelText, prefix, message, suffix)
	if level >= c.logLevel {
		log.Printf("%s\n", out)
	}
	if c.sink != nil && level >= c.sinkLogLevel {
		ts := time.Now().Format(time.RFC3339Nano)
		c.sink.Write([]byte(ts + " " + ansiColorStripper.ReplaceAllString(out, "") + "\n"))
	}
}

func (c *consoleLogger) Trace(msg string, args ...interface{}) {
	c.Log(LevelTrace, c.traceLevelColor, c.traceMessageColor, "TRACE", msg, args...)
	if c.child != nil {
		c.child.Trace(msg, args...)
	}
}

func (c *consoleLogger) Debug(msg string, args ...interface{}) {
	c.Log(LevelDebug, c.debugLevelColor, c.debugMessageColor, "DEBUG", msg, args...)
	if c.child != nil {
		c.child.Debug(msg, args...)
	}
}

func (c *consoleLogger) Info(msg string, args ...interface{}) {
	c.Log(LevelInfo, c.infoLevelColor, c.infoMessageColor, "INFO", msg, args...)
	if c.child != nil {
		c.child.Info(msg, args...)
	}
}

func (c *consoleLogger) Warn(msg string, args ...interface{}) {
	c.Log(LevelWarn, c.warnLevelColor, c.warnMessageColor, "WARN", msg, args...)
	if c.child != nil {
		c.child.Warn(msg, args...)
	}
}

func (c *consoleLogger) Error(msg string, args ...interface{}) {
	c.Log(LevelError, c.errorLevelColor, c.errorMessageColor, "ERROR", msg, args...)
	if c.child != nil {
		c.child.Error(msg, args...)
	}
}

func (c *consoleLogger) Fatal(msg string, args ...interface{}) {
	c.Log(LevelError, c.errorLevelColor, c.errorMessageColor, "ERROR", msg, args...)
	if c.child != nil {
		c.child.Error(msg, args...) // Error because we want to log the error before exiting
	}
	os.Exit(1)
}

func (c *consoleLogger) Stack(next Logger) Logger {
	clone := c.Clone()
	clone.child = next
	return clone
}

func (c *consoleLogger) SetLogLevel(level LogLevel) {
	c.logLevel = level
}

// NewConsoleLogger returns a new Logger instance which will log to the console
func NewConsoleLogger(levels ...LogLevel) SinkLogger {
	if len(levels) > 0 {
		return (&consoleLogger{logLevel: levels[0], sinkLogLevel: LevelNone}).Clone()
	}
	level := GetLevelFromEnv()

	return (&consoleLogger{logLevel: level, sinkLogLevel: LevelNone}).Clone()
}
