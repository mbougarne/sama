package provider

import "context"

type Preconditions struct {
	Fingerprint [32]byte
	TargetName  string
	Allowed     bool
	HighImpact  bool
}

// PowerReviewer performs only authorized reads on its exact immutable binding.
// A compiled factory must use connection/version-owned credentials and the
// shared outbound budget. No submit interface is passed to review services.
type PowerReviewer interface {
	Inspect(context.Context, Binding, string, string) (Preconditions, error)
}
