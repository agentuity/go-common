package eventing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewReplySubject(t *testing.T) {
	subject, err := newReplySubject()
	assert.NoError(t, err)
	assert.NotNil(t, subject)
	assert.Regexp(t, "^_INBOX.[0-9a-f-]+$", subject)
}
