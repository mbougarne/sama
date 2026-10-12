package connection

import "testing"

func TestTypedCredential(t *testing.T) {
	for _, c := range []Credential{{}, {Type: "ambient", Token: "token"}, {Type: "bearer_v1"}, {Type: "bearer_v1", Token: "token\nheader"}} {
		if c.Validate() != ErrCredential {
			t.Fatal("invalid credential")
		}
	}
	if (Credential{Type: "bearer_v1", Token: "synthetic-only"}).Validate() != nil {
		t.Fatal("typed credential rejected")
	}
}

func TestContaboTypedCredentialAndSecretProjection(t *testing.T) {
	c := Credential{Type: "contabo_v1", ClientID: "synthetic-client", ClientSecret: "synthetic-secret", Username: "synthetic-user", Password: "synthetic-password"}
	if c.Validate() != nil || c.ContainsSecret("DE/customer-id") {
		t.Fatal("typed variant rejected or empty token matched")
	}
	for _, value := range []string{c.ClientID, c.ClientSecret, c.Username, c.Password} {
		if !c.ContainsSecret("prefix/" + value) {
			t.Fatal("secret exclusion")
		}
	}
	for _, bad := range []Credential{
		{Type: "contabo_v1"}, {Type: "contabo_v1", Token: "secret"},
		{Type: "bearer_v1", Token: "token", ClientID: "client"},
	} {
		if bad.Validate() == nil {
			t.Fatal("mixed or missing variant accepted")
		}
	}
	c.Password = "bad\nheader"
	if c.Validate() == nil {
		t.Fatal("header injection")
	}
}
