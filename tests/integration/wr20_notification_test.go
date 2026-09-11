//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jws"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	notificationv1 "github.com/zhangzhe-ctrl/ani-notification-service/api/notification/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestWR20StageCFormalNotificationPasswordAction(t *testing.T) {
	n := newWR20Notification(t)
	e := n.e
	ctx := context.Background()
	sub := func(name string, run func(*testing.T)) {
		if !t.Run(name, run) {
			t.FailNow()
		}
	}
	request := func(t *testing.T, account, key string) (string, int) {
		code, body, response := e.request(t, e.browser(t), "POST", "/auth/password-actions", "", map[string]any{"account": account, "audience": "console"}, map[string]string{"Idempotency-Key": key})
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("action response is cacheable")
		}
		id, _ := body["operation_id"].(string)
		return id, code
	}
	complete := func(t *testing.T, token, password, key string) int {
		code, _, _ := e.request(t, e.browser(t), "POST", "/auth/password-actions/complete", "", map[string]any{"token": token, "new_password": password}, map[string]string{"Idempotency-Key": key})
		return code
	}
	receipt := func(t *testing.T, operation string) string {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			var id string
			err := e.owner.QueryRow(ctx, "SELECT notification_id FROM notification_outbox WHERE operation_id=$1 AND status='delivered'", operation).Scan(&id)
			if err == nil && id != "" {
				var accepted bool
				err = n.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM notifications WHERE notification_id=$1 AND source_id=$2 AND producer_id='ani-iam' AND scope_kind='human_principal')", id, operation).Scan(&accepted)
				if err != nil || !accepted {
					t.Fatal("IAM receipt has no Notification durable row")
				}
				recordReference(t, e.run, map[string]any{"stage": "C", "assertion": "Notification_durable_receipt", "operation_id": operation, "notification_id": id, "pass": true})
				return id
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("no durable receipt after retry")
		return ""
	}
	var resetOperation, resetNotification, resetToken string
	var originalBrowser *http.Client
	var originalAccess, unrelatedAccess string
	var unrelatedBrowser *http.Client
	var originalPassword = e.password
	sub("Workload_missing_forged_expired_audience_RPC_and_wrong_producer", func(t *testing.T) {
		wat := n.wat(t, biz.NotificationSubmitOperation, false)
		cases := []struct {
			name, token string
			want        codes.Code
		}{{"missing", "", codes.Unauthenticated}, {"forged", "invalid", codes.Unauthenticated}, {"expired", wr20MutateWAT(t, n, wat, "expiry"), codes.Unauthenticated}, {"audience", wr20MutateWAT(t, n, wat, "audience"), codes.Unauthenticated}, {"wrong_rpc", n.wat(t, biz.NotificationGetOwnOperation, false), codes.PermissionDenied}}
		for _, c := range cases {
			call, cancel := wr20WorkloadContext(c.token)
			_, err := n.client.SubmitNotification(call, &notificationv1.SubmitNotificationRequest{})
			cancel()
			if status.Code(err) != c.want {
				t.Fatalf("%s code=%s want=%s", c.name, status.Code(err), c.want)
			}
		}
		foreign := notificationv1.NewNotificationServiceClient(n.connection(t, n.address, n.receiver.DNSIdentity, n.foreignCert, n.foreignKey, e.iam.config.Server.Grpc.Tls.ClientCaFile))
		call, cancel := wr20WorkloadContext(n.wat(t, biz.NotificationSubmitOperation, true))
		_, err := foreign.SubmitNotification(call, &notificationv1.SubmitNotificationRequest{})
		cancel()
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("wrong producer code=%s", status.Code(err))
		}
		// A valid token cannot be paired with another direct mTLS peer.
		call, cancel = wr20WorkloadContext(wat)
		_, err = foreign.SubmitNotification(call, &notificationv1.SubmitNotificationRequest{})
		cancel()
		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("peer swap code=%s", status.Code(err))
		}
	})
	sub("Workload_current_Grant_revoke_and_version_fail_closed", func(t *testing.T) {
		wat := n.wat(t, biz.NotificationSubmitOperation, false)
		if _, err := e.owner.Exec(ctx, "UPDATE workload_grants SET status='revoked' WHERE principal_id=$1 AND audience=$2 AND operation=$3", n.producer.PrincipalID, biz.NotificationAudience, biz.NotificationSubmitOperation); err != nil {
			t.Fatal(err)
		}
		call, cancel := wr20WorkloadContext(wat)
		_, err := n.client.SubmitNotification(call, &notificationv1.SubmitNotificationRequest{})
		cancel()
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("revoked Grant code=%s", status.Code(err))
		}
		if _, err = e.owner.Exec(ctx, "UPDATE workload_grants SET status='active',version=version+1 WHERE principal_id=$1 AND audience=$2 AND operation=$3", n.producer.PrincipalID, biz.NotificationAudience, biz.NotificationSubmitOperation); err != nil {
			t.Fatal(err)
		}
		call, cancel = wr20WorkloadContext(wat)
		_, err = n.client.SubmitNotification(call, &notificationv1.SubmitNotificationRequest{})
		cancel()
		if status.Code(err) != codes.PermissionDenied {
			t.Fatal("stale Grant token accepted")
		}
	})
	sub("IAM_atomic_request_audit_outbox_rollback", func(t *testing.T) {
		_, err := e.owner.Exec(ctx, `CREATE FUNCTION wr20_reject_action_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.request_id='wr20-action-rollback' THEN RAISE EXCEPTION 'wr20 audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr20_action_audit BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr20_reject_action_audit()`)
		if err != nil {
			t.Fatal(err)
		}
		_, code := request(t, e.accounts[0], "wr20-action-rollback")
		if code != 503 {
			t.Fatalf("audit rollback HTTP %d", code)
		}
		var count int
		if err = e.owner.QueryRow(ctx, "SELECT count(*) FROM password_action_requests WHERE idempotency_key=$1", "wr20-action-rollback").Scan(&count); err != nil || count != 0 {
			t.Fatal("request survived failed audit transaction")
		}
		if err = e.owner.QueryRow(ctx, "SELECT count(*) FROM password_actions").Scan(&count); err != nil || count != 0 {
			t.Fatal("action survived failed audit transaction")
		}
		if err = e.owner.QueryRow(ctx, "SELECT count(*) FROM notification_outbox").Scan(&count); err != nil || count != 0 {
			t.Fatal("outbox survived failed audit transaction")
		}
		if _, err = e.owner.Exec(ctx, "DROP TRIGGER wr20_action_audit ON iam_audit_events; DROP FUNCTION wr20_reject_action_audit()"); err != nil {
			t.Fatal(err)
		}
	})
	sub("RESET_local_transaction_encrypted_outbox_timeout_and_idempotency", func(t *testing.T) {
		originalBrowser, originalAccess, _, _ = e.login(t, 0)
		unrelatedBrowser, unrelatedAccess, _, _ = e.login(t, 1)
		if err := n.process.Signal(syscall.SIGSTOP); err != nil {
			t.Fatal(err)
		}
		defer n.process.Signal(syscall.SIGCONT)
		var code int
		resetOperation, code = request(t, e.accounts[0], "wr20-reset-request")
		if code != 202 || resetOperation == "" {
			t.Fatalf("request status %d", code)
		}
		var state string
		var payload []byte
		var keyVersion string
		var actions, audits int
		err := e.owner.QueryRow(ctx, `SELECT status,destination_ciphertext,destination_key_version,(SELECT count(*) FROM password_actions WHERE operation_id=$1 AND purpose='reset' AND status='active'),(SELECT count(*) FROM iam_audit_events WHERE target_id=$1 AND action=$2) FROM notification_outbox WHERE operation_id=$1`, resetOperation, string(biz.AuditActionPasswordActionRequested)).Scan(&state, &payload, &keyVersion, &actions, &audits)
		if err != nil || len(payload) < 29 || keyVersion == "" || bytes.Contains(payload, []byte(e.accounts[0])) || actions != 1 || audits != 1 {
			t.Fatal("local transaction or encrypted snapshot assertion failed")
		}
		recordReference(t, e.run, map[string]any{"stage": "C", "assertion": "IAM_action_encrypted_outbox_audit_transaction", "operation_id": resetOperation, "pass": true})
		replay, code := request(t, e.accounts[0], "wr20-reset-request")
		if code != 202 || replay != resetOperation {
			t.Fatal("request replay drift")
		}
		_, code = request(t, e.accounts[1], "wr20-reset-request")
		if code != 409 {
			t.Fatalf("request conflict status %d", code)
		}
		time.Sleep(2500 * time.Millisecond) // one genuine deadline failure, no test timeout bypass
		if err = n.process.Signal(syscall.SIGCONT); err != nil {
			t.Fatal(err)
		}
		resetNotification = receipt(t, resetOperation)
		var attempts int
		var scrubbed bool
		if err = e.owner.QueryRow(ctx, "SELECT attempt_count,destination_ciphertext IS NULL AND destination_key_version IS NULL FROM notification_outbox WHERE operation_id=$1", resetOperation).Scan(&attempts, &scrubbed); err != nil || attempts < 2 || !scrubbed {
			t.Fatal("timeout retry or receipt-time scrub failed")
		}
	})
	sub("SMTP_acceptance_and_get_own_are_independent_of_action_completion", func(t *testing.T) {
		resetToken = n.mailToken(t, e.accounts[0], resetNotification)
		scope := &notificationv1.NotificationScope{Scope: &notificationv1.NotificationScope_HumanPrincipal{HumanPrincipal: &notificationv1.HumanPrincipalScope{PrincipalId: e.humans[0].String()}}}
		call, cancel := wr20WorkloadContext(n.wat(t, biz.NotificationGetOwnOperation, false))
		reply, err := n.client.GetSubmissionStatus(call, &notificationv1.GetSubmissionStatusRequest{NotificationId: resetNotification, Scope: scope})
		cancel()
		if err != nil || reply.GetNotification().GetNotificationId() != resetNotification {
			t.Fatal("formal get_own failed")
		}
		scope.GetHumanPrincipal().PrincipalId = e.humans[1].String()
		call, cancel = wr20WorkloadContext(n.wat(t, biz.NotificationGetOwnOperation, false))
		_, err = n.client.GetSubmissionStatus(call, &notificationv1.GetSubmissionStatusRequest{NotificationId: resetNotification, Scope: scope})
		cancel()
		if status.Code(err) != codes.NotFound {
			t.Fatalf("foreign scope get_own code=%s", status.Code(err))
		}
		var active bool
		if err = e.owner.QueryRow(ctx, "SELECT status='active' FROM password_actions WHERE operation_id=$1", resetOperation).Scan(&active); err != nil || !active {
			t.Fatal("SMTP acceptance completed password action")
		}
	})
	sub("RESET_formal_consume_password_Session_Refresh_revocation_and_safe_replay", func(t *testing.T) {
		newPassword := randomPassword(t)
		if code := complete(t, resetToken, newPassword, "wr20-reset-complete"); code != 204 {
			t.Fatalf("complete HTTP %d", code)
		}
		if code := complete(t, resetToken, newPassword, "wr20-reset-complete"); code != 204 {
			t.Fatalf("completion safe replay HTTP %d", code)
		}
		if code := complete(t, resetToken, newPassword, "wr20-reset-replay"); code != 401 {
			t.Fatalf("one-time replay HTTP %d", code)
		}
		if code := complete(t, resetToken, randomPassword(t), "wr20-reset-complete"); code != 409 {
			t.Fatalf("completion conflict HTTP %d", code)
		}
		code, _, _ := e.request(t, originalBrowser, "GET", "/auth/sessions", originalAccess, nil, nil)
		if code != 401 {
			t.Fatalf("old Session HTTP %d", code)
		}
		code, _, _ = e.request(t, originalBrowser, "POST", "/auth/refresh", "", map[string]any{}, nil)
		if code != 401 {
			t.Fatalf("old Refresh HTTP %d", code)
		}
		code, _, _ = e.request(t, unrelatedBrowser, "GET", "/auth/sessions", unrelatedAccess, nil, nil)
		if code != 200 {
			t.Fatal("unrelated Human was revoked")
		}
		code, _, _ = e.request(t, e.browser(t), "POST", "/auth/password/login", "", map[string]any{"account": e.accounts[0], "password": originalPassword, "audience": "console", "boundary": map[string]string{"type": "tenant", "tenant_id": referenceTenants[0].String()}}, nil)
		if code != 401 {
			t.Fatal("old password remains valid")
		}
		// The intentional wrong password creates the real one-second cooldown.
		// Wait for it to expire; do not flush the dedicated throttle state.
		time.Sleep(1100 * time.Millisecond)
		e.password = newPassword
		e.login(t, 0)
		e.password = originalPassword
		var count int
		if err := e.owner.QueryRow(ctx, "SELECT count(*) FROM iam_audit_events WHERE target_id=$1 AND action=$2", resetOperation, string(biz.AuditActionPasswordActionCompleted)).Scan(&count); err != nil || count != 1 {
			t.Fatal("completion audit duplicated")
		}
		recordReference(t, e.run, map[string]any{"stage": "C", "assertion": "action_completed_and_credential_state_changed", "operation_id": resetOperation, "pass": true})
	})
	sub("Unknown_account_uniform_acceptance_without_business_action", func(t *testing.T) {
		id, code := request(t, "unknown-wr20@example.test", "wr20-unknown")
		if code != 202 || id == "" {
			t.Fatal("unknown account enumerates")
		}
		var count int
		if err := e.owner.QueryRow(ctx, "SELECT count(*) FROM password_actions WHERE operation_id=$1", id).Scan(&count); err != nil || count != 0 {
			t.Fatal("unknown account created an action")
		}
	})
	sub("Notification_local_capability_type_scope_recipient_destination", func(t *testing.T) {
		base := wr20NotificationRequest(e, uuid.NewString())
		variations := []struct {
			name   string
			mutate func(*notificationv1.SubmitNotificationRequest)
			want   codes.Code
		}{
			{"type", func(r *notificationv1.SubmitNotificationRequest) {
				r.Notification = &notificationv1.SubmitNotificationRequest_IamTenantInvitation{IamTenantInvitation: &notificationv1.IamTenantInvitation{TenantDisplayName: "WR20 fixture", ActionUrl: e.origin + "/password-action?token=invalid-fixture", InvitationExpiresAt: timestamppb.New(time.Now().Add(15 * time.Minute))}}
				r.Scope = &notificationv1.NotificationScope{Scope: &notificationv1.NotificationScope_Tenant{Tenant: &notificationv1.TenantScope{TenantId: referenceTenants[0].String()}}}
				r.Recipient = &notificationv1.NotificationRecipient{Recipient: &notificationv1.NotificationRecipient_Direct{Direct: &notificationv1.DirectRecipient{}}}
			}, codes.PermissionDenied},
			{"recipient", func(r *notificationv1.SubmitNotificationRequest) {
				r.Recipient.GetHumanPrincipal().PrincipalId = e.humans[1].String()
			}, codes.InvalidArgument},
			{"destination", func(r *notificationv1.SubmitNotificationRequest) { r.Destination = nil }, codes.InvalidArgument},
		}
		for _, v := range variations {
			r := proto.Clone(base).(*notificationv1.SubmitNotificationRequest)
			v.mutate(r)
			call, cancel := wr20WorkloadContext(n.wat(t, biz.NotificationSubmitOperation, false))
			_, err := n.client.SubmitNotification(call, r)
			cancel()
			if status.Code(err) != v.want {
				t.Fatalf("%s code=%s want=%s", v.name, status.Code(err), v.want)
			}
		}
	})
}

