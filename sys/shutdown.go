package sys

import (
	"os"
	"os/signal"
	"syscall"
)

// CreateShutdownChannel returns a channel which can be used to block for a termination signal (SIGTERM, SIGINT, etc)
func CreateShutdownChannel() chan os.Signal {
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	return done
}
