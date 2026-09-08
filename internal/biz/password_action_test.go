package biz

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRequestPasswordActionIsUniformAndPersistsOnlyKnownTargetIntent(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	principalID := uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17")
	tests := []struct {
		name        string
		target      PasswordActionTarget
		found       bool
		wantPurpose PasswordActionPurpose
	}{
		{
			name:        "known account with credential requests reset",
			target:      PasswordActionTarget{PrincipalID: principalID, HasPassword: true, VerifiedEmail: "user@example.com"},
			found:       true,
			wantPurpose: PasswordActionPurposeReset,
		},
		{name: "unknown account remains non-enumerating"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operationID := uuid.MustParse("0198f062-b76d-7001-9000-000000000061")
			outboxID := uuid.MustParse("0198f062-b76d-7001-9000-000000000062")
			auditID := uuid.MustParse("0198f062-b76d-7001-9000-000000000063")
			reader := &passwordActionTestReader{target: test.target, found: test.found}
			uow := &passwordActionTestUnitOfWork{}
			usecase := NewAuthenticationUsecase(
				reader,
				&passwordActionTestPassword{},
				allowingLoginThrottle{},
				uow,
				&passwordActionTestTokenCodec{},
				staticSecretGenerator{},
				&fixedIDs{values: []uuid.UUID{operationID, outboxID, auditID}},
				fixedAuthClock{now: now}, allowingAPIKeyUsageObserver{})

			result, err := usecase.RequestPasswordAction(context.Background(), RequestPasswordActionCommand{
				Account:        " User@Example.COM ",
				Audience:       AudienceConsole,
				IdempotencyKey: "password-action-request-1",
			})
			if err != nil {
				t.Fatalf("RequestPasswordAction() error = %v", err)
			}
			if result.OperationID != operationID || !result.ExpiresAt.Equal(now.Add(30*time.Minute)) {
				t.Fatalf("uniform result = %#v", result)
			}
			if reader.account != "user@example.com" || reader.audience != AudienceConsole {
				t.Fatalf("target lookup = account:%q audience:%q", reader.account, reader.audience)
			}
			if uow.requested == nil {
				t.Fatal("password action request was not persisted")
			}
			wantDigest := sha256.Sum256([]byte("user@example.com"))
			if uow.requested.AccountDigest != wantDigest {
				t.Fatalf("persisted account digest = %x, want %x", uow.requested.AccountDigest, wantDigest)
			}
			if test.found {
				if uow.requested.Target == nil || uow.requested.Target.PrincipalID != principalID || uow.requested.Purpose != test.wantPurpose {
					t.Fatalf("known target mutation = %#v", uow.requested)
				}
				if uow.requested.Notification == nil || uow.requested.Notification.ID != outboxID ||
					uow.requested.Notification.DestinationEmail != "user@example.com" ||
					uow.requested.Audit == nil || uow.requested.Audit.ID != auditID {
					t.Fatalf("known target side effects = notification:%#v audit:%#v", uow.requested.Notification, uow.requested.Audit)
				}
			} else {
				if uow.requested.Target != nil || uow.requested.Notification != nil || uow.requested.Purpose != "" {
					t.Fatalf("unknown target leaked a durable action intent: %#v", uow.requested)
				}
				if uow.requested.Audit == nil || uow.requested.Audit.ID != auditID ||
					uow.requested.Audit.ActorID != uuid.Nil ||
					uow.requested.Audit.TargetID != operationID ||
					uow.requested.Audit.Action != AuditActionPasswordActionRequested {
					t.Fatalf("unknown target redacted Audit = %#v", uow.requested.Audit)
				}
			}
		})
	}
}

func TestRequestPasswordActionPreservesIdempotencyConflict(t *testing.T) {
	uow := &passwordActionTestUnitOfWork{requestErr: ErrIdempotencyConflict}
	usecase := NewAuthenticationUsecase(
		&passwordActionTestReader{},
		&passwordActionTestPassword{},
		allowingLoginThrottle{},
		uow,
		&passwordActionTestTokenCodec{},
		staticSecretGenerator{},
		&fixedIDs{values: []uuid.UUID{
			uuid.MustParse("0198f062-b76d-7001-9000-000000000071"),
			uuid.MustParse("0198f062-b76d-7001-9000-000000000072"),
			uuid.MustParse("0198f062-b76d-7001-9000-000000000073"),
		}},
		fixedAuthClock{now: time.Date(2026, 9, 6, 13, 0, 0, 0, time.UTC)}, allowingAPIKeyUsageObserver{})

	_, err := usecase.RequestPasswordAction(context.Background(), RequestPasswordActionCommand{
		Account:        "unknown@example.com",
		Audience:       AudienceConsole,
		IdempotencyKey: "conflicting-key",
	})
	if !errors.Is(err, ErrIdempotencyConflict) || errors.Is(err, ErrAuthenticationDependency) {
		t.Fatalf("RequestPasswordAction() error = %v, want only %v", err, ErrIdempotencyConflict)
	}
}

