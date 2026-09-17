//go:build integration

package integration_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"
)

type wr22IdentitySubmitter struct {
	send func(context.Context, biz.IdentityNotificationSubmission) (string, error)
}

func (s wr22IdentitySubmitter) SubmitIdentityNotification(ctx context.Context, v biz.IdentityNotificationSubmission) (string, error) {
	return s.send(ctx, v)
}

type wr22LostIdentityReceipt struct {
	biz.IdentityNotificationOutbox
	lose bool
}

func (o *wr22LostIdentityReceipt) FinishIdentityNotification(ctx context.Context, c biz.IdentityNotificationClaim, s biz.IdentityNotificationOutcome, r string, next, now time.Time) error {
	if o.lose && s == biz.IdentityNotificationDelivered {
		o.lose = false
		return errors.New("injected local receipt persistence outage")
	}
	return o.IdentityNotificationOutbox.FinishIdentityNotification(ctx, c, s, r, next, now)
}

// HTTP creates the durable intents using the formal IAM/Gateway processes.
// The dispatcher below uses the real runtime PostgreSQL role and protected
// payloads, but the in-memory receiver is explicitly NOT Notification/SMTP.
func TestWR22IdentityNotificationOutbox(t *testing.T) {
	b := newWR22BossEnvironment(t)
	e := b.wr22Environment
	ctx := context.Background()
	admin, adminToken, _, _ := e.login(t, 0)
	b.registerIntent(t, b.observedIdentity(t))
	boss := b.browser(t)
	location, state := b.beginOIDC(t, boss, "")
	code, returned := b.dexAuthorize(t, location)
	if state != returned {
		t.Fatal("BOSS state prerequisite")
	}
	if status, _, _ := b.callback(t, boss, code, state); status != 303 {
		t.Fatal("BOSS administrator prerequisite")
	}
	status, body, _ := b.request(t, boss, "POST", "/auth/refresh", "", nil, nil)
	if status != 200 {
		t.Fatal("BOSS session prerequisite")
	}
	bossToken := body["access_token"].(string)
	call := func(t *testing.T, platform bool, client *http.Client, token, method, path string, payload any, want int) map[string]any {
		t.Helper()
		headers := map[string]string{"Idempotency-Key": uuid.NewString()}
		var status int
		var body map[string]any
		if platform {
			status, body, _ = b.request(t, client, method, path, token, payload, headers)
		} else {
			status, body, _ = e.request(t, client, method, path, token, payload, headers)
		}
		if status != want {
			t.Fatalf("delivery prerequisite status=%d reason=%v want=%d", status, body["code"], want)
		}
		return body
	}
	platformRole := call(t, true, boss, bossToken, "POST", "/iam/platform/roles", map[string]any{"name": "Delivery test invite reader", "permissions": []string{"iam.platform-memberships/read"}}, 201)["role_id"].(string)
	runtime := mustPool(t, e.iam.config.Runtime.Postgresql.Dsn)
	defer runtime.Close()
	protector, err := data.LoadOutboxProtector(e.iam.config.Runtime.Notification.OutboxKeyFile)
	if err != nil {
		t.Fatal("own delivery protector prerequisite")
	}
	outbox, err := data.NewIdentityNotificationOutbox(data.NewData(runtime, protector), "en-US")
	if err != nil {
		t.Fatal("runtime delivery outbox prerequisite")
	}
	tenant := referenceTenants[0]
	type source struct {
		id    uuid.UUID
		email string
		path  string
		kind  biz.IdentityNotificationKind
	}
	create := func(t *testing.T, kind biz.IdentityNotificationKind) source {
		t.Helper()
		email := "wr22-delivery-" + uuid.NewString() + "@example.test"
		s := source{kind: kind, email: email}
		if kind == biz.IdentityNotificationPlatformInvitation {
			s.path = "/iam/platform/invitations"
			r := call(t, true, boss, bossToken, "POST", s.path, map[string]any{"email": email, "role_ids": []string{platformRole}, "locale": "zh-CN"}, 201)
			s.id = uuid.MustParse(r["invitation_id"].(string))
		} else {
			s.path = "/iam/tenants/" + tenant.String() + "/invitations"
			r := call(t, false, admin, adminToken, "POST", s.path, map[string]any{"email": email, "role_ids": []string{e.adminRoles[0].String()}}, 201)
			s.id = uuid.MustParse(r["invitation_id"].(string))
			if kind == biz.IdentityNotificationEmailVerification {
				r = call(t, false, e.browser(t), "", "POST", "/auth/invited-account-verifications", map[string]string{"account": email}, 202)
				s.id = uuid.MustParse(r["challenge_id"].(string))
			}
		}
		return s
	}
	rowState := func(t *testing.T, c biz.IdentityNotificationClaim) (string, int, bool) {
		t.Helper()
		var status string
		var attempts int
		var cleared bool
		var err error
		switch c.Kind {
		case biz.IdentityNotificationTenantInvitation:
			err = runtime.QueryRow(ctx, `SELECT status,attempt_count,payload_ciphertext IS NULL FROM tenant_invitation_outbox WHERE tenant_id=$1 AND id=$2`, c.TenantID, c.ID).Scan(&status, &attempts, &cleared)
		case biz.IdentityNotificationPlatformInvitation:
			err = runtime.QueryRow(ctx, `SELECT status,attempt_count,payload_ciphertext IS NULL FROM platform_invitation_outbox WHERE id=$1`, c.ID).Scan(&status, &attempts, &cleared)
		case biz.IdentityNotificationEmailVerification:
			err = runtime.QueryRow(ctx, `SELECT status,attempt_count,payload_ciphertext IS NULL FROM iam_invited_account_outbox WHERE id=$1`, c.ID).Scan(&status, &attempts, &cleared)
		}
		if err != nil {
			t.Fatal("delivery state read failed")
		}
		return status, attempts, cleared
	}
	claim := func(t *testing.T, kind biz.IdentityNotificationKind, now time.Time) biz.IdentityNotificationClaim {
		t.Helper()
		c, found, err := outbox.ClaimIdentityNotification(ctx, kind, now, 5*time.Minute)
		if err != nil || !found || !c.PayloadValid {
			t.Fatal("protected delivery claim failed")
		}
		return c
	}
	for _, kind := range []biz.IdentityNotificationKind{biz.IdentityNotificationTenantInvitation, biz.IdentityNotificationPlatformInvitation, biz.IdentityNotificationEmailVerification} {
		t.Run(string(kind), func(t *testing.T) {
			t.Run("competing_claims_exact_scope_and_terminal_receipt", func(t *testing.T) {
				s := create(t, kind)
				now := time.Now().UTC()
				claims := make(chan biz.IdentityNotificationClaim, 4)
				failures := make(chan error, 4)
				var wg sync.WaitGroup
				for i := 0; i < 4; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						c, ok, err := outbox.ClaimIdentityNotification(ctx, kind, now, 5*time.Minute)
						if err != nil {
							failures <- err
						} else if ok {
							claims <- c
						}
					}()
				}
				wg.Wait()
				close(claims)
				close(failures)
				if len(failures) != 0 || len(claims) != 1 {
					t.Fatal("competing claimers did not converge on one lease")
				}
				c := <-claims
				if c.SourceID != s.id || c.Email != s.email || !c.PayloadValid || c.AttemptCount != 1 || c.Version != 2 {
					t.Fatal("claim lost source or protected identity")
				}
				current, err := outbox.CurrentIdentityNotification(ctx, c, now)
				if err != nil || !current {
					t.Fatal("current claim not recognized")
				}
				foreign := c
				foreign.TenantID = referenceTenants[1]
				current, err = outbox.CurrentIdentityNotification(ctx, foreign, now)
				if kind == biz.IdentityNotificationTenantInvitation {
					if err != nil || current {
						t.Fatal("foreign Tenant read crossed boundary")
					}
					if err = outbox.FinishIdentityNotification(ctx, foreign, biz.IdentityNotificationCancelled, "", now, now); !errors.Is(err, biz.ErrIdentityNotificationSuperseded) {
						t.Fatal("foreign Tenant transition crossed boundary")
					}
				} else if err == nil {
					t.Fatal("global delivery accepted a Tenant context")
				}
				receipt := mustV7(t).String()
				if outbox.FinishIdentityNotification(ctx, c, biz.IdentityNotificationDelivered, receipt, now, now) != nil {
					t.Fatal("receipt persistence failed")
				}
				status, attempts, cleared := rowState(t, c)
				if status != "delivered" || attempts != 1 || !cleared {
					t.Fatal("delivered state retained protected payload")
				}
				if _, found, err := outbox.ClaimIdentityNotification(ctx, kind, now.Add(6*time.Minute), 5*time.Minute); err != nil || found {
					t.Fatal("terminal receipt reclaimed")
				}
				if err = outbox.FinishIdentityNotification(ctx, c, biz.IdentityNotificationRetry, "", now.Add(time.Second), now); !errors.Is(err, biz.ErrIdentityNotificationSuperseded) {
					t.Fatal("old receipt resurrected terminal row")
				}
			})
			if t.Failed() {
				return
			}
			t.Run("lost_receipt_retries_same_request_after_lease", func(t *testing.T) {
				s := create(t, kind)
				clock := &wr22InvitationClock{now: time.Now().UTC()}
				lost := &wr22LostIdentityReceipt{IdentityNotificationOutbox: outbox, lose: true}
				var sent []biz.IdentityNotificationSubmission
				receipt := mustV7(t).String()
				dispatcher, _ := biz.NewIdentityNotificationDispatcher(lost, wr22IdentitySubmitter{send: func(_ context.Context, v biz.IdentityNotificationSubmission) (string, error) {
					sent = append(sent, v)
					return receipt, nil
				}}, clock)
				if worked, err := dispatcher.DispatchNext(ctx, kind); !worked || err == nil {
					t.Fatal("receipt loss was not surfaced")
				}
				if worked, err := dispatcher.DispatchNext(ctx, kind); worked || err != nil {
					t.Fatal("live lease was reclaimed")
				}
				// Synthetic clock is a component lease prerequisite, not elapsed-time evidence.
				clock.now = clock.now.Add(6 * time.Minute)
				if worked, err := dispatcher.DispatchNext(ctx, kind); !worked || err != nil {
					t.Fatal("expired lease failed to recover")
				}
				if len(sent) != 2 || sent[0].SourceID != s.id || !reflect.DeepEqual(sent[0], sent[1]) {
					t.Fatal("ambiguous submission retry changed request identity or secret")
				}
				c := biz.IdentityNotificationClaim{Kind: kind, ID: sent[0].RequestID, TenantID: sent[0].TenantID}
				status, attempts, cleared := rowState(t, c)
				if status != "delivered" || attempts != 2 || !cleared {
					t.Fatal("lease recovery lost durable receipt")
				}
			})
			if t.Failed() {
				return
			}
			t.Run("resend_or_new_challenge_wins_inflight_receipt", func(t *testing.T) {
				s := create(t, kind)
				clock := &wr22InvitationClock{now: time.Now().UTC()}
				entered := make(chan biz.IdentityNotificationSubmission, 1)
				release := make(chan struct{})
				var releaseOnce sync.Once
				releaseSubmitter := func() { releaseOnce.Do(func() { close(release) }) }
				defer releaseSubmitter()
				done := make(chan error, 1)
				dispatcher, _ := biz.NewIdentityNotificationDispatcher(outbox, wr22IdentitySubmitter{send: func(_ context.Context, v biz.IdentityNotificationSubmission) (string, error) {
					entered <- v
					<-release
					return mustV7(t).String(), nil
				}}, clock)
				go func() { _, err := dispatcher.DispatchNext(ctx, kind); done <- err }()
				var old biz.IdentityNotificationSubmission
				select {
				case old = <-entered:
				case <-time.After(10 * time.Second):
					releaseSubmitter()
					t.Fatal("submission did not reach in-flight barrier")
				}
				if kind == biz.IdentityNotificationEmailVerification {
					call(t, false, e.browser(t), "", "POST", "/auth/invited-account-verifications", map[string]string{"account": s.email}, 202)
				} else {
					call(t, kind == biz.IdentityNotificationPlatformInvitation, map[bool]*http.Client{true: boss, false: admin}[kind == biz.IdentityNotificationPlatformInvitation], map[bool]string{true: bossToken, false: adminToken}[kind == biz.IdentityNotificationPlatformInvitation], "POST", s.path+"/"+s.id.String()+"/resend", map[string]any{"expected_version": 1}, 200)
				}
				releaseSubmitter()
				if err := <-done; err != nil {
					t.Fatal("superseded in-flight receipt should be benign")
				}
				c := biz.IdentityNotificationClaim{Kind: kind, ID: old.RequestID, TenantID: old.TenantID}
				status, _, cleared := rowState(t, c)
				if status != "cancelled" || !cleared {
					t.Fatal("old receipt overrode supersession")
				}
				next := claim(t, kind, time.Now().UTC())
				if next.ID == old.RequestID || (kind != biz.IdentityNotificationEmailVerification && next.Secret == old.Secret) || next.Email != old.Email {
					t.Fatal("replacement delivery did not rotate capability")
				}
				now := time.Now().UTC()
				if outbox.FinishIdentityNotification(ctx, next, biz.IdentityNotificationCancelled, "", now, now) != nil {
					t.Fatal("finish own replacement delivery")
				}
			})
		})
		if t.Failed() {
			return
		}
	}
	t.Run("retry_budget_attention_and_ciphertext_authentication", func(t *testing.T) {
		clock := &wr22InvitationClock{now: time.Now().UTC()}
		sent := 0
		dispatcher, _ := biz.NewIdentityNotificationDispatcher(outbox, wr22IdentitySubmitter{send: func(context.Context, biz.IdentityNotificationSubmission) (string, error) {
			sent++
			return "", biz.ErrIdentityNotificationRetryable
		}}, clock)
		// Codes above generated additional Tenant Invitation deliveries. Drain these
		// distinct eligible intents without modifying their Invitation authority.
		for {
			c, found, err := outbox.ClaimIdentityNotification(ctx, biz.IdentityNotificationTenantInvitation, clock.now, 5*time.Minute)
			if err != nil {
				t.Fatal("drain own delivery prerequisites")
			}
			if !found {
				break
			}
			if outbox.FinishIdentityNotification(ctx, c, biz.IdentityNotificationCancelled, "", clock.now, clock.now) != nil {
				t.Fatal("close own prerequisite delivery")
			}
		}
		s := create(t, biz.IdentityNotificationTenantInvitation)
		clock.now = time.Now().UTC()
		for i := 1; i <= 20; i++ {
			worked, err := dispatcher.DispatchNext(ctx, biz.IdentityNotificationTenantInvitation)
			want := biz.ErrIdentityNotificationRetryable
			if i == 20 {
				want = biz.ErrIdentityNotificationPermanent
			}
			if !worked || !errors.Is(err, want) {
				t.Fatal("durable retry budget classification")
			}
			clock.now = clock.now.Add(5 * time.Minute)
		}
		if sent != 20 {
			t.Fatal("retry budget sent unexpected count")
		}
		var status string
		var attempts int
		if runtime.QueryRow(ctx, `SELECT status,attempt_count FROM tenant_invitation_outbox WHERE tenant_id=$1 AND invitation_id=$2`, tenant, s.id).Scan(&status, &attempts) != nil || status != "attention_required" || attempts != 20 {
			t.Fatal("retry exhaustion did not persist attention")
		}
		damaged := create(t, biz.IdentityNotificationTenantInvitation)
		if _, err := e.owner.Exec(ctx, `UPDATE tenant_invitation_outbox SET payload_ciphertext=set_byte(payload_ciphertext,octet_length(payload_ciphertext)-1,get_byte(payload_ciphertext,octet_length(payload_ciphertext)-1)#1),version=version+1 WHERE tenant_id=$1 AND invitation_id=$2`, tenant, damaged.id); err != nil {
			t.Fatal("own ciphertext fault prerequisite")
		}
		clock.now = time.Now().UTC()
		if worked, err := dispatcher.DispatchNext(ctx, biz.IdentityNotificationTenantInvitation); !worked || !errors.Is(err, biz.ErrIdentityNotificationPermanent) || sent != 20 {
			t.Fatal("unauthenticated ciphertext reached submitter")
		}
	})
	t.Run("expired_code_never_submitted", func(t *testing.T) {
		create(t, biz.IdentityNotificationEmailVerification)
		clock := &wr22InvitationClock{now: time.Now().UTC().Add(11 * time.Minute)}
		sent := false
		dispatcher, _ := biz.NewIdentityNotificationDispatcher(outbox, wr22IdentitySubmitter{send: func(context.Context, biz.IdentityNotificationSubmission) (string, error) {
			sent = true
			return "", fmt.Errorf("must not send")
		}}, clock)
		if worked, err := dispatcher.DispatchNext(ctx, biz.IdentityNotificationEmailVerification); !worked || err != nil || sent {
			t.Fatal("expired code reached submitter")
		}
	})
}
