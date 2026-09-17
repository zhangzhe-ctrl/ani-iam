package biz

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrPlatformAdministrationDenied = errors.New("Platform administration is not authorized")

// Private fields prevent adapters from manufacturing a Platform boundary from
// request fields. This capability is revalidated within each repository UOW.
type PlatformCapability struct {
	claims                                                            AccessTokenClaims
	operation, revision, reason, requestID, correlationID, decisionID string
	caller                                                            DirectCaller
}

// CredentialBinding is read-only input for the data-layer recheck. It does not
// mint another capability and rejects the zero value.
func (c PlatformCapability) CredentialBinding() (AccessTokenClaims, string, error) {
	if c.claims.Boundary != AccessBoundaryPlatform || c.claims.Audience != AudienceBoss || c.claims.TenantID != uuid.Nil || c.claims.Subject == uuid.Nil || c.operation == "" || c.decisionID == "" || c.reason == "" || c.caller.Target.Audience != "ani-iam" {
		return AccessTokenClaims{}, "", ErrPlatformAdministrationDenied
	}
	claims := c.claims
	claims.AuthnMethods = append([]AuditAuthenticationMethod(nil), c.claims.AuthnMethods...)
	return claims, c.operation, nil
}

func (u *PlatformAuthorizationUsecase) AuthorizeAdministration(ctx context.Context, c CheckPermissionCommand, rpc, reason string) (PlatformCapability, error) {
	caller, ok := DirectCallerFromContext(ctx)
	if !ok || caller.Target != (WorkloadTarget{Audience: "ani-iam", Operation: "/iam.v1.IAMAdminService/" + rpc}) || rpc == "" || reason == "" || len(reason) > 128 {
		return PlatformCapability{}, ErrPlatformAdministrationDenied
	}
	decision, err := u.CheckPermission(ctx, c)
	if err != nil {
		return PlatformCapability{}, err
	}
	if !decision.Allowed {
		return PlatformCapability{}, ErrPlatformAdministrationDenied
	}
	claims, err := u.verifier.Verify(ctx, c.RawCredential)
	if err != nil || claims.Subject != decision.Principal.ID || claims.SessionID != decision.Principal.SessionID || claims.GrantID != decision.Principal.GrantID {
		return PlatformCapability{}, ErrInvalidCredential
	}
	request, correlation := c.RequestID, c.CorrelationID
	if request == "" {
		request = decision.DecisionID.String()
	}
	if correlation == "" {
		correlation = request
	}
	return PlatformCapability{claims: claims, operation: c.OperationID, revision: c.PolicyRevision, reason: reason, requestID: request, correlationID: correlation, decisionID: decision.DecisionID.String(), caller: caller}, nil
}

type PlatformMembership struct {
	ID, PrincipalID      uuid.UUID
	Status               MembershipStatus
	Version              int64
	RoleIDs              []uuid.UUID
	CreatedAt, UpdatedAt time.Time
}
type PlatformMembershipPage struct {
	Items      []PlatformMembership
	NextCursor string
}
type PlatformRolePage struct {
	Items      []PlatformRole
	NextCursor string
}

type PlatformAdministrationTransaction interface {
	CoreDLQAdministrationRepository
	CoreBootstrapAdministrationRepository
	TenantBootstrapRecoveryRepository
	TenantAdminRecoveryRepository
	PlatformInvitationRepository
	PlatformMutationRepository
	PlatformMembershipRepository
	PlatformAuditRepository
	LookupAuthority(context.Context, PlatformCapability, string, []string) (PlatformAuthorizationState, error)
	GetMembership(context.Context, PlatformCapability, uuid.UUID) (PlatformMembership, error)
	ListMemberships(context.Context, PlatformCapability, MembershipStatus, uuid.UUID, int32) ([]PlatformMembership, error)
	GetRole(context.Context, PlatformCapability, uuid.UUID) (PlatformRole, error)
	ListRoles(context.Context, PlatformCapability, uuid.UUID, int32) ([]PlatformRole, error)
	GetTenantAccess(context.Context, PlatformCapability, uuid.UUID) (TenantAccess, error)
	AppendAudit(context.Context, PlatformCapability, SecurityAuditEvent) error
}
type PlatformAdministrationUnitOfWork interface {
	WithinPlatformAdministration(context.Context, PlatformCapability, func(PlatformAdministrationTransaction) error) error
}
type PlatformAdministrationUsecase struct {
	dlqDecoder          CoreBrokerDecoder
	dlqFailures         CoreDLQFailureRecorder
	bootstrapProducer   string
	bootstrapAuthority  CoreBootstrapAuthorizer
	invitationSecrets   SecretGenerator
	recoveryVerifier    AccessCredentialVerifier
	recoveryLoginPolicy TenantAdminLoginPolicy
	uow                 PlatformAdministrationUnitOfWork
	registry            AuthorizationPolicyRegistry
	catalog             *PermissionCatalogReader
	loginPolicy         PlatformAdminLoginPolicy
	ids                 IDGenerator
	clock               Clock
}

