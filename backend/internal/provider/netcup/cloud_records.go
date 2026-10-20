package netcup

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"sama/backend/internal/provider"
)

// CloudZone identifies a domain already established by an authorized read.
// It does not authenticate an API key or assert the domain is DNS-managed.
func CloudZone(b provider.Binding, domainID, fqdn string) (Zone, error) {
	if b.Family() != CloudFamily {
		return Zone{}, provider.ErrBinding
	}
	id, err := provider.NumericID(json.Number(domainID))
	if err != nil {
		return Zone{}, err
	}
	name, err := zoneName(fqdn)
	if err != nil {
		return Zone{}, err
	}
	return Zone{b, id, name}, nil
}

// CloudRevision is evidence supplied by the revision read, not an assertion that
// a particular provider state means current/committed. That vocabulary is not
// enumerated by the inspected schema. No revision is created or committed here.
type CloudRevision struct {
	Identifier string
	Serial     *int64
	State      string
}

func canonicalUUID(raw string) bool {
	id, err := uuid.Parse(raw)
	return err == nil && id != uuid.Nil && id.String() == raw
}

// DecodeCloudRecords implements only the documented read response boundary.
// There is intentionally no credential-bearing CloudDNS client: the public
// schema names an undefined bearerAuth scheme and has no account identity read.
// Authentication/identity must be established before a factory can fetch data.
func DecodeCloudRecords(b provider.Binding, zone Zone, revision CloudRevision, body []byte) (Snapshot, error) {
	if b.Family() != CloudFamily || zone.binding != b {
		return Snapshot{}, provider.ErrBinding
	}
	if !canonicalUUID(revision.Identifier) || len(revision.State) == 0 || len(revision.State) > 128 || revision.Serial == nil || *revision.Serial < 0 || len(body) > 8<<20 {
		return Snapshot{}, provider.InvalidResponse()
	}
	var response struct {
		Success *bool           `json:"success"`
		Errors  json.RawMessage `json:"errors"`
		Meta    *struct {
			Pagination *struct {
				Page    int  `json:"page"`
				PerPage int  `json:"perPage"`
				Total   *int `json:"totalEntries"`
				Next    *int `json:"nextPage"`
				Last    int  `json:"lastPage"`
			} `json:"pagination"`
		} `json:"meta"`
		Result *[]struct {
			Identifier string `json:"identifier"`
			Name       string `json:"name"`
			Type       string `json:"type"`
			Data       string `json:"rdata"`
			TTL        *int64 `json:"ttl"`
			Immutable  *bool  `json:"immutable"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &response) != nil || response.Success == nil || !*response.Success || response.Meta == nil || response.Meta.Pagination == nil || response.Result == nil {
		return Snapshot{}, provider.InvalidResponse()
	}
	var errors []json.RawMessage
	if json.Unmarshal(response.Errors, &errors) != nil || errors == nil || len(errors) > 0 {
		return Snapshot{}, provider.InvalidResponse()
	}
	page := response.Meta.Pagination
	// This endpoint declares no page query parameters. Never invent a next URL
	// or mark a truncated response complete. Empty metadata is also unverified.
	if page.Page != 1 || page.PerPage < 1 || page.Total == nil || page.Next == nil || *page.Total != len(*response.Result) || *page.Total > maxRecords || *page.Next != 0 || page.Last != 1 || len(*response.Result) > page.PerPage {
		return Snapshot{}, provider.InvalidResponse()
	}
	sets := []RecordSet{}
	groups := map[string]int{}
	ids := map[string]bool{}
	for _, record := range *response.Result {
		if !canonicalUUID(record.Identifier) || ids[record.Identifier] || record.Immutable == nil {
			return Snapshot{}, provider.InvalidResponse()
		}
		ids[record.Identifier] = true
		// CloudDNS names are interpreted as DNS owner names scoped to this zone.
		name, err := recordName(record.Name, zone.name)
		if err != nil {
			return Snapshot{}, err
		}
		kind := strings.ToUpper(record.Type)
		key := name + "/" + kind
		index, ok := groups[key]
		if !ok {
			index = len(sets)
			groups[key] = index
			source := "record"
			if record.TTL == nil {
				source = "unknown"
			}
			sets = append(sets, RecordSet{Name: name, Type: kind, TTL: record.TTL, TTLSource: source, KnownType: knownType(kind), Immutable: record.Immutable})
		} else {
			set := sets[index]
			if (set.TTL == nil) != (record.TTL == nil) || set.TTL != nil && *set.TTL != *record.TTL {
				return Snapshot{}, provider.InvalidResponse()
			}
			if *record.Immutable {
				sets[index].Immutable = record.Immutable
			}
		}
		sets[index].Values = append(sets[index].Values, DNSValue{Data: record.Data})
	}
	return snapshot(zone, revision.Identifier+"/"+strconv.FormatInt(*revision.Serial, 10)+"/"+revision.State, sets)
}
