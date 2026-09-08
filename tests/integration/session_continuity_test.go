//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

func TestSessionContinuityWithRealPostgresAndRedis(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	seedTargetLoginFixture(t, ctx, environment, "test-password-hash")
	targetMembershipID := uuid.MustParse("0199c71e-e000-7001-9000-000000000001")
	seedPool := mustPool(t, environment.migrationDSN(primaryDB))
	_, err := seedPool.Exec(ctx, `
		INSERT INTO tenant_memberships (tenant_id, id, principal_id, status, version, created_at, updated_at)
		VALUES
			($1, '0199c71e-e000-7001-9000-000000000009', $3, 'removed', 1, now() - interval '1 day', now() - interval '1 day'),
			($1, $2, $3, 'active', 1, now(), now())
	`, tenantB, targetMembershipID, actorID)
	if err != nil {
		seedPool.Close()
		t.Fatalf("seed target tenant membership: %v", err)
	}
	if _, err := seedPool.Exec(ctx, `UPDATE tenant_lifecycle_projections SET fresh_until = now() + interval '1 hour'`); err != nil {
		seedPool.Close()
		t.Fatalf("extend isolated lifecycle fixture: %v", err)
	}
	seedPool.Close()

	redisClient := newVerticalSliceRedisClient(t, ctx)
	throttle, err := data.NewRedisLoginThrottle(redisClient, data.RedisLoginThrottleConfig{
		Namespace: "ani-iam:dp2-08:session-continuity", Limit: 5,
		Window: 15 * time.Minute, BaseDelay: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewRedisLoginThrottle() error = %v", err)
	}
	clock := &mutableIntegrationClock{now: time.Now().UTC().Truncate(time.Second)}
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x68}, ed25519.SeedSize))
	tokens, err := data.NewJWXAccessTokenCodec(
		"dp2-08-session-key", privateKey,
		map[string]ed25519.PublicKey{"dp2-08-session-key": privateKey.Public().(ed25519.PublicKey)},
		"ani-iam", clock,
	)
	if err != nil {
		t.Fatalf("NewJWXAccessTokenCodec() error = %v", err)
	}
	postgresData := data.NewData(environment.runtimePool)
	usecase := biz.NewAuthenticationUsecase(
		data.NewPostgresPasswordLoginReader(postgresData), acceptingIntegrationPasswordVerifier{}, throttle,
		data.NewPostgresLoginUnitOfWork(postgresData), tokens, data.NewSecretGenerator(),
		data.NewUUIDv7Generator(), clock, allowingAPIKeyUsageObserver{})

	login := func(t *testing.T, key string) biz.LoginResult {
		t.Helper()
		result, err := usecase.PasswordLogin(ctx, biz.PasswordLoginCommand{
			Account: "user@example.com", Password: "correct-password", Audience: biz.AudienceConsole,
			TenantID: tenantA, SourceIP: mustTestAddr(t, "203.0.113.10"), DeviceName: key,
			IdempotencyKey: key,
		})
		if err != nil {
			t.Fatalf("PasswordLogin(%s) error = %v", key, err)
		}
		return result
	}

	t.Run("rotation and reuse stay inside one boundary", func(t *testing.T) {
		initial := login(t, "dp2-08-cross-boundary-login")
		clock.Advance(time.Minute)
		target, err := usecase.SwitchTenant(ctx, biz.SwitchTenantCommand{
			RawCredential: initial.AccessToken, TargetTenantID: tenantB, IdempotencyKey: "dp2-08-switch-b",
		})
		if err != nil {
			t.Fatalf("SwitchTenant(tenant B) error = %v", err)
		}
		if target.Session.ID != initial.Session.ID || target.Grant.ID == initial.Grant.ID || target.TenantID != tenantB {
			t.Fatalf("target boundary result = %#v", target)
		}

		clock.Advance(time.Minute)
		rotated, err := usecase.RefreshSession(ctx, biz.RefreshSessionCommand{
			RefreshToken: initial.RefreshToken, CSRFToken: "csrf-a", Origin: "https://console.test.example",
			IdempotencyKey: "dp2-08-refresh-a",
		})
		if err != nil {
			t.Fatalf("RefreshSession(tenant A) error = %v", err)
		}
		if rotated.RefreshToken == initial.RefreshToken || rotated.Grant.ID != initial.Grant.ID ||
			rotated.Grant.Version != initial.Grant.Version || rotated.TenantID != tenantA {
			t.Fatalf("rotated tenant A result = %#v", rotated)
		}
		assertRefreshReplacement(t, ctx, environment, initial.RefreshToken, rotated.RefreshToken, initial.Grant.ID)

		_, err = usecase.RefreshSession(ctx, biz.RefreshSessionCommand{
			RefreshToken: initial.RefreshToken, CSRFToken: "csrf-a", Origin: "https://console.test.example",
			IdempotencyKey: "dp2-08-refresh-a-reuse",
		})
		if !errors.Is(err, biz.ErrInvalidCredential) {
			t.Fatalf("RefreshSession(reuse) error = %v, want %v", err, biz.ErrInvalidCredential)
		}
		var sourceGrantStatus, sourceFamilyStatus, replacementStatus string
		var sourceGrantVersion int64
		if err := environment.runtimePool.QueryRow(ctx, `
			SELECT grant_row.status, grant_row.version, family.status, replacement.status
			FROM session_grants AS grant_row
			JOIN refresh_token_families AS family ON family.tenant_id = grant_row.tenant_id AND family.grant_id = grant_row.id
			JOIN refresh_tokens AS replacement ON replacement.tenant_id = family.tenant_id AND replacement.family_id = family.id
			WHERE grant_row.tenant_id = $1 AND grant_row.id = $2 AND replacement.digest = $3
		`, tenantA, initial.Grant.ID, refreshDigest(rotated.RefreshToken)).Scan(
			&sourceGrantStatus, &sourceGrantVersion, &sourceFamilyStatus, &replacementStatus,
		); err != nil {
			t.Fatalf("query reused source boundary: %v", err)
		}
		if sourceGrantStatus != "active" || sourceGrantVersion != initial.Grant.Version+1 ||
			sourceFamilyStatus != "revoked" || replacementStatus != "revoked" {
			t.Fatalf("source boundary after reuse = grant:%s/%d family:%s token:%s",
				sourceGrantStatus, sourceGrantVersion, sourceFamilyStatus, replacementStatus)
		}
		var targetGrantStatus, targetFamilyStatus string
		var targetGrantVersion int64
		if err := environment.runtimePool.QueryRow(ctx, `
			SELECT grant_row.status, grant_row.version, family.status
			FROM session_grants AS grant_row
			JOIN refresh_token_families AS family ON family.tenant_id = grant_row.tenant_id AND family.grant_id = grant_row.id
			WHERE grant_row.tenant_id = $1 AND grant_row.id = $2
		`, tenantB, target.Grant.ID).Scan(&targetGrantStatus, &targetGrantVersion, &targetFamilyStatus); err != nil {
			t.Fatalf("query unaffected target boundary: %v", err)
		}
		if targetGrantStatus != "active" || targetGrantVersion != target.Grant.Version || targetFamilyStatus != "active" {
			t.Fatalf("target boundary after source reuse = grant:%s/%d family:%s",
				targetGrantStatus, targetGrantVersion, targetFamilyStatus)
		}
	})

	t.Run("required Audit failure rolls back refresh state", func(t *testing.T) {
		initial := login(t, "dp2-08-rollback-login")
		var duplicateAuditID uuid.UUID
		if err := environment.runtimePool.QueryRow(ctx, `
			SELECT event_id FROM iam_audit_events ORDER BY recorded_at, event_id LIMIT 1
		`).Scan(&duplicateAuditID); err != nil {
			t.Fatalf("select duplicate Audit ID: %v", err)
		}
		replacementID := uuid.MustParse("0199c71e-e000-7002-9000-000000000001")
		rollbackUsecase := biz.NewAuthenticationUsecase(
			data.NewPostgresPasswordLoginReader(postgresData), acceptingIntegrationPasswordVerifier{}, throttle,
			data.NewPostgresLoginUnitOfWork(postgresData), tokens,
			staticIntegrationSecretGenerator{secret: "rollback-refresh-replacement"},
			&fixedIDGenerator{ids: []uuid.UUID{
				replacementID,
				uuid.MustParse("0199c71e-e000-7002-9000-000000000002"),
				duplicateAuditID,
				uuid.MustParse("0199c71e-e000-7002-9000-000000000004"),
			}}, clock, allowingAPIKeyUsageObserver{})

		_, err := rollbackUsecase.RefreshSession(ctx, biz.RefreshSessionCommand{
			RefreshToken: initial.RefreshToken, CSRFToken: "csrf-rollback", Origin: "https://console.test.example",
			IdempotencyKey: "dp2-08-refresh-audit-rollback",
		})
		if !errors.Is(err, biz.ErrAuthenticationDependency) || !errors.Is(err, biz.ErrAuditConflict) {
			t.Fatalf("RefreshSession(Audit failure) error = %v", err)
		}
		var oldStatus string
		if err := environment.runtimePool.QueryRow(ctx, `SELECT status FROM refresh_tokens WHERE digest = $1`, refreshDigest(initial.RefreshToken)).Scan(&oldStatus); err != nil {
			t.Fatalf("query original token after rollback: %v", err)
		}
		var replacementCount int
		if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM refresh_tokens WHERE id = $1`, replacementID).Scan(&replacementCount); err != nil {
			t.Fatalf("query replacement after rollback: %v", err)
		}
		if oldStatus != "active" || replacementCount != 0 {
			t.Fatalf("refresh rollback state = old:%s replacements:%d", oldStatus, replacementCount)
		}
	})

	t.Run("concurrent refresh admits one rotation then revokes reused boundary", func(t *testing.T) {
		initial := login(t, "dp2-08-concurrent-login")
		start := make(chan struct{})
		type outcome struct {
			result biz.RefreshSessionResult
			err    error
		}
		outcomes := make(chan outcome, 2)
		for index := 0; index < 2; index++ {
			go func(index int) {
				<-start
				result, err := usecase.RefreshSession(ctx, biz.RefreshSessionCommand{
					RefreshToken: initial.RefreshToken, CSRFToken: "csrf-concurrent", Origin: "https://console.test.example",
					IdempotencyKey: "dp2-08-concurrent-" + string(rune('a'+index)),
				})
				outcomes <- outcome{result: result, err: err}
			}(index)
		}
		close(start)
		successes, reuses := 0, 0
		for index := 0; index < 2; index++ {
			outcome := <-outcomes
			switch {
			case outcome.err == nil:
				successes++
			case errors.Is(outcome.err, biz.ErrInvalidCredential):
				reuses++
			default:
				t.Fatalf("concurrent RefreshSession outcome = %#v", outcome)
			}
		}
		if successes != 1 || reuses != 1 {
			t.Fatalf("concurrent RefreshSession outcomes = success:%d reuse:%d", successes, reuses)
		}
		var grantStatus, familyStatus string
		var grantVersion int64
		if err := environment.runtimePool.QueryRow(ctx, `
			SELECT grant_row.status, grant_row.version, family.status
			FROM session_grants AS grant_row
			JOIN refresh_token_families AS family ON family.tenant_id = grant_row.tenant_id AND family.grant_id = grant_row.id
			WHERE grant_row.tenant_id = $1 AND grant_row.id = $2
		`, tenantA, initial.Grant.ID).Scan(&grantStatus, &grantVersion, &familyStatus); err != nil {
			t.Fatalf("query concurrent refresh boundary: %v", err)
		}
		if grantStatus != "active" || grantVersion != initial.Grant.Version+1 || familyStatus != "revoked" {
			t.Fatalf("concurrent refresh boundary = grant:%s/%d family:%s", grantStatus, grantVersion, familyStatus)
		}
	})

	t.Run("stale refresh after same-boundary switch resolves as reuse", func(t *testing.T) {
		initial := login(t, "dp2-08-switch-refresh-race-login")
		digest := sha256.Sum256([]byte(initial.RefreshToken))
		reader := data.NewPostgresPasswordLoginReader(postgresData)
		stale, err := reader.LookupRefreshSession(ctx, digest)
		if err != nil {
			t.Fatalf("LookupRefreshSession(before switch) error = %v", err)
		}
		clock.Advance(time.Minute)
		switched, err := usecase.SwitchTenant(ctx, biz.SwitchTenantCommand{
			RawCredential: initial.AccessToken, TargetTenantID: tenantA, IdempotencyKey: "dp2-08-switch-same-boundary",
		})
		if err != nil {
			t.Fatalf("SwitchTenant(same boundary) error = %v", err)
		}
		if switched.Grant.ID != initial.Grant.ID || switched.Grant.Version != initial.Grant.Version+1 {
			t.Fatalf("same-boundary switch grant = %#v", switched.Grant)
		}
		now := clock.Now()
		reuseAuditID := uuid.MustParse("0199c71e-e000-7003-9000-000000000001")
		mutationResult, err := data.NewPostgresLoginUnitOfWork(postgresData).RotateRefreshSession(ctx, biz.RefreshSessionMutation{
			Expected: stale,
			ReuseAudit: biz.SecurityAuditEvent{
				ID: reuseAuditID, ActorID: stale.Principal.ID,
				AuthenticationMethod: stale.Session.AuthnMethods[0], Boundary: biz.AuditBoundaryTenant,
				Action: biz.AuditActionRefreshTokenReused, TargetType: biz.AuditTargetTypeSessionGrant,
				TargetID: stale.Grant.ID, TargetVersion: stale.Grant.Version + 1,
				Result: biz.AuditResultSucceeded, Reason: biz.AuditReasonRefreshTokenReuse,
				RequestID: "dp2-08-stale-refresh", CorrelationID: "dp2-08-stale-refresh",
				DecisionID: reuseAuditID.String(), SourceService: biz.AuditSourceServiceIAM,
				OccurredAt: now, RecordedAt: now,
			},
			RotatedAt: now,
		})
		if err != nil || !mutationResult.Reused {
			t.Fatalf("RotateRefreshSession(stale after switch) result=%#v error=%v", mutationResult, err)
		}
		var grantVersion int64
		var familyStatus string
		if err := environment.runtimePool.QueryRow(ctx, `
			SELECT grant_row.version, family.status
			FROM session_grants AS grant_row
			JOIN refresh_token_families AS family
			  ON family.tenant_id = grant_row.tenant_id AND family.grant_id = grant_row.id
			WHERE grant_row.tenant_id = $1 AND grant_row.id = $2
		`, tenantA, initial.Grant.ID).Scan(&grantVersion, &familyStatus); err != nil {
			t.Fatalf("query stale refresh boundary: %v", err)
		}
		if grantVersion != initial.Grant.Version+2 || familyStatus != "revoked" {
			t.Fatalf("stale refresh boundary = version:%d family:%s", grantVersion, familyStatus)
		}
	})

	t.Run("tenant switch denies suspended target membership without creating a boundary", func(t *testing.T) {
		initial := login(t, "dp2-08-switch-negative-login")
		if _, err := environment.runtimePool.Exec(ctx, `
			UPDATE tenant_memberships
			SET status = 'suspended', version = version + 1, updated_at = now()
			WHERE tenant_id = $1 AND id = $2
		`, tenantB, targetMembershipID); err != nil {
			t.Fatalf("suspend target membership: %v", err)
		}
		t.Cleanup(func() {
			_, _ = environment.runtimePool.Exec(context.Background(), `
				UPDATE tenant_memberships
				SET status = 'active', version = version + 1, updated_at = now()
				WHERE tenant_id = $1 AND id = $2
			`, tenantB, targetMembershipID)
		})

		_, err := usecase.SwitchTenant(ctx, biz.SwitchTenantCommand{
			RawCredential: initial.AccessToken, TargetTenantID: tenantB, IdempotencyKey: "dp2-08-switch-suspended",
		})
		if !errors.Is(err, biz.ErrMembershipInactive) {
			t.Fatalf("SwitchTenant(suspended membership) error = %v, want %v", err, biz.ErrMembershipInactive)
		}
		var grants int
		if err := environment.runtimePool.QueryRow(ctx, `
			SELECT count(*) FROM session_grants WHERE tenant_id = $1 AND session_id = $2
		`, tenantB, initial.Session.ID).Scan(&grants); err != nil {
			t.Fatalf("query denied target boundary: %v", err)
		}
		if grants != 0 {
			t.Fatalf("suspended target switch created %d grants", grants)
		}
		if _, err := environment.runtimePool.Exec(ctx, `
			UPDATE tenant_memberships
			SET status = 'active', version = version + 1, updated_at = now()
			WHERE tenant_id = $1 AND id = $2
		`, tenantB, targetMembershipID); err != nil {
			t.Fatalf("restore target membership: %v", err)
		}
	})

	t.Run("logout revokes only current Session and audits once", func(t *testing.T) {
		current := login(t, "dp2-08-logout-current")
		other := login(t, "dp2-08-logout-other")
		clock.Advance(time.Minute)
		boundaryB, err := usecase.SwitchTenant(ctx, biz.SwitchTenantCommand{
			RawCredential: current.AccessToken, TargetTenantID: tenantB, IdempotencyKey: "dp2-08-logout-switch",
		})
		if err != nil {
			t.Fatalf("SwitchTenant(before logout) error = %v", err)
		}
		clock.Advance(20 * time.Minute)
		logout := biz.LogoutSessionCommand{
			RefreshToken: boundaryB.RefreshToken, CSRFToken: "csrf-logout", Origin: "https://console.test.example",
			IdempotencyKey: "dp2-08-logout",
		}
		if _, err := usecase.LogoutSession(ctx, logout); err != nil {
			t.Fatalf("LogoutSession() error = %v", err)
		}
		logout.IdempotencyKey = "dp2-08-logout-repeat"
		if _, err := usecase.LogoutSession(ctx, logout); err != nil {
			t.Fatalf("LogoutSession(repeat) error = %v", err)
		}
		var currentStatus, otherStatus string
		if err := environment.runtimePool.QueryRow(ctx, `SELECT status FROM sessions WHERE id = $1`, current.Session.ID).Scan(&currentStatus); err != nil {
			t.Fatalf("query current logged-out Session: %v", err)
		}
		if err := environment.runtimePool.QueryRow(ctx, `SELECT status FROM sessions WHERE id = $1`, other.Session.ID).Scan(&otherStatus); err != nil {
			t.Fatalf("query independent Session: %v", err)
		}
		var activeCurrentGrants, activeCurrentFamilies, activeCurrentTokens, logoutAudits int
		if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM session_grants WHERE session_id = $1 AND status = 'active'`, current.Session.ID).Scan(&activeCurrentGrants); err != nil {
			t.Fatalf("query current grants: %v", err)
		}
		if err := environment.runtimePool.QueryRow(ctx, `
			SELECT count(*) FROM refresh_token_families AS family
			JOIN session_grants AS grant_row ON grant_row.tenant_id = family.tenant_id AND grant_row.id = family.grant_id
			WHERE grant_row.session_id = $1 AND family.status = 'active'
		`, current.Session.ID).Scan(&activeCurrentFamilies); err != nil {
			t.Fatalf("query current families: %v", err)
		}
		if err := environment.runtimePool.QueryRow(ctx, `
			SELECT count(*) FROM refresh_tokens AS token
			JOIN refresh_token_families AS family ON family.tenant_id = token.tenant_id AND family.id = token.family_id
			JOIN session_grants AS grant_row ON grant_row.tenant_id = family.tenant_id AND grant_row.id = family.grant_id
			WHERE grant_row.session_id = $1 AND token.status = 'active'
		`, current.Session.ID).Scan(&activeCurrentTokens); err != nil {
			t.Fatalf("query current refresh tokens: %v", err)
		}
		if err := environment.runtimePool.QueryRow(ctx, `
			SELECT count(*) FROM iam_audit_events WHERE action = 'iam.session.logged_out' AND target_id = $1
		`, current.Session.ID).Scan(&logoutAudits); err != nil {
			t.Fatalf("query logout Audits: %v", err)
		}
		if currentStatus != "revoked" || otherStatus != "active" || activeCurrentGrants != 0 ||
			activeCurrentFamilies != 0 || activeCurrentTokens != 0 || logoutAudits != 1 {
			t.Fatalf("logout state = current:%s other:%s active grant/family/token:%d/%d/%d audits:%d",
				currentStatus, otherStatus, activeCurrentGrants, activeCurrentFamilies, activeCurrentTokens, logoutAudits)
		}

		environment.runtimePool.Close()
		_, err = usecase.RefreshSession(ctx, biz.RefreshSessionCommand{
			RefreshToken: other.RefreshToken, CSRFToken: "csrf-postgres-down", Origin: "https://console.test.example",
			IdempotencyKey: "dp2-08-refresh-postgres-down",
		})
		if !errors.Is(err, biz.ErrAuthenticationDependency) || !errors.Is(err, biz.ErrPersistenceUnavailable) {
			t.Fatalf("RefreshSession(PostgreSQL down) error = %v", err)
		}
	})
}

