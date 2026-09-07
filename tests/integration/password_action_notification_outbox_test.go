//go:build integration

package integration_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

func TestPostgresPasswordActionNotificationOutboxLeaseRetryAndDeliveryCAS(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	principalID := uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e2a")
	seedPasswordActionPrincipal(t, ctx, environment, principalID, uuid.MustParse("0198f062-b76d-7201-9000-0000000002a1"), "dispatch@example.com", true)
	target := biz.PasswordActionTarget{PrincipalID: principalID, HasPassword: true, VerifiedEmail: "dispatch@example.com"}
	now := time.Date(2026, 9, 7, 4, 0, 0, 0, time.UTC)
	mutation := passwordActionRequestMutation(
		uuid.MustParse("0198f062-b76d-7001-9000-0000000002a1"),
		uuid.MustParse("0198f062-b76d-7001-9000-0000000002a2"),
		uuid.MustParse("0198f062-b76d-7001-9000-0000000002a3"),
		"dispatch@example.com",
		"password-action-dispatch-request",
		now,
		&target,
	)
	if _, err := data.NewPostgresLoginUnitOfWork(data.NewData(environment.runtimePool)).RequestPasswordAction(ctx, mutation); err != nil {
		t.Fatalf("RequestPasswordAction() error = %v", err)
	}
	outbox := data.NewPostgresPasswordActionNotificationOutbox(data.NewData(environment.runtimePool))

	claimed, found, err := outbox.ClaimPasswordActionNotification(ctx, now, 5*time.Minute)
	if err != nil || !found {
		t.Fatalf("ClaimPasswordActionNotification() = %#v, %t, %v", claimed, found, err)
	}
	if claimed.ID != mutation.Notification.ID || claimed.OperationID != mutation.OperationID ||
		claimed.PrincipalID != principalID || claimed.Purpose != biz.PasswordActionPurposeReset ||
		claimed.DestinationEmail != "dispatch@example.com" || !claimed.IssuedAt.Equal(now) ||
		!claimed.ExpiresAt.Equal(mutation.ExpiresAt) || claimed.AttemptCount != 1 || claimed.Version != 2 {
		t.Fatalf("claimed notification = %#v", claimed)
	}
	if _, found, err := outbox.ClaimPasswordActionNotification(ctx, now.Add(4*time.Minute), 5*time.Minute); err != nil || found {
		t.Fatalf("claim before lease expiry = found:%t err:%v", found, err)
	}
	reclaimed, found, err := outbox.ClaimPasswordActionNotification(ctx, now.Add(5*time.Minute), 5*time.Minute)
	if err != nil || !found || reclaimed.ID != claimed.ID || reclaimed.AttemptCount != 2 || reclaimed.Version != 3 {
		t.Fatalf("claim at lease expiry = %#v, %t, %v", reclaimed, found, err)
	}

	retryAt := now.Add(8 * time.Minute)
	if err := outbox.ReschedulePasswordActionNotification(ctx, reclaimed.ID, reclaimed.Version, retryAt, now.Add(5*time.Minute)); err != nil {
		t.Fatalf("ReschedulePasswordActionNotification() error = %v", err)
	}
	if _, found, err := outbox.ClaimPasswordActionNotification(ctx, retryAt.Add(-time.Second), 5*time.Minute); err != nil || found {
		t.Fatalf("claim before retry schedule = found:%t err:%v", found, err)
	}
	retried, found, err := outbox.ClaimPasswordActionNotification(ctx, retryAt, 5*time.Minute)
	if err != nil || !found || retried.AttemptCount != 3 || retried.Version != 5 {
		t.Fatalf("scheduled retry claim = %#v, %t, %v", retried, found, err)
	}
	if err := outbox.MarkPasswordActionNotificationDelivered(ctx, retried.ID, retried.Version-1, "submission-stale", retryAt); !errors.Is(err, biz.ErrVersionConflict) {
		t.Fatalf("stale delivery CAS error = %v, want %v", err, biz.ErrVersionConflict)
	}
	if err := outbox.MarkPasswordActionNotificationDelivered(ctx, retried.ID, retried.Version, "submission-2a", retryAt); err != nil {
		t.Fatalf("MarkPasswordActionNotificationDelivered() error = %v", err)
	}
	if _, found, err := outbox.ClaimPasswordActionNotification(ctx, retryAt.Add(time.Minute), 5*time.Minute); err != nil || found {
		t.Fatalf("claim delivered row = found:%t err:%v", found, err)
	}
	var status, notificationID string
	var attempts int
	if err := environment.runtimePool.QueryRow(ctx, `SELECT status, attempt_count, notification_id FROM notification_outbox WHERE id = $1`, mutation.Notification.ID).Scan(&status, &attempts, &notificationID); err != nil {
		t.Fatalf("query delivered outbox: %v", err)
	}
	if status != "delivered" || attempts != 3 || notificationID != "submission-2a" {
		t.Fatalf("delivered outbox = status:%s attempts:%d notification:%s", status, attempts, notificationID)
	}
}

