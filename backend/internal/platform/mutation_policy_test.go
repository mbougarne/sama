package platform

import "testing"

func TestMutationSwitchConfigurationRejectsAmbiguity(t *testing.T) {
	for _, value := range []string{"*", "fixture/compute:compute.server.reboot,fixture/compute:compute.server.reboot", "https://example.com:token", ",", " fixture/compute:compute.server.reboot"} {
		_, err := LoadFrom(func(k string) (string, bool) { return value, k == "SAMA_ENABLED_MUTATIONS" }, nil)
		if err == nil {
			t.Fatal("invalid switch accepted")
		}
	}
	c, err := LoadFrom(func(k string) (string, bool) {
		return "fixture/compute:compute.server.reboot", k == "SAMA_ENABLED_MUTATIONS"
	}, nil)
	if err != nil || len(c.MutationSwitches) != 1 {
		t.Fatal("valid switch rejected", err)
	}
}
