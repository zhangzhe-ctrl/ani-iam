//go:build integration

package integration_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestWR22FormalTenantAudit(t *testing.T) {
	e := newWR22Environment(t)
	client, token, _, _ := e.login(t, 0)
	other, otherToken, _, _ := e.login(t, 1)
	ctx := context.Background()
	now := time.Now().UTC()
	ids := []uuid.UUID{mustV7(t), mustV7(t), mustV7(t)}
	for index, id := range ids {
		actorIndex := 0
		if index == 2 {
			actorIndex = 1
		}
		days := 180 + index
		_, err := e.owner.Exec(ctx, `INSERT INTO iam_audit_events(tenant_id,event_id,actor_id,authentication_method,boundary,action,target_type,target_id,target_version,result,reason,request_id,correlation_id,decision_id,source_service,occurred_at,recorded_at) VALUES($1,$2,$3,'password','tenant','wr22.audit.retention','audit_fixture',$2,1,'succeeded','wr22_synthetic_time',$2::uuid::text,$2::uuid::text,$2::uuid::text,'ani-iam',$4,$4)`, referenceTenants[actorIndex], id, e.humans[actorIndex], now.Add(-time.Duration(days)*24*time.Hour))
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) {
				t.Fatalf("isolated audit history fixture failed: SQLSTATE=%s constraint=%s", pgErr.Code, pgErr.ConstraintName)
			}
			t.Fatal("isolated audit history fixture failed")
		}
	}
	request := func(t *testing.T, c *http.Client, bearer, path string, want int) map[string]any {
		t.Helper()
		code, r, response := e.request(t, c, "GET", path, bearer, nil, nil)
		if code != want {
			t.Fatalf("audit query status=%d reason=%s want=%d", code, r["code"], want)
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("audit response is cacheable")
		}
		return r
	}
	t.Run("real_query_includes_180_and_181_day_records", func(t *testing.T) {
		for _, id := range ids[:2] {
			v := request(t, client, token, "/iam/audit-events/"+id.String(), 200)
			if v["event_id"] != id.String() || v["result"] != "success" || v["source_service"] != "ani-iam" || v["actor_type"] != "principal" {
				t.Fatal("audit public fields differ")
			}
			at, err := time.Parse(time.RFC3339Nano, v["recorded_at"].(string))
			if err != nil || at.After(now.Add(-180*24*time.Hour)) {
				t.Fatal("old audit record time differs")
			}
		}
	})
	t.Run("tenant_filter_cursor_and_foreign_id_are_isolated", func(t *testing.T) {
		base := "/iam/audit-events?action=wr22.audit.retention&result=success&limit=1"
		first := request(t, client, token, base, 200)
		if len(first["items"].([]any)) != 1 || first["next_cursor"] == nil {
			t.Fatal("audit first page invalid")
		}
		cursor := first["next_cursor"].(string)
		second := request(t, client, token, base+"&cursor="+cursor, 200)
		if len(second["items"].([]any)) != 1 || first["items"].([]any)[0].(map[string]any)["event_id"] == second["items"].([]any)[0].(map[string]any)["event_id"] {
			t.Fatal("audit pagination repeated or lost record")
		}
		request(t, other, otherToken, base+"&cursor="+cursor, 400)
		request(t, client, token, "/iam/audit-events?action=changed&result=success&cursor="+cursor, 400)
		request(t, client, token, "/iam/audit-events/"+ids[2].String(), 404)
		request(t, other, otherToken, "/iam/audit-events/"+ids[0].String(), 404)
		request(t, client, "", "/iam/audit-events", 401)
	})
	t.Run("actual_runtime_role_cannot_update_or_delete", func(t *testing.T) {
		pool := mustPool(t, e.iam.config.Runtime.Postgresql.Dsn)
		defer pool.Close()
		for _, query := range []string{`UPDATE iam_audit_events SET reason='forbidden' WHERE tenant_id=$1 AND event_id=$2`, `DELETE FROM iam_audit_events WHERE tenant_id=$1 AND event_id=$2`} {
			_, err := pool.Exec(ctx, query, referenceTenants[0], ids[0])
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
				t.Fatal("runtime audit mutation was not denied by database privilege")
			}
		}
		request(t, client, token, "/iam/audit-events/"+ids[0].String(), 200)
	})
	recordReference(t, e.run, map[string]any{"check": "audit_history_query", "days": []int{180, 181}, "clock_mode": "controlled fixture timestamps", "runtime_update_delete": "denied", "production_elapsed_retention": "not_verified"})
}
