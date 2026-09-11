package biz

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const passwordActionLifetime = 30 * time.Minute

const (
	AuditAuthenticationMethodAnonymous AuditAuthenticationMethod = "anonymous"
	AuditAuthenticationMethodAction    AuditAuthenticationMethod = "password_action"
	AuditBoundaryPrincipal             AuditBoundary             = "principal"
	AuditActionPasswordActionRequested AuditAction               = "iam.password.action.requested"
	AuditActionPasswordActionCompleted AuditAction               = "iam.password.action.completed"
	AuditTargetTypePasswordAction      AuditTargetType           = "password_action"
	AuditReasonPasswordActionRequested AuditReason               = "PASSWORD_ACTION_REQUESTED"
	AuditReasonPasswordSetup           AuditReason               = "PASSWORD_SETUP"
	AuditReasonPasswordReset           AuditReason               = "PASSWORD_RESET"
)

var (
	ErrIdempotencyConflict         = errors.New("idempotency key conflicts with another request")
	ErrNewPasswordRequired         = errors.New("new password is required")
	ErrPasswordActionTokenRequired = errors.New("password action token is required")
	ErrPasswordActionInvalid       = errors.New("password action is invalid")
)

type RequestPasswordActionCommand struct {
	Account        string
	Audience       Audience
	IdempotencyKey string
}

type RequestPasswordActionResult struct {
	OperationID uuid.UUID
	ExpiresAt   time.Time
}

type CompletePasswordActionCommand struct {
	ActionToken    string
	NewPassword    string
	IdempotencyKey string
}

type PasswordActionTarget struct {
	PrincipalID   uuid.UUID
	HasPassword   bool
	VerifiedEmail string
}

type PasswordActionNotification struct {
	ID               uuid.UUID
	OperationID      uuid.UUID
	PrincipalID      uuid.UUID
	Purpose          PasswordActionPurpose
	DestinationEmail string
}

// PasswordActionRequestMutation persists only a digest for unknown accounts.
// Every request carries a redacted Audit; a known target additionally carries
// Target and Notification in the same transaction.
type PasswordActionRequestMutation struct {
	OperationID    uuid.UUID
	AccountDigest  [sha256.Size]byte
	Audience       Audience
	Target         *PasswordActionTarget
	Purpose        PasswordActionPurpose
	ExpiresAt      time.Time
	CreatedAt      time.Time
	IdempotencyKey string
	Notification   *PasswordActionNotification
	Audit          *SecurityAuditEvent
}

type PasswordActionCompletion struct {
	RequestFingerprint [32]byte
	Claims             PasswordActionTokenClaims
	IdentityID         uuid.UUID
	PasswordHash       string
	IdempotencyKey     string
	CompletedAt        time.Time
	Audit              SecurityAuditEvent
}

type CompletePasswordActionResult struct {
	PrincipalID       uuid.UUID
	CredentialVersion int64
}

type PasswordActionReader interface {
	LookupPasswordActionTarget(context.Context, string, Audience) (PasswordActionTarget, bool, error)
}

type PasswordHasher interface {
	Hash(string) (string, error)
}

type PasswordActionTokenCodec interface {
	IssuePasswordAction(context.Context, PasswordActionTokenClaims) (string, error)
	VerifyPasswordAction(context.Context, string) (PasswordActionTokenClaims, error)
}

type PasswordActionUnitOfWork interface {
	RequestPasswordAction(context.Context, PasswordActionRequestMutation) (RequestPasswordActionResult, error)
	CompletePasswordAction(context.Context, PasswordActionCompletion) (CompletePasswordActionResult, error)
}

type AuthenticationReader interface {
	PasswordLoginReader
	PasswordActionReader
	SessionContinuityReader
	PrincipalValidationReader
}

type AuthenticationPassword interface {
	PasswordVerifier
	PasswordHasher
}

type AuthenticationUnitOfWork interface {
	LoginUnitOfWork
	PasswordActionUnitOfWork
	SessionContinuityUnitOfWork
	PrincipalValidationAuditWriter
}

type AuthenticationTokenCodec interface {
	AccessTokenIssuer
	AccessCredentialVerifier
	PasswordActionTokenCodec
}

