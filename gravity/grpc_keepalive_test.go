package gravity

import (
	"testing"
	"time"
)

func TestGravityTransportKeepaliveParams(t *testing.T) {
	if gravityTransportKeepaliveParams.Time != gravityTransportKeepaliveTime {
		t.Fatalf("keepalive time = %v, want %v", gravityTransportKeepaliveParams.Time, gravityTransportKeepaliveTime)
	}
	if gravityTransportKeepaliveParams.Timeout != gravityTransportKeepaliveTimeout {
		t.Fatalf("keepalive timeout = %v, want %v", gravityTransportKeepaliveParams.Timeout, gravityTransportKeepaliveTimeout)
	}
	if gravityTransportKeepaliveParams.PermitWithoutStream {
		t.Fatal("PermitWithoutStream must be false to avoid idle transport pings on the gravity client")
	}
	if gravityTransportKeepaliveParams.Time < 5*time.Minute {
		t.Fatalf("keepalive time = %v, want >= 5m to stay above gRPC-Go server enforcement defaults", gravityTransportKeepaliveParams.Time)
	}
}
