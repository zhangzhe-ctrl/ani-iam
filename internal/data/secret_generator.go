package data

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

const opaqueSecretBytes = 32

type secretGenerator struct{}

func NewSecretGenerator() biz.SecretGenerator {
	return secretGenerator{}
}

func (secretGenerator) NewSecret() (string, error) {
	secret := make([]byte, opaqueSecretBytes)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("generate opaque secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(secret), nil
}

var _ biz.SecretGenerator = secretGenerator{}
