package biz

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrCoreBootstrapAuthority = errors.New("Core Bootstrap execution authority unavailable or invalid")

type CoreBootstrapSource struct {
	TenantID, OperationID, EventID uuid.UUID
	Producer, Fingerprint          string
	SourceSequence                 int64
}

// This is a result from the injected current authority checker. A source
// producer string or receipt alone cannot produce this authorization. The
// concrete broker/Grant checker remains unregistered until D02 is accepted.
type CoreBootstrapExecutionAuthorization struct {
	Source                                               CoreBootstrapSource
	ProducerPrincipalID, ExecutorPrincipalID, DecisionID uuid.UUID
	ProducerVersion, ExecutorVersion                     int64
	ValidUntil                                           time.Time
}
type CoreBootstrapAuthorizer interface {
	AuthorizeCoreBootstrap(context.Context, CoreBootstrapSource) (CoreBootstrapExecutionAuthorization, error)
}

type CoreBootstrapWorkerResult struct {
	OperationID, InvitationID, RoleID, DeliveryID, AuditID uuid.UUID
	Status                                                 string
}
type CoreBootstrapWork struct {
	Jobs                     []CoreBootstrapJob
	Source                   CoreBootstrapSource
	Intent                   CoreBootstrapIntent
	Version                  int64
	Status                   string
	Superseded, AccessExists bool
	AccessStatus             TenantAccessStatus
	Lifecycle                CoreShadowTenant
	Result                   *CoreBootstrapWorkerResult
	Invitation               *TenantInvitationState
}
type CoreBootstrapInvitationDraft struct {
	RoleID, DeliveryID, AuditID uuid.UUID
	Invitation                  TenantInvitationState
	Token                       string // Only passed in memory to the encrypted Invitation outbox.
	Permissions                 []Permission
}
type CoreBootstrapWorkTransaction interface {
	LoadCoreBootstrapWork(context.Context, TenantScope, uuid.UUID) (CoreBootstrapWork, error)
	RecheckBootstrapExecution(context.Context, TenantScope, CoreBootstrapExecutionAuthorization) error
	CreateCoreBootstrapInvitation(context.Context, TenantScope, CoreBootstrapWork, CoreBootstrapInvitationDraft, CoreBootstrapExecutionAuthorization) error
	ExpireCoreBootstrapInvitation(context.Context, TenantScope, CoreBootstrapWork, CoreBootstrapExecutionAuthorization, uuid.UUID, time.Time) error
	AppendBootstrapWorkerAudit(context.Context, TenantScope, SecurityAuditEvent) error
}
type CoreBootstrapWorkUnitOfWork interface {
	WithinCoreBootstrapWork(context.Context, TenantScope, func(context.Context, CoreBootstrapWorkTransaction) error) error
}
type CoreBootstrapWorker struct {
	uow       CoreBootstrapWorkUnitOfWork
	authority CoreBootstrapAuthorizer
	catalog   PermissionCatalog
	ids       IDGenerator
	secrets   SecretGenerator
	clock     Clock
}

func NewCoreBootstrapWorker(u CoreBootstrapWorkUnitOfWork, a CoreBootstrapAuthorizer, c PermissionCatalog, ids IDGenerator, secrets SecretGenerator, clock Clock) (*CoreBootstrapWorker, error) {
	if u == nil || a == nil || c == nil || ids == nil || secrets == nil || clock == nil {
		return nil, ErrCoreBootstrapAuthority
	}
	return &CoreBootstrapWorker{u, a, c, ids, secrets, clock}, nil
}