func (u *AuthenticationUsecase) RequestPasswordAction(ctx context.Context, command RequestPasswordActionCommand) (RequestPasswordActionResult, error) {
	normalizedAccount := strings.ToLower(strings.TrimSpace(command.Account))
	if normalizedAccount == "" {
		return RequestPasswordActionResult{}, ErrAccountRequired
	}
	if command.Audience == "" {
		return RequestPasswordActionResult{}, ErrAudienceRequired
	}
	if command.Audience != AudienceConsole {
		return RequestPasswordActionResult{}, ErrAuthenticationDependency
	}
	if strings.TrimSpace(command.IdempotencyKey) == "" {
		return RequestPasswordActionResult{}, ErrIdempotencyKeyRequired
	}
	target, found, err := u.reader.LookupPasswordActionTarget(ctx, normalizedAccount, command.Audience)
	if err != nil {
		return RequestPasswordActionResult{}, fmt.Errorf("lookup password-action target: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	ids, err := u.newIDs(3)
	if err != nil {
		return RequestPasswordActionResult{}, err
	}
	// Persist the same whole-second timestamps encoded by JWT NumericDate.
	// Completion deliberately requires exact equality with the durable expiry.
	now := u.clock.Now().UTC().Truncate(time.Second)
	mutation := PasswordActionRequestMutation{
		OperationID:    ids[0],
		AccountDigest:  sha256.Sum256([]byte(normalizedAccount)),
		Audience:       command.Audience,
		ExpiresAt:      now.Add(passwordActionLifetime),
		CreatedAt:      now,
		IdempotencyKey: strings.TrimSpace(command.IdempotencyKey),
		Audit: &SecurityAuditEvent{
			ID:                   ids[2],
			ActorID:              uuid.Nil,
			AuthenticationMethod: AuditAuthenticationMethodAnonymous,
			Boundary:             AuditBoundaryPrincipal,
			Action:               AuditActionPasswordActionRequested,
			TargetType:           AuditTargetTypePasswordAction,
			TargetID:             ids[0],
			TargetVersion:        1,
			Result:               AuditResultSucceeded,
			Reason:               AuditReasonPasswordActionRequested,
			RequestID:            strings.TrimSpace(command.IdempotencyKey),
			CorrelationID:        strings.TrimSpace(command.IdempotencyKey),
			DecisionID:           ids[2].String(),
			SourceService:        AuditSourceServiceIAM,
			OccurredAt:           now,
			RecordedAt:           now,
		},
	}
	if found {
		if target.PrincipalID == uuid.Nil || target.PrincipalID.Version() != 7 {
			return RequestPasswordActionResult{}, ErrAuthenticationDependency
		}
		destinationEmail := strings.ToLower(strings.TrimSpace(target.VerifiedEmail))
		if destinationEmail == "" || destinationEmail != target.VerifiedEmail {
			return RequestPasswordActionResult{}, ErrAuthenticationDependency
		}
		purpose := PasswordActionPurposeSetup
		if target.HasPassword {
			purpose = PasswordActionPurposeReset
		}
		mutation.Target = &target
		mutation.Purpose = purpose
		mutation.Notification = &PasswordActionNotification{
			ID:               ids[1],
			OperationID:      ids[0],
			PrincipalID:      target.PrincipalID,
			Purpose:          purpose,
			DestinationEmail: destinationEmail,
		}
	}
	result, err := u.uow.RequestPasswordAction(ctx, mutation)
	if err != nil {
		if errors.Is(err, ErrIdempotencyConflict) {
			return RequestPasswordActionResult{}, ErrIdempotencyConflict
		}
		return RequestPasswordActionResult{}, fmt.Errorf("request password action: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	return result, nil
}

func (u *AuthenticationUsecase) CompletePasswordAction(ctx context.Context, command CompletePasswordActionCommand) (CompletePasswordActionResult, error) {
	if strings.TrimSpace(command.ActionToken) == "" || strings.TrimSpace(command.ActionToken) != command.ActionToken {
		return CompletePasswordActionResult{}, ErrPasswordActionTokenRequired
	}
	if command.NewPassword == "" {
		return CompletePasswordActionResult{}, ErrNewPasswordRequired
	}
	idempotencyKey := strings.TrimSpace(command.IdempotencyKey)
	if idempotencyKey == "" {
		return CompletePasswordActionResult{}, ErrIdempotencyKeyRequired
	}
	claims, err := u.tokens.VerifyPasswordAction(ctx, command.ActionToken)
	if err != nil || claims.PrincipalID == uuid.Nil || claims.PrincipalID.Version() != 7 ||
		claims.OperationID == uuid.Nil || claims.OperationID.Version() != 7 ||
		(claims.Purpose != PasswordActionPurposeSetup && claims.Purpose != PasswordActionPurposeReset) {
		return CompletePasswordActionResult{}, ErrPasswordActionInvalid
	}
	passwordHash, err := u.password.Hash(command.NewPassword)
	if err != nil {
		return CompletePasswordActionResult{}, fmt.Errorf("hash new password: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	if strings.TrimSpace(passwordHash) == "" {
		return CompletePasswordActionResult{}, ErrAuthenticationDependency
	}
	ids, err := u.newIDs(2)
	if err != nil {
		return CompletePasswordActionResult{}, err
	}
	now := u.clock.Now().UTC()
	reason := AuditReasonPasswordSetup
	if claims.Purpose == PasswordActionPurposeReset {
		reason = AuditReasonPasswordReset
	}
	fingerprinter := hmac.New(sha256.New, []byte(command.ActionToken))
	_, _ = fingerprinter.Write([]byte("ani-iam:password-completion:v1\x00" + command.NewPassword))
	var fingerprint [32]byte
	copy(fingerprint[:], fingerprinter.Sum(nil))
	completion := PasswordActionCompletion{
		RequestFingerprint: fingerprint,
		Claims:             claims,
		IdentityID:         ids[0],
		PasswordHash:       passwordHash,
		IdempotencyKey:     idempotencyKey,
		CompletedAt:        now,
		Audit: SecurityAuditEvent{
			ID:                   ids[1],
			ActorID:              claims.PrincipalID,
			AuthenticationMethod: AuditAuthenticationMethodAction,
			Boundary:             AuditBoundaryPrincipal,
			Action:               AuditActionPasswordActionCompleted,
			TargetType:           AuditTargetTypePasswordAction,
			TargetID:             claims.OperationID,
			TargetVersion:        2,
			Result:               AuditResultSucceeded,
			Reason:               reason,
			RequestID:            idempotencyKey,
			CorrelationID:        idempotencyKey,
			DecisionID:           ids[1].String(),
			SourceService:        AuditSourceServiceIAM,
			OccurredAt:           now,
			RecordedAt:           now,
		},
	}
	result, err := u.uow.CompletePasswordAction(ctx, completion)
	if err != nil {
		if errors.Is(err, ErrPasswordActionInvalid) {
			return CompletePasswordActionResult{}, ErrPasswordActionInvalid
		}
		if errors.Is(err, ErrIdempotencyConflict) {
			return CompletePasswordActionResult{}, ErrIdempotencyConflict
		}
		return CompletePasswordActionResult{}, fmt.Errorf("complete password action: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	return result, nil
}
