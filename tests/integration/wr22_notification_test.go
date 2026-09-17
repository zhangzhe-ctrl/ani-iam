//go:build integration

package integration_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	notificationv1 "github.com/zhangzhe-ctrl/ani-notification-service/api/notification/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The Tenant is a WR22 isolated prerequisite. WR23 separately proves Core's
// creation/bootstrap producer. Every recipient here is new and is activated
// solely through formal IAM, Gateway, Notification and actual SMTP delivery.
func TestWR22FormalIdentityNotification(t *testing.T) {
	n := newWR22Notification(t)
	b := n.boss
	e := b.wr22Environment
	ctx := context.Background()
	b.registerIntent(t, b.observedIdentity(t))
	boss := b.browser(t)
	location, state := b.beginOIDC(t, boss, "")
	code, returned := b.dexAuthorize(t, location)
	if state != returned {
		t.Fatal("BOSS state prerequisite")
	}
	s, _, _ := b.callback(t, boss, code, state)
	if s != 303 {
		t.Fatal("first administrator prerequisite")
	}
	s, v, _ := b.request(t, boss, "POST", "/auth/refresh", "", nil, nil)
	if s != 200 {
		t.Fatal("BOSS session prerequisite")
	}
	bossToken := v["access_token"].(string)
	admin, adminToken, _, _ := e.login(t, 0)
	tenant := referenceTenants[0]
	base := "/iam/tenants/" + tenant.String()
	var platformRole uuid.UUID
	if e.owner.QueryRow(ctx, `SELECT id FROM platform_roles WHERE code='platform-admin'`).Scan(&platformRole) != nil {
		t.Fatal("Platform role prerequisite")
	}
	sub := func(name string, f func(*testing.T)) {
		if !t.Run(name, f) {
			t.FailNow()
		}
	}
	call := func(t *testing.T, client *http.Client, token, method, path string, payload any, want int) map[string]any {
		t.Helper()
		s, v, _ := e.request(t, client, method, path, token, payload, map[string]string{"Idempotency-Key": uuid.NewString()})
		if s != want {
			t.Fatalf("formal identity delivery status=%d reason=%v want=%d", s, v["code"], want)
		}
		return v
	}
	invite := func(t *testing.T, email string, platform bool, client *http.Client, token, role string) string {
		t.Helper()
		var v map[string]any
		if platform {
			s, body, _ := b.request(t, boss, "POST", "/iam/platform/invitations", bossToken, map[string]any{"email": email, "role_ids": []string{platformRole.String()}}, map[string]string{"Idempotency-Key": uuid.NewString()})
			if s != 201 {
				t.Fatalf("Platform invitation status=%d reason=%v", s, body["code"])
			}
			v = body
		} else {
			v = call(t, client, token, "POST", base+"/invitations", map[string]any{"email": email, "role_ids": []string{role}}, 201)
		}
		return v["invitation_id"].(string)
	}
	receipt := func(t *testing.T, source, kind string) string {
		t.Helper()
		sql := `SELECT notification_id,id::text,payload_ciphertext IS NULL FROM tenant_invitation_outbox WHERE tenant_id=$1 AND invitation_id=$2 AND status='delivered' ORDER BY delivery_generation DESC LIMIT 1`
		args := []any{tenant, source}
		scope, typeName, scopeID := "tenant", "iam_tenant_invitation", tenant.String()
		if kind == "platform" {
			sql = `SELECT notification_id,id::text,payload_ciphertext IS NULL FROM platform_invitation_outbox WHERE invitation_id=$1 AND status='delivered' ORDER BY delivery_generation DESC LIMIT 1`
			args = []any{source}
			scope, typeName, scopeID = "platform_invitation", "iam_platform_invitation", source
		}
		if kind == "code" {
			sql = `SELECT notification_id,id::text,payload_ciphertext IS NULL FROM iam_invited_account_outbox WHERE challenge_id=$1 AND status='delivered'`
			args = []any{source}
			scope, typeName, scopeID = "email_verification", "iam_email_verification", source
		}
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			var id, request string
			var scrubbed bool
			if e.owner.QueryRow(ctx, sql, args...).Scan(&id, &request, &scrubbed) == nil && id != "" {
				var count, deliveries int
				if !scrubbed || n.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM notifications WHERE notification_id=$1 AND source_id=$2 AND producer_id='ani-iam' AND scope_kind=$3 AND notification_type=$4 AND scope_id=$5 AND request_id=$6),(SELECT count(*) FROM deliveries WHERE notification_id=$1)`, id, source, scope, typeName, scopeID, request).Scan(&count, &deliveries) != nil || count != 1 || deliveries != 1 {
					t.Fatal("IAM receipt lacks exact durable Notification and one Delivery")
				}
				recordReference(t, e.run, map[string]any{"stage": "N1", "assertion": "exact_durable_receipt_and_cleared_IAM_payload", "notification_id": id, "source_id": source, "scope_kind": scope, "pass": true})
				return id
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("identity delivery durable receipt timeout")
		return ""
	}
	type activated struct {
		email, id, notification, challenge, codeNotification, token string
		human                                                       uuid.UUID
		browser                                                     *http.Client
		access                                                      string
	}
	activate := func(t *testing.T, platform bool, client *http.Client, access, role string) activated {
		t.Helper()
		a := activated{email: "wr22-mail-" + uuid.NewString() + "@example.test", browser: e.browser(t)}
		a.id = invite(t, a.email, platform, client, access, role)
		kind := "tenant"
		if platform {
			kind = "platform"
		}
		a.notification = receipt(t, a.id, kind)
		a.token = n.mailValue(t, a.email, a.notification, kind)
		a.challenge = call(t, a.browser, "", "POST", "/auth/invited-account-verifications", map[string]string{"account": a.email}, 202)["challenge_id"].(string)
		a.codeNotification = receipt(t, a.challenge, "code")
		verification := n.mailValue(t, a.email, a.codeNotification, "code")
		call(t, a.browser, "", "POST", "/auth/invited-accounts", map[string]string{"account": a.email, "challenge_id": a.challenge, "verification_code": verification, "new_password": e.password}, 204)
		if e.owner.QueryRow(ctx, `SELECT principal_id FROM verified_emails WHERE normalized_email=$1`, a.email).Scan(&a.human) != nil {
			t.Fatal("formal invited Human missing")
		}
		var authorities int
		if e.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_memberships WHERE principal_id=$1)+(SELECT count(*) FROM platform_memberships WHERE principal_id=$1)+(SELECT count(*) FROM sessions WHERE principal_id=$1)`, a.human).Scan(&authorities) != nil || authorities != 0 {
			t.Fatal("activation granted Membership or Session")
		}
		v := call(t, a.browser, "", "POST", "/auth/invitation-acceptances", map[string]string{"account": a.email, "password": e.password, "invitation_token": a.token}, 200)
		wantBoundary := "tenant"
		if platform {
			wantBoundary = "platform"
		}
		if v["boundary"] != wantBoundary || v["access_token"] != nil {
			t.Fatal("acceptance boundary or no-Session invariant")
		}
		if platform {
			if _, ok := v["tenant_id"]; ok {
				t.Fatal("Platform acceptance carries Tenant")
			}
			a.browser = b.browser(t)
			s, v, _ := b.request(t, a.browser, "POST", "/auth/password/login", "", map[string]any{"account": a.email, "password": e.password, "audience": "boss", "boundary": map[string]string{"type": "platform"}}, map[string]string{"Idempotency-Key": uuid.NewString()})
			if s != 200 {
				t.Fatalf("activated BOSS login status=%d reason=%v", s, v["code"])
			}
			a.access = v["access_token"].(string)
		} else {
			if v["tenant_id"] != tenant.String() {
				t.Fatal("Invitation selected foreign Tenant")
			}
			a.access = call(t, a.browser, "", "POST", "/auth/password/login", map[string]any{"account": a.email, "password": e.password, "audience": "console", "boundary": map[string]string{"type": "tenant", "tenant_id": tenant.String()}}, 200)["access_token"].(string)
		}
		return a
	}
	var tenantAdmin, platformAdmin activated
	sub("real_SMTP_invited_Tenant_administrator_activation_and_first_Membership", func(t *testing.T) {
		tenantAdmin = activate(t, false, admin, adminToken, e.adminRoles[0].String())
		call(t, tenantAdmin.browser, tenantAdmin.access, "GET", base+"/members", nil, 200)
	})
	sub("activated_Tenant_administrator_invites_ordinary_member", func(t *testing.T) {
		role := call(t, tenantAdmin.browser, tenantAdmin.access, "POST", base+"/roles", map[string]any{"name": "N1 ordinary reader", "permissions": []string{"iam.memberships/read"}}, 201)["role_id"].(string)
		member := activate(t, false, tenantAdmin.browser, tenantAdmin.access, role)
		call(t, member.browser, member.access, "GET", base+"/members", nil, 200)
		call(t, member.browser, member.access, "POST", base+"/invitations", map[string]any{"email": "unauthorized@wr22.test", "role_ids": []string{role}}, 403)
	})
	sub("real_SMTP_Platform_invitation_activation_has_no_Tenant", func(t *testing.T) {
		platformAdmin = activate(t, true, nil, "", "")
		var count int
		if e.owner.QueryRow(ctx, `SELECT count(*) FROM tenant_memberships WHERE principal_id=$1`, platformAdmin.human).Scan(&count) != nil || count != 0 {
			t.Fatal("Platform administrator acquired Tenant")
		}
	})
	sub("exact_online_Workload_authority_and_direct_peer", func(t *testing.T) {
		wat := n.wat(t, biz.NotificationSubmitOperation, false)
		for _, c := range []struct {
			token string
			want  codes.Code
		}{{"", codes.Unauthenticated}, {"invalid", codes.Unauthenticated}, {n.wat(t, biz.NotificationGetOwnOperation, false), codes.PermissionDenied}} {
			cctx, cancel := wr20WorkloadContext(c.token)
			_, err := n.client.SubmitNotification(cctx, &notificationv1.SubmitNotificationRequest{})
			cancel()
			if status.Code(err) != c.want {
				t.Fatalf("direct Workload denial code=%s want=%s", status.Code(err), c.want)
			}
		}
		foreign := notificationv1.NewNotificationServiceClient(n.connection(t, n.address, n.receiver.DNSIdentity, n.foreignCert, n.foreignKey, e.iam.config.Server.Grpc.Tls.ClientCaFile))
		for _, c := range []struct {
			token string
			want  codes.Code
		}{{n.wat(t, biz.NotificationSubmitOperation, true), codes.PermissionDenied}, {wat, codes.Unauthenticated}} {
			cctx, cancel := wr20WorkloadContext(c.token)
			_, err := foreign.SubmitNotification(cctx, &notificationv1.SubmitNotificationRequest{})
			cancel()
			if status.Code(err) != c.want {
				t.Fatalf("producer/peer denial code=%s", status.Code(err))
			}
		}
	})
	sub("typed_scope_source_and_composite_FK_reject_cross_boundary", func(t *testing.T) {
		id := mustV7(t).String()
		req := &notificationv1.SubmitNotificationRequest{RequestId: mustV7(t).String(), Source: &notificationv1.SourceReference{ResourceId: mustV7(t).String(), ResourceVersion: 1}, CorrelationId: id, OccurredAt: timestamppb.Now(), DeliverBefore: timestamppb.New(time.Now().Add(time.Minute)), Locale: "en-US", Scope: &notificationv1.NotificationScope{Scope: &notificationv1.NotificationScope_EmailVerification{EmailVerification: &notificationv1.EmailVerificationScope{VerificationId: id}}}, Recipient: &notificationv1.NotificationRecipient{Recipient: &notificationv1.NotificationRecipient_Direct{Direct: &notificationv1.DirectRecipient{}}}, Destination: &notificationv1.DestinationSnapshot{Destination: &notificationv1.DestinationSnapshot_Email{Email: &notificationv1.EmailDestinationSnapshot{NormalizedEmail: "denied@wr22.test"}}}, Notification: &notificationv1.SubmitNotificationRequest_IamEmailVerification{IamEmailVerification: &notificationv1.IamEmailVerification{Code: "123456", VerificationExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}}}
		cctx, cancel := wr20WorkloadContext(n.wat(t, biz.NotificationSubmitOperation, false))
		_, err := n.client.SubmitNotification(cctx, req)
		cancel()
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("source mismatch code=%s", status.Code(err))
		}
		assertSQLRejected(t, n.pool, "23503", `INSERT INTO iam_email_verification_payloads(notification_id,scope_kind,scope_id,notification_type,aad_schema_version,code_key_version,code_ciphertext,verification_expires_at,scrubbed_at) SELECT $1,scope_kind,scope_id,notification_type,aad_schema_version,code_key_version,code_ciphertext,verification_expires_at,scrubbed_at FROM iam_email_verification_payloads WHERE notification_id=$2`, platformAdmin.notification, tenantAdmin.codeNotification)
		cctx, cancel = wr20WorkloadContext(n.wat(t, biz.NotificationGetOwnOperation, false))
		_, err = n.client.GetSubmissionStatus(cctx, &notificationv1.GetSubmissionStatusRequest{NotificationId: platformAdmin.notification, Scope: &notificationv1.NotificationScope{Scope: &notificationv1.NotificationScope_Tenant{Tenant: &notificationv1.TenantScope{TenantId: tenant.String()}}}})
		cancel()
		if status.Code(err) != codes.NotFound {
			t.Fatalf("cross-scope read code=%s", status.Code(err))
		}
	})
	sub("lost_receipt_retries_same_intent_without_duplicate_Notification", func(t *testing.T) {
		// Existing mTLS connection was established by the successful submissions.
		n.gate.drop.Store(true)
		defer n.gate.reset()
		email := "wr22-receipt-" + uuid.NewString() + "@example.test"
		id := invite(t, email, false, admin, adminToken, e.adminRoles[0].String())
		deadline := time.Now().Add(15 * time.Second)
		seen := false
		for time.Now().Before(deadline) {
			var count int
			if n.pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE source_id=$1`, id).Scan(&count) == nil && count == 1 {
				seen = true
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if !seen {
			t.Fatal("Notification did not durably accept before response loss")
		}
		var delivered bool
		if e.owner.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tenant_invitation_outbox WHERE tenant_id=$1 AND invitation_id=$2 AND notification_id IS NOT NULL)`, tenant, id).Scan(&delivered) != nil || delivered {
			t.Fatal("IAM fabricated lost receipt")
		}
		time.Sleep(2300 * time.Millisecond)
		n.gate.reset()
		notification := receipt(t, id, "tenant")
		_ = n.mailValue(t, email, notification, "tenant")
		var count, attempts int
		if n.pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE source_id=$1`, id).Scan(&count) != nil || count != 1 {
			t.Fatal("retry duplicated Notification")
		}
		if e.owner.QueryRow(ctx, `SELECT attempt_count FROM tenant_invitation_outbox WHERE tenant_id=$1 AND invitation_id=$2`, tenant, id).Scan(&attempts) != nil || attempts < 2 {
			t.Fatal("response-loss retry not observed")
		}
	})
	sub("revoked_delivery_Grant_requires_current_authority_and_formal_resend", func(t *testing.T) {
		sql := `UPDATE workload_grants SET status=$1,version=version+1 WHERE principal_id=$2 AND audience=$3 AND operation=$4`
		mutate := func(value string) {
			if _, err := e.owner.Exec(ctx, sql, value, n.producer.PrincipalID, biz.NotificationAudience, biz.NotificationSubmitOperation); err != nil {
				t.Fatal("own producer Grant mutation")
			}
		}
		mutate("revoked")
		defer mutate("active")
		email := "wr22-recover-" + uuid.NewString() + "@example.test"
		id := invite(t, email, false, admin, adminToken, e.adminRoles[0].String())
		deadline := time.Now().Add(15 * time.Second)
		stopped := false
		for time.Now().Before(deadline) {
			var status string
			if e.owner.QueryRow(ctx, `SELECT status FROM tenant_invitation_outbox WHERE tenant_id=$1 AND invitation_id=$2`, tenant, id).Scan(&status) == nil && status == "attention_required" {
				stopped = true
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if !stopped {
			t.Fatal("revoked delivery did not stop for controlled recovery")
		}
		var count int
		if n.pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE source_id=$1`, id).Scan(&count) != nil || count != 0 {
			t.Fatal("revoked producer delivered")
		}
		mutate("active")
		call(t, admin, adminToken, "POST", base+"/invitations/"+id+"/resend", map[string]any{"expected_version": 1}, 200)
		notification := receipt(t, id, "tenant")
		_ = n.mailValue(t, email, notification, "tenant")
	})
	sub("same_worker_delivers_BOSS_password_setup_and_reset_with_Session_revocation", func(t *testing.T) {
		// The original administrator has only its real OIDC Identity initially.
		const account = "wr22-boss@example.test"
		var oldBrowser *http.Client
		var oldAccess string
		for _, purpose := range []string{"setup", "reset"} {
			if oldBrowser != nil {
				s, _, _ := b.request(t, oldBrowser, "GET", "/auth/sessions", oldAccess, nil, nil)
				if s != 200 {
					t.Fatal("BOSS Session was not usable before password reset")
				}
			}
			s, v, _ := b.request(t, b.browser(t), "POST", "/auth/password-actions", "", map[string]string{"account": account, "audience": "boss"}, map[string]string{"Idempotency-Key": uuid.NewString()})
			if s != 202 {
				t.Fatalf("BOSS password email request status=%d reason=%v", s, v["code"])
			}
			operation := v["operation_id"].(string)
			notification := ""
			deadline := time.Now().Add(30 * time.Second)
			for time.Now().Before(deadline) {
				if e.owner.QueryRow(ctx, `SELECT notification_id FROM notification_outbox WHERE operation_id=$1 AND status='delivered'`, operation).Scan(&notification) == nil && notification != "" {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			if notification == "" {
				t.Fatal("BOSS password email durable receipt timeout")
			}
			var principal uuid.UUID
			var actualPurpose string
			if e.owner.QueryRow(ctx, `SELECT principal_id,purpose FROM password_actions WHERE operation_id=$1`, operation).Scan(&principal, &actualPurpose) != nil || actualPurpose != purpose {
				t.Fatal("BOSS password setup/reset intent differs")
			}
			var exact bool
			if n.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM notifications WHERE notification_id=$1 AND source_id=$2 AND scope_kind='human_principal' AND scope_id=$3 AND notification_type='iam_password_action' AND producer_id='ani-iam')`, notification, operation, principal).Scan(&exact) != nil || !exact {
				t.Fatal("BOSS password notification scope differs")
			}
			secret := n.mailValue(t, account, notification, "boss-password")
			password := randomPassword(t)
			s, v, _ = b.request(t, b.browser(t), "POST", "/auth/password-actions/complete", "", map[string]string{"token": secret, "new_password": password}, map[string]string{"Idempotency-Key": uuid.NewString()})
			if s != 204 {
				t.Fatalf("SMTP BOSS password completion status=%d reason=%v", s, v["code"])
			}
			if oldBrowser != nil {
				s, _, _ = b.request(t, oldBrowser, "GET", "/auth/sessions", oldAccess, nil, nil)
				if s != 401 {
					t.Fatalf("old BOSS Session after reset status=%d", s)
				}
			}
			if purpose == "reset" {
				var active int
				if e.owner.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE principal_id=$1 AND status='active'`, principal).Scan(&active) != nil || active != 0 {
					t.Fatal("password reset left active old Session")
				}
			}
			oldBrowser = b.browser(t)
			s, v, _ = b.request(t, oldBrowser, "POST", "/auth/password/login", "", map[string]any{"account": account, "password": password, "audience": "boss", "boundary": map[string]string{"type": "platform"}}, map[string]string{"Idempotency-Key": uuid.NewString()})
			if s != 200 {
				t.Fatalf("SMTP BOSS password login status=%d reason=%v", s, v["code"])
			}
			oldAccess = v["access_token"].(string)
			var tenants int
			if e.owner.QueryRow(ctx, `SELECT count(*) FROM tenant_memberships WHERE principal_id=$1`, principal).Scan(&tenants) != nil || tenants != 0 {
				t.Fatal("BOSS password action created Tenant Membership")
			}
		}
	})
}
