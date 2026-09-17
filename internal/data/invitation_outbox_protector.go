package data

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
)

// The delivery token is never part of a public result, ledger or audit. This
// AEAD domain cannot be swapped with a password-action outbox ciphertext.
type tenantInvitationDeliveryPayload struct{ Email, Token string }

func invitationDeliveryAAD(version string, tenant, invitation, delivery uuid.UUID, generation int64) []byte {
	return []byte(fmt.Sprintf("ani-iam:tenant-invitation-delivery:v1|%s|%s|%s|%s|%d", version, tenant, invitation, delivery, generation))
}
func (p *OutboxProtector) sealTenantInvitation(tenant, invitation, delivery uuid.UUID, generation int64, payload tenantInvitationDeliveryPayload) (string, []byte, error) {
	if p == nil || p.keys[p.active] == nil || tenant == uuid.Nil || invitation == uuid.Nil || delivery == uuid.Nil || generation < 1 || payload.Email == "" || len(payload.Token) < 32 {
		return "", nil, errOutboxProtection
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", nil, errOutboxProtection
	}
	aead := p.keys[p.active]
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", nil, errOutboxProtection
	}
	return p.active, aead.Seal(nonce, nonce, raw, invitationDeliveryAAD(p.active, tenant, invitation, delivery, generation)), nil
}
func (p *OutboxProtector) openTenantInvitation(version string, tenant, invitation, delivery uuid.UUID, generation int64, encrypted []byte) (tenantInvitationDeliveryPayload, error) {
	var value tenantInvitationDeliveryPayload
	if p == nil || p.keys[version] == nil {
		return value, errOutboxProtection
	}
	aead := p.keys[version]
	if len(encrypted) < aead.NonceSize()+aead.Overhead() {
		return value, errOutboxProtection
	}
	raw, err := aead.Open(nil, encrypted[:aead.NonceSize()], encrypted[aead.NonceSize():], invitationDeliveryAAD(version, tenant, invitation, delivery, generation))
	if err != nil || json.Unmarshal(raw, &value) != nil || value.Email == "" || len(value.Token) < 32 {
		return tenantInvitationDeliveryPayload{}, errOutboxProtection
	}
	return value, nil
}

type platformInvitationDeliveryPayload struct{ Email, Token string }

func platformInvitationDeliveryAAD(version string, invitation, delivery uuid.UUID, generation int64) []byte {
	return []byte(fmt.Sprintf("ani-iam:platform-invitation-delivery:v1|%s|%s|%s|%d", version, invitation, delivery, generation))
}
func (p *OutboxProtector) sealPlatformInvitation(invitation, delivery uuid.UUID, generation int64, payload platformInvitationDeliveryPayload) (string, []byte, error) {
	if p == nil || p.keys[p.active] == nil || invitation == uuid.Nil || delivery == uuid.Nil || generation < 1 || payload.Email == "" || len(payload.Token) < 32 {
		return "", nil, errOutboxProtection
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", nil, errOutboxProtection
	}
	aead := p.keys[p.active]
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", nil, errOutboxProtection
	}
	return p.active, aead.Seal(nonce, nonce, raw, platformInvitationDeliveryAAD(p.active, invitation, delivery, generation)), nil
}
func (p *OutboxProtector) openPlatformInvitation(version string, invitation, delivery uuid.UUID, generation int64, encrypted []byte) (platformInvitationDeliveryPayload, error) {
	var value platformInvitationDeliveryPayload
	if p == nil || p.keys[version] == nil {
		return value, errOutboxProtection
	}
	aead := p.keys[version]
	if len(encrypted) < aead.NonceSize()+aead.Overhead() {
		return value, errOutboxProtection
	}
	raw, err := aead.Open(nil, encrypted[:aead.NonceSize()], encrypted[aead.NonceSize():], platformInvitationDeliveryAAD(version, invitation, delivery, generation))
	if err != nil || json.Unmarshal(raw, &value) != nil || value.Email == "" || len(value.Token) < 32 {
		return platformInvitationDeliveryPayload{}, errOutboxProtection
	}
	return value, nil
}
