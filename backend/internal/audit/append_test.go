package audit

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestValidateRetainsSafeCorrelationAndAllowlistedMetadata(t *testing.T) {
	connectionID := "018f1f4e-7b0b-7cc3-98df-86d5c7d6d80a"
	event := Event{
		ID:        uuid.MustParse("018f1f4e-7b0b-7cc3-98df-86d5c7d6d801"),
		Workspace: uuid.MustParse("018f1f4e-7b0b-7cc3-98df-86d5c7d6d802"),
		ActorKind: ActorSystem,
		Type:      "connection.credential_rotated",
		RequestID: "req.2026-09-20_1",
		Metadata:  map[string]string{"connection_id": connectionID},
	}
	encoded, err := validate(event)
	if err != nil || !strings.Contains(string(encoded), connectionID) {
		t.Fatal("safe event correlation metadata was rejected")
	}
}

func TestValidateRejectsUnknownMetadataAndUnsafeCorrelation(t *testing.T) {
	event := Event{
		ID:        uuid.MustParse("018f1f4e-7b0b-7cc3-98df-86d5c7d6d801"),
		Workspace: uuid.MustParse("018f1f4e-7b0b-7cc3-98df-86d5c7d6d802"),
		ActorKind: ActorSystem,
		Type:      "workspace.created",
		RequestID: "request-1",
		Metadata:  map[string]string{"raw_body": "sentinel-secret"},
	}
	if _, err := validate(event); err == nil || strings.Contains(err.Error(), "sentinel-secret") {
		t.Fatal("unknown metadata was accepted or reflected in an error")
	}
	event.Metadata = nil
	event.RequestID = "bad\r\nid"
	if _, err := validate(event); err == nil {
		t.Fatal("unsafe correlation field was accepted")
	}
}
