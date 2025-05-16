package stream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
)

type WriteFlusher interface {
	io.Writer
	http.Flusher
}

// Pipe will read data from r and write it to w flushing the data as it comes in
func Pipe(w WriteFlusher, r io.Reader) error {
	buf := make([]byte, 1024*512)

	// we want to stream the response to the client as it comes in
	// vs waiting for the whole response to come in before sending it to the client
	for {
		n, err := r.Read(buf)
		if err != nil {
			if err == io.EOF {
				if n > 0 {
					if _, err := w.Write(buf[:n]); err != nil {
						return fmt.Errorf("write chunk: %w", err)
					}
				}
				break
			}
			if errors.Is(err, context.Canceled) {
				break
			}
			return fmt.Errorf("copy chunk: %w", err)
		}
		if n == 0 {
			break
		}
		if _, err := w.Write(buf[:n]); err != nil {
			return fmt.Errorf("write chunk: %w", err)
		}
		w.Flush()
	}

	return nil
}
