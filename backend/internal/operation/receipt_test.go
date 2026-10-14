package operation

import (
	"errors"
	"sama/backend/internal/provider"
	"testing"
)

func TestReceiptAmbiguityNeverAuthorizesAnotherSubmission(t *testing.T) {
	for _, r := range []provider.Receipt{{Outcome: "accepted"}, {Outcome: "accepted", Reference: "https://secret.invalid"}, {Outcome: "correlation", Reference: "request-123"}, {Outcome: "succeeded", Reference: "a-123"}, {Outcome: "ambiguous"}} {
		state, ref := classifyReceipt(r, nil)
		if state != "reconciling" || ref != nil {
			t.Fatal("unsafe receipt", state)
		}
	}
	if state, _ := classifyReceipt(provider.Receipt{Outcome: "rejected"}, errors.New("raw synthetic secret")); state != "reconciling" {
		t.Fatal("post-send error rejected")
	}
	if state, ref := classifyReceipt(provider.Receipt{Outcome: "accepted", Reference: "action-1"}, nil); state != "waiting_provider" || ref == nil {
		t.Fatal("receipt lost")
	}
}
