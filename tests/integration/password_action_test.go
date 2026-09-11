//go:build integration

package integration_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

func TestPostgresPasswordActionRequestIsNonEnumeratingAndReplacesPriorIntent(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	principalID := uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17")
	seedPasswordActionPrincipal(t, ctx, environment, principalID, uuid.MustParse("0198f062-b76d-7201-9000-000000000071"), "user@example.com", true)

	reader := data.NewPostgresPasswordLoginReader(passwordActionTestData(t, environment.runtimePool))
	target, found, err := reader.LookupPasswordActionTarget(ctx, "user@example.com", biz.AudienceConsole)
	if err != nil || !found || target.PrincipalID != principalID || !target.HasPassword || target.VerifiedEmail != "user@example.com" {
		t.Fatalf("LookupPasswordActionTarget() = %#v, %t, %v", target, found, err)
	}
	unknown, found, err := reader.LookupPasswordActionTarget(ctx, "unknown@example.com", biz.AudienceConsole)
	if err != nil || found || unknown != (biz.PasswordActionTarget{}) {
		t.Fatalf("LookupPasswordActionTarget(unknown) = %#v, %t, %v", unknown, found, err)
	}

	uow := data.NewPostgresLoginUnitOfWork(passwordActionTestData(t, environment.runtimePool))
	now := time.Date(2026, 9, 6, 13, 0, 0, 0, time.UTC)
	first := passwordActionRequestMutation(
		uuid.MustParse("0198f062-b76d-7001-9000-000000000071"),
		uuid.MustParse("0198f062-b76d-7001-9000-000000000072"),
		uuid.MustParse("0198f062-b76d-7001-9000-000000000073"),
		"user@example.com",
		"password-action-request-first",
		now,
		&target,
	)
	result, err := uow.RequestPasswordAction(ctx, first)
	if err != nil {
		t.Fatalf("RequestPasswordAction(first) error = %v", err)
	}
	if result.OperationID != first.OperationID || !result.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("RequestPasswordAction(first) result = %#v", result)
	}

	unknownMutation := passwordActionRequestMutation(
		uuid.MustParse("0198f062-b76d-7001-9000-000000000074"),
		uuid.MustParse("0198f062-b76d-7001-9000-000000000075"),
		uuid.MustParse("0198f062-b76d-7001-9000-000000000076"),
		"unknown@example.com",
		"password-action-request-unknown",
		now,
		nil,
	)
	unknownMutation.Purpose = ""
	unknownMutation.Notification = nil
	unknownResult, err := uow.RequestPasswordAction(ctx, unknownMutation)
	if err != nil {
		t.Fatalf("RequestPasswordAction(unknown) error = %v", err)
	}
	if unknownResult.OperationID != unknownMutation.OperationID || !unknownResult.ExpiresAt.Equal(unknownMutation.ExpiresAt) {
		t.Fatalf("RequestPasswordAction(unknown) result = %#v", unknownResult)
	}

	replacement := passwordActionRequestMutation(
		uuid.MustParse("0198f062-b76d-7001-9000-000000000077"),
		uuid.MustParse("0198f062-b76d-7001-9000-000000000078"),
		uuid.MustParse("0198f062-b76d-7001-9000-000000000079"),
		"user@example.com",
		"password-action-request-replacement",
		now.Add(time.Minute),
		&target,
	)
	if _, err := uow.RequestPasswordAction(ctx, replacement); err != nil {
		t.Fatalf("RequestPasswordAction(replacement) error = %v", err)
	}

	var firstStatus, firstOutboxStatus, firstDestination, replacementStatus, replacementOutboxStatus string
	if err := environment.runtimePool.QueryRow(ctx, `SELECT status FROM password_actions WHERE operation_id = $1`, first.OperationID).Scan(&firstStatus); err != nil {
		t.Fatalf("query first action status: %v", err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT status FROM notification_outbox WHERE operation_id = $1`, first.OperationID).Scan(&firstOutboxStatus); err != nil {
		t.Fatalf("query first outbox status: %v", err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT CASE WHEN destination_ciphertext IS NULL AND destination_key_version IS NULL THEN 'scrubbed' ELSE 'retained' END FROM notification_outbox WHERE operation_id = $1`, first.OperationID).Scan(&firstDestination); err != nil {
		t.Fatalf("query first outbox destination: %v", err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT status FROM password_actions WHERE operation_id = $1`, replacement.OperationID).Scan(&replacementStatus); err != nil {
		t.Fatalf("query replacement action status: %v", err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT status FROM notification_outbox WHERE operation_id = $1`, replacement.OperationID).Scan(&replacementOutboxStatus); err != nil {
		t.Fatalf("query replacement outbox status: %v", err)
	}
	if firstStatus != "replaced" || firstOutboxStatus != "cancelled" || firstDestination != "scrubbed" ||
		replacementStatus != "active" || replacementOutboxStatus != "pending" {
		t.Fatalf("replacement states = first:%s/%s/%s replacement:%s/%s", firstStatus, firstOutboxStatus, firstDestination, replacementStatus, replacementOutboxStatus)
	}

	retry := replacement
	retry.OperationID = uuid.MustParse("0198f062-b76d-7001-9000-00000000007a")
	retry.Notification.ID = uuid.MustParse("0198f062-b76d-7001-9000-00000000007b")
	retry.Audit.ID = uuid.MustParse("0198f062-b76d-7001-9000-00000000007c")
	retryResult, err := uow.RequestPasswordAction(ctx, retry)
	if err != nil {
		t.Fatalf("RequestPasswordAction(idempotent retry) error = %v", err)
	}
	if retryResult.OperationID != replacement.OperationID || !retryResult.ExpiresAt.Equal(replacement.ExpiresAt) {
		t.Fatalf("idempotent retry result = %#v, want replacement identity", retryResult)
	}

	conflict := retry
	conflict.AccountDigest = sha256.Sum256([]byte("different@example.com"))
	if _, err := uow.RequestPasswordAction(ctx, conflict); !errors.Is(err, biz.ErrIdempotencyConflict) {
		t.Fatalf("RequestPasswordAction(idempotency conflict) error = %v, want %v", err, biz.ErrIdempotencyConflict)
	}

	var requestCount, actionCount, outboxCount, auditCount int
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM password_action_requests`).Scan(&requestCount); err != nil {
		t.Fatalf("count password action requests: %v", err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM password_actions`).Scan(&actionCount); err != nil {
		t.Fatalf("count password actions: %v", err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM notification_outbox`).Scan(&outboxCount); err != nil {
		t.Fatalf("count notification outbox: %v", err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM iam_audit_events WHERE boundary = 'principal'`).Scan(&auditCount); err != nil {
		t.Fatalf("count principal password-action audits: %v", err)
	}
	if requestCount != 3 || actionCount != 2 || outboxCount != 2 || auditCount != 3 {
		t.Fatalf("durable counts = request:%d action:%d outbox:%d audit:%d, want 3/2/2/3", requestCount, actionCount, outboxCount, auditCount)
	}
	var unknownAuditJSON string
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT to_jsonb(iam_audit_events)::text
		FROM iam_audit_events
		WHERE event_id = $1
	`, unknownMutation.Audit.ID).Scan(&unknownAuditJSON); err != nil {
		t.Fatalf("query unknown-account Audit: %v", err)
	}
	if strings.Contains(unknownAuditJSON, "unknown@example.com") || strings.Contains(unknownAuditJSON, fmt.Sprintf("%x", unknownMutation.AccountDigest)) {
		t.Fatalf("unknown-account Audit leaked account material: %s", unknownAuditJSON)
	}
}

func TestPostgresPasswordActionRequestConcurrentIdempotentReplayConverges(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	principalID := uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e1b")
	seedPasswordActionPrincipal(t, ctx, environment, principalID, uuid.MustParse("0198f062-b76d-7201-9000-0000000000b1"), "request-replay@example.com", true)
	target := biz.PasswordActionTarget{PrincipalID: principalID, HasPassword: true, VerifiedEmail: "request-replay@example.com"}
	now := time.Date(2026, 9, 6, 16, 30, 0, 0, time.UTC)
	const attempts = 8
	type requestResult struct {
		result biz.RequestPasswordActionResult
		err    error
	}
	results := make(chan requestResult, attempts)
	start := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(attempts)
	for index := 0; index < attempts; index++ {
		index := index
		go func() {
			ready.Done()
			<-start
			mutation := passwordActionRequestMutation(
				uuid.MustParse(fmt.Sprintf("0198f062-b76d-7001-9000-%012x", 0xb10+index*3)),
				uuid.MustParse(fmt.Sprintf("0198f062-b76d-7001-9000-%012x", 0xb11+index*3)),
				uuid.MustParse(fmt.Sprintf("0198f062-b76d-7001-9000-%012x", 0xb12+index*3)),
				"request-replay@example.com",
				"password-action-concurrent-request-replay",
				now,
				&target,
			)
			result, err := data.NewPostgresLoginUnitOfWork(passwordActionTestData(t, environment.runtimePool)).RequestPasswordAction(ctx, mutation)
			results <- requestResult{result: result, err: err}
		}()
	}
	ready.Wait()
	close(start)

	var canonical biz.RequestPasswordActionResult
	for index := 0; index < attempts; index++ {
		got := <-results
		if got.err != nil {
			t.Fatalf("concurrent idempotent RequestPasswordAction() result %d error = %v", index, got.err)
		}
		if canonical.OperationID == uuid.Nil {
			canonical = got.result
		} else if got.result != canonical {
			t.Fatalf("concurrent idempotent request result %d = %#v, want %#v", index, got.result, canonical)
		}
	}
	var requestCount, actionCount, outboxCount, auditCount int
	for table, destination := range map[string]*int{
		"password_action_requests": &requestCount,
		"password_actions":         &actionCount,
		"notification_outbox":      &outboxCount,
	} {
		if err := environment.runtimePool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(destination); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM iam_audit_events WHERE action = 'iam.password.action.requested'`).Scan(&auditCount); err != nil {
		t.Fatalf("count concurrent request Audits: %v", err)
	}
	if requestCount != 1 || actionCount != 1 || outboxCount != 1 || auditCount != 1 {
		t.Fatalf("concurrent request durable rows = request:%d action:%d outbox:%d audit:%d, want 1/1/1/1", requestCount, actionCount, outboxCount, auditCount)
	}
}