func TestCompletePasswordActionHashesBeforeAtomicCompletion(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 10, 0, 0, time.UTC)
	principalID := uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17")
	operationID := uuid.MustParse("0198f062-b76d-7001-9000-000000000061")
	claims := PasswordActionTokenClaims{
		Issuer:      "ani-iam",
		PrincipalID: principalID,
		OperationID: operationID,
		Purpose:     PasswordActionPurposeReset,
		IssuedAt:    now.Add(-time.Minute),
		ExpiresAt:   now.Add(29 * time.Minute),
	}
	password := &passwordActionTestPassword{hash: "$argon2id$v=19$m=65536,t=3,p=4$fixture$fixture"}
	uow := &passwordActionTestUnitOfWork{completeResult: CompletePasswordActionResult{
		PrincipalID:       principalID,
		CredentialVersion: 2,
	}}
	codec := &passwordActionTestTokenCodec{claims: claims}
	identityID := uuid.MustParse("0198f062-b76d-7001-9000-000000000064")
	auditID := uuid.MustParse("0198f062-b76d-7001-9000-000000000065")
	usecase := NewAuthenticationUsecase(
		&passwordActionTestReader{},
		password,
		allowingLoginThrottle{},
		uow,
		codec,
		staticSecretGenerator{},
		&fixedIDs{values: []uuid.UUID{identityID, auditID}},
		fixedAuthClock{now: now}, allowingAPIKeyUsageObserver{})

	result, err := usecase.CompletePasswordAction(context.Background(), CompletePasswordActionCommand{
		ActionToken:    "opaque-signed-action-token",
		NewPassword:    "new test password",
		IdempotencyKey: "password-action-complete-1",
	})
	if err != nil {
		t.Fatalf("CompletePasswordAction() error = %v", err)
	}
	if result != uow.completeResult {
		t.Fatalf("CompletePasswordAction() result = %#v, want %#v", result, uow.completeResult)
	}
	if codec.verified != "opaque-signed-action-token" || password.hashed != "new test password" {
		t.Fatalf("verification/hash inputs = token:%q password:%q", codec.verified, password.hashed)
	}
	if uow.completed == nil || uow.completed.Claims != claims || uow.completed.IdentityID != identityID || uow.completed.PasswordHash != password.hash ||
		uow.completed.IdempotencyKey != "password-action-complete-1" || !uow.completed.CompletedAt.Equal(now) {
		t.Fatalf("completion mutation = %#v", uow.completed)
	}
	if uow.completed.Audit.Action != AuditActionPasswordActionCompleted ||
		uow.completed.Audit.ID != auditID ||
		uow.completed.Audit.Reason != AuditReasonPasswordReset ||
		uow.completed.Audit.ActorID != principalID ||
		uow.completed.Audit.TargetID != operationID {
		t.Fatalf("completion audit = %#v", uow.completed.Audit)
	}
}

func TestCompletePasswordActionRejectsInvalidTokenBeforeHashing(t *testing.T) {
	password := &passwordActionTestPassword{}
	uow := &passwordActionTestUnitOfWork{}
	codec := &passwordActionTestTokenCodec{verifyErr: errors.New("signature rejected")}
	usecase := NewAuthenticationUsecase(
		&passwordActionTestReader{},
		password,
		allowingLoginThrottle{},
		uow,
		codec,
		staticSecretGenerator{},
		&fixedIDs{},
		fixedAuthClock{}, allowingAPIKeyUsageObserver{})

	_, err := usecase.CompletePasswordAction(context.Background(), CompletePasswordActionCommand{
		ActionToken:    "invalid-action-token",
		NewPassword:    "new test password",
		IdempotencyKey: "password-action-complete-invalid",
	})
	if !errors.Is(err, ErrPasswordActionInvalid) {
		t.Fatalf("CompletePasswordAction() error = %v, want %v", err, ErrPasswordActionInvalid)
	}
	if password.hashed != "" || uow.completed != nil {
		t.Fatalf("invalid token reached hash/UOW: hashed=%q completion=%#v", password.hashed, uow.completed)
	}
}

