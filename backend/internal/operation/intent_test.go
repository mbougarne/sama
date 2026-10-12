package operation

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCanonicalIntentBindsActorTargetReviewAndRejectsSecretFields(t *testing.T) {
	a, err := DecodeIntent([]byte(`{"action":"compute.server.reboot","parameters":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := DecodeIntent([]byte("{\n\"parameters\":{}, \"action\":\"compute.server.reboot\"}"))
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]uuid.UUID, 5)
	for n := range ids {
		ids[n], _ = uuid.NewV7()
	}
	digest, err := Digest(a, ids[0], ids[1], ids[2], ids[3], ids[4])
	equivalent, _ := Digest(b, ids[0], ids[1], ids[2], ids[3], ids[4])
	if err != nil || digest != equivalent {
		t.Fatal("canonical equivalents differ")
	}
	for n := range ids {
		changed := append([]uuid.UUID(nil), ids...)
		changed[n], _ = uuid.NewV7()
		hash, _ := Digest(a, changed[0], changed[1], changed[2], changed[3], changed[4])
		if hash == digest {
			t.Fatal("scope not bound", n)
		}
	}
	for _, input := range []string{
		`{"action":"compute.server.reboot"}`,
		`{"action":"compute.server.reboot","parameters":{"credential":"synthetic-secret"}}`,
		`{"action":"compute.server.reboot","parameters":{},"target":"edited"}`,
		`{"action":"compute.server.delete","parameters":{}}`,
		`{"action":"compute.server.reboot","parameters":{}} {}`,
	} {
		_, err := DecodeIntent([]byte(input))
		if err == nil || strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatal("unsafe intent decode")
		}
	}
}
