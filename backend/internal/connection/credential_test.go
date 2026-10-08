package connection

import "testing"

func TestTypedCredential(t *testing.T) {
	for _, c := range []Credential{{}, {"ambient", "token"}, {"bearer_v1", ""}, {"bearer_v1", "token\nheader"}} {
		if c.Validate() != ErrCredential {
			t.Fatal("invalid credential")
		}
	}
	if (Credential{"bearer_v1", "synthetic-only"}).Validate() != nil {
		t.Fatal("typed credential rejected")
	}
}
