package operation

import "testing"

func TestPolicyCannotQualifyOrGrantAndVersionsInvalidateReviews(t *testing.T) {
	key := "fixture/compute:compute.server.reboot"
	if NewPolicy([]string{key}, nil).permits("fixture/compute", "compute.server.reboot") {
		t.Fatal("configuration qualified adapter")
	}
	p := NewPolicy([]string{key}, map[string]bool{key: true})
	if !p.permits("fixture/compute", "compute.server.reboot") || p.permits("fixture/compute", "compute.server.shutdown") {
		t.Fatal("action boundary")
	}
	s := (&Service{}).WithPolicy(p)
	version := s.policyVersion(1)
	if version == s.policyVersion(2) {
		t.Fatal("workspace policy ignored")
	}
	s.WithPolicy(NewPolicy(nil, map[string]bool{key: true}))
	if version == s.policyVersion(1) || s.policy.permits("fixture/compute", "compute.server.reboot") {
		t.Fatal("disabled policy reused review")
	}
}
