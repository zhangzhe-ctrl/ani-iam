package biz

import (
	"context"
	"crypto/sha256"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

const AuditBoundaryPlatform AuditBoundary = "platform"

type PlatformRole struct {
	ID                      uuid.UUID
	Code                    string
	DisplayName             string
	System                  bool
	SystemDefinitionVersion int64
	Version                 int64
	Permissions             []Permission
}

type PlatformLoginState struct {
	IdentityID       uuid.UUID
	IdentityActive   bool
	Principal        Principal
	MembershipID     uuid.UUID
	MembershipStatus MembershipStatus
	NormalizedEmail  string
}

type PlatformFirstAdministratorState struct {
	Manifest             FirstAdministratorManifest
	Completed            bool
	AdministratorPresent bool
}

type PlatformFirstAdministratorCreation struct {
	Environment  string
	IntentID     uuid.UUID
	PrincipalID  uuid.UUID
	IdentityID   uuid.UUID
	MembershipID uuid.UUID
	BindingID    uuid.UUID
	RoleID       uuid.UUID
	Provider     string
	Identity     OIDCVerifiedIdentity
	AuditEventID uuid.UUID
	Now          time.Time
}

type PlatformLoginMutation struct {
	Session Session
	Grant   SessionGrant
	Family  RefreshTokenFamily
	Refresh RefreshToken
	Audit   SecurityAuditEvent
}

// This transaction port is exclusive to credential authentication. Ordinary
// Platform administration uses an authenticated capability, not this seam.
type PlatformLoginTransaction interface {
	LookupOIDC(context.Context, string, string, string) (PlatformLoginState, error)
	FirstAdministrator(context.Context, string) (PlatformFirstAdministratorState, error)
	IdentityAvailable(context.Context, string, string, string) (bool, error)
	AdministratorRole(context.Context) (PlatformRole, error)
	InsertAdministratorRole(context.Context, PlatformRole, time.Time) error
	CreateFirstAdministrator(context.Context, PlatformFirstAdministratorCreation) error
	SaveLogin(context.Context, PlatformLoginMutation) error
}

type PlatformLoginUnitOfWork interface {
	WithinPlatformLogin(context.Context, string, func(PlatformLoginTransaction) error) error
}

type PlatformLoginConfig struct {
	Environment  string
	Provider     string
	OIDCIssuer   string
	AccessIssuer string
}

type PlatformLoginUsecase struct {
	config  PlatformLoginConfig
	uow     PlatformLoginUnitOfWork
	catalog PermissionCatalog
	tokens  AccessTokenIssuer
	secrets SecretGenerator
	ids     IDGenerator
	clock   Clock
}

func NewPlatformLoginUsecase(config PlatformLoginConfig, uow PlatformLoginUnitOfWork, catalog PermissionCatalog, tokens AccessTokenIssuer, secrets SecretGenerator, ids IDGenerator, clock Clock) (*PlatformLoginUsecase, error) {
	if !bootstrapName.MatchString(config.Environment) || strings.TrimSpace(config.Provider) == "" || config.OIDCIssuer == "" || config.AccessIssuer == "" || uow == nil || catalog == nil || tokens == nil || secrets == nil || ids == nil || clock == nil {
		return nil, ErrOIDCConfigurationInvalid
	}
	return &PlatformLoginUsecase{config: config, uow: uow, catalog: catalog, tokens: tokens, secrets: secrets, ids: ids, clock: clock}, nil
}

// CompleteVerifiedOIDC is called only after the BOSS provider has validated
// code, PKCE, nonce, signature, issuer and audience for its single-use flow.
func (u *PlatformLoginUsecase) CompleteVerifiedOIDC(ctx context.Context, identity OIDCVerifiedIdentity, device, requestID string) (LoginResult, error) {
	if identity.Issuer != u.config.OIDCIssuer || identity.Subject == "" || !identity.EmailVerified {
		return LoginResult{}, ErrInvalidCredential
	}
	email, err := normalizeOIDCEmail(identity.Email)
	if err != nil {
		return LoginResult{}, ErrOIDCEmailUnverified
	}
	identity.Email = email
	if requestID == "" || len(requestID) > 128 || len(device) > 128 {
		return LoginResult{}, ErrInvalidCredential
	}
	var ids [11]uuid.UUID
	seen := map[uuid.UUID]bool{}
	for i := range ids {
		ids[i], err = u.ids.NewID()
		if err != nil || ids[i].Version() != 7 || seen[ids[i]] {
			return LoginResult{}, ErrAuthenticationDependency
		}
		seen[ids[i]] = true
	}
	secret, err := u.secrets.NewSecret()
	if err != nil || len(secret) < 32 {
		return LoginResult{}, ErrAuthenticationDependency
	}
	var result LoginResult
	err = u.uow.WithinPlatformLogin(ctx, u.config.Environment, func(tx PlatformLoginTransaction) error {
		now := u.clock.Now().UTC()
		state, err := tx.LookupOIDC(ctx, u.config.Provider, identity.Issuer, identity.Subject)
		first := errors.Is(err, ErrOIDCIdentityNotFound)
		var firstState PlatformFirstAdministratorState
		if first {
			firstState, err = tx.FirstAdministrator(ctx, u.config.Environment)
			if err != nil {
				return err
			}
			m := firstState.Manifest
			if firstState.Completed || firstState.AdministratorPresent || m.Environment != u.config.Environment || m.IntentID == uuid.Nil || !m.ExpiresAt.After(now) || m.Issuer != identity.Issuer || m.Subject != identity.Subject || m.Email != email {
				return ErrInvalidCredential
			}
			available, err := tx.IdentityAvailable(ctx, email, identity.Issuer, identity.Subject)
			if err != nil {
				return err
			}
			if !available {
				return ErrOIDCEmailConflict
			}
			permissions := PlatformAdministratorPermissions()
			for _, p := range permissions {
				if !u.catalog.Contains(p) {
					return ErrPermissionUncatalogued
				}
			}
			role, err := tx.AdministratorRole(ctx)
			if errors.Is(err, ErrRoleNotFound) {
				role = PlatformRole{ID: ids[3], Code: "platform-admin", DisplayName: "Platform administrator", System: true, SystemDefinitionVersion: 1, Version: 1, Permissions: permissions}
				if err = tx.InsertAdministratorRole(ctx, role, now); err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else if !role.System || role.SystemDefinitionVersion != 1 || role.Code != "platform-admin" || !samePlatformPermissions(role.Permissions, permissions) {
				return ErrFirstAdministratorConflict
			}
			state = PlatformLoginState{IdentityID: ids[1], IdentityActive: true, Principal: Principal{ID: ids[0], Status: PrincipalStatusActive}, MembershipID: ids[2], MembershipStatus: MembershipStatusActive, NormalizedEmail: email}
			if err = tx.CreateFirstAdministrator(ctx, PlatformFirstAdministratorCreation{Environment: u.config.Environment, IntentID: m.IntentID, PrincipalID: ids[0], IdentityID: ids[1], MembershipID: ids[2], RoleID: role.ID, BindingID: ids[4], Provider: u.config.Provider, Identity: identity, AuditEventID: ids[10], Now: now}); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if !state.IdentityActive || state.IdentityID == uuid.Nil || state.Principal.ID == uuid.Nil || state.NormalizedEmail != email {
			return ErrInvalidCredential
		}
		if state.Principal.Status != PrincipalStatusActive {
			return ErrPrincipalInactive
		}
		if state.MembershipID == uuid.Nil || state.MembershipStatus != MembershipStatusActive {
			return ErrMembershipInactive
		}
		accessExpiry, idleExpiry, absoluteExpiry := loginDeadlines(AudienceBoss, now)
		session := Session{ID: ids[5], PrincipalID: state.Principal.ID, Audience: AudienceBoss, Status: SessionStatusActive, Version: 1, AuthnMethods: []AuditAuthenticationMethod{AuditAuthenticationMethodOIDC}, DeviceName: strings.TrimSpace(device), IdleExpiresAt: idleExpiry, AbsoluteExpiry: absoluteExpiry, ReauthenticatedAt: now, CreatedAt: now, UpdatedAt: now}
		grant := SessionGrant{ID: ids[6], SessionID: session.ID, MembershipID: state.MembershipID, Status: GrantStatusActive, Version: 1, CreatedAt: now, UpdatedAt: now}
		family := RefreshTokenFamily{ID: ids[7], GrantID: grant.ID, Status: GrantStatusActive, Version: 1, CreatedAt: now, UpdatedAt: now}
		refresh := RefreshToken{ID: ids[8], FamilyID: family.ID, Digest: sha256.Sum256([]byte(secret)), Status: RefreshTokenStatusActive, IssuedAt: now, ExpiresAt: absoluteExpiry}
		audit := SecurityAuditEvent{ID: ids[10], ActorID: state.Principal.ID, AuthenticationMethod: AuditAuthenticationMethodOIDC, Boundary: AuditBoundaryPlatform, Action: AuditActionOIDCLoginSucceeded, TargetType: AuditTargetTypeSession, TargetID: session.ID, TargetVersion: 1, Result: AuditResultSucceeded, Reason: AuditReasonOIDCLogin, RequestID: requestID, CorrelationID: requestID, DecisionID: ids[10].String(), SourceService: AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now}
		if first {
			audit.Action = "iam.administrator.bootstrap.completed"
			audit.TargetType = "first_administrator_intent"
			audit.TargetID = firstState.Manifest.IntentID
			audit.Reason = "FIRST_ADMINISTRATOR_OIDC_BOOTSTRAP"
		}
		access, err := u.tokens.Issue(ctx, AccessTokenClaims{Boundary: AccessBoundaryPlatform, Issuer: u.config.AccessIssuer, Subject: state.Principal.ID, Audience: AudienceBoss, TokenID: ids[9], SessionID: session.ID, GrantID: grant.ID, GrantVersion: grant.Version, IssuedAt: now, ExpiresAt: accessExpiry, AuthnMethods: session.AuthnMethods})
		if err != nil {
			return ErrAuthenticationDependency
		}
		if err = tx.SaveLogin(ctx, PlatformLoginMutation{Session: session, Grant: grant, Family: family, Refresh: refresh, Audit: audit}); err != nil {
			return err
		}
		result = LoginResult{Boundary: AccessBoundaryPlatform, Principal: state.Principal, Session: session, Grant: grant, AccessToken: access, RefreshToken: secret, AccessTokenExpiresAt: accessExpiry}
		return nil
	})
	if err != nil {
		return LoginResult{}, err
	}
	return result, nil
}

// Version 1 is a finite ordinary-administration role. Recovery and Auditor
// capabilities are deliberately separate explicit Role Bindings.
func PlatformAdministratorPermissions() []Permission {
	var result []Permission
	for resource, actions := range map[string][]string{
		"iam.platform-memberships":   {"read", "update", "remove"},
		"iam.platform-roles":         {"read", "create", "update", "delete"},
		"iam.platform-role-bindings": {"create", "delete"},
		"iam.platform-invitations":   {"read", "create", "resend", "cancel"},
		"iam.tenant-access":          {"read", "update"},
	} {
		for _, action := range actions {
			result = append(result, Permission{Scope: PermissionScopePlatform, Resource: resource, Action: action})
		}
	}
	slices.SortFunc(result, comparePlatformPermission)
	return result
}

func comparePlatformPermission(a, b Permission) int {
	return strings.Compare(string(a.Scope)+"\x00"+a.Resource+"\x00"+a.Action, string(b.Scope)+"\x00"+b.Resource+"\x00"+b.Action)
}
func samePlatformPermissions(a, b []Permission) bool {
	a = slices.Clone(a)
	b = slices.Clone(b)
	slices.SortFunc(a, comparePlatformPermission)
	slices.SortFunc(b, comparePlatformPermission)
	return slices.Equal(a, b)
}
