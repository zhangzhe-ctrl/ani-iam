package data

import (
	"bytes"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestInvitedAccountCodeAndDeliveryPurposeIsolation(t *testing.T) {
	p, err := NewOutboxProtector("v1", map[string][]byte{"v1": bytes.Repeat([]byte{1}, 32), "v2": bytes.Repeat([]byte{2}, 32)})
	if err != nil {
		t.Fatal("key prerequisite")
	}
	challenge, delivery := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	expires := time.Now().UTC().Truncate(time.Second).Add(10 * time.Minute)
	value := invitedAccountDeliveryPayload{Email: "invited@example.test", Code: "042781", ExpiresAt: expires}
	version, encrypted, err := p.sealInvitedAccount(challenge, delivery, value)
	if err != nil {
		t.Fatal("code delivery sealing")
	}
	if bytes.Contains(encrypted, []byte(value.Email)) || bytes.Contains(encrypted, []byte(value.Code)) {
		t.Fatal("cleartext verification delivery persisted")
	}
	got, err := p.openInvitedAccount(version, challenge, delivery, encrypted)
	if err != nil || got.Email != value.Email || got.Code != value.Code || !got.ExpiresAt.Equal(expires) {
		t.Fatal("verification delivery round trip")
	}
	for _, x := range []struct {
		v    string
		c, d uuid.UUID
	}{{"v2", challenge, delivery}, {"v1", uuid.Must(uuid.NewV7()), delivery}, {"v1", challenge, uuid.Must(uuid.NewV7())}} {
		if _, err = p.openInvitedAccount(x.v, x.c, x.d, encrypted); err == nil {
			t.Fatal("verification delivery substitution accepted")
		}
	}
	if _, err = p.openPlatformInvitation(version, challenge, delivery, 1, encrypted); err == nil {
		t.Fatal("code accepted as Invitation delivery")
	}
	original, err := p.verificationDigest(version, challenge, value.Email, value.Code, expires)
	if err != nil {
		t.Fatal("code verifier")
	}
	for _, x := range []struct {
		v           string
		c           uuid.UUID
		email, code string
		expires     time.Time
	}{{"v2", challenge, value.Email, value.Code, expires}, {"v1", uuid.Must(uuid.NewV7()), value.Email, value.Code, expires}, {"v1", challenge, "other@example.test", value.Code, expires}, {"v1", challenge, value.Email, "042782", expires}, {"v1", challenge, value.Email, value.Code, expires.Add(time.Second)}} {
		digest, e := p.verificationDigest(x.v, x.c, x.email, x.code, x.expires)
		if e != nil || bytes.Equal(original, digest) {
			t.Fatal("code verifier failed purpose binding")
		}
	}
	receipt, err := p.completionDigest(version, challenge, [32]byte{})
	if err != nil || bytes.Equal(original, receipt) {
		t.Fatal("receipt and code share authentication domain")
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, err = p.openInvitedAccount(version, challenge, delivery, encrypted); err == nil {
		t.Fatal("tampered code delivery accepted")
	}
}