func TestPostgresPasswordActionRequestRejectsMismatchedDestinationAtomically(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	principalID := uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e1d")
	seedPasswordActionPrincipal(t, ctx, environment, principalID, uuid.MustParse("0198f062-b76d-7201-9000-0000000000d1"), "verified@example.com", true)
	target := biz.PasswordActionTarget{PrincipalID: principalID, HasPassword: true, VerifiedEmail: "verified@example.com"}
	now := time.Date(2026, 9, 6, 17, 30, 0, 0, time.UTC)
	mutation := passwordActionRequestMutation(
		uuid.MustParse("0198f062-b76d-7001-9000-0000000000d1"),
		uuid.MustParse("0198f062-b76d-7001-9000-0000000000d2"),
		uuid.MustParse("0198f062-b76d-7001-9000-0000000000d3"),
		"verified@example.com",
		"password-action-mismatched-destination",
		now,
		&target,
	)
	mutation.Notification.DestinationEmail = "different@example.com"
	if _, err := data.NewPostgresLoginUnitOfWork(passwordActionTestData(t, environment.runtimePool)).RequestPasswordAction(ctx, mutation); !errors.Is(err, biz.ErrInvalidPersistenceState) {
		t.Fatalf("RequestPasswordAction(mismatched destination) error = %v, want %v", err, biz.ErrInvalidPersistenceState)
	}
	for _, table := range []string{"password_action_requests", "password_actions", "notification_outbox"} {
		var count int
		if err := environment.runtimePool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s rows = %d after rejected destination, want 0", table, count)
		}
	}
	var auditCount int
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM iam_audit_events WHERE action = 'iam.password.action.requested'`).Scan(&auditCount); err != nil {
		t.Fatalf("count rejected destination Audits: %v", err)
	}
	if auditCount != 0 {
		t.Fatalf("request Audits = %d after rejected destination, want 0", auditCount)
	}
}

func TestPostgresPasswordResetConsumesOnceAndRevokesOnlyTargetPrincipal(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	principalID := uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17")
	otherPrincipalID := uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e18")
	now := time.Date(2026, 9, 6, 14, 0, 0, 0, time.UTC)
	seedPasswordActionPrincipal(t, ctx, environment, principalID, uuid.MustParse("0198f062-b76d-7201-9000-000000000081"), "user@example.com", true)
	seedPasswordActionPrincipal(t, ctx, environment, otherPrincipalID, uuid.MustParse("0198f062-b76d-7201-9000-000000000082"), "other@example.com", true)
	seedPasswordActionSessions(t, ctx, environment, principalID, otherPrincipalID, now)

	uow := data.NewPostgresLoginUnitOfWork(passwordActionTestData(t, environment.runtimePool))
	target := biz.PasswordActionTarget{PrincipalID: principalID, HasPassword: true, VerifiedEmail: "user@example.com"}
	request := passwordActionRequestMutation(
		uuid.MustParse("0198f062-b76d-7001-9000-000000000081"),
		uuid.MustParse("0198f062-b76d-7001-9000-000000000082"),
		uuid.MustParse("0198f062-b76d-7001-9000-000000000083"),
		"user@example.com",
		"password-action-reset-request",
		now,
		&target,
	)
	if _, err := uow.RequestPasswordAction(ctx, request); err != nil {
		t.Fatalf("RequestPasswordAction(reset) error = %v", err)
	}
	completion := biz.PasswordActionCompletion{
		Claims: biz.PasswordActionTokenClaims{
			Issuer:      "ani-iam",
			PrincipalID: principalID,
			OperationID: request.OperationID,
			Purpose:     biz.PasswordActionPurposeReset,
			IssuedAt:    now.Add(time.Minute),
			ExpiresAt:   request.ExpiresAt,
		},
		IdentityID:     uuid.MustParse("0198f062-b76d-7201-9000-000000000083"),
		PasswordHash:   "test-replacement-phc",
		IdempotencyKey: "password-action-reset-complete",
		CompletedAt:    now.Add(2 * time.Minute),
		Audit: biz.SecurityAuditEvent{
			ID:                   uuid.MustParse("0198f062-b76d-7001-9000-000000000084"),
			ActorID:              principalID,
			AuthenticationMethod: biz.AuditAuthenticationMethodAction,
			Boundary:             biz.AuditBoundaryPrincipal,
			Action:               biz.AuditActionPasswordActionCompleted,
			TargetType:           biz.AuditTargetTypePasswordAction,
			TargetID:             request.OperationID,
			TargetVersion:        2,
			Result:               biz.AuditResultSucceeded,
			Reason:               biz.AuditReasonPasswordReset,
			RequestID:            "password-action-reset-complete",
			CorrelationID:        "password-action-reset-complete",
			DecisionID:           "0198f062-b76d-7001-9000-000000000084",
			SourceService:        biz.AuditSourceServiceIAM,
			OccurredAt:           now.Add(2 * time.Minute),
			RecordedAt:           now.Add(2 * time.Minute),
		},
	}
	rollbackAttempt := completion
	rollbackAttempt.Audit.ID = request.Audit.ID
	rollbackAttempt.Audit.DecisionID = request.Audit.ID.String()
	if _, err := uow.CompletePasswordAction(ctx, rollbackAttempt); !errors.Is(err, biz.ErrAuditConflict) {
		t.Fatalf("CompletePasswordAction(forced audit rollback) error = %v, want %v", err, biz.ErrAuditConflict)
	}
	var rolledBackHash, rolledBackActionStatus string
	var rolledBackCredentialVersion, rolledBackCompletionCount int
	if err := environment.runtimePool.QueryRow(ctx, `SELECT password_hash, version FROM password_credentials WHERE principal_id = $1`, principalID).Scan(&rolledBackHash, &rolledBackCredentialVersion); err != nil {
		t.Fatalf("query credential after forced rollback: %v", err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT status FROM password_actions WHERE operation_id = $1`, request.OperationID).Scan(&rolledBackActionStatus); err != nil {
		t.Fatalf("query action after forced rollback: %v", err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM password_action_completions WHERE operation_id = $1`, request.OperationID).Scan(&rolledBackCompletionCount); err != nil {
		t.Fatalf("query completion after forced rollback: %v", err)
	}
	if rolledBackHash != "test-existing-phc" || rolledBackCredentialVersion != 1 || rolledBackActionStatus != "active" || rolledBackCompletionCount != 0 {
		t.Fatalf("forced rollback state = hash:%q credential-version:%d action:%s completions:%d", rolledBackHash, rolledBackCredentialVersion, rolledBackActionStatus, rolledBackCompletionCount)
	}
	assertPasswordActionSessionStates(t, ctx, environment, principalID, "active", otherPrincipalID, "active")

	result, err := uow.CompletePasswordAction(ctx, completion)
	if err != nil {
		t.Fatalf("CompletePasswordAction(reset) error = %v", err)
	}
	if result.PrincipalID != principalID || result.CredentialVersion != 2 {
		t.Fatalf("CompletePasswordAction(reset) result = %#v", result)
	}

	var passwordHash, actionStatus, outboxStatus string
	var credentialVersion int64
	var failedAttempts int
	var lockedUntil *time.Time
	if err := environment.runtimePool.QueryRow(ctx, `SELECT password_hash, version, failed_attempts, locked_until FROM password_credentials WHERE principal_id = $1`, principalID).Scan(&passwordHash, &credentialVersion, &failedAttempts, &lockedUntil); err != nil {
		t.Fatalf("query reset credential: %v", err)
	}
	if passwordHash != completion.PasswordHash || credentialVersion != 2 || failedAttempts != 0 || lockedUntil != nil {
		t.Fatalf("reset credential = hash:%q version:%d failures:%d locked:%v", passwordHash, credentialVersion, failedAttempts, lockedUntil)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT status FROM password_actions WHERE operation_id = $1`, request.OperationID).Scan(&actionStatus); err != nil {
		t.Fatalf("query consumed action: %v", err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT status FROM notification_outbox WHERE operation_id = $1`, request.OperationID).Scan(&outboxStatus); err != nil {
		t.Fatalf("query completed action outbox: %v", err)
	}
	if actionStatus != "consumed" || outboxStatus != "cancelled" {
		t.Fatalf("completion states = action:%s outbox:%s", actionStatus, outboxStatus)
	}
	assertPasswordActionSessionStates(t, ctx, environment, principalID, "revoked", otherPrincipalID, "active")

	retry, err := uow.CompletePasswordAction(ctx, completion)
	if err != nil || retry != result {
		t.Fatalf("CompletePasswordAction(idempotent retry) = %#v, %v, want %#v", retry, err, result)
	}
	conflict := completion
	conflict.IdempotencyKey = "password-action-reset-complete-other-key"
	conflict.Audit.ID = uuid.MustParse("0198f062-b76d-7001-9000-000000000085")
	if _, err := uow.CompletePasswordAction(ctx, conflict); !errors.Is(err, biz.ErrPasswordActionInvalid) {
		t.Fatalf("CompletePasswordAction(replay) error = %v, want %v", err, biz.ErrPasswordActionInvalid)
	}
}

func TestPostgresPasswordSetupCreatesIdentityAndCredentialOnce(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	principalID := uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e19")
	seedPasswordActionPrincipal(t, ctx, environment, principalID, uuid.Nil, "setup@example.com", false)
	reader := data.NewPostgresPasswordLoginReader(passwordActionTestData(t, environment.runtimePool))
	target, found, err := reader.LookupPasswordActionTarget(ctx, "setup@example.com", biz.AudienceConsole)
	if err != nil || !found || target.PrincipalID != principalID || target.HasPassword {
		t.Fatalf("LookupPasswordActionTarget(setup) = %#v, %t, %v", target, found, err)
	}

	uow := data.NewPostgresLoginUnitOfWork(passwordActionTestData(t, environment.runtimePool))
	now := time.Date(2026, 9, 6, 15, 0, 0, 0, time.UTC)
	request := passwordActionRequestMutation(
		uuid.MustParse("0198f062-b76d-7001-9000-000000000091"),
		uuid.MustParse("0198f062-b76d-7001-9000-000000000092"),
		uuid.MustParse("0198f062-b76d-7001-9000-000000000093"),
		"setup@example.com",
		"password-action-setup-request",
		now,
		&target,
	)
	request.Purpose = biz.PasswordActionPurposeSetup
	request.Notification.Purpose = biz.PasswordActionPurposeSetup
	if _, err := uow.RequestPasswordAction(ctx, request); err != nil {
		t.Fatalf("RequestPasswordAction(setup) error = %v", err)
	}
	identityID := uuid.MustParse("0198f062-b76d-7201-9000-000000000091")
	completion := biz.PasswordActionCompletion{
		Claims: biz.PasswordActionTokenClaims{
			Issuer:      "ani-iam",
			PrincipalID: principalID,
			OperationID: request.OperationID,
			Purpose:     biz.PasswordActionPurposeSetup,
			IssuedAt:    now.Add(time.Minute),
			ExpiresAt:   request.ExpiresAt,
		},
		IdentityID:     identityID,
		PasswordHash:   "test-setup-phc",
		IdempotencyKey: "password-action-setup-complete",
		CompletedAt:    now.Add(2 * time.Minute),
		Audit: biz.SecurityAuditEvent{
			ID:                   uuid.MustParse("0198f062-b76d-7001-9000-000000000094"),
			ActorID:              principalID,
			AuthenticationMethod: biz.AuditAuthenticationMethodAction,
			Boundary:             biz.AuditBoundaryPrincipal,
			Action:               biz.AuditActionPasswordActionCompleted,
			TargetType:           biz.AuditTargetTypePasswordAction,
			TargetID:             request.OperationID,
			TargetVersion:        2,
			Result:               biz.AuditResultSucceeded,
			Reason:               biz.AuditReasonPasswordSetup,
			RequestID:            "password-action-setup-complete",
			CorrelationID:        "password-action-setup-complete",
			DecisionID:           "0198f062-b76d-7001-9000-000000000094",
			SourceService:        biz.AuditSourceServiceIAM,
			OccurredAt:           now.Add(2 * time.Minute),
			RecordedAt:           now.Add(2 * time.Minute),
		},
	}
	result, err := uow.CompletePasswordAction(ctx, completion)
	if err != nil {
		t.Fatalf("CompletePasswordAction(setup) error = %v", err)
	}
	if result.PrincipalID != principalID || result.CredentialVersion != 1 {
		t.Fatalf("CompletePasswordAction(setup) result = %#v", result)
	}
	var gotIdentityID uuid.UUID
	var subject, passwordHash string
	var credentialVersion int64
	if err := environment.runtimePool.QueryRow(ctx, `SELECT identity.id, identity.subject, credential.password_hash, credential.version FROM identities AS identity JOIN password_credentials AS credential ON credential.identity_id = identity.id WHERE identity.principal_id = $1`, principalID).Scan(&gotIdentityID, &subject, &passwordHash, &credentialVersion); err != nil {
		t.Fatalf("query setup identity/credential: %v", err)
	}
	if gotIdentityID != identityID || subject != "setup@example.com" || passwordHash != completion.PasswordHash || credentialVersion != 1 {
		t.Fatalf("setup identity/credential = id:%s subject:%q hash:%q version:%d", gotIdentityID, subject, passwordHash, credentialVersion)
	}
	var intent string
	if err := environment.runtimePool.QueryRow(ctx, `SELECT intent FROM notification_outbox WHERE operation_id = $1`, request.OperationID).Scan(&intent); err != nil {
		t.Fatalf("query setup notification intent: %v", err)
	}
	if intent != "password_setup" {
		t.Fatalf("setup notification intent = %q, want password_setup", intent)
	}

	expiredTarget := biz.PasswordActionTarget{PrincipalID: principalID, HasPassword: true, VerifiedEmail: "setup@example.com"}
	expiredRequest := passwordActionRequestMutation(
		uuid.MustParse("0198f062-b76d-7001-9000-000000000095"),
		uuid.MustParse("0198f062-b76d-7001-9000-000000000096"),
		uuid.MustParse("0198f062-b76d-7001-9000-000000000097"),
		"setup@example.com",
		"password-action-expired-request",
		now.Add(3*time.Minute),
		&expiredTarget,
	)
	if _, err := uow.RequestPasswordAction(ctx, expiredRequest); err != nil {
		t.Fatalf("RequestPasswordAction(expiry) error = %v", err)
	}
	expiredCompletion := completion
	expiredCompletion.Claims.OperationID = expiredRequest.OperationID
	expiredCompletion.Claims.Purpose = biz.PasswordActionPurposeReset
	expiredCompletion.Claims.IssuedAt = expiredRequest.CreatedAt
	expiredCompletion.Claims.ExpiresAt = expiredRequest.ExpiresAt
	expiredCompletion.IdentityID = uuid.MustParse("0198f062-b76d-7201-9000-000000000098")
	expiredCompletion.PasswordHash = "must-not-commit-expired-phc"
	expiredCompletion.IdempotencyKey = "password-action-expired-complete"
	expiredCompletion.CompletedAt = expiredRequest.ExpiresAt
	expiredCompletion.Audit.ID = uuid.MustParse("0198f062-b76d-7001-9000-000000000098")
	expiredCompletion.Audit.TargetID = expiredRequest.OperationID
	expiredCompletion.Audit.Reason = biz.AuditReasonPasswordReset
	expiredCompletion.Audit.RequestID = expiredCompletion.IdempotencyKey
	expiredCompletion.Audit.CorrelationID = expiredCompletion.IdempotencyKey
	expiredCompletion.Audit.DecisionID = expiredCompletion.Audit.ID.String()
	expiredCompletion.Audit.OccurredAt = expiredCompletion.CompletedAt
	expiredCompletion.Audit.RecordedAt = expiredCompletion.CompletedAt
	if _, err := uow.CompletePasswordAction(ctx, expiredCompletion); !errors.Is(err, biz.ErrPasswordActionInvalid) {
		t.Fatalf("CompletePasswordAction(expired) error = %v, want %v", err, biz.ErrPasswordActionInvalid)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT password_hash, version FROM password_credentials WHERE principal_id = $1`, principalID).Scan(&passwordHash, &credentialVersion); err != nil {
		t.Fatalf("query credential after expired completion: %v", err)
	}
	if passwordHash != completion.PasswordHash || credentialVersion != 1 {
		t.Fatalf("expired completion changed credential = hash:%q version:%d", passwordHash, credentialVersion)
	}
}

func TestPostgresPasswordActionCompletionHasOneConcurrentWinner(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	principalID := uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e1a")
	seedPasswordActionPrincipal(t, ctx, environment, principalID, uuid.MustParse("0198f062-b76d-7201-9000-0000000000a1"), "race@example.com", true)
	uow := data.NewPostgresLoginUnitOfWork(passwordActionTestData(t, environment.runtimePool))
	now := time.Date(2026, 9, 6, 16, 0, 0, 0, time.UTC)
	target := biz.PasswordActionTarget{PrincipalID: principalID, HasPassword: true, VerifiedEmail: "race@example.com"}
	request := passwordActionRequestMutation(
		uuid.MustParse("0198f062-b76d-7001-9000-0000000000a1"),
		uuid.MustParse("0198f062-b76d-7001-9000-0000000000a2"),
		uuid.MustParse("0198f062-b76d-7001-9000-0000000000a3"),
		"race@example.com",
		"password-action-race-request",
		now,
		&target,
	)
	if _, err := uow.RequestPasswordAction(ctx, request); err != nil {
		t.Fatalf("RequestPasswordAction(race) error = %v", err)
	}
	base := biz.PasswordActionCompletion{
		Claims: biz.PasswordActionTokenClaims{
			Issuer:      "ani-iam",
			PrincipalID: principalID,
			OperationID: request.OperationID,
			Purpose:     biz.PasswordActionPurposeReset,
			IssuedAt:    now.Add(time.Minute),
			ExpiresAt:   request.ExpiresAt,
		},
		PasswordHash: "test-race-phc",
		CompletedAt:  now.Add(2 * time.Minute),
		Audit: biz.SecurityAuditEvent{
			ActorID:              principalID,
			AuthenticationMethod: biz.AuditAuthenticationMethodAction,
			Boundary:             biz.AuditBoundaryPrincipal,
			Action:               biz.AuditActionPasswordActionCompleted,
			TargetType:           biz.AuditTargetTypePasswordAction,
			TargetID:             request.OperationID,
			TargetVersion:        2,
			Result:               biz.AuditResultSucceeded,
			Reason:               biz.AuditReasonPasswordReset,
			SourceService:        biz.AuditSourceServiceIAM,
			OccurredAt:           now.Add(2 * time.Minute),
			RecordedAt:           now.Add(2 * time.Minute),
		},
	}
	completions := []biz.PasswordActionCompletion{base, base}
	completions[0].IdentityID = uuid.MustParse("0198f062-b76d-7201-9000-0000000000a4")
	completions[0].IdempotencyKey = "password-action-race-a"
	completions[0].Audit.ID = uuid.MustParse("0198f062-b76d-7001-9000-0000000000a4")
	completions[0].Audit.RequestID = completions[0].IdempotencyKey
	completions[0].Audit.CorrelationID = completions[0].IdempotencyKey
	completions[0].Audit.DecisionID = completions[0].Audit.ID.String()
	completions[1].IdentityID = uuid.MustParse("0198f062-b76d-7201-9000-0000000000a5")
	completions[1].IdempotencyKey = "password-action-race-b"
	completions[1].Audit.ID = uuid.MustParse("0198f062-b76d-7001-9000-0000000000a5")
	completions[1].Audit.RequestID = completions[1].IdempotencyKey
	completions[1].Audit.CorrelationID = completions[1].IdempotencyKey
	completions[1].Audit.DecisionID = completions[1].Audit.ID.String()

	start := make(chan struct{})
	results := make(chan error, len(completions))
	var ready sync.WaitGroup
	ready.Add(len(completions))
	for _, completion := range completions {
		completion := completion
		go func() {
			ready.Done()
			<-start
			_, err := uow.CompletePasswordAction(ctx, completion)
			results <- err
		}()
	}
	ready.Wait()
	close(start)
	var successes, invalid int
	for range completions {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, biz.ErrPasswordActionInvalid):
			invalid++
		default:
			t.Fatalf("concurrent CompletePasswordAction() error = %v", err)
		}
	}
	if successes != 1 || invalid != 1 {
		t.Fatalf("concurrent completion results = %d success/%d invalid, want 1/1", successes, invalid)
	}
	var completionCount, completionAuditCount int
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM password_action_completions WHERE operation_id = $1`, request.OperationID).Scan(&completionCount); err != nil {
		t.Fatalf("count concurrent completion rows: %v", err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM iam_audit_events WHERE action = 'iam.password.action.completed' AND target_id = $1`, request.OperationID).Scan(&completionAuditCount); err != nil {
		t.Fatalf("count concurrent completion audits: %v", err)
	}
	if completionCount != 1 || completionAuditCount != 1 {
		t.Fatalf("concurrent durable rows = completion:%d audit:%d, want 1/1", completionCount, completionAuditCount)
	}
}

