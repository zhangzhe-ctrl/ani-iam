//go:build integration

package integration_test

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestWR22FormalInvitationAcceptance(t *testing.T) {
	b := newWR22BossEnvironment(t)
	ctx := context.Background()
	b.registerIntent(t, b.observedIdentity(t))
	boss := b.browser(t)
	location, state := b.beginOIDC(t, boss, "")
	code, returned := b.dexAuthorize(t, location)
	if state != returned {
		t.Fatal("acceptance BOSS OIDC state prerequisite")
	}
	status, _, _ := b.callback(t, boss, code, state)
	if status != 303 {
		t.Fatal("acceptance real first admin prerequisite")
	}
	status, body, _ := b.request(t, boss, "POST", "/auth/refresh", "", nil, nil)
	if status != 200 {
		t.Fatal("acceptance BOSS token prerequisite")
	}
	bossToken := body["access_token"].(string)
	admin, adminToken, _, _ := b.login(t, 0)
	recipient, recipientToken, _, _ := b.login(t, 1)
	tenant := referenceTenants[0]
	target := b.humans[1]
	tenantBase := "/iam/tenants/" + tenant.String() + "/invitations"
	const platformBase = "/iam/platform/invitations"
	bossClients := map[*http.Client]bool{boss: true}
	call := func(t *testing.T, c *http.Client, token, method, path, key string, payload any, want int) map[string]any {
		t.Helper()
		headers := map[string]string{}
		if key != "" {
			headers["Idempotency-Key"] = key
		}
		var status int
		var body map[string]any
		var response *http.Response
		if bossClients[c] {
			status, body, response = b.request(t, c, method, path, token, payload, headers)
		} else {
			status, body, response = b.wr22Environment.request(t, c, method, path, token, payload, headers)
		}
		if status != want {
			t.Fatalf("invitation acceptance status=%d reason=%v want=%d", status, body["code"], want)
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("invitation acceptance cacheable")
		}
		return body
	}
	// Component prerequisite ONLY: decrypt the own formal process encrypted
	// outbox using this isolated run's private key. No SMTP delivery is claimed.
	tokenFor := func(t *testing.T, id string, platform bool) string {
		t.Helper()
		var delivery, version string
		var generation int64
		var encrypted, digest []byte
		if platform {
			if b.owner.QueryRow(ctx, `SELECT o.id,o.payload_key_version,o.delivery_generation,o.payload_ciphertext,i.token_digest FROM platform_invitation_outbox o JOIN platform_invitations i ON i.id=o.invitation_id AND i.delivery_generation=o.delivery_generation WHERE i.id=$1`, id).Scan(&delivery, &version, &generation, &encrypted, &digest) != nil {
				t.Fatal("own Platform encrypted outbox prerequisite")
			}
		} else {
			if b.owner.QueryRow(ctx, `SELECT o.id,o.payload_key_version,o.delivery_generation,o.payload_ciphertext,i.token_digest FROM tenant_invitation_outbox o JOIN tenant_invitations i ON i.tenant_id=o.tenant_id AND i.id=o.invitation_id AND i.delivery_generation=o.delivery_generation WHERE i.tenant_id=$1 AND i.id=$2`, tenant, id).Scan(&delivery, &version, &generation, &encrypted, &digest) != nil {
				t.Fatal("own Tenant encrypted outbox prerequisite")
			}
		}
		raw, err := os.ReadFile(b.iam.config.Runtime.Notification.OutboxKeyFile)
		if err != nil {
			t.Fatal("own outbox key unavailable")
		}
		var cfg struct {
			Keys map[string]string `json:"keys"`
		}
		if json.Unmarshal(raw, &cfg) != nil {
			t.Fatal("own key shape")
		}
		key, err := base64.StdEncoding.DecodeString(cfg.Keys[version])
		if err != nil {
			t.Fatal("own key encoding")
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			t.Fatal("own key length")
		}
		aead, err := cipher.NewGCM(block)
		if err != nil || len(encrypted) < aead.NonceSize()+aead.Overhead() {
			t.Fatal("own encrypted delivery shape")
		}
		aad := fmt.Sprintf("ani-iam:tenant-invitation-delivery:v1|%s|%s|%s|%s|%d", version, tenant, id, delivery, generation)
		if platform {
			aad = fmt.Sprintf("ani-iam:platform-invitation-delivery:v1|%s|%s|%s|%d", version, id, delivery, generation)
		}
		plain, err := aead.Open(nil, encrypted[:aead.NonceSize()], encrypted[aead.NonceSize():], []byte(aad))
		var payload struct{ Email, Token string }
		if err != nil || json.Unmarshal(plain, &payload) != nil || payload.Email != b.accounts[1] {
			t.Fatal("own authenticated delivery payload prerequisite")
		}
		sum := sha256.Sum256([]byte(payload.Token))
		if string(sum[:]) != string(digest) {
			t.Fatal("own payload digest differs")
		}
		return payload.Token
	}
	createTenant := func(t *testing.T) string {
		return call(t, admin, adminToken, "POST", tenantBase, uuid.NewString(), map[string]any{"email": b.accounts[1], "role_ids": []string{b.adminRoles[0].String()}}, 201)["invitation_id"].(string)
	}
	acceptPayload := func(token string) map[string]any { return map[string]any{"invitation_token": token} }
	t.Run("runtime_Lifecycle_projection_remains_read_only", func(t *testing.T) {
		runtime := mustPool(t, b.iam.config.Runtime.Postgresql.Dsn)
		defer runtime.Close()
		var role, lifecycle string
		if runtime.QueryRow(ctx, `SELECT current_user`).Scan(&role) != nil || role != "ani_iam_runtime" {
			t.Fatal("own runtime role prerequisite")
		}
		_, err := runtime.Exec(ctx, `SELECT status FROM tenant_lifecycle_projections WHERE tenant_id=$1 FOR SHARE`, tenant)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "42501" {
			t.Fatal("readonly Lifecycle lock prerequisite expected SQLSTATE 42501")
		}
		if runtime.QueryRow(ctx, `SELECT status FROM tenant_lifecycle_projections WHERE tenant_id=$1`, tenant).Scan(&lifecycle) != nil || lifecycle != "active" {
			t.Fatal("runtime cannot read Lifecycle projection")
		}
	})
	id := createTenant(t)
	secret := tokenFor(t, id, false)
	t.Run("separate_recipient_token_boundary_and_cancellation", func(t *testing.T) {
		call(t, admin, adminToken, "POST", tenantBase+"/"+id+"/accept", uuid.NewString(), acceptPayload(secret), 403)
		call(t, recipient, secret, "POST", tenantBase+"/"+id+"/accept", uuid.NewString(), acceptPayload(secret), 401)
		call(t, recipient, recipientToken, "POST", tenantBase+"/"+id+"/accept", uuid.NewString(), acceptPayload(secret+"x"), 403)
		foreign := "/iam/tenants/" + referenceTenants[1].String() + "/invitations/" + id + "/accept"
		call(t, recipient, recipientToken, "POST", foreign, uuid.NewString(), acceptPayload(secret), 403)
		call(t, recipient, recipientToken, "POST", platformBase+"/"+id+"/accept", uuid.NewString(), acceptPayload(secret), 403)
		call(t, admin, adminToken, "POST", tenantBase+"/"+id+"/cancel", uuid.NewString(), map[string]any{"expected_version": 1}, 204)
		call(t, recipient, recipientToken, "POST", tenantBase+"/"+id+"/accept", uuid.NewString(), acceptPayload(secret), 409)
	})
	t.Run("synthetic_expiry_and_resend_rotate_acceptance_secret", func(t *testing.T) {
		id = createTenant(t)
		secret = tokenFor(t, id, false)
		tx, err := b.owner.Begin(ctx)
		if err != nil {
			t.Fatal("own Invitation expiry transaction")
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `ALTER TABLE tenant_invitations DISABLE TRIGGER tenant_invitation_identity`); err != nil {
			t.Fatal("own expiry fixture guard")
		}
		if _, err = tx.Exec(ctx, `UPDATE tenant_invitations SET created_at=now()-interval '8 days',expires_at=now()-interval '1 day' WHERE tenant_id=$1 AND id=$2`, tenant, id); err != nil {
			t.Fatal("own expired invitation fixture")
		}
		if _, err = tx.Exec(ctx, `ALTER TABLE tenant_invitations ENABLE TRIGGER tenant_invitation_identity`); err != nil || tx.Commit(ctx) != nil {
			t.Fatal("own expiry guard restore")
		}
		call(t, recipient, recipientToken, "POST", tenantBase+"/"+id+"/accept", uuid.NewString(), acceptPayload(secret), 409)
		call(t, admin, adminToken, "POST", tenantBase+"/"+id+"/resend", uuid.NewString(), map[string]any{"expected_version": 1}, 200)
		call(t, recipient, recipientToken, "POST", tenantBase+"/"+id+"/accept", uuid.NewString(), acceptPayload(secret), 403)
		secret = tokenFor(t, id, false)
	})
	if t.Failed() {
		return
	}
	fault := func(t *testing.T) {
		t.Helper()
		if _, err := b.owner.Exec(ctx, `CREATE SEQUENCE wr22_acceptance_fault_hits; GRANT USAGE,SELECT ON SEQUENCE wr22_acceptance_fault_hits TO ani_iam_runtime; CREATE FUNCTION wr22_acceptance_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action IN ('iam.invitation.accepted','iam.platform.invitation.accepted') THEN PERFORM nextval('wr22_acceptance_fault_hits'); RAISE EXCEPTION 'isolated acceptance Audit fault' USING ERRCODE='P0022'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr22_acceptance_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr22_acceptance_fault()`); err != nil {
			t.Fatal("own acceptance Audit fault setup")
		}
	}
	clearFault := func(t *testing.T) {
		t.Helper()
		var hit bool
		if b.owner.QueryRow(ctx, `SELECT is_called FROM wr22_acceptance_fault_hits`).Scan(&hit) != nil || !hit {
			t.Fatal("acceptance Audit fault not reached")
		}
		if _, err := b.owner.Exec(ctx, `DROP TRIGGER wr22_acceptance_fault ON iam_audit_events; DROP FUNCTION wr22_acceptance_fault(); DROP SEQUENCE wr22_acceptance_fault_hits`); err != nil {
			t.Fatal("own acceptance Audit restore")
		}
	}
	t.Run("tenant_acceptance_audit_failure_rolls_back_entire_join", func(t *testing.T) {
		fault(t)
		defer clearFault(t)
		call(t, recipient, recipientToken, "POST", tenantBase+"/"+id+"/accept", uuid.NewString(), acceptPayload(secret), 503)
		var members, pending, deliveries, refs int
		if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1 AND principal_id=$2),(SELECT count(*) FROM tenant_invitations WHERE tenant_id=$1 AND id=$3 AND status='pending' AND accepted_principal_id IS NULL),(SELECT count(*) FROM tenant_invitation_outbox WHERE tenant_id=$1 AND invitation_id=$3 AND status='pending' AND payload_ciphertext IS NOT NULL),(SELECT count(*) FROM tenant_invitation_roles WHERE tenant_id=$1 AND invitation_id=$3)`, tenant, target, id).Scan(&members, &pending, &deliveries, &refs) != nil || members != 0 || pending != 1 || deliveries != 1 || refs != 1 {
			t.Fatal("Tenant acceptance Audit failure left partial effect")
		}
	})
	if t.Failed() {
		return
	}
	key := uuid.NewString()
	member := ""
	t.Run("concurrent_acceptance_atomic_receipt_and_actual_new_tenant_login", func(t *testing.T) {
		var wg sync.WaitGroup
		out := make(chan string, 4)
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r := call(t, recipient, recipientToken, "POST", tenantBase+"/"+id+"/accept", key, acceptPayload(secret), 200)
				v, _ := r["membership_id"].(string)
				out <- v
			}()
		}
		wg.Wait()
		close(out)
		for v := range out {
			if v == "" {
				t.Fatal("accepted Membership missing")
			}
			if member == "" {
				member = v
			}
			if member != v {
				t.Fatal("concurrent Invitation created duplicate Membership")
			}
		}
		call(t, recipient, recipientToken, "POST", tenantBase+"/"+id+"/accept", uuid.NewString(), acceptPayload(secret), 409)
		var n, audits, roles, refs int
		if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1 AND principal_id=$2),(SELECT count(*) FROM iam_audit_events WHERE tenant_id=$1 AND target_id=$3 AND action='iam.invitation.accepted' AND actor_id=$2 AND caller_principal_id IS NOT NULL),(SELECT count(*) FROM tenant_role_bindings WHERE tenant_id=$1 AND membership_id=$4),(SELECT count(*) FROM tenant_invitation_roles WHERE tenant_id=$1 AND invitation_id=$3)`, tenant, target, id, member).Scan(&n, &audits, &roles, &refs) != nil || n != 1 || audits != 1 || roles != 1 || refs != 0 {
			t.Fatal("Invitation membership/binding/audit/receipt atomicity failed")
		}
		c := b.wr22Environment.browser(t)
		status, login, _ := b.wr22Environment.request(t, c, "POST", "/auth/password/login", "", map[string]any{"account": b.accounts[1], "password": b.password, "audience": "console", "boundary": map[string]string{"type": "tenant", "tenant_id": tenant.String()}}, nil)
		if status != 200 {
			t.Fatal("accepted target cannot log into actual new Tenant")
		}
		call(t, c, login["access_token"].(string), "GET", "/iam/tenants/"+tenant.String()+"/roles", "", nil, 200)
		duplicate := createTenant(t)
		call(t, recipient, recipientToken, "POST", tenantBase+"/"+duplicate+"/accept", uuid.NewString(), acceptPayload(tokenFor(t, duplicate, false)), 409)
		call(t, admin, adminToken, "POST", tenantBase+"/"+duplicate+"/cancel", uuid.NewString(), map[string]any{"expected_version": 1}, 204)
	})
	if t.Failed() {
		return
	}
	t.Run("current_Session_Grant_and_exact_direct_caller_before_receipt", func(t *testing.T) {
		c, token, _, _ := b.login(t, 1)
		call(t, c, token, "POST", tenantBase+"/"+id+"/accept", key, acceptPayload(secret), 200)
		if status, _, _ := b.wr22Environment.request(t, c, "POST", "/auth/logout", "", nil, map[string]string{"Idempotency-Key": uuid.NewString()}); status != 204 {
			t.Fatal("own formal source logout failed")
		}
		call(t, c, token, "POST", tenantBase+"/"+id+"/accept", key, acceptPayload(secret), 401)
		c, token, _, _ = b.login(t, 1)
		var session uuid.UUID
		if b.owner.QueryRow(ctx, `SELECT s.id FROM sessions s JOIN session_grants g ON g.session_id=s.id AND g.tenant_id=$1 WHERE s.principal_id=$2 AND s.audience='console' AND s.status='active' ORDER BY s.created_at DESC LIMIT 1`, referenceTenants[1], target).Scan(&session) != nil {
			t.Fatal("own source Session prerequisite")
		}
		if _, err := b.owner.Exec(ctx, `UPDATE session_grants SET status='revoked',version=version+1,updated_at=now() WHERE tenant_id=$1 AND session_id=$2`, referenceTenants[1], session); err != nil {
			t.Fatal("own source Grant revoke")
		}
		call(t, c, token, "POST", tenantBase+"/"+id+"/accept", key, acceptPayload(secret), 401)
		var grant, caller, binding uuid.UUID
		if b.owner.QueryRow(ctx, `SELECT g.id,g.principal_id,b.id FROM workload_grants g JOIN workload_identity_bindings b ON b.principal_id=g.principal_id WHERE g.operation='/iam.v1.IAMAdminService/AcceptTenantInvitation' AND g.status='active' AND b.status='active'`).Scan(&grant, &caller, &binding) != nil {
			t.Fatal("exact acceptance caller prerequisite")
		}
		if _, err := b.owner.Exec(ctx, `UPDATE workload_grants SET status='revoked',version=version+1,updated_at=now() WHERE id=$1`, grant); err != nil {
			t.Fatal("own acceptance Grant revoke")
		}
		call(t, recipient, recipientToken, "POST", tenantBase+"/"+id+"/accept", key, acceptPayload(secret), 403)
		call(t, admin, adminToken, "GET", tenantBase+"/"+id, "", nil, 200)
		if _, err := b.owner.Exec(ctx, `UPDATE workload_grants SET status='active',version=version+1,updated_at=now() WHERE id=$1`, grant); err != nil {
			t.Fatal("own acceptance Grant restore")
		}
		if _, err := b.owner.Exec(ctx, `UPDATE workload_identity_bindings SET status='revoked',version=version+1,updated_at=now() WHERE id=$1 AND principal_id=$2`, binding, caller); err != nil {
			t.Fatal("own acceptance caller Binding revoke")
		}
		call(t, recipient, recipientToken, "POST", tenantBase+"/"+id+"/accept", key, acceptPayload(secret), 401)
		if _, err := b.owner.Exec(ctx, `UPDATE workload_identity_bindings SET status='active',version=version+1,updated_at=now() WHERE id=$1 AND principal_id=$2`, binding, caller); err != nil {
			t.Fatal("own acceptance caller Binding restore")
		}
		call(t, recipient, recipientToken, "POST", tenantBase+"/"+id+"/accept", key, acceptPayload(secret), 200)
	})
	if t.Failed() {
		return
	}
	t.Run("current_recipient_before_receipt_and_removed_relation_requires_new_invitation", func(t *testing.T) {
		if _, err := b.owner.Exec(ctx, `UPDATE tenant_memberships SET status='suspended',version=version+1,updated_at=now() WHERE tenant_id=$1 AND principal_id=$2 AND status='active'`, referenceTenants[1], target); err != nil {
			t.Fatal("own source Membership revoke")
		}
		call(t, recipient, recipientToken, "POST", tenantBase+"/"+id+"/accept", key, acceptPayload(secret), 403)
		if _, err := b.owner.Exec(ctx, `UPDATE tenant_memberships SET status='active',version=version+1,updated_at=now() WHERE tenant_id=$1 AND principal_id=$2 AND status='suspended'`, referenceTenants[1], target); err != nil {
			t.Fatal("own source Membership restore")
		}
		old := createTenant(t)
		oldSecret := tokenFor(t, old, false)
		call(t, admin, adminToken, "DELETE", "/iam/tenants/"+tenant.String()+"/members/"+member+"?expected_version=1", uuid.NewString(), nil, 204)
		call(t, recipient, recipientToken, "POST", tenantBase+"/"+old+"/accept", uuid.NewString(), acceptPayload(oldSecret), 409)
		call(t, admin, adminToken, "POST", tenantBase+"/"+old+"/cancel", uuid.NewString(), map[string]any{"expected_version": 1}, 204)
		fresh := createTenant(t)
		r := call(t, recipient, recipientToken, "POST", tenantBase+"/"+fresh+"/accept", uuid.NewString(), acceptPayload(tokenFor(t, fresh, false)), 200)
		if r["membership_id"] == member {
			t.Fatal("fresh invitation revived removed Membership")
		}
		var oldStatus string
		if b.owner.QueryRow(ctx, `SELECT status FROM tenant_memberships WHERE tenant_id=$1 AND id=$2`, tenant, member).Scan(&oldStatus) != nil || oldStatus != "removed" {
			t.Fatal("removed invitation history changed")
		}
		member = r["membership_id"].(string)
	})
	t.Run("cancel_accept_race_is_atomic", func(t *testing.T) {
		call(t, admin, adminToken, "DELETE", "/iam/tenants/"+tenant.String()+"/members/"+member+"?expected_version=1", uuid.NewString(), nil, 204)
		inv := createTenant(t)
		token := tokenFor(t, inv, false)
		out := make(chan int, 2)
		go func() {
			s, _, _ := b.wr22Environment.request(t, recipient, "POST", tenantBase+"/"+inv+"/accept", recipientToken, acceptPayload(token), map[string]string{"Idempotency-Key": uuid.NewString()})
			out <- s
		}()
		go func() {
			s, _, _ := b.wr22Environment.request(t, admin, "POST", tenantBase+"/"+inv+"/cancel", adminToken, map[string]any{"expected_version": 1}, map[string]string{"Idempotency-Key": uuid.NewString()})
			out <- s
		}()
		first, second := <-out, <-out
		if !((first == 200 || first == 204) && second == 409 || (second == 200 || second == 204) && first == 409) {
			t.Fatal("cancel/accept CAS had ambiguous outcome")
		}
		var state string
		var n int
		if b.owner.QueryRow(ctx, `SELECT status,(SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1 AND principal_id=$2 AND status='active') FROM tenant_invitations WHERE tenant_id=$1 AND id=$3`, tenant, target, inv).Scan(&state, &n) != nil || (state == "accepted" && n != 1) || (state == "cancelled" && n != 0) {
			t.Fatal("cancel/accept partial effect")
		}
	})
	platformRole := call(t, boss, bossToken, "POST", "/iam/platform/roles", uuid.NewString(), map[string]any{"name": "WR22 invited Platform reader", "permissions": []string{"iam.platform-memberships/read"}}, 201)["role_id"].(string)
	createPlatform := func(t *testing.T) string {
		return call(t, boss, bossToken, "POST", platformBase, uuid.NewString(), map[string]any{"email": b.accounts[1], "role_ids": []string{platformRole}}, 201)["invitation_id"].(string)
	}
	platformID := createPlatform(t)
	platformSecret := tokenFor(t, platformID, true)
	t.Run("platform_acceptance_independent_of_source_Tenant_and_audit_rollback", func(t *testing.T) {
		call(t, admin, adminToken, "POST", platformBase+"/"+platformID+"/accept", uuid.NewString(), acceptPayload(platformSecret), 403)
		fault(t)
		defer clearFault(t)
		call(t, recipient, recipientToken, "POST", platformBase+"/"+platformID+"/accept", uuid.NewString(), acceptPayload(platformSecret), 503)
		var members, pending, refs int
		if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM platform_memberships WHERE principal_id=$1),(SELECT count(*) FROM platform_invitations WHERE id=$2 AND status='pending'),(SELECT count(*) FROM platform_invitation_roles WHERE invitation_id=$2)`, target, platformID).Scan(&members, &pending, &refs) != nil || members != 0 || pending != 1 || refs != 1 {
			t.Fatal("Platform acceptance Audit failure left authority")
		}
	})
	if t.Failed() {
		return
	}
	var platformMember, platformToken string
	var platformClient *http.Client
	t.Run("platform_acceptance_receipt_and_real_BOSS_password_login", func(t *testing.T) {
		key := uuid.NewString()
		r := call(t, recipient, recipientToken, "POST", platformBase+"/"+platformID+"/accept", key, acceptPayload(platformSecret), 200)
		platformMember = r["membership_id"].(string)
		boundary := r["boundary"].(map[string]any)
		if boundary["type"] != "platform" || len(boundary) != 1 {
			t.Fatal("accepted Platform Membership contains Tenant")
		}
		raw, _ := json.Marshal(r)
		if strings.Contains(string(raw), platformSecret) || strings.Contains(string(raw), "invitation_token") {
			t.Fatal("acceptance returned invitation secret")
		}
		call(t, recipient, recipientToken, "POST", platformBase+"/"+platformID+"/accept", key, acceptPayload(platformSecret), 200)
		call(t, recipient, recipientToken, "POST", platformBase+"/"+platformID+"/accept", uuid.NewString(), acceptPayload(platformSecret), 409)
		c := b.browser(t)
		s, login, _ := b.request(t, c, "POST", "/auth/password/login", "", map[string]any{"account": b.accounts[1], "password": b.password, "audience": "boss", "boundary": map[string]string{"type": "platform"}}, nil)
		if s != 200 {
			t.Fatal("invited Platform Human cannot complete real BOSS login")
		}
		s, _, _ = b.request(t, c, "GET", "/iam/platform/members", login["access_token"].(string), nil, nil)
		if s != 200 {
			t.Fatal("invited Platform Role did not provide current authority")
		}
		platformClient, platformToken = c, login["access_token"].(string)
		bossClients[c] = true
		inv := createPlatform(t)
		call(t, recipient, recipientToken, "POST", platformBase+"/"+inv+"/accept", uuid.NewString(), acceptPayload(tokenFor(t, inv, true)), 409)
	})
	if t.Failed() {
		return
	}
	t.Run("BOSS_recipient_can_accept_Tenant_without_source_Tenant_membership", func(t *testing.T) {
		var current string
		err := b.owner.QueryRow(ctx, `SELECT id FROM tenant_memberships WHERE tenant_id=$1 AND principal_id=$2 AND status='active'`, tenant, target).Scan(&current)
		if err == nil {
			call(t, admin, adminToken, "DELETE", "/iam/tenants/"+tenant.String()+"/members/"+current+"?expected_version=1", uuid.NewString(), nil, 204)
		}
		inv := createTenant(t)
		r := call(t, platformClient, platformToken, "POST", tenantBase+"/"+inv+"/accept", uuid.NewString(), acceptPayload(tokenFor(t, inv, false)), 200)
		boundary := r["boundary"].(map[string]any)
		if boundary["type"] != "tenant" || boundary["tenant_id"] != tenant.String() {
			t.Fatal("BOSS acceptance derived target from source boundary")
		}
	})
	t.Run("Platform_removed_history_and_concurrent_acceptance", func(t *testing.T) {
		old := createPlatform(t)
		oldSecret := tokenFor(t, old, true)
		call(t, boss, bossToken, "DELETE", "/iam/platform/members/"+platformMember+"?expected_version=1", uuid.NewString(), nil, 204)
		call(t, recipient, recipientToken, "POST", platformBase+"/"+old+"/accept", uuid.NewString(), acceptPayload(oldSecret), 409)
		call(t, boss, bossToken, "POST", platformBase+"/"+old+"/cancel", uuid.NewString(), map[string]any{"expected_version": 1}, 204)
		fresh := createPlatform(t)
		secret := tokenFor(t, fresh, true)
		key := uuid.NewString()
		out := make(chan string, 3)
		var wg sync.WaitGroup
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r := call(t, recipient, recipientToken, "POST", platformBase+"/"+fresh+"/accept", key, acceptPayload(secret), 200)
				out <- r["membership_id"].(string)
			}()
		}
		wg.Wait()
		close(out)
		var next string
		for v := range out {
			if next == "" {
				next = v
			}
			if v == "" || v == platformMember || v != next {
				t.Fatal("Platform acceptance revived or duplicated Membership")
			}
		}
		var active, removed, audits, refs int
		if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM platform_memberships WHERE principal_id=$1 AND status='active'),(SELECT count(*) FROM platform_memberships WHERE id=$2 AND principal_id=$1 AND status='removed'),(SELECT count(*) FROM iam_audit_events WHERE boundary='platform' AND tenant_id IS NULL AND target_id=$3 AND action='iam.platform.invitation.accepted'),(SELECT count(*) FROM platform_invitation_roles WHERE invitation_id=$3)`, target, platformMember, fresh).Scan(&active, &removed, &audits, &refs) != nil || active != 1 || removed != 1 || audits != 1 || refs != 0 {
			t.Fatal("Platform acceptance boundary or atomicity failed")
		}
	})

}
