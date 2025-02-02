package compress

import (
	"bytes"
	"testing"
)

func TestGunzip(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		want    []byte
		wantErr bool
	}{
		{
			name:    "empty input",
			data:    []byte{},
			want:    []byte{},
			wantErr: true,
		},
		{
			name:    "valid gzip data",
			data:    []byte{31, 139, 8, 0, 0, 0, 0, 0, 0, 255, 242, 72, 205, 201, 201, 87, 8, 207, 47, 202, 73, 1, 4, 0, 0, 255, 255, 86, 177, 23, 74, 11, 0, 0, 0},
			want:    []byte("Hello World"),
			wantErr: false,
		},
		{
			name:    "invalid gzip data",
			data:    []byte{1, 2, 3, 4},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Gunzip(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("Gunzip() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !bytes.Equal(got, tt.want) {
				t.Errorf("Gunzip() = %v, want %v", got, tt.want)
			}
		})
	}
}
