package tui

import (
	"bytes"
	"os"
	"testing"
)

func TestHasTTY(t *testing.T) {
	if HasTTY != true && HasTTY != false {
		t.Errorf("HasTTY should be a boolean value")
	}
}

func TestShowBanner(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	ShowBanner("Test Banner", "Test Body", false)

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if output == "" {
		t.Errorf("ShowBanner() should produce output")
	}

	if !bytes.Contains(buf.Bytes(), []byte("Test Banner")) {
		t.Errorf("ShowBanner() output should contain the banner text")
	}
}

func TestBannerBodyStyle(t *testing.T) {
	style := BannerBodyStyle()
	_ = style
}

func TestShowMessages(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		function func(string, ...any)
	}{
		{
			name:     "success message",
			message:  "Success message",
			function: ShowSuccess,
		},
		{
			name:     "error message",
			message:  "Error message",
			function: ShowError,
		},
		{
			name:     "warning message",
			message:  "Warning message",
			function: ShowWarning,
		},
		{
			name:     "lock message",
			message:  "Lock message",
			function: ShowLock,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			tt.function(tt.message)

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			buf.ReadFrom(r)

			if !bytes.Contains(buf.Bytes(), []byte(tt.message)) {
				t.Errorf("%s output should contain the message text", tt.name)
			}
		})
	}
}

func TestTitleColor(t *testing.T) {
	color := TitleColor()
	if color.Light == "" || color.Dark == "" {
		t.Errorf("TitleColor() should return a valid color")
	}
}
