package provider

import "context"

// Receipt carries only an opaque native action ID and a definite classification.
// Rejected requires proof of no effect; timeouts, resets and 5xx are ambiguous.
// Correlation headers never qualify a retry. Submit is called at most once.
type Receipt struct{ Outcome, Reference string }
type ActionObservation struct {
	Outcome        string
	NativeEvidence bool
}

type PowerMutation interface {
	Submit(context.Context, Binding, string, string, [32]byte) (Receipt, error)
	Observe(context.Context, Binding, string, string, *string) (ActionObservation, error)
}