func TestPostgresPasswordActionCompletionConcurrentIdempotentReplayConverges(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	principalID := uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e1c")
	seedPasswordActionPrincipal(t, ctx, environment, principalID, uuid.MustParse("0198f062-b76d-7201-9000-0000000000c1"), "complete-replay@example.com", true)
	uow := data.NewPostgresLoginUnitOfWork(passwordActionTestData(t, environment.runtimePool))
	now := time.Date(2026, 9, 6, 17, 0, 0, 0, time.UTC)
	target := biz.PasswordActionTarget{PrincipalID: principalID, HasPassword: true, VerifiedEmail: "complete-replay@example.com"}
	request := passwordActionRequestMutation(
		uuid.MustParse("0198f062-b76d-7001-9000-0000000000c1"),
		uuid.MustParse("0198f062-b76d-7001-9000-0000000000c2"),
		uuid.MustParse("0198f062-b76d-7001-9000-0000000000c3"),
		"complete-replay@example.com",
		"password-action-concurrent-complete-request",
		now,
		&target,
	)
	if _, err := uow.RequestPasswordAction(ctx, request); err != nil {
		t.Fatalf("RequestPasswordAction(concurrent completion replay) error = %v", err)
	}
	const attempts = 8
	type completionResult struct {
		result biz.CompletePasswordActionResult
		err    error
	}
	results := make(chan completionResult, attempts)
	start := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(attempts)
	for index := 0; index < attempts; index++ {
		index := index
		go func() {
			ready.Done()
			<-start
			auditID := uuid.MustParse(fmt.Sprintf("0198f062-b76d-7001-9000-%012x", 0xc10+index))
			result, err := data.NewPostgresLoginUnitOfWork(passwordActionTestData(t, environment.runtimePool)).CompletePasswordAction(ctx, biz.PasswordActionCompletion{
				Claims: biz.PasswordActionTokenClaims{
					Issuer:      "ani-iam",
					PrincipalID: principalID,
					OperationID: request.OperationID,
					Purpose:     biz.PasswordActionPurposeReset,
					IssuedAt:    now,
					ExpiresAt:   request.ExpiresAt,
				},
				PasswordHash:   "test-concurrent-replay-phc",
				IdempotencyKey: "password-action-concurrent-complete-replay",
				CompletedAt:    now.Add(time.Minute),
				Audit: biz.SecurityAuditEvent{
					ID:                   auditID,
					ActorID:              principalID,
					AuthenticationMethod: biz.AuditAuthenticationMethodAction,
					Boundary:             biz.AuditBoundaryPrincipal,
					Action:               biz.AuditActionPasswordActionCompleted,
					TargetType:           biz.AuditTargetTypePasswordAction,
					TargetID:             request.OperationID,
					TargetVersion:        2,
					Result:               biz.AuditResultSucceeded,
					Reason:               biz.AuditReasonPasswordReset,
					RequestID:            "password-action-concurrent-complete-replay",
					CorrelationID:        "password-action-concurrent-complete-replay",
					DecisionID:           auditID.String(),
					SourceService:        biz.AuditSourceServiceIAM,
					OccurredAt:           now.Add(time.Minute),
					RecordedAt:           now.Add(time.Minute),
				},
			})
			results <- completionResult{result: result, err: err}
		}()
	}
	ready.Wait()
	close(start)

	want := biz.CompletePasswordActionResult{PrincipalID: principalID, CredentialVersion: 2}
	for index := 0; index < attempts; index++ {
		got := <-results
		if got.err != nil || got.result != want {
			t.Fatalf("concurrent idempotent CompletePasswordAction() result %d = %#v, %v, want %#v", index, got.result, got.err, want)
		}
	}
	var completionCount, auditCount int
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM password_action_completions WHERE operation_id = $1`, request.OperationID).Scan(&completionCount); err != nil {
		t.Fatalf("count concurrent completion replay rows: %v", err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM iam_audit_events WHERE action = 'iam.password.action.completed' AND target_id = $1`, request.OperationID).Scan(&auditCount); err != nil {
		t.Fatalf("count concurrent completion replay Audits: %v", err)
	}
	if completionCount != 1 || auditCount != 1 {
		t.Fatalf("concurrent completion replay durable rows = completion:%d audit:%d, want 1/1", completionCount, auditCount)
	}
}