func TestCompletePasswordActionPreservesIdempotencyConflict(t *testing.T) {
	now := time.Date(2026, 9, 6, 13, 10, 0, 0, time.UTC)
	principalID := uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17")
	operationID := uuid.MustParse("0198f062-b76d-7001-9000-000000000081")
	uow := &passwordActionTestUnitOfWork{completeErr: ErrIdempotencyConflict}
	usecase := NewAuthenticationUsecase(
		&passwordActionTestReader{},
		&passwordActionTestPassword{hash: "$argon2id$v=19$m=65536,t=3,p=4$fixture$fixture"},
		allowingLoginThrottle{},
		uow,
		&passwordActionTestTokenCodec{claims: PasswordActionTokenClaims{
			Issuer:      "ani-iam",
			PrincipalID: principalID,
			OperationID: operationID,
			Purpose:     PasswordActionPurposeReset,
			IssuedAt:    now.Add(-time.Minute),
			ExpiresAt:   now.Add(29 * time.Minute),
		}},
		staticSecretGenerator{},
		&fixedIDs{values: []uuid.UUID{
			uuid.MustParse("0198f062-b76d-7001-9000-000000000082"),
			uuid.MustParse("0198f062-b76d-7001-9000-000000000083"),
		}},
		fixedAuthClock{now: now}, allowingAPIKeyUsageObserver{})

	_, err := usecase.CompletePasswordAction(context.Background(), CompletePasswordActionCommand{
		ActionToken:    "opaque-action-token",
		NewPassword:    "new test password",
		IdempotencyKey: "conflicting-completion-key",
	})
	if !errors.Is(err, ErrIdempotencyConflict) || errors.Is(err, ErrAuthenticationDependency) {
		t.Fatalf("CompletePasswordAction() error = %v, want only %v", err, ErrIdempotencyConflict)
	}
}

type passwordActionTestReader struct {
	target   PasswordActionTarget
	found    bool
	account  string
	audience Audience
}

func (*passwordActionTestReader) LookupPasswordLogin(context.Context, TenantScope, string) (PasswordLoginState, error) {
	return PasswordLoginState{}, ErrInvalidCredential
}

func (r *passwordActionTestReader) LookupPasswordActionTarget(_ context.Context, account string, audience Audience) (PasswordActionTarget, bool, error) {
	r.account = account
	r.audience = audience
	return r.target, r.found, nil
}

type passwordActionTestPassword struct {
	hash   string
	hashed string
}

func (*passwordActionTestPassword) Verify(string, string) (bool, error) { return false, nil }
func (*passwordActionTestPassword) VerifyUnknown(string) error          { return nil }
func (p *passwordActionTestPassword) Hash(password string) (string, error) {
	p.hashed = password
	return p.hash, nil
}

type passwordActionTestUnitOfWork struct {
	requested      *PasswordActionRequestMutation
	requestErr     error
	completed      *PasswordActionCompletion
	completeResult CompletePasswordActionResult
	completeErr    error
}

func (*passwordActionTestUnitOfWork) CommitLogin(context.Context, TenantScope, LoginMutation) error {
	return nil
}
func (*passwordActionTestUnitOfWork) RecordLoginFailure(context.Context, TenantScope, LoginFailureMutation) error {
	return nil
}
func (*passwordActionTestUnitOfWork) RecordTenantPrincipalValidation(context.Context, TenantScope, SecurityAuditEvent) error {
	return nil
}
func (*passwordActionTestUnitOfWork) RecordUnboundPrincipalValidation(context.Context, SecurityAuditEvent) error {
	return nil
}

func (u *passwordActionTestUnitOfWork) RequestPasswordAction(_ context.Context, mutation PasswordActionRequestMutation) (RequestPasswordActionResult, error) {
	u.requested = &mutation
	if u.requestErr != nil {
		return RequestPasswordActionResult{}, u.requestErr
	}
	return RequestPasswordActionResult{OperationID: mutation.OperationID, ExpiresAt: mutation.ExpiresAt}, nil
}

func (u *passwordActionTestUnitOfWork) CompletePasswordAction(_ context.Context, completion PasswordActionCompletion) (CompletePasswordActionResult, error) {
	u.completed = &completion
	if u.completeErr != nil {
		return CompletePasswordActionResult{}, u.completeErr
	}
	return u.completeResult, nil
}

type passwordActionTestTokenCodec struct {
	claims    PasswordActionTokenClaims
	verifyErr error
	verified  string
}

func (*passwordActionTestTokenCodec) Issue(context.Context, AccessTokenClaims) (string, error) {
	return "test-access-token", nil
}

func (*passwordActionTestTokenCodec) IssuePasswordAction(context.Context, PasswordActionTokenClaims) (string, error) {
	return "test-action-token", nil
}

func (c *passwordActionTestTokenCodec) VerifyPasswordAction(_ context.Context, raw string) (PasswordActionTokenClaims, error) {
	c.verified = raw
	return c.claims, c.verifyErr
}
