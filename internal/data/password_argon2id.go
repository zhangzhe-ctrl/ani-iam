package data

import (
	"errors"
	"fmt"

	"github.com/alexedwards/argon2id"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

var ErrArgon2idParametersMismatch = errors.New("argon2id parameters do not match the target profile")

var targetArgon2idParams = &argon2id.Params{
	Memory:      64 * 1024,
	Iterations:  3,
	Parallelism: 4,
	SaltLength:  16,
	KeyLength:   32,
}

const unknownAccountPasswordHash = "$argon2id$v=19$m=65536,t=3,p=4$wmZ4DebQvV94M2F2i/zEbA$n6n75AwgNsfve9aRCzF3G/wMV2yD7QQsA49VHlIfeNw"

type Argon2idPasswordHasher struct{}

func NewArgon2idPasswordHasher() *Argon2idPasswordHasher {
	return &Argon2idPasswordHasher{}
}

func (*Argon2idPasswordHasher) Hash(password string) (string, error) {
	return argon2id.CreateHash(password, targetArgon2idParams)
}

func (*Argon2idPasswordHasher) Verify(encodedHash string, password string) (bool, error) {
	params, _, _, err := argon2id.DecodeHash(encodedHash)
	if err != nil {
		return false, err
	}
	if params.Memory != targetArgon2idParams.Memory ||
		params.Iterations != targetArgon2idParams.Iterations ||
		params.Parallelism != targetArgon2idParams.Parallelism ||
		params.SaltLength != targetArgon2idParams.SaltLength ||
		params.KeyLength != targetArgon2idParams.KeyLength {
		return false, fmt.Errorf(
			"%w: m=%d,t=%d,p=%d,salt=%d,key=%d",
			ErrArgon2idParametersMismatch,
			params.Memory,
			params.Iterations,
			params.Parallelism,
			params.SaltLength,
			params.KeyLength,
		)
	}
	return argon2id.ComparePasswordAndHash(password, encodedHash)
}

func (hasher *Argon2idPasswordHasher) VerifyUnknown(password string) error {
	_, err := hasher.Verify(unknownAccountPasswordHash, password)
	return err
}

var _ biz.AuthenticationPassword = (*Argon2idPasswordHasher)(nil)