// Reconcile performs one local transaction. It never creates a Human or logs
// in the recipient, even when that email already belongs to a verified Human.
// A durable dispatcher and same-identity reissue are separate execution gates.
func (u *CoreBootstrapWorker) Reconcile(ctx context.Context, scope TenantScope, operation uuid.UUID) (CoreBootstrapWorkerResult, error) {
	var result CoreBootstrapWorkerResult
	if operation.Version() != 7 {
		return result, ErrCoreBootstrapInvalid
	}
	err := u.uow.WithinCoreBootstrapWork(ctx, scope, func(ctx context.Context, tx CoreBootstrapWorkTransaction) error {
		work, err := tx.LoadCoreBootstrapWork(ctx, scope, operation)
		if err != nil {
			return err
		}
		if _, err = work.Intent.CanonicalPayload(); err != nil {
			return err
		}
		auth, err := u.authority.AuthorizeCoreBootstrap(ctx, work.Source)
		if err != nil {
			return err
		}
		if auth.Source != work.Source || auth.ProducerPrincipalID.Version() != 7 || auth.ExecutorPrincipalID.Version() != 7 || auth.ProducerPrincipalID == auth.ExecutorPrincipalID || auth.DecisionID.Version() != 7 || auth.ProducerVersion < 1 || auth.ExecutorVersion < 1 || !u.clock.Now().Before(auth.ValidUntil) {
			return ErrCoreBootstrapAuthority
		}
		if err = tx.RecheckBootstrapExecution(ctx, scope, auth); err != nil {
			return err
		}
		if work.Superseded {
			return ErrCoreBootstrapConflict
		}
		if work.Result != nil {
			result = *work.Result
			result.Status = work.Status
			return nil
		}
		if work.Status != "pending" || work.Version != 1 || work.AccessExists {
			return ErrCoreBootstrapConflict
		}
		if !work.Lifecycle.Fresh {
			return ErrTenantLifecycleStale
		}
		if work.Lifecycle.Status != "active" {
			return ErrTenantLifecycleBlocked
		}
		permissions := u.catalog.Permissions(PermissionScopeTenant)
		if len(permissions) == 0 {
			return ErrCoreBootstrapAuthority
		}
		seen := map[Permission]bool{}
		for _, p := range permissions {
			if p.Scope != PermissionScopeTenant || !u.catalog.Contains(p) || seen[p] {
				return ErrCoreBootstrapAuthority
			}
			seen[p] = true
		}
		var generated [4]uuid.UUID
		unique := map[uuid.UUID]bool{}
		for i := range generated {
			generated[i], err = u.ids.NewID()
			if err != nil || generated[i].Version() != 7 || unique[generated[i]] {
				return ErrAuthenticationDependency
			}
			unique[generated[i]] = true
		}
		role, invitation, delivery, audit := generated[0], generated[1], generated[2], generated[3]
		// Reuse the exact Invitation secret format and entropy validation.
		token, err := (&TenantInvitationUsecase{secrets: u.secrets}).token(scope, invitation)
		if err != nil {
			return err
		}
		now := u.clock.Now().UTC()
		inv := TenantInvitation{ID: invitation, NormalizedEmail: work.Intent.NormalizedEmail, RoleIDs: []uuid.UUID{role}, Locale: work.Intent.Locale, Status: InvitationPending, ExpiresAt: now.Add(invitationLifetime), Version: 1, DeliveryGeneration: 1, DeliveryStatus: "pending", CreatedBy: auth.ExecutorPrincipalID, CreatedAt: now, UpdatedAt: now}
		draft := CoreBootstrapInvitationDraft{RoleID: role, DeliveryID: delivery, AuditID: audit, Invitation: TenantInvitationState{Invitation: inv, TokenDigest: sha256.Sum256([]byte(token))}, Token: token, Permissions: permissions}
		if err = tx.CreateCoreBootstrapInvitation(ctx, scope, work, draft, auth); err != nil {
			return err
		}
		event := SecurityAuditEvent{ID: audit, ActorID: auth.ExecutorPrincipalID, AuthenticationMethod: AuditAuthenticationMethodInternal, Boundary: AuditBoundaryTenant, Action: "iam.bootstrap.invitation.created", TargetType: "tenant_bootstrap", TargetID: operation, TargetVersion: work.Version + 1, Result: AuditResultSucceeded, Reason: "CORE_BOOTSTRAP_INVITATION_CREATED", RequestID: work.Source.EventID.String(), CorrelationID: operation.String(), DecisionID: auth.DecisionID.String(), SourceService: AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now}
		if err = tx.AppendBootstrapWorkerAudit(ctx, scope, event); err != nil {
			return err
		}
		// The authority may expire while waiting on a local lock or storage.
		if err = tx.RecheckBootstrapExecution(ctx, scope, auth); err != nil {
			return err
		}
		result = CoreBootstrapWorkerResult{OperationID: operation, InvitationID: invitation, RoleID: role, DeliveryID: delivery, AuditID: audit, Status: "waiting_for_principal_verification"}
		return nil
	})
	if err != nil {
		return CoreBootstrapWorkerResult{}, err
	}
	return result, nil
}

