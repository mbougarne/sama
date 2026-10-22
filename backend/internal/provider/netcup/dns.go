// Package netcup keeps SCP, Legacy DNS and CloudDNS qualification separate.
package netcup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"sama/backend/internal/provider"
)

const LegacyFamily = "netcup.legacy_dns"
const CloudFamily = "netcup.clouddns"
const maxRecords = 2000

// Zone is an adapter-created identity, never a globally reusable provider ID.
type Zone struct {
	binding  provider.Binding
	nativeID string
	name     string
}

type DNSValue = provider.DNSValue
type RecordSet = provider.DNSRecordSet

// Snapshot carries read evidence, not a conditional-write token. A matching
// local fingerprint cannot close the race between a provider read and write.
type Snapshot struct {
	Zone        Zone
	Version     string
	Fingerprint string
	RecordSets  []RecordSet
}

var dnsLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var dnsType = regexp.MustCompile(`^[A-Z][A-Z0-9]{0,15}$`)

func zoneName(raw string) (string, error) {
	name := strings.ToLower(strings.TrimSuffix(raw, "."))
	if len(name) < 3 || len(name) > 253 || !strings.Contains(name, ".") {
		return "", provider.InvalidResponse()
	}
	for _, part := range strings.Split(name, ".") {
		if !dnsLabel.MatchString(part) {
			return "", provider.InvalidResponse()
		}
	}
	return name, nil
}

func recordName(raw, zone string) (string, error) {
	if raw == "@" {
		return zone, nil
	}
	absolute := strings.HasSuffix(raw, ".")
	name := strings.ToLower(strings.TrimSuffix(raw, "."))
	if name != zone && !strings.HasSuffix(name, "."+zone) {
		if absolute {
			return "", provider.InvalidResponse()
		}
		name += "." + zone
	}
	if len(name) > 253 {
		return "", provider.InvalidResponse()
	}
	for i, label := range strings.Split(name, ".") {
		if i == 0 && label == "*" {
			continue
		}
		// Service records may contain underscores. They remain read-only.
		if len(label) == 0 || len(label) > 63 || !dnsLabel.MatchString(strings.ReplaceAll(label, "_", "a")) {
			return "", provider.InvalidResponse()
		}
	}
	return name, nil
}

func knownType(kind string) bool {
	// Official Legacy recordtypes XSD and CloudDNS DnsRecordInput enum,
	// inspected 2026-10-20. A known native type grants no write capability.
	return slices.Contains([]string{"A", "AAAA", "MX", "TXT", "CNAME", "SRV", "NS", "DS", "TLSA", "CAA", "SSHFP", "SMIMEA", "OPENPGPKEY"}, kind)
}

func snapshot(zone Zone, version string, sets []RecordSet) (Snapshot, error) {
	for i := range sets {
		set := &sets[i]
		if !dnsType.MatchString(set.Type) || set.TTL != nil && (*set.TTL < 0 || *set.TTL > 2147483647) {
			return Snapshot{}, provider.InvalidResponse()
		}
		for _, v := range set.Values {
			if !utf8.ValidString(v.Data) || len(v.Data) > 8192 || strings.ContainsRune(v.Data, 0) || len(v.Priority) > 10 {
				return Snapshot{}, provider.InvalidResponse()
			}
		}
		slices.SortFunc(set.Values, func(a, b DNSValue) int {
			if a.Priority != b.Priority {
				return strings.Compare(a.Priority, b.Priority)
			}
			return strings.Compare(a.Data, b.Data)
		})
	}
	slices.SortFunc(sets, func(a, b RecordSet) int {
		if a.Name != b.Name {
			return strings.Compare(a.Name, b.Name)
		}
		return strings.Compare(a.Type, b.Type)
	})
	data, err := json.Marshal(struct {
		Workspace, Connection, Family string
		CredentialVersion             int64
		NativeID, Name, Version       string
		Sets                          []RecordSet
	}{
		zone.binding.Workspace().String(), zone.binding.Connection().String(), zone.binding.Family(), zone.binding.Version(), zone.nativeID, zone.name, version, sets,
	})
	if err != nil {
		return Snapshot{}, provider.InvalidResponse()
	}
	sum := sha256.Sum256(data)
	return Snapshot{zone, version, hex.EncodeToString(sum[:]), sets}, nil
}

// CheckWrite deliberately never grants a write: neither inspected DNS contract
// establishes an atomic comparison against the reviewed version. Keep this gate
// at the adapter boundary even if callers have operate permission.
func CheckWrite(binding provider.Binding, before, current Snapshot) error {
	if binding != before.Zone.binding || binding != current.Zone.binding || before.Zone.nativeID != current.Zone.nativeID || before.Zone.name != current.Zone.name {
		return provider.ErrBinding
	}
	if before.Fingerprint == "" || before.Fingerprint != current.Fingerprint {
		return provider.ErrPrecondition
	}
	return provider.ErrActionUnsupported
}
