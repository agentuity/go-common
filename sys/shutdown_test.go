package sys

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCreateShutdownChannel(t *testing.T) {
	done := CreateShutdownChannel()
	assert.NotNil(t, done)
	
	
	_, ok := done.(chan os.Signal)
	assert.True(t, ok)
}
