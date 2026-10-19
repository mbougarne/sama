package digitalocean

import (
	"testing"

	"sama/backend/internal/connection"
	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
	"sama/backend/internal/provider/qualification"
	"sama/backend/internal/testpower"
)

func TestLivePowerActions(t *testing.T) {
	for _, action := range []string{"compute.server.power_on", "compute.server.shutdown", "compute.server.reboot", "compute.server.power_off"} {
		t.Run(action, func(t *testing.T) {
			testpower.Live(t, Family, action, func(b provider.Binding, c connection.Credential, budget *outbound.Budget) (qualification.PowerClient, error) {
				return New(b, c, budget)
			})
		})
	}
}
