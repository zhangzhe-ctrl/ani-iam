package data

import (
	"bytes"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestTenantInvitationOutboxEncryptionBindsPurposeBoundaryAndGeneration(t *testing.T) {
	p, err := NewOutboxProtector("unit", map[string][]byte{"unit": bytes.Repeat([]byte{23}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	tenant, invitation, delivery := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	value := tenantInvitationDeliveryPayload{Email: "invitee@example.test", Token: strings.Repeat("x", 43)}
	version, encrypted, err := p.sealTenantInvitation(tenant, invitation, delivery, 1, value)
	if err != nil || bytes.Contains(encrypted, []byte(value.Token)) || bytes.Contains(encrypted, []byte(value.Email)) {
		t.Fatal("delivery encryption failed")
	}
	clear, err := p.openTenantInvitation(version, tenant, invitation, delivery, 1, encrypted)
	if err != nil || clear != value {
		t.Fatal("own invitation decryption failed")
	}
	for _, change := range []string{"tenant", "invitation", "delivery", "generation", "ciphertext"} {
		t.Run(change, func(t *testing.T) {
			a, b, c, g := tenant, invitation, delivery, int64(1)
			payload := bytes.Clone(encrypted)
			switch change {
			case "tenant":
				a = uuid.Must(uuid.NewV7())
			case "invitation":
				b = uuid.Must(uuid.NewV7())
			case "delivery":
				c = uuid.Must(uuid.NewV7())
			case "generation":
				g = 2
			case "ciphertext":
				payload[len(payload)-1] ^= 1
			}
			if _, err = p.openTenantInvitation(version, a, b, c, g, payload); err == nil {
				t.Fatal("foreign or modified delivery decrypted")
			}
		})
	}
	if _, err = p.open(version, encrypted, tenant, invitation, delivery); err == nil {
		t.Fatal("invitation decrypted as password action")
	}
}

func TestPlatformInvitationOutboxCannotCrossPurposeOrDelivery(t *testing.T) {
	p, err := NewOutboxProtector("unit", map[string][]byte{"unit": bytes.Repeat([]byte{23}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	invitation, delivery, tenant := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	value := platformInvitationDeliveryPayload{Email: "platform-invitee@example.test", Token: strings.Repeat("x", 43)}
	version, ciphertext, err := p.sealPlatformInvitation(invitation, delivery, 1, value)
	if err != nil || bytes.Contains(ciphertext, []byte(value.Token)) {
		t.Fatal("Platform invitation encryption failed")
	}
	clear, err := p.openPlatformInvitation(version, invitation, delivery, 1, ciphertext)
	if err != nil || clear != value {
		t.Fatal("Platform invitation decryption failed")
	}
	if _, err = p.openPlatformInvitation(version, invitation, delivery, 2, ciphertext); err == nil {
		t.Fatal("generation swap decrypted")
	}
	if _, err = p.openPlatformInvitation(version, delivery, invitation, 1, ciphertext); err == nil {
		t.Fatal("delivery swap decrypted")
	}
	if _, err = p.openTenantInvitation(version, tenant, invitation, delivery, 1, ciphertext); err == nil {
		t.Fatal("Platform ciphertext decrypted as Tenant invitation")
	}
	if _, err = p.open(version, ciphertext, tenant, invitation, delivery); err == nil {
		t.Fatal("Platform ciphertext decrypted as password action")
	}
	_, tenantCipher, err := p.sealTenantInvitation(tenant, invitation, delivery, 1, tenantInvitationDeliveryPayload{Email: value.Email, Token: value.Token})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.openPlatformInvitation(version, invitation, delivery, 1, tenantCipher); err == nil {
		t.Fatal("Tenant ciphertext decrypted as Platform invitation")
	}
}
