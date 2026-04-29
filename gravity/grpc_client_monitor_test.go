package gravity

import (
	"testing"

	pb "github.com/agentuity/go-common/gravity/proto"
)

func TestMonitorReportsDisabled(t *testing.T) {
	t.Setenv("AGENTUITY_GRAVITY_DISABLE_MONITOR_REPORTS", "true")
	t.Setenv("HADRON_DISABLE_GRAVITY_MONITOR_REPORTS", "")

	if !monitorReportsDisabled() {
		t.Fatal("expected AGENTUITY_GRAVITY_DISABLE_MONITOR_REPORTS=true to disable monitor reports")
	}

	t.Setenv("AGENTUITY_GRAVITY_DISABLE_MONITOR_REPORTS", "")
	t.Setenv("HADRON_DISABLE_GRAVITY_MONITOR_REPORTS", "1")

	if !monitorReportsDisabled() {
		t.Fatal("expected HADRON_DISABLE_GRAVITY_MONITOR_REPORTS=1 to disable monitor reports")
	}
}

func TestValidateOutgoingSessionMessage(t *testing.T) {
	if err := validateOutgoingSessionMessage(nil); err == nil {
		t.Fatal("expected nil session message to be rejected")
	}

	if err := validateOutgoingSessionMessage(&pb.SessionMessage{Id: "missing-type"}); err == nil {
		t.Fatal("expected session message with nil MessageType to be rejected")
	}

	msg := &pb.SessionMessage{
		Id: "ok",
		MessageType: &pb.SessionMessage_Ping{
			Ping: &pb.PingRequest{},
		},
	}
	if err := validateOutgoingSessionMessage(msg); err != nil {
		t.Fatalf("expected valid session message to pass validation, got %v", err)
	}
}
