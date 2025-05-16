package stream

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type mockFlusher struct {
	flushErr error
}

func (m *mockFlusher) Flush() error {
	return m.flushErr
}

func TestPipe(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		flushErr error
		readErr  error
		writeErr error
		wantErr  bool
		wantData string
	}{
		{
			name:     "successful pipe with small data",
			input:    "hello world",
			wantData: "hello world",
		},
		{
			name:     "successful pipe with empty data",
			input:    "",
			wantData: "",
		},
		{
			name:     "successful pipe with large data",
			input:    strings.Repeat("a", 1024*1024), // 1MB of data
			wantData: strings.Repeat("a", 1024*1024),
		},
		{
			name:     "flush error",
			input:    "test data",
			flushErr: errors.New("flush error"),
			wantErr:  true,
		},
		{
			name:    "read error",
			input:   "test data",
			readErr: errors.New("read error"),
			wantErr: true,
		},
		{
			name:     "write error",
			input:    "test data",
			writeErr: errors.New("write error"),
			wantErr:  true,
		},
		{
			name:    "context canceled",
			input:   "test data",
			readErr: context.Canceled,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a reader that can simulate errors
			reader := &errorReader{
				data: []byte(tt.input),
				err:  tt.readErr,
			}

			// Create a writer that can simulate errors
			writer := &errorWriter{
				err: tt.writeErr,
			}

			// Create a mock flusher
			flusher := &mockFlusher{
				flushErr: tt.flushErr,
			}

			// Run the pipe
			err := Pipe(flusher, writer, reader)

			// Check error conditions
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Check the output data
			if got := writer.String(); got != tt.wantData {
				t.Errorf("got %q, want %q", got, tt.wantData)
			}
		})
	}
}

// errorReader is a reader that can simulate errors
type errorReader struct {
	data []byte
	err  error
	pos  int
}

func (r *errorReader) Read(p []byte) (n int, err error) {
	if r.err != nil {
		return 0, r.err
	}
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// errorWriter is a writer that can simulate errors
type errorWriter struct {
	bytes.Buffer
	err error
}

func (w *errorWriter) Write(p []byte) (n int, err error) {
	if w.err != nil {
		return 0, w.err
	}
	return w.Buffer.Write(p)
}
