package identifier

import (
	"encoding/json"
	"testing"
)

func TestNewProducesUUIDv7AndCanonicalJSON(t *testing.T) {
	id, err := New()
	if err != nil {
		t.Fatal("UUIDv7 generation failed")
	}
	if id.Version() != 7 || id.Variant() != 1 {
		t.Fatal("generated identifier is not an RFC UUIDv7")
	}
	encoded, err := json.Marshal(id)
	if err != nil {
		t.Fatal("identifier JSON encoding failed")
	}
	var decoded string
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != id.String() {
		t.Fatal("identifier did not round-trip as its canonical JSON string")
	}
	parsed, err := Parse(decoded)
	if err != nil || parsed != id {
		t.Fatal("canonical JSON identifier did not pass public parsing")
	}
}

func TestParseRejectsNonCanonicalAndNumericIdentifiers(t *testing.T) {
	for _, value := range []string{
		"42",
		"018f1f4e7b0b7cc398df86d5c7d6d80a",
		"018F1F4E-7B0B-7CC3-98DF-86D5C7D6D80A",
		"{018f1f4e-7b0b-7cc3-98df-86d5c7d6d80a}",
		"f47ac10b-58cc-4372-a567-0e02b2c3d479",
		"",
	} {
		if _, err := Parse(value); err == nil || err.Error() != ErrInvalid.Error() {
			t.Fatal("non-canonical public identifier was accepted")
		}
	}
}