func passwordActionRequestMutation(operationID, outboxID, auditID uuid.UUID, account, idempotencyKey string, now time.Time, target *biz.PasswordActionTarget) biz.PasswordActionRequestMutation {
	mutation := biz.PasswordActionRequestMutation{
		OperationID:    operationID,
		AccountDigest:  sha256.Sum256([]byte(account)),
		Audience:       biz.AudienceConsole,
		Target:         target,
		Purpose:        biz.PasswordActionPurposeReset,
		ExpiresAt:      now.Add(30 * time.Minute),
		CreatedAt:      now,
		IdempotencyKey: idempotencyKey,
		Audit: &biz.SecurityAuditEvent{
			ID:                   auditID,
			ActorID:              uuid.Nil,
			AuthenticationMethod: biz.AuditAuthenticationMethodAnonymous,
			Boundary:             biz.AuditBoundaryPrincipal,
			Action:               biz.AuditActionPasswordActionRequested,
			TargetType:           biz.AuditTargetTypePasswordAction,
			TargetID:             operationID,
			TargetVersion:        1,
			Result:               biz.AuditResultSucceeded,
			Reason:               biz.AuditReasonPasswordActionRequested,
			RequestID:            idempotencyKey,
			CorrelationID:        idempotencyKey,
			DecisionID:           auditID.String(),
			SourceService:        biz.AuditSourceServiceIAM,
			OccurredAt:           now,
			RecordedAt:           now,
		},
	}
	if target != nil {
		mutation.Notification = &biz.PasswordActionNotification{
			ID:               outboxID,
			OperationID:      operationID,
			PrincipalID:      target.PrincipalID,
			Purpose:          biz.PasswordActionPurposeReset,
			DestinationEmail: account,
		}
	}
	return mutation
}

