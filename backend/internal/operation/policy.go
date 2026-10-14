package operation

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"
	"strconv"
	"strings"
)

// Policy is immutable. Switches only narrow the separately qualified registry;
// a configuration entry can never qualify an adapter or grant account permission.
type Policy struct {
	allowed map[string]bool
	version int64
}

func NewPolicy(switches []string, qualified map[string]bool) *Policy {
	keys := append([]string(nil), switches...)
	sort.Strings(keys)
	p := &Policy{allowed: map[string]bool{}}
	for _, key := range keys {
		if qualified[key] {
			p.allowed[key] = true
		}
	}
	hash := sha256.Sum256([]byte(strings.Join(keys, "\n")))
	p.version = int64(binary.BigEndian.Uint64(hash[:8]) & 0x7fffffffffffffff)
	return p
}

func (p *Policy) permits(family, action string) bool {
	return p != nil && p.allowed[family+":"+action]
}

func (s *Service) WithPolicy(p *Policy) *Service { s.policy = p; return s }

func (s *Service) policyVersion(workspace int64) int64 {
	if s.policy == nil {
		return workspace
	}
	h := sha256.Sum256([]byte(strconv.FormatInt(workspace, 10) + ":" + strconv.FormatInt(s.policy.version, 10)))
	return int64(binary.BigEndian.Uint64(h[:8])&0x3fffffffffffffff) + 1
}
