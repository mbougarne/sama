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

func TestWorkersRequireExplicitDatabaseCallingMode(t *testing.T) {
	for _, values := range []map[string]string{
		{"SAMA_WORKERS_ENABLED": "yes"},
		{"SAMA_WORKERS_ENABLED": "1"},
		{"SAMA_WORKERS_ENABLED": "1", "SAMA_DATABASE_URL": "postgres://localhost/sama", "SAMA_PROVIDER_CALLING_PROCESSES": "0"},
	} {
		_, err := LoadFrom(func(name string) (string, bool) { v, ok := values[name]; return v, ok }, func(string) ([]byte, error) { return nil, nil })
		if err == nil {
			t.Fatal("unsafe worker configuration accepted")
		}
	}
}
