package qualification

import (
	"context"
	"errors"
	"time"

	"sama/backend/internal/connection"
	"sama/backend/internal/provider"
)

type PowerClient interface {
	Reader
	provider.PowerReviewer
	provider.PowerMutation
}

// PowerCase authorizes one existing disposable target and, where needed, one
// explicit restoration action. Limits describe the operator's external budget;
// the probe neither purchases resources nor claims it can enforce provider cost.
type PowerCase struct {
	Action            string `json:"action"`
	NativeID          string `json:"native_id"`
	Disposable        bool   `json:"disposable"`
	SpendCeiling      string `json:"spend_ceiling"`
	DisruptionSeconds int    `json:"disruption_seconds"`
	RestoreAction     string `json:"restore_action"`
}

type PowerEvidence struct {
	Action            string `json:"action"`
	IdentityVerified  bool   `json:"identity_verified"`
	Submitted         bool   `json:"submitted"`
	NativeCompletion  bool   `json:"native_completion"`
	FinalStateChecked bool   `json:"final_state_checked"`
	CleanupVerified   bool   `json:"cleanup_verified"`
	RestoreSubmitted  bool   `json:"restore_submitted"`
}

var ErrPowerIncomplete = errors.New("provider power qualification incomplete")

func (c PowerCase) valid() bool {
	if !c.Disposable || c.NativeID == "" || len(c.NativeID) > 128 || c.SpendCeiling == "" || len(c.SpendCeiling) > 100 || c.DisruptionSeconds < 1 || c.DisruptionSeconds > 300 {
		return false
	}
	switch c.Action {
	case "compute.server.power_on":
		return c.RestoreAction == "compute.server.shutdown" || c.RestoreAction == "compute.server.power_off"
	case "compute.server.shutdown", "compute.server.power_off":
		return c.RestoreAction == "compute.server.power_on"
	case "compute.server.reboot":
		return c.RestoreAction == ""
	}
	return false
}

// PowerProbe does not qualify a registry. Identity/permissions must already pass
// the read qualification gate. A partial result or an ambiguous call never
// triggers a retry or a restoration that could overlap the original action.
func PowerProbe(ctx context.Context, b provider.Binding, client PowerClient, credentialInput connection.Credential, c PowerCase) (PowerEvidence, error) {
	e := PowerEvidence{Action: c.Action}
	if client == nil || !c.valid() || credentialInput.Validate() != nil || b.Workspace().Version() != 7 || b.Connection().Version() != 7 || b.Version() < 1 {
		return e, provider.ErrBinding
	}
	ctx, stop := context.WithTimeout(ctx, time.Duration(c.DisruptionSeconds)*time.Second)
	defer stop()
	account, capabilities, err := client.Validate(ctx, credentialInput)
	if err != nil || account == "" || credentialInput.ContainsSecret(account) {
		return e, provider.ErrIdentityUnverified
	}
	verified := false
	for _, capability := range capabilities {
		if capability == "compute.server.read" {
			verified = true
		}
	}
	if !verified {
		return e, provider.ErrIdentityUnverified
	}
	e.IdentityVerified = true
	initial, err := client.Read(ctx, b, c.NativeID)
	if err != nil || initial.Status != "running" && initial.Status != "stopped" {
		return e, ErrPowerIncomplete
	}
	if c.RestoreAction != "" {
		// Inspect the restoration's implementation without submitting it. Its state
		// restriction is expected to be false before the first action completes.
		if _, err = client.Inspect(ctx, b, c.RestoreAction, c.NativeID); err != nil {
			return e, err
		}
	}
	if err = executePowerCase(ctx, b, client, c.Action, c.NativeID, &e.Submitted, &e.NativeCompletion); err != nil {
		return e, err
	}
	final, err := client.Read(ctx, b, c.NativeID)
	if err != nil || final.Status != desiredPowerState(c.Action) {
		return e, ErrPowerIncomplete
	}
	e.FinalStateChecked = true
	if c.RestoreAction != "" {
		var native bool
		if err = executePowerCase(ctx, b, client, c.RestoreAction, c.NativeID, &e.RestoreSubmitted, &native); err != nil {
			return e, err
		}
	}
	restored, err := client.Read(ctx, b, c.NativeID)
	if err != nil || restored.Status != initial.Status {
		return e, ErrPowerIncomplete
	}
	e.CleanupVerified = true
	return e, nil
}

func executePowerCase(ctx context.Context, b provider.Binding, client PowerClient, action, id string, submitted, native *bool) error {
	pre, err := client.Inspect(ctx, b, action, id)
	if err != nil {
		return err
	}
	if !pre.Allowed || pre.Fingerprint == ([32]byte{}) {
		return provider.ErrPrecondition
	}
	*submitted = true // The boundary may have been crossed even if the response is lost.
	r, err := client.Submit(ctx, b, action, id, pre.Fingerprint)
	if err != nil || r.Outcome == "rejected" {
		return ErrPowerIncomplete
	}
	var reference *string
	if r.Reference != "" {
		reference = &r.Reference
	}
	for range 100 {
		o, err := client.Observe(ctx, b, action, id, reference)
		if err != nil {
			return ErrPowerIncomplete
		}
		if o.Outcome == "succeeded" && o.NativeEvidence {
			*native = true
			return nil
		}
		if o.Outcome != "pending" {
			return ErrPowerIncomplete
		}
		timer := time.NewTimer(3 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ErrPowerIncomplete
		case <-timer.C:
		}
	}
	return ErrPowerIncomplete
}

func desiredPowerState(action string) string {
	if action == "compute.server.shutdown" || action == "compute.server.power_off" {
		return "stopped"
	}
	return "running"
}
