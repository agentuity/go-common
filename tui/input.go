package tui

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/agentuity/go-common/logger"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

var inputTheme = huh.ThemeBase16()

func Input(logger logger.Logger, title string, description string) string {
	return InputWithPlaceholder(logger, title, description, "")
}

func InputWithPlaceholder(logger logger.Logger, title string, description string, placeholder string) string {
	var value string
	if err := huh.NewInput().
		Title(title).
		Prompt("> ").
		Description(description).
		Placeholder(placeholder).
		Value(&value).
		WithTheme(inputTheme).
		Run(); err != nil {
		logger.Fatal("%s", err)
	}
	if value == "" {
		return placeholder
	}
	return value
}

func InputWithValidation(logger logger.Logger, title string, description string, maxLength int, validate func(string) error) string {
	var value string
	if err := huh.NewInput().
		Title(title).
		Prompt("> ").
		Description(description + "\n").
		CharLimit(maxLength).
		Validate(validate).
		Value(&value).
		WithTheme(inputTheme).
		Run(); err != nil {
		logger.Fatal("%s", err)
	}
	return value
}

func Password(logger logger.Logger, title string, description string) string {
	var value string
	if err := huh.NewInput().
		Title(title).
		Prompt("> ").
		Description(description + "\n").
		EchoMode(huh.EchoModePassword).
		Value(&value).
		WithTheme(inputTheme).
		Run(); err != nil {
		logger.Fatal("%s", err)
	}
	return value
}

// WaitForAnyKey prints the message "Press any key to continue..." and waits for any key press
func WaitForAnyKey() {
	WaitForAnyKeyMessage("Press any key to continue... ")
}

// WaitForAnyKeyMessage prints a message and waits for any key press
func WaitForAnyKeyMessage(message string) {
	if !HasTTY {
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	ch := make(chan struct{}, 1)
	go func() {
		buf := make([]byte, 1)
		os.Stdin.Read(buf)
		fmt.Print(ansi.CursorBackward(1)) // remove the char from the screen output
		ch <- struct{}{}
	}()
	fmt.Print(Secondary(message))
	select {
	case <-ctx.Done():
		fmt.Println()
		os.Exit(1)
		return
	case <-ch:
		select {
		case <-ctx.Done():
			fmt.Println()
			os.Exit(1)
		default:
			return
		}
	}
}

const emptyRune = byte(0)

// AskForConfirm asks the user for confirmation as a single value
func AskForConfirm(message string, defaultValue byte) byte {
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return emptyRune
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	ch := make(chan byte, 1)
	go func() {
		buf := make([]byte, 1)
		os.Stdin.Read(buf)
		fmt.Print(ansi.CursorBackward(1)) // remove the char from the screen output
		ch <- buf[0]
	}()
	fmt.Print(message)
	select {
	case <-ctx.Done():
		fmt.Println()
		os.Exit(1)
	case answer := <-ch:
		select {
		case <-ctx.Done():
			fmt.Println()
			os.Exit(1)
		default:
		}
		if answer == '\n' || answer == '\r' {
			return defaultValue
		}
		return answer
	}
	return emptyRune
}
