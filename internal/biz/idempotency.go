package biz

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrIdempotencyExpired = errors.New("idempotency replay window expired")
)

type MutationIdentity struct {
	ActorID   uuid.UUID
	CallerID  uuid.UUID
	Operation string
	Key       string
	Intent    [sha256.Size]byte
}

// StoredMutation contains the non-secret outcome of one committed operation.
// Repositories never commit a placeholder independently of the business UoW.
type StoredMutation struct {
	Identity  MutationIdentity
	Result    []byte
	CreatedAt time.Time
	ExpiresAt time.Time
}

type MutationResultTransaction interface {
	LockMutation(context.Context, TenantScope, MutationIdentity) error
	FindMutation(context.Context, TenantScope, MutationIdentity) (StoredMutation, bool, error)
	SaveMutation(context.Context, TenantScope, StoredMutation) error
}

func mutationIdentity(actor TenantAuthorizationActor, operation, key string, fields any) (MutationIdentity, error) {
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 256 {
		return MutationIdentity{}, ErrIdempotencyKeyRequired
	}
	if err := validateTenantActor(actor); err != nil {
		return MutationIdentity{}, err
	}
	encoded, err := json.Marshal(struct {
		Caller uuid.UUID
		Fields any
	}{actor.DirectCaller.Identity.PrincipalID, fields})
	if err != nil {
		return MutationIdentity{}, ErrInvalidPersistenceState
	}
	return MutationIdentity{ActorID: actor.PrincipalID, CallerID: actor.DirectCaller.Identity.PrincipalID,
		Operation: operation, Key: key, Intent: sha256.Sum256(encoded)}, nil
}

func executeMutation[T any](ctx context.Context, tx MutationResultTransaction, scope TenantScope, identity MutationIdentity, now time.Time, result *T, mutation func() error) error {
	if err := tx.LockMutation(ctx, scope, identity); err != nil {
		return err
	}
	previous, found, err := tx.FindMutation(ctx, scope, identity)
	if err != nil {
		return err
	}
	if found {
		if previous.Identity.Intent != identity.Intent || previous.Identity.CallerID != identity.CallerID {
			return ErrIdempotencyConflict
		}
		if !now.Before(previous.ExpiresAt) {
			return ErrIdempotencyExpired
		}
		decoder := json.NewDecoder(bytes.NewReader(previous.Result))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(result); err != nil {
			return ErrInvalidPersistenceState
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return ErrInvalidPersistenceState
		}
		if key, ok := any(result).(*CreateAPIKeyResult); ok {
			key.Replayed = true
			key.Secret = ""
		}
		return nil
	}
	if err := mutation(); err != nil {
		return err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return ErrInvalidPersistenceState
	}
	return tx.SaveMutation(ctx, scope, StoredMutation{Identity: identity, Result: encoded, CreatedAt: now.UTC(), ExpiresAt: now.UTC().Add(24 * time.Hour)})
}