func NewPlatformAdministrationUsecase(uow PlatformAdministrationUnitOfWork, registry AuthorizationPolicyRegistry, catalog *PermissionCatalogReader, ids IDGenerator, clock Clock) *PlatformAdministrationUsecase {
	return &PlatformAdministrationUsecase{uow: uow, registry: registry, catalog: catalog, ids: ids, clock: clock}
}

func (u *PlatformAdministrationUsecase) withinAuthorized(ctx context.Context, cap PlatformCapability, operation string, fn func(PlatformAdministrationTransaction, time.Time) error) error {
	claims, op, err := cap.CredentialBinding()
	if err != nil || op != operation {
		return ErrPlatformAdministrationDenied
	}
	if u == nil || u.uow == nil || u.registry == nil || u.ids == nil || u.clock == nil {
		return ErrAuthenticationDependency
	}
	if cap.revision != u.registry.Revision() {
		return &AuthorizationPolicyMismatchError{Expected: u.registry.Revision(), Actual: cap.revision}
	}
	policy, ok := u.registry.Lookup(op)
	if !ok || policy.Scope != PermissionScopePlatform {
		return ErrAuthorizationOperationUnregistered
	}
	return u.uow.WithinPlatformAdministration(ctx, cap, func(tx PlatformAdministrationTransaction) error {
		now := u.clock.Now().UTC()
		if !now.Before(claims.ExpiresAt) {
			return ErrInvalidCredential
		}
		state, err := tx.LookupAuthority(ctx, cap, policy.Resource, policy.Actions)
		if err != nil {
			return err
		}
		if platformAuthorizationDenial(state, claims, now) != "" || !state.PermissionAllowed {
			return ErrPlatformAdministrationDenied
		}
		return fn(tx, now)
	})
}
func newPlatformAdministrationAudit(cap PlatformCapability, resource string, id, target uuid.UUID, version int64, now time.Time) SecurityAuditEvent {
	return SecurityAuditEvent{ID: id, ActorID: cap.claims.Subject, DirectCaller: cap.caller, AuthenticationMethod: firstAuthenticationMethod(cap.claims.AuthnMethods), Boundary: AuditBoundaryPlatform, Action: AuditAction("iam.platform." + cap.operation), TargetType: AuditTargetType(resource), TargetID: target, TargetVersion: version, Result: AuditResultSucceeded, Reason: AuditReason(cap.reason), RequestID: cap.requestID, CorrelationID: cap.correlationID, DecisionID: cap.decisionID, SourceService: AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now}
}
func (u *PlatformAdministrationUsecase) within(ctx context.Context, cap PlatformCapability, operation string, target uuid.UUID, version *int64, read func(PlatformAdministrationTransaction) error) error {
	return u.withinAuthorized(ctx, cap, operation, func(tx PlatformAdministrationTransaction, now time.Time) error {
		if err := read(tx); err != nil {
			return err
		}
		id, err := u.ids.NewID()
		if err != nil || id.Version() != 7 {
			return ErrInvalidGeneratedID
		}
		if target == uuid.Nil {
			target = id
		}
		targetVersion := int64(1)
		if version != nil {
			targetVersion = *version
		}
		if targetVersion < 1 {
			return ErrInvalidPersistenceState
		}
		policy, _ := u.registry.Lookup(operation)
		return tx.AppendAudit(ctx, cap, newPlatformAdministrationAudit(cap, policy.Resource, id, target, targetVersion, now))
	})
}
func (u *PlatformAdministrationUsecase) GetMembership(ctx context.Context, cap PlatformCapability, id uuid.UUID) (PlatformMembership, error) {
	if id == uuid.Nil {
		return PlatformMembership{}, ErrMembershipNotFound
	}
	var result PlatformMembership
	err := u.within(ctx, cap, "getPlatformIAMMember", id, &result.Version, func(tx PlatformAdministrationTransaction) error {
		var err error
		result, err = tx.GetMembership(ctx, cap, id)
		return err
	})
	return result, err
}
func (u *PlatformAdministrationUsecase) GetRole(ctx context.Context, cap PlatformCapability, id uuid.UUID) (PlatformRole, error) {
	if id == uuid.Nil {
		return PlatformRole{}, ErrRoleNotFound
	}
	var result PlatformRole
	err := u.within(ctx, cap, "getPlatformIAMRole", id, &result.Version, func(tx PlatformAdministrationTransaction) error {
		var err error
		result, err = tx.GetRole(ctx, cap, id)
		return err
	})
	return result, err
}
func (u *PlatformAdministrationUsecase) GetTenantAccess(ctx context.Context, cap PlatformCapability, id uuid.UUID) (TenantAccess, error) {
	if id == uuid.Nil {
		return TenantAccess{}, ErrTenantAccessNotFound
	}
	var result TenantAccess
	err := u.within(ctx, cap, "getTenantAccess", id, &result.Version, func(tx PlatformAdministrationTransaction) error {
		var err error
		result, err = tx.GetTenantAccess(ctx, cap, id)
		return err
	})
	return result, err
}

