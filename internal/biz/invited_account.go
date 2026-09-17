package biz

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"github.com/google/uuid"
	"net/netip"
	"strings"
	"time"
	"unicode/utf8"
)

const InvitedAccountVerificationLifetime = 10 * time.Minute
const InvitedAccountVerificationAttempts = 5

var ErrInvitedAccountInvalid = errors.New("invited account request is invalid")

type InvitedAccountVerificationRequest struct {
	Account, IdempotencyKey string
	SourceIP                netip.Addr
}
type InvitedAccountCompletionRequest struct {
	ChallengeID                                            uuid.UUID
	Account, VerificationCode, NewPassword, IdempotencyKey string
	SourceIP                                               netip.Addr
}

// This deliberately reveals neither eligibility nor any authentication result.
type InvitedAccountVerificationResult struct {
	ChallengeID uuid.UUID
	ExpiresAt   time.Time
}
type InvitedAccountVerificationMutation struct {
	ChallengeID, DeliveryID               uuid.UUID
	NormalizedEmail, Code, IdempotencyKey string
	AccountDigest                         [32]byte
	Caller                                DirectCaller
	CreatedAt, ExpiresAt                  time.Time
	Audit                                 SecurityAuditEvent
}
type InvitedAccountCompletionMutation struct {
	ChallengeID, PrincipalID, IdentityID                uuid.UUID
	NormalizedEmail, Code, PasswordHash, IdempotencyKey string
	// The persistence protector applies its private purpose key before storage.
	// This intermediate value is never an independently stored password verifier.
	Intent             [32]byte
	Caller             DirectCaller
	CompletedAt        time.Time
	Audit, DenialAudit SecurityAuditEvent
}
type InvitedAccountUnitOfWork interface {
	RequestInvitedAccountVerification(context.Context, InvitedAccountVerificationMutation) (InvitedAccountVerificationResult, error)
	CompleteInvitedAccount(context.Context, InvitedAccountCompletionMutation) error
}
type InvitedAccountCodeGenerator interface{ GenerateEmailVerificationCode() (string, error) }
type InvitedAccountLimiter interface {
	AcquireVerificationRequest(context.Context, string, netip.Addr) error
	AcquireAccountCompletion(context.Context, netip.Addr) error
}
type InvitedAccountUsecase struct {
	uow      InvitedAccountUnitOfWork
	codes    InvitedAccountCodeGenerator
	password PasswordHasher
	limiter  InvitedAccountLimiter
	ids      IDGenerator
	clock    Clock
}