func assertRefreshReplacement(t *testing.T, ctx context.Context, environment *postgresEnvironment, oldRaw, newRaw string, grantID uuid.UUID) {
	t.Helper()
	var oldStatus string
	var oldReplacedBy uuid.UUID
	var newID, familyID uuid.UUID
	var newStatus string
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT old.status, old.replaced_by, replacement.id, replacement.family_id, replacement.status
		FROM refresh_tokens AS old
		JOIN refresh_tokens AS replacement
		  ON replacement.tenant_id = old.tenant_id AND replacement.id = old.replaced_by
		JOIN refresh_token_families AS family
		  ON family.tenant_id = old.tenant_id AND family.id = old.family_id
		WHERE old.digest = $1 AND replacement.digest = $2 AND family.grant_id = $3
	`, refreshDigest(oldRaw), refreshDigest(newRaw), grantID).Scan(&oldStatus, &oldReplacedBy, &newID, &familyID, &newStatus); err != nil {
		t.Fatalf("query refresh replacement edge: %v", err)
	}
	if oldStatus != "consumed" || oldReplacedBy != newID || newStatus != "active" || familyID == uuid.Nil {
		t.Fatalf("refresh replacement = old:%s/%s new:%s/%s family:%s", oldStatus, oldReplacedBy, newStatus, newID, familyID)
	}
}

func refreshDigest(raw string) []byte {
	digest := sha256.Sum256([]byte(raw))
	return digest[:]
}

func mustTestAddr(t *testing.T, raw string) netip.Addr {
	t.Helper()
	address, err := netip.ParseAddr(raw)
	if err != nil {
		t.Fatalf("parse test address: %v", err)
	}
	return address
}

type mutableIntegrationClock struct {
	mu  sync.RWMutex
	now time.Time
}

func (c *mutableIntegrationClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

func (c *mutableIntegrationClock) Advance(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}