type platformAdminCursor struct {
	Actor     uuid.UUID        `json:"actor"`
	Operation string           `json:"operation"`
	Revision  string           `json:"revision"`
	Status    MembershipStatus `json:"status"`
	After     uuid.UUID        `json:"after"`
}

func platformAdminPage(cap PlatformCapability, operation string, status MembershipStatus, raw string, limit uint32) (uuid.UUID, int32, error) {
	if limit > 100 || len(raw) > 2048 || (status != "" && status != MembershipStatusActive && status != MembershipStatusSuspended && status != MembershipStatusRemoved) {
		return uuid.Nil, 0, ErrRoleInvalid
	}
	if limit == 0 {
		limit = 50
	}
	after := uuid.Nil
	if raw != "" {
		data, err := base64.RawURLEncoding.DecodeString(raw)
		var c platformAdminCursor
		if err != nil || json.Unmarshal(data, &c) != nil || c.Actor != cap.claims.Subject || c.Operation != operation || c.Revision != cap.revision || c.Status != status || c.After == uuid.Nil {
			return uuid.Nil, 0, ErrRoleInvalid
		}
		after = c.After
	}
	return after, int32(limit), nil
}
func platformAdminNext(cap PlatformCapability, operation string, status MembershipStatus, after uuid.UUID) string {
	raw, _ := json.Marshal(platformAdminCursor{Actor: cap.claims.Subject, Operation: operation, Revision: cap.revision, Status: status, After: after})
	return base64.RawURLEncoding.EncodeToString(raw)
}
func (u *PlatformAdministrationUsecase) ListMemberships(ctx context.Context, cap PlatformCapability, status MembershipStatus, cursor string, limit uint32) (PlatformMembershipPage, error) {
	const op = "listPlatformIAMMembers"
	after, n, err := platformAdminPage(cap, op, status, cursor, limit)
	if err != nil {
		return PlatformMembershipPage{}, err
	}
	result := PlatformMembershipPage{Items: []PlatformMembership{}}
	err = u.within(ctx, cap, op, uuid.Nil, nil, func(tx PlatformAdministrationTransaction) error {
		rows, err := tx.ListMemberships(ctx, cap, status, after, n+1)
		if err != nil {
			return err
		}
		if len(rows) > int(n) {
			rows = rows[:n]
			result.NextCursor = platformAdminNext(cap, op, status, rows[len(rows)-1].ID)
		}
		result.Items = rows
		return nil
	})
	return result, err
}
func (u *PlatformAdministrationUsecase) ListRoles(ctx context.Context, cap PlatformCapability, cursor string, limit uint32) (PlatformRolePage, error) {
	const op = "listPlatformIAMRoles"
	after, n, err := platformAdminPage(cap, op, "", cursor, limit)
	if err != nil {
		return PlatformRolePage{}, err
	}
	result := PlatformRolePage{Items: []PlatformRole{}}
	err = u.within(ctx, cap, op, uuid.Nil, nil, func(tx PlatformAdministrationTransaction) error {
		rows, err := tx.ListRoles(ctx, cap, after, n+1)
		if err != nil {
			return err
		}
		if len(rows) > int(n) {
			rows = rows[:n]
			result.NextCursor = platformAdminNext(cap, op, "", rows[len(rows)-1].ID)
		}
		result.Items = rows
		return nil
	})
	return result, err
}
func (u *PlatformAdministrationUsecase) ListPermissions(ctx context.Context, cap PlatformCapability, cursor string, limit uint32) (PermissionCatalogPage, error) {
	var result PermissionCatalogPage
	err := u.within(ctx, cap, "listPlatformIAMPermissions", uuid.Nil, nil, func(PlatformAdministrationTransaction) error {
		var err error
		result, err = u.catalog.List(PermissionScopePlatform, strings.TrimSpace(cursor), limit)
		return err
	})
	return result, err
}
