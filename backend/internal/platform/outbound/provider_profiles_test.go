package outbound

import "testing"

func TestCompiledReadProfilesNeverCrossCredentialHosts(t *testing.T) {
	for _, probe := range []struct {
		profile Profile
		url     string
	}{
		{DigitalOcean, "https://api.digitalocean.com/v2/droplets"},
		{Vultr, "https://api.vultr.com/v2/instances"},
		{Contabo, "https://api.contabo.com/v1/compute/instances"},
		{ContaboAuth, "https://auth.contabo.com/auth/realms/contabo/protocol/openid-connect/token"},
	} {
		if _, err := probe.profile.URL(probe.url); err != nil {
			t.Fatal("compiled endpoint rejected")
		}
		for _, raw := range []string{"https://evil.example/v2/droplets", "http://api.digitalocean.com/v2/droplets", "https://api.contabo.com/v1/../v2/instances", "https://user:secret@api.vultr.com/v2/instances"} {
			if _, err := probe.profile.URL(raw); err == nil {
				t.Fatal("credential endpoint escape")
			}
		}
	}
}
