package sys

import (
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCreateShutdownChannel(t *testing.T) {
	done := CreateShutdownChannel()
	assert.NotNil(t, done)
	
	go func() {
		time.Sleep(100 * time.Millisecond)
		
		p, err := os.FindProcess(os.Getpid())
		if err == nil {
			p.Signal(syscall.SIGUSR1)
		}
	}()
	
	usr1 := make(chan os.Signal, 1)
	signal.Notify(usr1, syscall.SIGUSR1)
	
	select {
	case <-usr1:
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for signal")
	}
	
	signal.Stop(usr1)
	close(usr1)
}