func seedPasswordActionPrincipal(t *testing.T, ctx context.Context, environment *postgresEnvironment, principalID, identityID uuid.UUID, account string, withCredential bool) {
	t.Helper()
	seedPool := mustPool(t, environment.migrationDSN(primaryDB))
	defer seedPool.Close()
	if _, err := seedPool.Exec(ctx, `INSERT INTO principals (id, principal_type, status, version, created_at, updated_at) VALUES ($1, 'human', 'active', 1, now(), now())`, principalID); err != nil {
		t.Fatalf("seed password-action principal: %v", err)
	}
	if _, err := seedPool.Exec(ctx, `INSERT INTO verified_emails (principal_id, normalized_email, verified_at, created_at, updated_at) VALUES ($1, $2, now(), now(), now())`, principalID, account); err != nil {
		t.Fatalf("seed password-action verified email: %v", err)
	}
	if !withCredential {
		return
	}
	if _, err := seedPool.Exec(ctx, `INSERT INTO identities (id, principal_id, provider, issuer, subject, status, version, created_at, updated_at) VALUES ($1, $2, 'password', 'ani-local', $3, 'active', 1, now(), now())`, identityID, principalID, account); err != nil {
		t.Fatalf("seed password-action identity: %v", err)
	}
	if _, err := seedPool.Exec(ctx, `INSERT INTO password_credentials (principal_id, identity_id, password_hash, algorithm, version, created_at, updated_at) VALUES ($1, $2, 'test-existing-phc', 'argon2id', 1, now(), now())`, principalID, identityID); err != nil {
		t.Fatalf("seed password-action credential: %v", err)
	}
}

