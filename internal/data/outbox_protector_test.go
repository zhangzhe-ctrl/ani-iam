package data

import (
	"bytes"
	"github.com/google/uuid"
	"testing"
)

func TestOutboxEncryptionBindsRecordPrincipalAndKeyVersion(t *testing.T) {
	p, err := NewOutboxProtector("one", map[string][]byte{"one": bytes.Repeat([]byte{1}, 32), "two": bytes.Repeat([]byte{2}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	id, op, human := uuid.New(), uuid.New(), uuid.New()
	version, encrypted, err := p.seal(id, op, human, "human@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("human@example.test")) {
		t.Fatal("plaintext persisted")
	}
	plain, err := p.open(version, encrypted, id, op, human)
	if err != nil || plain != "human@example.test" {
		t.Fatal("round trip failed")
	}
	for _, ids := range [][3]uuid.UUID{{uuid.New(), op, human}, {id, uuid.New(), human}, {id, op, uuid.New()}} {
		if _, err = p.open(version, encrypted, ids[0], ids[1], ids[2]); err == nil {
			t.Fatal("record substitution accepted")
		}
	}
	for _, v := range []string{"two", "missing"} {
		if _, err = p.open(v, encrypted, id, op, human); err == nil {
			t.Fatal("key substitution accepted")
		}
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, err = p.open(version, encrypted, id, op, human); err == nil {
		t.Fatal("tampering accepted")
	}
}
