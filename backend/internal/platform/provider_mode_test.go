package platform

import "testing"

func TestProviderCallingProcessLimit(t *testing.T) {
	for _, value := range []string{"0", "1", "2", "-1", "invalid"} {
		budgets, err := loadBudgets(func(key string) (string, bool) { return value, key == "SAMA_PROVIDER_CALLING_PROCESSES" })
		valid := value == "0" || value == "1"
		if valid && err != nil || !valid && err == nil {
			t.Fatal(value, err)
		}
		if value == "0" && budgets.ProviderCallingProcesses != 0 {
			t.Fatal("DB-only mode")
		}
	}
}
