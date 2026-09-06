package biz

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

const testPolicyRevision = "sha256:f222e2c6d3cd6442449cd722389d3d4fbfcdc7a0fee950c9d28385d3c264affa"

func TestCheckPermissionAllowsRegisteredOperationOnce(t *testing.T) {
	tenantID := uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70")
	principalID := uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17")
	sessionID := uuid.MustParse("0198f062-b76d-7001-9000-000000000001")
	grantID := uuid.MustParse("0198f062-b76d-7001-9000-000000000002")
	decisionID := uuid.MustParse("0198f062-b76d-7001-9000-000000000007")
	reader := &recordingAuthorizationReader{state: AuthorizationState{
		PrincipalStatus:   PrincipalStatusActive,
		MembershipStatus:  MembershipStatusActive,
		TenantAccess:      TenantAccessStatusActive,
		Lifecycle:         TenantLifecycleStatusActive,
		LifecycleFresh:    true,
		SessionStatus:     SessionStatusActive,
		GrantStatus:       GrantStatusActive,
		GrantVersion:      1,
		PermissionAllowed: true,
	}}
	usecase := NewAuthorizationUsecase(
		staticPolicyRegistry{revision: testPolicyRevision},
		&staticAccessTokenVerifier{claims: AccessTokenClaims{
			Subject:      principalID,
			Audience:     AudienceConsole,
			SessionID:    sessionID,
			GrantID:      grantID,
			GrantVersion: 1,
			TenantID:     tenantID,
			ExpiresAt:    time.Date(2026, 9, 4, 7, 0, 0, 0, time.UTC),
			AuthnMethods: []AuditAuthenticationMethod{AuditAuthenticationMethodPassword},
		}},
		reader,
		&fixedIDs{values: []uuid.UUID{decisionID}},
		fixedAuthClock{now: time.Date(2026, 9, 4, 6, 50, 0, 0, time.UTC)},
	)

	decision, err := usecase.CheckPermission(context.Background(), CheckPermissionCommand{
		RawCredential:  "signed-access-token",
		OperationID:    "listInstances",
		PolicyRevision: testPolicyRevision,
		TargetTenantID: tenantID,
	})
	if err != nil {
		t.Fatalf("CheckPermission() error = %v", err)
	}
	if !decision.Allowed || decision.Reason != AuthorizationReasonAllowed || decision.DecisionID != decisionID {
		t.Fatalf("decision = %#v", decision)
	}
	if decision.Principal.ID != principalID || decision.Principal.SessionID != sessionID || decision.Principal.GrantID != grantID {
		t.Fatalf("principal context = %#v", decision.Principal)
	}
	if reader.calls != 1 || reader.lookup.Resource != "instances" || len(reader.lookup.Actions) != 1 || reader.lookup.Actions[0] != "read" {
		t.Fatalf("reader calls/lookup = %d / %#v", reader.calls, reader.lookup)
	}
}

func TestCheckPermissionFailsClosedOnPolicyRevisionMismatch(t *testing.T) {
	verifier := &staticAccessTokenVerifier{}
	reader := &recordingAuthorizationReader{}
	usecase := NewAuthorizationUsecase(
		staticPolicyRegistry{revision: testPolicyRevision},
		verifier,
		reader,
		&fixedIDs{},
		fixedAuthClock{},
	)

	_, err := usecase.CheckPermission(context.Background(), CheckPermissionCommand{
		RawCredential:  "signed-access-token",
		OperationID:    "listInstances",
		PolicyRevision: "sha256:wrong",
		TargetTenantID: uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70"),
	})
	if !errors.Is(err, ErrAuthorizationPolicyMismatch) {
		t.Fatalf("CheckPermission() error = %v, want %v", err, ErrAuthorizationPolicyMismatch)
	}
	if verifier.calls != 0 || reader.calls != 0 {
		t.Fatalf("dependencies called on policy mismatch: verifier=%d reader=%d", verifier.calls, reader.calls)
	}
}

type staticPolicyRegistry struct{ revision string }

func (r staticPolicyRegistry) Revision() string { return r.revision }

func (r staticPolicyRegistry) Lookup(operationID string) (AuthorizationPolicy, bool) {
	if operationID != "listInstances" {
		return AuthorizationPolicy{}, false
	}
	return AuthorizationPolicy{OperationID: operationID, Resource: "instances", Actions: []string{"read"}}, true
}

type staticAccessTokenVerifier struct {
	claims AccessTokenClaims
	err    error
	calls  int
}

func (v *staticAccessTokenVerifier) Verify(context.Context, string) (AccessTokenClaims, error) {
	v.calls++
	return v.claims, v.err
}

type recordingAuthorizationReader struct {
	state  AuthorizationState
	err    error
	calls  int
	lookup AuthorizationLookup
}

func (r *recordingAuthorizationReader) LookupAuthorization(_ context.Context, _ TenantScope, lookup AuthorizationLookup) (AuthorizationState, error) {
	r.calls++
	r.lookup = lookup
	return r.state, r.err
}