func wr20NotificationRequest(e *wr20Environment, id string) *notificationv1.SubmitNotificationRequest {
	now := time.Now().UTC()
	return &notificationv1.SubmitNotificationRequest{RequestId: id, Source: &notificationv1.SourceReference{ResourceId: uuid.NewString(), ResourceVersion: 1}, CorrelationId: uuid.NewString(), OccurredAt: timestamppb.New(now), DeliverBefore: timestamppb.New(now.Add(15 * time.Minute)), Locale: "en-US", Scope: &notificationv1.NotificationScope{Scope: &notificationv1.NotificationScope_HumanPrincipal{HumanPrincipal: &notificationv1.HumanPrincipalScope{PrincipalId: e.humans[0].String()}}}, Recipient: &notificationv1.NotificationRecipient{Recipient: &notificationv1.NotificationRecipient_HumanPrincipal{HumanPrincipal: &notificationv1.HumanPrincipalRecipient{PrincipalId: e.humans[0].String()}}}, Destination: &notificationv1.DestinationSnapshot{Destination: &notificationv1.DestinationSnapshot_Email{Email: &notificationv1.EmailDestinationSnapshot{NormalizedEmail: e.accounts[0]}}}, Notification: &notificationv1.SubmitNotificationRequest_IamPasswordAction{IamPasswordAction: &notificationv1.IamPasswordAction{ActionUrl: e.origin + "/password-action?token=invalid-fixture", Purpose: notificationv1.IamPasswordActionPurpose_IAM_PASSWORD_ACTION_PURPOSE_RESET, Audience: notificationv1.IamPasswordActionAudience_IAM_PASSWORD_ACTION_AUDIENCE_CONSOLE, ActionExpiresAt: timestamppb.New(now.Add(15 * time.Minute))}}}
}
func wr20MutateWAT(t *testing.T, n *wr20Notification, raw, kind string) string {
	t.Helper()
	token, err := jwt.Parse([]byte(raw), jwt.WithVerify(false), jwt.WithValidate(false))
	if err != nil {
		t.Fatal("negative token fixture parse")
	}
	if kind == "expiry" {
		_ = token.Set(jwt.ExpirationKey, time.Now().Add(-time.Minute))
	} else {
		_ = token.Set(jwt.AudienceKey, []string{"wrong-audience"})
	}
	bytes, err := os.ReadFile(n.e.iam.config.Runtime.AccessToken.PrivateKeyFile)
	if err != nil {
		t.Fatal("negative token fixture key")
	}
	block, _ := pem.Decode(bytes)
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal("negative token fixture key parse")
	}
	h := jws.NewHeaders()
	_ = h.Set(jws.TypeKey, "ANI-WORKLOAD+JWT")
	_ = h.Set(jws.KeyIDKey, n.e.iam.config.Runtime.AccessToken.ActiveKeyId)
	signed, err := jwt.Sign(token, jwt.WithKey(jwa.EdDSA(), parsed.(ed25519.PrivateKey), jws.WithProtectedHeaders(h)))
	if err != nil {
		t.Fatal("negative token fixture signing")
	}
	return strings.TrimSpace(string(signed))
}
