package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	linkForegroupColor  = lipgloss.AdaptiveColor{Light: "#000099", Dark: "#9F9FFF"}
	linkStyle           = lipgloss.NewStyle().Foreground(linkForegroupColor).Underline(true)
	paragraphStyle      = lipgloss.NewStyle().AlignVertical(lipgloss.Top).AlignHorizontal(lipgloss.Left)
	textStyleColor      = lipgloss.AdaptiveColor{Light: "#00AAAA", Dark: "#00FFFF"}
	mutedStyleColor     = lipgloss.AdaptiveColor{Light: "#666666", Dark: "#999999"}
	warningStyleColor   = lipgloss.AdaptiveColor{Light: "#FFA500", Dark: "#FFA500"}
	titleStyleColor     = lipgloss.AdaptiveColor{Light: "#071330", Dark: "#F652A0"}
	secondaryStyleColor = lipgloss.AdaptiveColor{Light: "#214358", Dark: "#AEB8C4"}
	commandStyle        = lipgloss.NewStyle().Foreground(textStyleColor)
	textStyle           = lipgloss.NewStyle().Foreground(secondaryStyleColor)
)

func Title(text string) string {
	return lipgloss.NewStyle().Bold(true).Foreground(titleStyleColor).Render(text)
}

func Padding(text string) string {
	return lipgloss.NewStyle().Padding(1).Render(text)
}

func Bold(text string) string {
	return lipgloss.NewStyle().Bold(true).Foreground(textStyleColor).Render(text)
}

func Secondary(text string) string {
	return lipgloss.NewStyle().Foreground(secondaryStyleColor).Render(text)
}

func Muted(text string) string {
	return lipgloss.NewStyle().Foreground(mutedStyleColor).Render(text)
}

func Warning(text string) string {
	return lipgloss.NewStyle().Foreground(warningStyleColor).Render(text)
}

func supportsHyperlinks() bool {
	term := os.Getenv("TERM")
	termProgram := os.Getenv("TERM_PROGRAM")
	wtSession := os.Getenv("WT_SESSION")

	// List of known terminal programs that support OSC 8
	if strings.Contains(termProgram, "iTerm.app") ||
		strings.Contains(termProgram, "WezTerm") ||
		strings.Contains(termProgram, "ghostty") ||
		strings.Contains(termProgram, "Apple_Terminal") || // sometimes Terminal.app may pass through
		strings.Contains(termProgram, "Hyper") ||
		strings.Contains(term, "xterm-kitty") ||
		strings.Contains(term, "xterm-256color") ||
		wtSession != "" { // Windows Terminal sets this
		return true
	}
	return false
}

func hyperlink(url string, text string) string {
	return fmt.Sprintf("\x1b]8;;%s\x07%s\x1b]8;;\x07", url, text)
}

func Link(url string, args ...any) string {
	link := fmt.Sprintf(url, args...)
	if supportsHyperlinks() {
		return hyperlink(link, link)
	}
	return linkStyle.Render(link)
}

func Paragraph(text string, lines ...string) string {
	lines = append([]string{text}, lines...)
	var out strings.Builder
	for i, line := range lines {
		out.WriteString(paragraphStyle.Render(line))
		if i < len(lines)-1 {
			out.WriteString("\n\n")
		}
	}
	return out.String()
}

func Body(text string) string {
	return paragraphStyle.Render(text)
}

func PadLeft(str string, length int, pad string) string {
	if len(str) >= length {
		return str
	}
	return strings.Repeat(pad, length-len(str)) + str
}

func PadRight(str string, length int, pad string) string {
	if len(str) >= length {
		return str
	}
	return str + strings.Repeat(pad, length-len(str))
}

func Command(cmd string, args ...string) string {
	cmdline := "agentuity " + strings.Join(append([]string{cmd}, args...), " ")
	return commandStyle.Render(cmdline)
}

func Highlight(cmd string, args ...string) string {
	cmdline := strings.Join(append([]string{cmd}, args...), " ")
	return commandStyle.Render(cmdline)
}

func Directory(dir string, args ...string) string {
	val := strings.Join(append([]string{dir}, args...), " ")
	return commandStyle.Render(val)
}

func Text(val string) string {
	return textStyle.Render(val)
}

func MaxWidth(text string, width int) string {
	len := lipgloss.Width(text)
	if len > width {
		text = text[:width-3] + "..."
	}
	return text
}