// Expiry is an authorized state transition, serialized with Invitation
// acceptance and Recovery. It never infers a new intended administrator.
func (u *CoreBootstrapWorker) ExpireInvitation(ctx context.Context, scope TenantScope, operation uuid.UUID, generation int64) (CoreBootstrapWorkerResult, error) {
	var result CoreBootstrapWorkerResult
	if operation.Version() != 7 || generation < 1 {
		return result, ErrCoreBootstrapInvalid
	}
	err := u.uow.WithinCoreBootstrapWork(ctx, scope, func(ctx context.Context, tx CoreBootstrapWorkTransaction) error {
		work, err := tx.LoadCoreBootstrapWork(ctx, scope, operation)
		if err != nil {
			return err
		}
		if _, err = work.Intent.CanonicalPayload(); err != nil {
			return err
		}
		auth, err := u.authority.AuthorizeCoreBootstrap(ctx, work.Source)
		if err != nil {
			return err
		}
		if auth.Source != work.Source || auth.ProducerPrincipalID.Version() != 7 || auth.ExecutorPrincipalID.Version() != 7 || auth.ProducerPrincipalID == auth.ExecutorPrincipalID || auth.DecisionID.Version() != 7 || auth.ProducerVersion < 1 || auth.ExecutorVersion < 1 || !u.clock.Now().Before(auth.ValidUntil) {
			return ErrCoreBootstrapAuthority
		}
		if err = tx.RecheckBootstrapExecution(ctx, scope, auth); err != nil {
			return err
		}
		result = CoreBootstrapWorkerResult{OperationID: operation, Status: work.Status}
		if work.Result != nil {
			result = *work.Result
			result.Status = work.Status
		}
		if work.Superseded || work.Status == "succeeded" || work.Status == "attention_required" {
			return nil
		}
		if work.Status != "waiting_for_principal_verification" || work.Result == nil || work.Invitation == nil {
			return ErrCoreBootstrapConflict
		}
		inv := work.Invitation.Invitation
		if inv.DeliveryGeneration != generation {
			return nil
		}
		now := u.clock.Now().UTC()
		if now.Before(inv.ExpiresAt) {
			return nil
		}
		if inv.Status != InvitationPending && inv.Status != InvitationExpired {
			return ErrCoreBootstrapConflict
		}
		audit, err := u.ids.NewID()
		if err != nil || audit.Version() != 7 {
			return ErrAuthenticationDependency
		}
		if err = tx.ExpireCoreBootstrapInvitation(ctx, scope, work, auth, audit, now); err != nil {
			return err
		}
		event := SecurityAuditEvent{ID: audit, ActorID: auth.ExecutorPrincipalID, AuthenticationMethod: AuditAuthenticationMethodInternal, Boundary: AuditBoundaryTenant, Action: "iam.bootstrap.invitation.expired", TargetType: "tenant_bootstrap", TargetID: operation, TargetVersion: work.Version + 1, Result: AuditResultSucceeded, Reason: "CORE_BOOTSTRAP_INVITATION_EXPIRED", RequestID: work.Source.EventID.String(), CorrelationID: operation.String(), DecisionID: auth.DecisionID.String(), SourceService: AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now}
		if err = tx.AppendBootstrapWorkerAudit(ctx, scope, event); err != nil {
			return err
		}
		if err = tx.RecheckBootstrapExecution(ctx, scope, auth); err != nil {
			return err
		}
		result.Status = "attention_required"
		return nil
	})
	if err != nil {
		return CoreBootstrapWorkerResult{}, err
	}
	return result, nil
}

func validCoreBootstrapExecution(a CoreBootstrapExecutionAuthorization, source CoreBootstrapSource, now time.Time) bool {
	return a.Source == source && a.ProducerPrincipalID.Version() == 7 && a.ExecutorPrincipalID.Version() == 7 && a.ProducerPrincipalID != a.ExecutorPrincipalID && a.DecisionID.Version() == 7 && a.ProducerVersion > 0 && a.ExecutorVersion > 0 && now.Before(a.ValidUntil)
}
