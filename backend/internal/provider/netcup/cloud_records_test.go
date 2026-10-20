package netcup

import (
	"encoding/json"
	"strings"
	"testing"

	"sama/backend/internal/provider"
)

// Synthetic wire fields follow the official netcup API schema version
// 2026.0921.153936, inspected 2026-10-20. State is deliberately opaque: the
// schema does not enumerate it. This fixture proves no live provider behavior.
const cloudRecords = `{"success":true,"errors":[],"messages":[],"meta":{"pagination":{"page":1,"perPage":100,"totalEntries":3,"nextPage":0,"lastPage":1}},"result":[{"identifier":"10000000-0000-4000-8000-000000000001","name":"@","type":"A","rdata":"192.0.2.1","ttl":0,"immutable":false},{"identifier":"10000000-0000-4000-8000-000000000002","name":"_service","type":"TYPE65400","rdata":"opaque","ttl":null,"immutable":false},{"identifier":"10000000-0000-4000-8000-000000000003","name":"www.example.test.","type":"TXT","rdata":"example text","ttl":300,"immutable":true}]}`

func cloudFixture(t *testing.T) (provider.Binding, Zone, CloudRevision) {
	t.Helper()
	b := binding(t, CloudFamily, 1)
	z, err := CloudZone(b, "12", "EXAMPLE.TEST.")
	if err != nil {
		t.Fatal(err)
	}
	serial := int64(42)
	return b, z, CloudRevision{Identifier: "20000000-0000-4000-8000-000000000001", Serial: &serial, State: "opaque-native-state"}
}

func TestCloudReadPreservesTTLUnknownTypesAndImmutability(t *testing.T) {
	b, z, r := cloudFixture(t)
	s, err := DecodeCloudRecords(b, z, r, []byte(cloudRecords))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.RecordSets) != 3 || len(s.Fingerprint) != 64 || !strings.HasSuffix(s.Version, "/42/opaque-native-state") {
		t.Fatal("lost revision evidence")
	}
	var unknownTTL, zero, immutable bool
	for _, set := range s.RecordSets {
		switch set.Name {
		case "_service.example.test":
			unknownTTL = set.TTL == nil && set.TTLSource == "unknown" && !set.KnownType
		case "example.test":
			zero = set.TTL != nil && *set.TTL == 0 && set.TTLSource == "record"
		case "www.example.test":
			immutable = set.KnownType && set.Immutable != nil && *set.Immutable
		}
	}
	if !unknownTTL || !zero || !immutable {
		t.Fatal("fabricated TTL or editable type")
	}
	if CheckWrite(b, s, s) != provider.ErrActionUnsupported {
		t.Fatal("revision UUID treated as CAS")
	}
	other := binding(t, CloudFamily, 1)
	if _, err := DecodeCloudRecords(other, z, r, []byte(cloudRecords)); err != provider.ErrBinding {
		t.Fatal("cross-workspace snapshot")
	}
}

func TestCloudRejectsIncompleteOrContradictoryReadEvidence(t *testing.T) {
	for _, kind := range []string{"failure", "missing_errors", "errors", "missing_meta", "missing_total", "missing_next", "next_page", "truncated", "limit", "duplicate_id", "outside_zone", "missing_immutable", "bad_type", "negative_ttl"} {
		t.Run(kind, func(t *testing.T) {
			b, z, r := cloudFixture(t)
			var wire map[string]any
			if json.Unmarshal([]byte(cloudRecords), &wire) != nil {
				t.Fatal("fixture")
			}
			meta := wire["meta"].(map[string]any)["pagination"].(map[string]any)
			records := wire["result"].([]any)
			switch kind {
			case "failure":
				wire["success"] = false
			case "missing_errors":
				delete(wire, "errors")
			case "errors":
				wire["errors"] = []any{map[string]any{"message": "private provider diagnostic"}}
			case "missing_meta":
				delete(wire, "meta")
			case "next_page":
				meta["nextPage"] = 2
			case "missing_total":
				delete(meta, "totalEntries")
			case "missing_next":
				delete(meta, "nextPage")
			case "truncated":
				meta["totalEntries"] = 4
			case "limit":
				meta["perPage"] = 1
			case "duplicate_id":
				records[1].(map[string]any)["identifier"] = records[0].(map[string]any)["identifier"]
			case "outside_zone":
				records[0].(map[string]any)["name"] = "outside.test."
			case "missing_immutable":
				delete(records[0].(map[string]any), "immutable")
			case "bad_type":
				records[0].(map[string]any)["type"] = "A/path"
			case "negative_ttl":
				records[0].(map[string]any)["ttl"] = -1
			}
			body, _ := json.Marshal(wire)
			if _, err := DecodeCloudRecords(b, z, r, body); err == nil {
				t.Fatal("unsafe read accepted")
			}
		})
	}
}

func TestCloudRevisionAndZoneAreNotReusableIdentities(t *testing.T) {
	b, z, r := cloudFixture(t)
	for _, bad := range []CloudRevision{{}, {Identifier: r.Identifier, State: r.State}, {Identifier: "../revision", Serial: r.Serial, State: r.State}} {
		if _, err := DecodeCloudRecords(b, z, bad, []byte(cloudRecords)); err == nil {
			t.Fatal("unverified revision")
		}
	}
	legacy := binding(t, LegacyFamily, 1)
	if _, err := CloudZone(legacy, "12", "example.test"); err != provider.ErrBinding {
		t.Fatal("credential family crossed")
	}
	if _, err := CloudZone(b, "../12", "example.test"); err == nil {
		t.Fatal("unscoped domain ID")
	}
	if _, err := DecodeCloudRecords(b, z, r, []byte(strings.Repeat("x", (8<<20)+1))); err == nil {
		t.Fatal("byte cap")
	}
}
