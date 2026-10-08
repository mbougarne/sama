package credentials

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeyringPermissionsDuplicatesAndRequiredKeys(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	valid := `{"active":"key-1","keys":[{"id":"key-1","key":"` + key + `"}]}`
	path := filepath.Join(t.TempDir(), "keyring")
	for _, input := range []string{valid, strings.Replace(valid, `"active":"key-1"`, `"active":"key-1","active":"key-1"`, 1), strings.Replace(valid, `}]}`, `},{"id":"key-1","key":"`+key+`"}]}`, 1), strings.Replace(valid, key, "invalid-secret-sentinel", 1), strings.Replace(valid, `"active":"key-1"`, `"active":"absent"`, 1), valid + valid, strings.Repeat("x", 65537)} {
		if err := os.WriteFile(path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		ring, err := LoadKeyring(path)
		if input == valid {
			if err != nil || ring.Require([]string{"key-1"}) != nil || ring.Require([]string{"absent"}) != ErrKeyring {
				t.Fatal("key availability", err)
			}
		} else if err != ErrKeyring {
			t.Fatal("invalid keyring accepted")
		}
	}
	if err := os.WriteFile(path, []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []os.FileMode{0644, 0400, 0700} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadKeyring(path); err != ErrKeyring {
			t.Fatal("unsafe permissions")
		}
	}
	if _, err := LoadKeyring(filepath.Dir(path)); err != ErrKeyring {
		t.Fatal("directory accepted")
	}
	link := path + "-link"
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKeyring(link); err != ErrKeyring {
		t.Fatal("symlink accepted")
	}
}
