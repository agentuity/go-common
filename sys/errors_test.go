package sys

import (
	"testing"

	"github.com/agentuity/go-common/logger"
	"github.com/stretchr/testify/assert"
)

func TestPanicError(t *testing.T) {
	err := assert.AnError
	result := panicError(1, err)
	assert.Error(t, result)
	assert.Contains(t, result.Error(), err.Error())

	result = panicError(1, "test panic")
	assert.Error(t, result)
	assert.Contains(t, result.Error(), "test panic")
}

func TestRecoverPanic(t *testing.T) {
	log := logger.NewTestLogger()

	RecoverPanic(log)

}
