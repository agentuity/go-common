package sys

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCreateShutdownChannel(t *testing.T) {
	done := CreateShutdownChannel()
	assert.NotNil(t, done)
}