func TestPostgresPasswordActionNotificationOutboxAttentionRequiredIsTerminal(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	principalID := uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e2c")
	seedPasswordActionPrincipal(t, ctx, environment, principalID, uuid.MustParse("0198f062-b76d-7201-9000-0000000002c1"), "attention@example.com", true)
	now := time.Date(2026, 9, 7, 4, 30, 0, 0, time.UTC)
	target := biz.PasswordActionTarget{PrincipalID: principalID, HasPassword: true, VerifiedEmail: "attention@example.com"}
	mutation := passwordActionRequestMutation(
		uuid.MustParse("0198f062-b76d-7001-9000-0000000002c1"),
		uuid.MustParse("0198f062-b76d-7001-9000-0000000002c2"),
		uuid.MustParse("0198f062-b76d-7001-9000-0000000002c3"),
		"attention@example.com",
		"password-action-attention-request",
		now,
		&target,
	)
	if _, err := data.NewPostgresLoginUnitOfWork(data.NewData(environment.runtimePool)).RequestPasswordAction(ctx, mutation); err != nil {
		t.Fatalf("RequestPasswordAction() error = %v", err)
	}
	outbox := data.NewPostgresPasswordActionNotificationOutbox(data.NewData(environment.runtimePool))
	claimed, found, err := outbox.ClaimPasswordActionNotification(ctx, now, 5*time.Minute)
	if err != nil || !found {
		t.Fatalf("ClaimPasswordActionNotification() = %#v, %t, %v", claimed, found, err)
	}
	if err := outbox.MarkPasswordActionNotificationAttentionRequired(ctx, claimed.ID, claimed.Version-1, now); !errors.Is(err, biz.ErrVersionConflict) {
		t.Fatalf("stale attention CAS error = %v, want %v", err, biz.ErrVersionConflict)
	}
	if err := outbox.MarkPasswordActionNotificationAttentionRequired(ctx, claimed.ID, claimed.Version, now); err != nil {
		t.Fatalf("MarkPasswordActionNotificationAttentionRequired() error = %v", err)
	}
	if _, found, err := outbox.ClaimPasswordActionNotification(ctx, now.Add(10*time.Minute), 5*time.Minute); err != nil || found {
		t.Fatalf("claim attention-required row = found:%t err:%v", found, err)
	}
	var status string
	if err := environment.runtimePool.QueryRow(ctx, `SELECT status FROM notification_outbox WHERE id = $1`, claimed.ID).Scan(&status); err != nil {
		t.Fatalf("query attention-required outbox: %v", err)
	}
	if status != "attention_required" {
		t.Fatalf("outbox status = %q, want attention_required", status)
	}
}

func TestPostgresPasswordActionNotificationOutboxConcurrentClaimHasOneWinner(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	principalID := uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e2b")
	seedPasswordActionPrincipal(t, ctx, environment, principalID, uuid.MustParse("0198f062-b76d-7201-9000-0000000002b1"), "claim-race@example.com", true)
	now := time.Date(2026, 9, 7, 4, 15, 0, 0, time.UTC)
	target := biz.PasswordActionTarget{PrincipalID: principalID, HasPassword: true, VerifiedEmail: "claim-race@example.com"}
	mutation := passwordActionRequestMutation(
		uuid.MustParse("0198f062-b76d-7001-9000-0000000002b1"),
		uuid.MustParse("0198f062-b76d-7001-9000-0000000002b2"),
		uuid.MustParse("0198f062-b76d-7001-9000-0000000002b3"),
		"claim-race@example.com",
		"password-action-claim-race-request",
		now,
		&target,
	)
	if _, err := data.NewPostgresLoginUnitOfWork(data.NewData(environment.runtimePool)).RequestPasswordAction(ctx, mutation); err != nil {
		t.Fatalf("RequestPasswordAction() error = %v", err)
	}

	type claimResult struct {
		found bool
		err   error
	}
	start := make(chan struct{})
	results := make(chan claimResult, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for range 2 {
		go func() {
			ready.Done()
			<-start
			_, found, err := data.NewPostgresPasswordActionNotificationOutbox(data.NewData(environment.runtimePool)).ClaimPasswordActionNotification(ctx, now, 5*time.Minute)
			results <- claimResult{found: found, err: err}
		}()
	}
	ready.Wait()
	close(start)
	winners := 0
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent claim error = %v", result.err)
		}
		if result.found {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent claim winners = %d, want 1", winners)
	}
}
