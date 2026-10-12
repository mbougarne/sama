package operation

import "testing"

func TestAllowedOperationTransitions(t *testing.T) {
	for _, pair := range [][2]string{{"queued", "dispatching"}, {"dispatching", "reconciling"}, {"reconciling", "unknown"}, {"unknown", "reconciling"}, {"waiting_provider", "waiting_provider"}, {"reconciling", "succeeded"}} {
		if !Allowed(pair[0], pair[1]) {
			t.Fatal("legal transition rejected", pair)
		}
	}
	for _, pair := range [][2]string{{"queued", "succeeded"}, {"queued", "unknown"}, {"unknown", "failed"}, {"unknown", "dispatching"}, {"dispatching", "cancelled"}, {"failed", "queued"}, {"succeeded", "reconciling"}, {"cancelled", "dispatching"}, {"untrusted-body", "failed"}} {
		if Allowed(pair[0], pair[1]) {
			t.Fatal("unsafe transition accepted", pair)
		}
	}
}
