package data

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

var ErrInvalidRedisOIDCOperationStoreConfiguration = errors.New("invalid Redis OIDC operation-store configuration")

type redisOIDCOperationStore struct {
	client    redis.UniversalClient
	namespace string
}

var createOrGetOIDCOperation = redis.NewScript(`
local existing_operation_key = redis.call("GET", KEYS[2])
if existing_operation_key then
  local existing_data = redis.call("HGET", existing_operation_key, "data")
  if existing_data then
    return {1, existing_data}
  end
  redis.call("DEL", KEYS[2])
end
if redis.call("EXISTS", KEYS[1]) ~= 0 then
  return redis.error_reply("OIDC operation state collision")
end
redis.call("HSET", KEYS[1], "data", ARGV[1], "idempotency_key", KEYS[2])
redis.call("PEXPIRE", KEYS[1], ARGV[2])
redis.call("SET", KEYS[2], KEYS[1], "PX", ARGV[2])
return {0, ARGV[1]}
`)

var consumeOIDCOperation = redis.NewScript(`
local data = redis.call("HGET", KEYS[1], "data")
if not data then
  return nil
end
local idempotency_key = redis.call("HGET", KEYS[1], "idempotency_key")
redis.call("DEL", KEYS[1])
if idempotency_key then
  redis.call("DEL", idempotency_key)
end
return data
`)

func NewRedisOIDCOperationStore(client redis.UniversalClient, namespace string) (biz.OIDCOperationStore, error) {
	namespace = strings.Trim(strings.TrimSpace(namespace), ":")
	if client == nil || namespace == "" {
		return nil, ErrInvalidRedisOIDCOperationStoreConfiguration
	}
	return &redisOIDCOperationStore{client: client, namespace: namespace}, nil
}

func (s *redisOIDCOperationStore) CreateOrGet(ctx context.Context, operation biz.OIDCOperation) (biz.OIDCOperation, error) {
	if err := validateOIDCOperation(operation); err != nil {
		return biz.OIDCOperation{}, err
	}
	lifetime := operation.ExpiresAt.UTC().Sub(operation.CreatedAt.UTC())
	if lifetime <= 0 || lifetime > 10*time.Minute || lifetime.Milliseconds() < 1 {
		return biz.OIDCOperation{}, errors.New("OIDC operation lifetime is invalid")
	}
	encoded, err := json.Marshal(operation)
	if err != nil {
		return biz.OIDCOperation{}, fmt.Errorf("encode OIDC operation: %w", err)
	}
	result, err := createOrGetOIDCOperation.Run(
		ctx,
		s.client,
		[]string{s.operationKey(operation.State), s.idempotencyKey(operation.IdempotencyKey)},
		encoded,
		lifetime.Milliseconds(),
	).Slice()
	if err != nil {
		return biz.OIDCOperation{}, fmt.Errorf("create Redis OIDC operation: %w", err)
	}
	if len(result) != 2 {
		return biz.OIDCOperation{}, errors.New("create Redis OIDC operation returned invalid state")
	}
	status, ok := result[0].(int64)
	if !ok || (status != 0 && status != 1) {
		return biz.OIDCOperation{}, errors.New("create Redis OIDC operation returned invalid status")
	}
	storedBytes, ok := result[1].(string)
	if !ok {
		if bytes, bytesOK := result[1].([]byte); bytesOK {
			storedBytes = string(bytes)
		} else {
			return biz.OIDCOperation{}, errors.New("create Redis OIDC operation returned invalid payload")
		}
	}
	stored, err := decodeOIDCOperation([]byte(storedBytes))
	if err != nil {
		return biz.OIDCOperation{}, err
	}
	if status == 1 && stored.RequestFingerprint != operation.RequestFingerprint {
		return biz.OIDCOperation{}, biz.ErrIdempotencyConflict
	}
	return stored, nil
}

func (s *redisOIDCOperationStore) Consume(ctx context.Context, state string) (biz.OIDCOperation, error) {
	if strings.TrimSpace(state) == "" {
		return biz.OIDCOperation{}, biz.ErrOIDCStateInvalid
	}
	result, err := consumeOIDCOperation.Run(ctx, s.client, []string{s.operationKey(state)}).Result()
	if errors.Is(err, redis.Nil) {
		return biz.OIDCOperation{}, biz.ErrOIDCStateInvalid
	}
	if err != nil {
		return biz.OIDCOperation{}, fmt.Errorf("consume Redis OIDC operation: %w", err)
	}
	var encoded []byte
	switch value := result.(type) {
	case string:
		encoded = []byte(value)
	case []byte:
		encoded = value
	default:
		return biz.OIDCOperation{}, biz.ErrOIDCStateInvalid
	}
	operation, err := decodeOIDCOperation(encoded)
	if err != nil || operation.State != state {
		return biz.OIDCOperation{}, biz.ErrOIDCStateInvalid
	}
	return operation, nil
}

func (s *redisOIDCOperationStore) operationKey(state string) string {
	digest := sha256.Sum256([]byte(state))
	return s.namespace + ":oidc:operation:" + hex.EncodeToString(digest[:])
}

func (s *redisOIDCOperationStore) idempotencyKey(idempotencyKey string) string {
	digest := sha256.Sum256([]byte(idempotencyKey))
	return s.namespace + ":oidc:idempotency:" + hex.EncodeToString(digest[:])
}

func validateOIDCOperation(operation biz.OIDCOperation) error {
	if operation.Kind != biz.OIDCFlowLogin && operation.Kind != biz.OIDCFlowIdentityLink {
		return errors.New("OIDC operation kind is invalid")
	}
	if strings.TrimSpace(operation.Provider) == "" || operation.Audience == "" || operation.TenantID == [16]byte{} ||
		strings.TrimSpace(operation.State) == "" || strings.TrimSpace(operation.Nonce) == "" || strings.TrimSpace(operation.CodeVerifier) == "" ||
		strings.TrimSpace(operation.RedirectURI) == "" || strings.TrimSpace(operation.IdempotencyKey) == "" ||
		len(operation.RequestFingerprint) != sha256.Size*2 || operation.CreatedAt.IsZero() || operation.ExpiresAt.IsZero() {
		return errors.New("OIDC operation is invalid")
	}
	if operation.Kind == biz.OIDCFlowIdentityLink && (operation.PrincipalID == [16]byte{} || operation.SessionID == [16]byte{}) {
		return errors.New("OIDC identity-link operation binding is invalid")
	}
	if operation.BrowserProofDigest != "" && (operation.Kind != biz.OIDCFlowIdentityLink ||
		len(operation.BrowserProofDigest) != sha256.Size*2 || operation.LinkGrantID == [16]byte{} ||
		operation.LinkGrantVersion <= 0 || operation.LinkCredentialExpiresAt.IsZero() || len(operation.LinkAuthnMethods) == 0) {
		return errors.New("OIDC browser-proof binding is invalid")
	}
	return nil
}

func decodeOIDCOperation(encoded []byte) (biz.OIDCOperation, error) {
	var operation biz.OIDCOperation
	if err := json.Unmarshal(encoded, &operation); err != nil {
		return biz.OIDCOperation{}, errors.New("decode Redis OIDC operation")
	}
	if err := validateOIDCOperation(operation); err != nil {
		return biz.OIDCOperation{}, errors.New("decode Redis OIDC operation")
	}
	return operation, nil
}

var _ biz.OIDCOperationStore = (*redisOIDCOperationStore)(nil)
