package qualification

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"sama/backend/internal/connection"
	"sama/backend/internal/provider"
)

type fakePower struct {
	status              string
	submits             int
	identity, ambiguous bool
	observed            string
}

func (f *fakePower) Validate(context.Context, connection.Credential) (string, []string, error) {
	if !f.identity {
		return "", nil, provider.ErrIdentityUnverified
	}
	return "synthetic-account", []string{"compute.server.read"}, nil
}
func (f *fakePower) Page(context.Context, provider.Binding, string) (provider.InventoryPage, error) {
	return provider.InventoryPage{}, nil
}
func (f *fakePower) Read(context.Context, provider.Binding, string) (provider.Observation, error) {
	return provider.Observation{Status: f.status}, nil
}
func (f *fakePower) Inspect(_ context.Context, b provider.Binding, action, id string) (provider.Preconditions, error) {
	return provider.PowerPreconditions(b, action, provider.Observation{NativeID: id, Name: "target", Status: f.status})
}
func (f *fakePower) Submit(_ context.Context, _ provider.Binding, action, _ string, _ [32]byte) (provider.Receipt, error) {
	f.submits++
	if f.ambiguous {
		return provider.Receipt{Outcome: "ambiguous"}, provider.InvalidResponse()
	}
	f.status = desiredPowerState(action)
	return provider.Receipt{Outcome: "accepted", Reference: "native-9"}, nil
}
func (f *fakePower) Observe(context.Context, provider.Binding, string, string, *string) (provider.ActionObservation, error) {
	if f.observed != "" {
		return provider.ActionObservation{Outcome: f.observed}, nil
	}
	return provider.ActionObservation{Outcome: "succeeded", NativeEvidence: true}, nil
}

func TestPowerProbeRequiresConsentIdentityCompletionAndCleanup(t *testing.T) {
	w, _ := uuid.NewV7()
	c, _ := uuid.NewV7()
	b, _ := provider.NewBinding(w, c, "synthetic", 1)
	credential := connection.Credential{Type: "bearer_v1", Token: "synthetic-token"}
	valid := PowerCase{Action: "compute.server.power_on", NativeID: "42", Disposable: true, SpendCeiling: "explicit external ceiling", DisruptionSeconds: 30, RestoreAction: "compute.server.shutdown"}
	for _, probe := range []struct {
		name                string
		identity, ambiguous bool
		observed            string
		submits             int
		ok                  bool
	}{
		{"identity missing", false, false, "", 0, false},
		{"lost receipt", true, true, "", 1, false},
		{"unknown", true, false, "unknown", 1, false},
		{"restored", true, false, "", 2, true},
	} {
		t.Run(probe.name, func(t *testing.T) {
			f := &fakePower{status: "stopped", identity: probe.identity, ambiguous: probe.ambiguous, observed: probe.observed}
			e, err := PowerProbe(context.Background(), b, f, credential, valid)
			if f.submits != probe.submits || (err == nil) != probe.ok || e.CleanupVerified != probe.ok {
				t.Fatal(e, err, f.submits)
			}
			if !probe.ok && e.RestoreSubmitted {
				t.Fatal("cleanup overlapped uncertainty")
			}
		})
	}
	for _, invalid := range []PowerCase{
		{}, {Action: "compute.server.power_on", NativeID: "42", SpendCeiling: "ceiling", DisruptionSeconds: 30, RestoreAction: "compute.server.shutdown"},
		{Action: "compute.server.power_on", NativeID: "42", Disposable: true, SpendCeiling: "ceiling", DisruptionSeconds: 30},
		{Action: "compute.server.reboot", NativeID: "42", Disposable: true, SpendCeiling: "ceiling", DisruptionSeconds: 301},
	} {
		f := &fakePower{identity: true, status: "stopped"}
		if _, err := PowerProbe(context.Background(), b, f, credential, invalid); err == nil || f.submits != 0 {
			t.Fatal("incomplete authorization admitted")
		}
	}
}