func seedPasswordActionSessions(t *testing.T, ctx context.Context, environment *postgresEnvironment, principalID, otherPrincipalID uuid.UUID, now time.Time) {
	t.Helper()
	seedPool := mustPool(t, environment.migrationDSN(primaryDB))
	defer seedPool.Close()
	if _, err := seedPool.Exec(ctx, `INSERT INTO tenant_access (tenant_id, status, version, created_at, updated_at) VALUES ($1, 'active', 1, now(), now())`, tenantA); err != nil {
		t.Fatalf("seed password-action Tenant Access: %v", err)
	}
	membershipA := uuid.MustParse("0198f062-b76d-704f-ac04-ab231ee78f81")
	membershipB := uuid.MustParse("0198f062-b76d-704f-ac04-ab231ee78f82")
	for _, row := range []struct {
		id          uuid.UUID
		principalID uuid.UUID
	}{{membershipA, principalID}, {membershipB, otherPrincipalID}} {
		if _, err := seedPool.Exec(ctx, `INSERT INTO tenant_memberships (tenant_id, id, principal_id, status, version, created_at, updated_at) VALUES ($1, $2, $3, 'active', 1, now(), now())`, tenantA, row.id, row.principalID); err != nil {
			t.Fatalf("seed password-action membership: %v", err)
		}
	}
	type sessionFixture struct {
		sessionID    uuid.UUID
		grantID      uuid.UUID
		familyID     uuid.UUID
		tokenID      uuid.UUID
		principalID  uuid.UUID
		membershipID uuid.UUID
		digestSeed   string
	}
	fixtures := []sessionFixture{
		{uuid.MustParse("0198f062-b76d-7101-9000-000000000081"), uuid.MustParse("0198f062-b76d-7101-9000-000000000082"), uuid.MustParse("0198f062-b76d-7101-9000-000000000083"), uuid.MustParse("0198f062-b76d-7101-9000-000000000084"), principalID, membershipA, "target-one"},
		{uuid.MustParse("0198f062-b76d-7101-9000-000000000085"), uuid.MustParse("0198f062-b76d-7101-9000-000000000086"), uuid.MustParse("0198f062-b76d-7101-9000-000000000087"), uuid.MustParse("0198f062-b76d-7101-9000-000000000088"), principalID, membershipA, "target-two"},
		{uuid.MustParse("0198f062-b76d-7101-9000-000000000089"), uuid.MustParse("0198f062-b76d-7101-9000-00000000008a"), uuid.MustParse("0198f062-b76d-7101-9000-00000000008b"), uuid.MustParse("0198f062-b76d-7101-9000-00000000008c"), otherPrincipalID, membershipB, "other-one"},
	}
	for _, fixture := range fixtures {
		if _, err := seedPool.Exec(ctx, `INSERT INTO sessions (id, principal_id, audience, status, authn_methods, device_name, idle_expires_at, absolute_expires_at, reauthenticated_at, version, created_at, updated_at) VALUES ($1, $2, 'console', 'active', ARRAY['password']::text[], 'browser', $3::timestamptz + interval '7 days', $3::timestamptz + interval '30 days', $3, 1, $3, $3)`, fixture.sessionID, fixture.principalID, now); err != nil {
			t.Fatalf("seed password-action session: %v", err)
		}
		if _, err := seedPool.Exec(ctx, `INSERT INTO session_grants (tenant_id, id, session_id, membership_id, status, version, created_at, updated_at) VALUES ($1, $2, $3, $4, 'active', 1, now(), now())`, tenantA, fixture.grantID, fixture.sessionID, fixture.membershipID); err != nil {
			t.Fatalf("seed password-action grant: %v", err)
		}
		if _, err := seedPool.Exec(ctx, `INSERT INTO refresh_token_families (tenant_id, id, grant_id, status, version, created_at, updated_at) VALUES ($1, $2, $3, 'active', 1, now(), now())`, tenantA, fixture.familyID, fixture.grantID); err != nil {
			t.Fatalf("seed password-action family: %v", err)
		}
		digest := sha256.Sum256([]byte(fixture.digestSeed))
		if _, err := seedPool.Exec(ctx, `INSERT INTO refresh_tokens (tenant_id, id, family_id, digest, status, issued_at, expires_at) VALUES ($1, $2, $3, $4, 'active', now(), now() + interval '30 days')`, tenantA, fixture.tokenID, fixture.familyID, digest[:]); err != nil {
			t.Fatalf("seed password-action refresh token: %v", err)
		}
	}
}

