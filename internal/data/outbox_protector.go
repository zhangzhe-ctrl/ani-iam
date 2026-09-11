package data

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"os"
	"strings"
)

var errOutboxProtection = errors.New("notification outbox protection is unavailable or invalid")

type OutboxProtector struct {
	active string
	keys   map[string]cipher.AEAD
}

func NewOutboxProtector(active string, keys map[string][]byte) (*OutboxProtector, error) {
	if active == "" || len(keys) == 0 {
		return nil, errOutboxProtection
	}
	p := &OutboxProtector{active: active, keys: make(map[string]cipher.AEAD, len(keys))}
	for version, key := range keys {
		if version == "" || len(version) > 64 || strings.ContainsAny(version, "| \r\n\t") || len(key) != 32 {
			return nil, errOutboxProtection
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, errOutboxProtection
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, errOutboxProtection
		}
		p.keys[version] = aead
	}
	if p.keys[active] == nil {
		return nil, errOutboxProtection
	}
	return p, nil
}
func LoadOutboxProtector(path string) (*OutboxProtector, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errOutboxProtection
	}
	var input struct {
		Active string            `json:"active"`
		Keys   map[string]string `json:"keys"`
	}
	if json.Unmarshal(raw, &input) != nil {
		return nil, errOutboxProtection
	}
	keys := map[string][]byte{}
	for version, text := range input.Keys {
		key, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			return nil, errOutboxProtection
		}
		keys[version] = key
	}
	return NewOutboxProtector(input.Active, keys)
}
func outboxAAD(version string, id, operation, principal uuid.UUID) []byte {
	return []byte("ani-iam:password-notification:v1|" + version + "|" + id.String() + "|" + operation.String() + "|" + principal.String())
}
func (p *OutboxProtector) seal(id, operation, principal uuid.UUID, email string) (string, []byte, error) {
	if p == nil || p.keys[p.active] == nil || id == uuid.Nil || operation == uuid.Nil || principal == uuid.Nil || email == "" {
		return "", nil, errOutboxProtection
	}
	aead := p.keys[p.active]
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", nil, errOutboxProtection
	}
	return p.active, aead.Seal(nonce, nonce, []byte(email), outboxAAD(p.active, id, operation, principal)), nil
}
func (p *OutboxProtector) open(version string, payload []byte, id, operation, principal uuid.UUID) (string, error) {
	if p == nil {
		return "", errOutboxProtection
	}
	aead := p.keys[version]
	if aead == nil || len(payload) < aead.NonceSize()+aead.Overhead() {
		return "", errOutboxProtection
	}
	plain, err := aead.Open(nil, payload[:aead.NonceSize()], payload[aead.NonceSize():], outboxAAD(version, id, operation, principal))
	if err != nil {
		return "", errOutboxProtection
	}
	return string(plain), nil
}
