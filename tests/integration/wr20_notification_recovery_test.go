//go:build integration

package integration_test

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jws"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	notificationv1 "github.com/zhangzhe-ctrl/ani-notification-service/api/notification/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestWR20StageCRecoveryAndSetup(t *testing.T) {
	n := newWR20Notification(t)
	e := n.e
	ctx := context.Background()
	sub := func(name string, run func(*testing.T)) {
		if !t.Run(name, run) {
			t.FailNow()
		}
	}
	request := func(t *testing.T, account, key string) string {
		code, body, _ := e.request(t, e.browser(t), "POST", "/auth/password-actions", "", map[string]any{"account": account, "audience": "console"}, map[string]string{"Idempotency-Key": key})
		id, _ := body["operation_id"].(string)
		if code != 202 || id == "" {
			t.Fatalf("password request HTTP %d", code)
		}
		return id
	}
	complete := func(t *testing.T, token, password, key string) int {
		code, _, _ := e.request(t, e.browser(t), "POST", "/auth/password-actions/complete", "", map[string]any{"token": token, "new_password": password}, map[string]string{"Idempotency-Key": key})
		return code
	}
	waitFor := func(t *testing.T, name string, duration time.Duration, check func() bool) {
		t.Helper()
		deadline := time.Now().Add(duration)
		for time.Now().Before(deadline) {
			if check() {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal(name)
	}
	receipt := func(t *testing.T, op string) string {
		var id string
		waitFor(t, "durable Notification receipt missing", 20*time.Second, func() bool {
			return e.owner.QueryRow(ctx, "SELECT notification_id FROM notification_outbox WHERE operation_id=$1 AND status='delivered'", op).Scan(&id) == nil && id != ""
		})
		return id
	}
	sub("SETUP_new_Human_has_no_invented_Session_or_Tenant_delegation", func(t *testing.T) {
		human, membership := mustV7(t), mustV7(t)
		account := "wr20-setup@example.test"
		if _, err := e.owner.Exec(ctx, `INSERT INTO principals(id,principal_type,status,version,created_at,updated_at) VALUES($1,'human','active',1,now(),now())`, human); err != nil {
			t.Fatal(err)
		}
		if _, err := e.owner.Exec(ctx, `INSERT INTO verified_emails(principal_id,normalized_email,verified_at,created_at,updated_at) VALUES($1,$2,now(),now(),now())`, human, account); err != nil {
			t.Fatal(err)
		}
		if _, err := e.owner.Exec(ctx, `INSERT INTO tenant_memberships(tenant_id,id,principal_id,status,version,created_at,updated_at) VALUES($1,$2,$3,'active',1,now(),now())`, referenceTenants[0], membership, human); err != nil {
			t.Fatal(err)
		}
		op := request(t, account, "wr20-setup")
		id := receipt(t, op)
		token := n.mailToken(t, account, id)
		var purpose string
		var sessions, credentials int
		if err := e.owner.QueryRow(ctx, `SELECT purpose,(SELECT count(*) FROM sessions WHERE principal_id=$2),(SELECT count(*) FROM password_credentials WHERE principal_id=$2) FROM password_actions WHERE operation_id=$1`, op, human).Scan(&purpose, &sessions, &credentials); err != nil || purpose != "setup" || sessions != 0 || credentials != 0 {
			t.Fatal("setup prerequest state was invented")
		}
		password := randomPassword(t)
		if code := complete(t, token, password, "wr20-setup-complete"); code != 204 {
			t.Fatalf("setup completion HTTP %d", code)
		}
		if err := e.owner.QueryRow(ctx, "SELECT count(*) FROM password_credentials WHERE principal_id=$1", human).Scan(&credentials); err != nil || credentials != 1 {
			t.Fatal("setup did not create exactly one credential")
		}
		code, _, _ := e.request(t, e.browser(t), "POST", "/auth/password/login", "", map[string]any{"account": account, "password": password, "audience": "console", "boundary": map[string]string{"type": "tenant", "tenant_id": referenceTenants[0].String()}}, nil)
		if code != 200 {
			t.Fatalf("setup password formal login HTTP %d", code)
		}
		recordReference(t, e.run, map[string]any{"stage": "C", "assertion": "SETUP_formal_action_and_login", "operation_id": op, "notification_id": id, "pass": true})
	})
	sub("Lost_response_durable_receipt_restart_duplicate_and_conflict", func(t *testing.T) {
		// The setup submission established the dispatcher's TLS connection.
		n.gate.drop.Store(true)
		defer n.gate.reset()
		op := request(t, e.accounts[1], "wr20-lost-response")
		var id string
		waitFor(t, "Notification did not durably accept while response was lost", 10*time.Second, func() bool {
			return n.pool.QueryRow(ctx, "SELECT notification_id FROM notifications WHERE producer_id='ani-iam' AND source_id=$1", op).Scan(&id) == nil
		})
		time.Sleep(1200 * time.Millisecond)
		var received bool
		if err := e.owner.QueryRow(ctx, "SELECT notification_id IS NOT NULL FROM notification_outbox WHERE operation_id=$1", op).Scan(&received); err != nil || received {
			t.Fatal("fault did not lose the receipt response")
		}
		n.stop()
		n.gate.reset()
		n.start(t)
		n.waitReady(t, true)
		if got := receipt(t, op); got != id {
			t.Fatal("restart changed durable receipt")
		}
		token := n.mailToken(t, e.accounts[1], id)
		var requestID string
		var created, expires time.Time
		if err := e.owner.QueryRow(ctx, `SELECT o.id,a.created_at,a.expires_at FROM notification_outbox o JOIN password_actions a ON a.operation_id=o.operation_id WHERE o.operation_id=$1`, op).Scan(&requestID, &created, &expires); err != nil {
			t.Fatal(err)
		}
		r := wr20NotificationRequest(e, requestID)
		r.Source.ResourceId = op
		r.CorrelationId = op
		r.OccurredAt = timestamppb.New(created)
		r.DeliverBefore = timestamppb.New(expires)
		r.Scope.GetHumanPrincipal().PrincipalId = e.humans[1].String()
		r.Recipient.GetHumanPrincipal().PrincipalId = e.humans[1].String()
		r.Destination.GetEmail().NormalizedEmail = e.accounts[1]
		r.GetIamPasswordAction().ActionUrl = e.origin + "/password-action?token=" + token
		r.GetIamPasswordAction().ActionExpiresAt = timestamppb.New(expires)
		call, cancel := wr20WorkloadContext(n.wat(t, biz.NotificationSubmitOperation, false))
		reply, err := n.client.SubmitNotification(call, r)
		cancel()
		if err != nil || reply.GetReceipt().GetNotificationId() != id || !reply.GetReceipt().GetReplayed() {
			t.Fatalf("durable submission replay code=%s", status.Code(err))
		}
		r.Destination.GetEmail().NormalizedEmail = "changed@example.test"
		call, cancel = wr20WorkloadContext(n.wat(t, biz.NotificationSubmitOperation, false))
		_, err = n.client.SubmitNotification(call, r)
		cancel()
		if status.Code(err) != codes.AlreadyExists {
			t.Fatalf("conflicting submission code=%s", status.Code(err))
		}
		var count int
		if err = n.pool.QueryRow(ctx, "SELECT count(*) FROM notifications WHERE producer_id='ani-iam' AND source_id=$1", op).Scan(&count); err != nil || count != 1 {
			t.Fatal("retry created duplicate business notification")
		}
		recordReference(t, e.run, map[string]any{"stage": "C", "assertion": "lost_response_restart_replayed_one_receipt", "operation_id": op, "notification_id": id, "pass": true})
	})
	sub("SMTP_failure_persistent_retry_and_process_restart", func(t *testing.T) {
		if err := n.sink.Stop(ctx, nil); err != nil {
			t.Fatal("stop task SMTP sink")
		}
		defer n.sink.Start(ctx)
		op := request(t, e.accounts[1], "wr20-smtp-retry")
		id := receipt(t, op)
		var state string
		waitFor(t, "SMTP failure did not persist a retry", 10*time.Second, func() bool {
			return n.pool.QueryRow(ctx, "SELECT state FROM deliveries WHERE notification_id=$1", id).Scan(&state) == nil && state == "retry_scheduled"
		})
		n.stop()
		if err := n.sink.Start(ctx); err != nil {
			t.Fatal("restart task SMTP sink")
		}
		// Docker can allocate different published ports when a container with
		// automatic bindings starts again. Re-read owned endpoints before the
		// receiver restarts; its persisted delivery/retry state stays intact.
		smtpPort, err := n.sink.MappedPort(ctx, "1025/tcp")
		if err != nil {
			t.Fatal("restarted SMTP endpoint")
		}
		apiPort, err := n.sink.MappedPort(ctx, "8025/tcp")
		if err != nil {
			t.Fatal("restarted sink API endpoint")
		}
		previous := n.smtpAddress
		n.smtpAddress = "127.0.0.1:" + smtpPort.Port()
		n.sinkAPI = "http://127.0.0.1:" + apiPort.Port()
		raw, err := os.ReadFile(n.configFile)
		if err != nil {
			t.Fatal("read isolated receiver config")
		}
		var config map[string]any
		if json.Unmarshal(raw, &config) != nil {
			t.Fatal("receiver config decode")
		}
		config["notification"].(map[string]any)["smtp"].(map[string]any)["address"] = n.smtpAddress
		raw, err = json.Marshal(config)
		if err != nil {
			t.Fatal("receiver config encode")
		}
		writeReferencePrivate(t, n.configFile, raw)
		recordReference(t, e.run, map[string]any{"stage": "C", "assertion": "sink_restart_endpoints", "smtp_before": previous, "smtp_after": n.smtpAddress, "sink_api": n.sinkAPI})
		n.start(t)
		n.waitReady(t, true)
		token := n.mailToken(t, e.accounts[1], id) // wait for the original bounded policy; no database time shortcut
		var attempts int
		if err := n.pool.QueryRow(ctx, `SELECT count(*) FROM delivery_attempts a JOIN deliveries d ON d.scope_kind=a.scope_kind AND d.scope_id=a.scope_id AND d.delivery_id=a.delivery_id WHERE d.notification_id=$1`, id).Scan(&attempts); err != nil || attempts < 2 {
			t.Fatal("SMTP retry attempt evidence missing")
		}
		if code := complete(t, token, randomPassword(t), "wr20-smtp-action-complete"); code != 204 {
			t.Fatalf("retried SMTP action completion HTTP %d", code)
		}
		recordReference(t, e.run, map[string]any{"stage": "C", "assertion": "SMTP_failure_restart_retry_action_completed", "operation_id": op, "notification_id": id, "attempts": attempts, "pass": true})
	})
	sub("Action_expiry_and_wrong_Principal_binding_have_no_mutation", func(t *testing.T) {
		op := request(t, e.accounts[0], "wr20-expiry")
		id := receipt(t, op)
		token := n.mailToken(t, e.accounts[0], id)
		expired := wr20ExpiredAction(t, n, token)
		if code := complete(t, expired, randomPassword(t), "wr20-expired-completion"); code != 401 {
			t.Fatalf("expired action HTTP %d", code)
		}
		if _, err := e.owner.Exec(ctx, "UPDATE password_actions SET principal_id=$2 WHERE operation_id=$1", op, e.humans[1]); err != nil {
			t.Fatal(err)
		}
		if code := complete(t, token, randomPassword(t), "wr20-wrong-owner"); code != 401 {
			t.Fatalf("wrong binding HTTP %d", code)
		}
		if _, err := e.owner.Exec(ctx, "UPDATE password_actions SET principal_id=$2 WHERE operation_id=$1", op, e.humans[0]); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := e.owner.QueryRow(ctx, "SELECT count(*) FROM password_action_completions WHERE operation_id=$1", op).Scan(&count); err != nil || count != 0 {
			t.Fatal("invalid action mutated credentials")
		}
		// A replaced action also loses validity while its old mail remains readable.
		replacement := request(t, e.accounts[0], "wr20-replacement")
		if replacement == op {
			t.Fatal("new request failed to replace action")
		}
		if code := complete(t, token, randomPassword(t), "wr20-replaced-consume"); code != 401 {
			t.Fatalf("replaced action HTTP %d", code)
		}
	})
	sub("Resolver_current_verification_Grant_and_IAM_outage_readiness", func(t *testing.T) {
		token := n.wat(t, biz.NotificationSubmitOperation, false)
		if _, err := e.owner.Exec(ctx, "UPDATE workload_grants SET status='revoked' WHERE principal_id=$1 AND audience='ani-iam' AND operation=$2", n.receiver.PrincipalID, biz.VerifyWorkloadCallerRPC); err != nil {
			t.Fatal(err)
		}
		call, cancel := wr20WorkloadContext(token)
		_, err := n.client.SubmitNotification(call, &notificationv1.SubmitNotificationRequest{})
		cancel()
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("receiver Grant missing code=%s", status.Code(err))
		}
		n.waitReady(t, false)
		if _, err = e.owner.Exec(ctx, "UPDATE workload_grants SET status='active' WHERE principal_id=$1 AND audience='ani-iam' AND operation=$2", n.receiver.PrincipalID, biz.VerifyWorkloadCallerRPC); err != nil {
			t.Fatal(err)
		}
		n.waitReady(t, true)
		if err = e.iam.process.Signal(syscall.SIGSTOP); err != nil {
			t.Fatal(err)
		}
		defer e.iam.process.Signal(syscall.SIGCONT)
		n.waitReady(t, false)
		call, cancel = wr20WorkloadContext(token)
		_, err = n.client.SubmitNotification(call, &notificationv1.SubmitNotificationRequest{})
		cancel()
		if status.Code(err) != codes.Unavailable && status.Code(err) != codes.DeadlineExceeded {
			t.Fatalf("IAM outage code=%s", status.Code(err))
		}
		_ = e.iam.process.Signal(syscall.SIGCONT)
		n.waitReady(t, true)
	})
}

func wr20ExpiredAction(t *testing.T, n *wr20Notification, raw string) string {
	t.Helper()
	token, err := jwt.Parse([]byte(raw), jwt.WithVerify(false), jwt.WithValidate(false))
	if err != nil {
		t.Fatal("negative action parse")
	}
	_ = token.Set(jwt.ExpirationKey, time.Now().Add(-time.Minute))
	bytes, err := os.ReadFile(n.e.iam.config.Runtime.AccessToken.PrivateKeyFile)
	if err != nil {
		t.Fatal("negative action key")
	}
	block, _ := pem.Decode(bytes)
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal("negative action key parse")
	}
	message, err := jws.Parse([]byte(raw))
	if err != nil || len(message.Signatures()) != 1 {
		t.Fatal("negative action headers")
	}
	signed, err := jwt.Sign(token, jwt.WithKey(jwa.EdDSA(), key.(ed25519.PrivateKey), jws.WithProtectedHeaders(message.Signatures()[0].ProtectedHeaders())))
	if err != nil {
		t.Fatal("negative action signing")
	}
	return string(signed)
}
