package operation

import (
	"crypto/sha256"
	"encoding/hex"
	"sama/backend/internal/platform/outbound"
)

func quotaKey(brand, family, account string) string {
	h := sha256.Sum256([]byte(brand + "\x00" + family + "\x00" + account))
	return hex.EncodeToString(h[:])
}
func (s *Service) WithBudget(b *outbound.Budget) *Service { s.budget = b; return s }
