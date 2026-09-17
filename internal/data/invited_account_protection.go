package data

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"math/big"
	"time"
)

type invitedAccountCodeGenerator struct{}

func NewInvitedAccountCodeGenerator() biz.InvitedAccountCodeGenerator {
	return invitedAccountCodeGenerator{}
}
func (invitedAccountCodeGenerator) GenerateEmailVerificationCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", errOutboxProtection
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
func deriveInvitedAccountKey(key []byte) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte("ani-iam:invited-account-verification-key:v1"))
	return h.Sum(nil)
}
func (p *OutboxProtector) invitedAccountDigest(version, purpose string, id uuid.UUID, value any) ([]byte, error) {
	if p == nil || len(p.verificationKeys[version]) != 32 || id.Version() != 7 {
		return nil, errOutboxProtection
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, errOutboxProtection
	}
	h := hmac.New(sha256.New, p.verificationKeys[version])
	_, _ = h.Write([]byte("ani-iam:invited-account:" + purpose + ":v1|" + version + "|" + id.String() + "|"))
	_, _ = h.Write(raw)
	return h.Sum(nil), nil
}
func (p *OutboxProtector) verificationDigest(version string, id uuid.UUID, email, code string, expires time.Time) ([]byte, error) {
	return p.invitedAccountDigest(version, "email-code", id, struct {
		Email, Code string
		Expires     int64
	}{email, code, expires.Unix()})
}
func (p *OutboxProtector) completionDigest(version string, id uuid.UUID, intent [32]byte) ([]byte, error) {
	return p.invitedAccountDigest(version, "completion-receipt", id, intent)
}

type invitedAccountDeliveryPayload struct {
	Email, Code string
	ExpiresAt   time.Time
}

func invitedAccountDeliveryAAD(version string, challenge, delivery uuid.UUID) []byte {
	return []byte("ani-iam:invited-account-delivery:v1|" + version + "|" + challenge.String() + "|" + delivery.String())
}
func (p *OutboxProtector) sealInvitedAccount(challenge, delivery uuid.UUID, payload invitedAccountDeliveryPayload) (string, []byte, error) {
	if p == nil || p.keys[p.active] == nil || challenge.Version() != 7 || delivery.Version() != 7 || payload.Email == "" || len(payload.Code) != 6 || payload.ExpiresAt.IsZero() {
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
	return p.active, aead.Seal(nonce, nonce, raw, invitedAccountDeliveryAAD(p.active, challenge, delivery)), nil
}
func (p *OutboxProtector) openInvitedAccount(version string, challenge, delivery uuid.UUID, encrypted []byte) (invitedAccountDeliveryPayload, error) {
	var value invitedAccountDeliveryPayload
	if p == nil || p.keys[version] == nil {
		return value, errOutboxProtection
	}
	aead := p.keys[version]
	if len(encrypted) < aead.NonceSize()+aead.Overhead() {
		return value, errOutboxProtection
	}
	raw, err := aead.Open(nil, encrypted[:aead.NonceSize()], encrypted[aead.NonceSize():], invitedAccountDeliveryAAD(version, challenge, delivery))
	if err != nil || json.Unmarshal(raw, &value) != nil || value.Email == "" || len(value.Code) != 6 || value.ExpiresAt.IsZero() {
		return invitedAccountDeliveryPayload{}, errOutboxProtection
	}
	return value, nil
}