func assertPasswordActionSessionStates(t *testing.T, ctx context.Context, environment *postgresEnvironment, principalID uuid.UUID, wantStatus string, otherPrincipalID uuid.UUID, wantOtherStatus string) {
	t.Helper()
	for _, check := range []struct {
		principalID uuid.UUID
		wantStatus  string
		wantCount   int
	}{{principalID, wantStatus, 2}, {otherPrincipalID, wantOtherStatus, 1}} {
		for _, table := range []string{"sessions", "session_grants", "refresh_token_families", "refresh_tokens"} {
			var count int
			query := `SELECT count(*) FROM sessions WHERE principal_id = $1 AND status = $2`
			switch table {
			case "session_grants":
				query = `SELECT count(*) FROM session_grants AS grant_row JOIN sessions AS session_row ON session_row.id = grant_row.session_id WHERE session_row.principal_id = $1 AND grant_row.status = $2`
			case "refresh_token_families":
				query = `SELECT count(*) FROM refresh_token_families AS family JOIN session_grants AS grant_row ON grant_row.tenant_id = family.tenant_id AND grant_row.id = family.grant_id JOIN sessions AS session_row ON session_row.id = grant_row.session_id WHERE session_row.principal_id = $1 AND family.status = $2`
			case "refresh_tokens":
				query = `SELECT count(*) FROM refresh_tokens AS token JOIN refresh_token_families AS family ON family.tenant_id = token.tenant_id AND family.id = token.family_id JOIN session_grants AS grant_row ON grant_row.tenant_id = family.tenant_id AND grant_row.id = family.grant_id JOIN sessions AS session_row ON session_row.id = grant_row.session_id WHERE session_row.principal_id = $1 AND token.status = $2`
			}
			if err := environment.runtimePool.QueryRow(ctx, query, check.principalID, check.wantStatus).Scan(&count); err != nil {
				t.Fatalf("query %s status for %s: %v", table, check.principalID, err)
			}
			if count != check.wantCount {
				t.Fatalf("%s status %q count for %s = %d, want %d", table, check.wantStatus, check.principalID, count, check.wantCount)
			}
		}
	}
}

func passwordActionTestData(t *testing.T, pool *pgxpool.Pool) *data.Data {
	t.Helper()
	p, err := data.NewOutboxProtector("test", map[string][]byte{"test": []byte("0123456789abcdef0123456789abcdef")})
	if err != nil {
		t.Fatal(err)
	}
	return data.NewData(pool, p)
}
