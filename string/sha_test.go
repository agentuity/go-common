package string

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSHA256(t *testing.T) {
	testCases := []struct {
		name   string
		data   []byte
		extras [][]byte
		want   string
	}{
		{
			name: "single data",
			data: []byte("foobar"),
			want: "c3ab8ff13720e8ad9047dd39466b3c8974e592c2fa383d4a3960714caef0c4f2",
		},
		{
			name:   "data with extra",
			data:   []byte("foo"),
			extras: [][]byte{[]byte("bar")},
			want:   "c3ab8ff13720e8ad9047dd39466b3c8974e592c2fa383d4a3960714caef0c4f2",
		},
		{
			name: "data with multiple extras",
			data: []byte("f"),
			extras: [][]byte{
				[]byte("o"),
				[]byte("o"),
				[]byte("b"),
				[]byte("a"),
				[]byte("r"),
			},
			want: "c3ab8ff13720e8ad9047dd39466b3c8974e592c2fa383d4a3960714caef0c4f2",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := SHA256(tc.data, tc.extras...)
			assert.Equal(t, tc.want, got, "unexpected hash result")
		})
	}
}