func NewInvitedAccountUsecase(uow InvitedAccountUnitOfWork, codes InvitedAccountCodeGenerator, password PasswordHasher, limiter InvitedAccountLimiter, ids IDGenerator, clock Clock) *InvitedAccountUsecase {
	return &InvitedAccountUsecase{uow: uow, codes: codes, password: password, limiter: limiter, ids: ids, clock: clock}
}
func (u *InvitedAccountUsecase) caller(ctx context.Context, rpc string) (DirectCaller, error) {
	if u == nil || u.uow == nil || u.codes == nil || u.password == nil || u.limiter == nil || u.ids == nil || u.clock == nil {
		return DirectCaller{}, ErrAuthenticationDependency
	}
	c, ok := DirectCallerFromContext(ctx)
	if !ok || c.Target != (WorkloadTarget{Audience: "ani-iam", Operation: "/iam.v1.AuthenticationService/" + rpc}) {
		return DirectCaller{}, ErrInvalidCredential
	}
	return c, nil
}
func invitedAccountInput(account, key string, ip netip.Addr) (string, error) {
	email, err := normalizeInvitationEmail(account)
	if err != nil || len(key) < 1 || len(key) > 128 || strings.TrimSpace(key) != key || !ip.IsValid() {
		return "", ErrInvitedAccountInvalid
	}
	return email, nil
}
func validEmailVerificationCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func (u *InvitedAccountUsecase) newID() (uuid.UUID, error) {
	id, err := u.ids.NewID()
	if err != nil || id.Version() != 7 {
		return uuid.Nil, ErrInvalidGeneratedID
	}
	return id, nil
}
func (u *InvitedAccountUsecase) audit(id, target uuid.UUID, caller DirectCaller, key string, action AuditAction, reason AuditReason, now time.Time) SecurityAuditEvent {
	return SecurityAuditEvent{ID: id, DirectCaller: caller, AuthenticationMethod: AuditAuthenticationMethodAnonymous, Boundary: AuditBoundaryPrincipal, Action: action, TargetType: "invited_account_verification", TargetID: target, TargetVersion: 1, Result: AuditResultSucceeded, Reason: reason, RequestID: key, CorrelationID: key, DecisionID: id.String(), SourceService: AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now}
}
func (u *InvitedAccountUsecase) RequestVerification(ctx context.Context, r InvitedAccountVerificationRequest) (InvitedAccountVerificationResult, error) {
	caller, err := u.caller(ctx, "RequestInvitedAccountVerification")
	if err != nil {
		return InvitedAccountVerificationResult{}, err
	}
	email, err := invitedAccountInput(r.Account, r.IdempotencyKey, r.SourceIP)
	if err != nil {
		return InvitedAccountVerificationResult{}, err
	}
	if err = u.limiter.AcquireVerificationRequest(ctx, email, r.SourceIP); err != nil {
		return InvitedAccountVerificationResult{}, err
	}
	challenge, err := u.newID()
	if err != nil {
		return InvitedAccountVerificationResult{}, err
	}
	delivery, err := u.newID()
	if err != nil {
		return InvitedAccountVerificationResult{}, err
	}
	auditID, err := u.newID()
	if err != nil {
		return InvitedAccountVerificationResult{}, err
	}
	code, err := u.codes.GenerateEmailVerificationCode()
	if err != nil || !validEmailVerificationCode(code) {
		return InvitedAccountVerificationResult{}, ErrAuthenticationDependency
	}
	now := u.clock.Now().UTC().Truncate(time.Second)
	audit := u.audit(auditID, challenge, caller, r.IdempotencyKey, "iam.account.verification.requested", "EMAIL_VERIFICATION_REQUESTED", now)
	return u.uow.RequestInvitedAccountVerification(ctx, InvitedAccountVerificationMutation{ChallengeID: challenge, DeliveryID: delivery, NormalizedEmail: email, Code: code, IdempotencyKey: r.IdempotencyKey, AccountDigest: sha256.Sum256([]byte(email)), Caller: caller, CreatedAt: now, ExpiresAt: now.Add(InvitedAccountVerificationLifetime), Audit: audit})
}
func (u *InvitedAccountUsecase) Complete(ctx context.Context, r InvitedAccountCompletionRequest) error {
	caller, err := u.caller(ctx, "CompleteInvitedAccount")
	if err != nil {
		return err
	}
	email, err := invitedAccountInput(r.Account, r.IdempotencyKey, r.SourceIP)
	if err != nil {
		return err
	}
	if r.ChallengeID.Version() != 7 || !validEmailVerificationCode(r.VerificationCode) || !utf8.ValidString(r.NewPassword) || utf8.RuneCountInString(r.NewPassword) < 12 || len(r.NewPassword) > 1024 {
		return ErrInvitedAccountInvalid
	}
	if err = u.limiter.AcquireAccountCompletion(ctx, r.SourceIP); err != nil {
		return err
	}
	passwordHash, err := u.password.Hash(r.NewPassword)
	if err != nil || passwordHash == "" {
		return ErrAuthenticationDependency
	}
	principal, err := u.newID()
	if err != nil {
		return err
	}
	identity, err := u.newID()
	if err != nil {
		return err
	}
	auditID, err := u.newID()
	if err != nil {
		return err
	}
	denialID, err := u.newID()
	if err != nil {
		return err
	}
	now := u.clock.Now().UTC()
	audit := u.audit(auditID, r.ChallengeID, caller, r.IdempotencyKey, "iam.account.created", "INDEPENDENT_EMAIL_VERIFIED", now)
	audit.ActorID = principal
	audit.AuthenticationMethod = "email_verification"
	denied := u.audit(denialID, r.ChallengeID, caller, r.IdempotencyKey, "iam.account.verification.denied", "CREDENTIAL_INVALID", now)
	denied.Result = AuditResultDenied
	fingerprint := hmac.New(sha256.New, []byte(r.VerificationCode))
	_, _ = fingerprint.Write([]byte("ani-iam:invited-account-completion:v1\x00" + r.NewPassword))
	var intent [32]byte
	copy(intent[:], fingerprint.Sum(nil))
	return u.uow.CompleteInvitedAccount(ctx, InvitedAccountCompletionMutation{ChallengeID: r.ChallengeID, PrincipalID: principal, IdentityID: identity, NormalizedEmail: email, Code: r.VerificationCode, PasswordHash: passwordHash, IdempotencyKey: r.IdempotencyKey, Intent: intent, Caller: caller, CompletedAt: now, Audit: audit, DenialAudit: denied})
}
