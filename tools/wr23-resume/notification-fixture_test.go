//go:build wr23fixture

package data

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This WR23-owned build overlay runs the fixed Notification owner migration
// and role setup. It never edits the dependency tree or its historical guards.
// The production process receives only the restricted runtime credential.
func TestWR23PrepareRestrictedDatabase(t *testing.T) {
	run := os.Getenv("WR23_RUN_DIR")
	if !strings.HasPrefix(run, "/home/ubuntu/workspace/ani-iam-runs/wr23-resume-") {
		t.Fatal("dedicated WR23 run required")
	}
	read := func(name string) string {
		path := os.Getenv(name)
		rel, err := filepath.Rel(filepath.Join(run, "private"), path)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			t.Fatal("private input required")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal("private input unavailable")
		}
		return string(raw)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, read("WR23_NOTIFICATION_OWNER_FILE"))
	if err != nil {
		t.Fatal("owner connection failed")
	}
	defer pool.Close()
	var count int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN ('r','p')").Scan(&count); err != nil || count != 0 {
		t.Fatal("only an empty task database is accepted")
	}
	if err = ApplyMigrations(ctx, pool); err != nil {
		t.Fatal("exact schema bootstrap failed")
	}
	// The finite WR23 environment bootstrap creates this role using its own
	// cluster owner. This database owner has no role-management privilege.
	var safe bool
	if err = pool.QueryRow(ctx, `SELECT rolcanlogin AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolinherit AND NOT rolreplication AND NOT rolbypassrls FROM pg_roles WHERE rolname='wr23_notify_runtime'`).Scan(&safe); err != nil || !safe {
		t.Fatal("restricted role was not provisioned")
	}
	_, err = pool.Exec(ctx, `REVOKE ALL ON SCHEMA public FROM PUBLIC; GRANT USAGE ON SCHEMA public TO wr23_notify_runtime;
 GRANT SELECT ON ALL TABLES IN SCHEMA public TO wr23_notify_runtime;
 GRANT INSERT, UPDATE ON notifications, deliveries, delivery_attempts, iam_password_action_payloads, iam_tenant_invitation_payloads, iam_platform_invitation_payloads, iam_email_verification_payloads TO wr23_notify_runtime;
 GRANT INSERT ON email_attempt_evidence TO wr23_notify_runtime;
 REVOKE TEMPORARY ON DATABASE wr23_notification FROM PUBLIC`)
	if err != nil {
		t.Fatal("restricted privileges failed")
	}
	// The schema contract intentionally includes ACLs. The owner seals the exact
	// fresh catalog after provisioning, before the application may start.
	digest, err := catalogDigest(ctx, pool)
	if err != nil {
		t.Fatal("catalog digest failed")
	}
	if _, err = pool.Exec(ctx, "UPDATE notification_schema_epoch SET catalog_sha256=$1", digest); err != nil {
		t.Fatal("catalog seal failed")
	}
	runtime, err := pgxpool.New(ctx, read("WR23_NOTIFICATION_RUNTIME_FILE"))
	if err != nil {
		t.Fatal("runtime connection failed")
	}
	defer runtime.Close()
	if err = SchemaReady(ctx, runtime); err != nil {
		t.Fatal("restricted runtime schema validation failed")
	}
	for _, sql := range []string{"CREATE TABLE denied(id int)", "UPDATE notification_schema_epoch SET epoch=epoch", "TRUNCATE notifications"} {
		if _, err = runtime.Exec(ctx, sql); err == nil {
			t.Fatal("restricted role gained owner authority")
		}
	}
}
