package cp0_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	datapkg "github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

const (
	cp0RealDependenciesEnv = "ANI_IAM_CP0_REAL_DEPS"
	frozenANICommit        = "963bc88836c54a1b09cf100b37eb2f2cb2a5a4be"
	frozenAtlasSumSHA256   = "175516a68751bc2941f9a3154b6933dacddd74be10b435addef122623d6ac1af"
	postgresImage          = "postgres@sha256:5660c2cbfea50c7a9127d17dc4e48543eedd3d7a41a595a2dfa572471e37e64c"
	redisImage             = "redis@sha256:ff02b58f971e7d7d156a1267e283fcbbeee91773b6aa36c49dac28ecfe28eadf"
	postgresPassword       = "cp0-04-runtime-only"
	migrationOwner         = "ani"
	runtimeRole            = "ani_app_user"
	starterPlan            = "00000000-0000-0000-0000-000000000001"
	tenantA                = "00000000-0000-4000-8000-00000000000a"
	tenantB                = "00000000-0000-4000-8000-00000000000b"
	userA                  = "10000000-0000-4000-8000-00000000000a"
	userB                  = "10000000-0000-4000-8000-00000000000b"
	platformUser           = "10000000-0000-4000-8000-00000000000c"
)

func TestLegacyStorageRealDependencies(t *testing.T) {
	if os.Getenv(cp0RealDependenciesEnv) != "1" {
		t.Skip("set ANI_IAM_CP0_REAL_DEPS=1 to run the pinned PostgreSQL/Redis integration gate")
	}
	t.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	postgresContainer, postgresAddress := startPostgresContainer(t, ctx)
	migrationFiles := replayFrozenMigrations(t, ctx, postgresContainer)
	if got := filepath.Base(migrationFiles[len(migrationFiles)-1]); got != "20260831_001_async_tasks_rls_fix.sql" {
		t.Fatalf("migration head = %q", got)
	}
	redisContainer, redisAddress := startRedisContainer(t, ctx)
	_ = redisContainer

	adminDSN := fmt.Sprintf("postgres://%s:%s@%s/ani?sslmode=disable", migrationOwner, postgresPassword, postgresAddress)
	adminPool := openAdminPool(t, ctx, adminDSN)
	seedLegacyAuthRows(t, ctx, adminPool)
	if _, err := adminPool.Exec(ctx, `ALTER ROLE ani_app_user PASSWORD '`+postgresPassword+`'`); err != nil {
		t.Fatalf("set isolated runtime role password: %v", err)
	}

	runtimeDSN := fmt.Sprintf("postgres://%s:%s@%s/ani?sslmode=disable", runtimeRole, postgresPassword, postgresAddress)
	postgres, err := datapkg.OpenLegacyPostgres(ctx, datapkg.LegacyPostgresConfig{DSN: runtimeDSN, ExpectedRole: runtimeRole})
	if err != nil {
		t.Fatalf("OpenLegacyPostgres() error = %v", err)
	}
	t.Cleanup(postgres.Close)

	namespace := "cp0:04:" + uuid.NewString() + ":"
	redisAdapter, err := datapkg.OpenLegacyRedis(ctx, datapkg.LegacyRedisConfig{Address: redisAddress, Namespace: namespace})
	if err != nil {
		t.Fatalf("OpenLegacyRedis() error = %v", err)
	}
	t.Cleanup(func() { _ = redisAdapter.Close() })

	t.Run("restricted runtime role and real RLS", func(t *testing.T) {
		assertRestrictedRole(t, postgres.RoleInfo())
		assertSuperuserRejected(t, ctx, adminDSN)
		testLegacyRLS(t, ctx, postgres)
	})

	t.Run("redis namespace TTL and state", func(t *testing.T) {
		testLegacyRedisSemantics(t, ctx, redisAdapter)
	})

	t.Run("dependency failures do not partially succeed", func(t *testing.T) {
		testCoordinatedRevocation(t, ctx, adminPool, runtimeDSN, redisAddress, namespace, postgres, redisAdapter)
	})
}

func startPostgresContainer(t *testing.T, ctx context.Context) (testcontainers.Container, string) {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        postgresImage,
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       "ani",
				"POSTGRES_PASSWORD": postgresPassword,
				"POSTGRES_USER":     migrationOwner,
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(45 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start pinned PostgreSQL container: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := container.Terminate(cleanupCtx); err != nil {
			t.Errorf("terminate isolated PostgreSQL container: %v", err)
		}
	})
	return container, containerAddress(t, ctx, container, "5432/tcp")
}

func startRedisContainer(t *testing.T, ctx context.Context) (testcontainers.Container, string) {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        redisImage,
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForLog("Ready to accept connections").WithStartupTimeout(30 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start pinned Redis container: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := container.Terminate(cleanupCtx); err != nil {
			t.Errorf("terminate isolated Redis container: %v", err)
		}
	})
	return container, containerAddress(t, ctx, container, "6379/tcp")
}

func containerAddress(t *testing.T, ctx context.Context, container testcontainers.Container, port string) string {
	t.Helper()
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	mapped, err := container.MappedPort(ctx, port)
	if err != nil {
		t.Fatalf("container mapped port %s: %v", port, err)
	}
	return net.JoinHostPort(host, mapped.Port())
}

func replayFrozenMigrations(t *testing.T, ctx context.Context, container testcontainers.Container) []string {
	t.Helper()
	aniRoot := aniRepositoryRoot(t)
	atlasSum := gitObject(t, aniRoot, "repo/deploy/migrations/atlas.sum")
	if got := fmt.Sprintf("%x", sha256.Sum256(atlasSum)); got != frozenAtlasSumSHA256 {
		t.Fatalf("frozen atlas.sum SHA-256 = %s", got)
	}

	cmd := exec.CommandContext(ctx, "git", "-C", aniRoot, "ls-tree", "-r", "--name-only", frozenANICommit, "--", "repo/deploy/migrations")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("list frozen migrations: %v", err)
	}
	var files []string
	for _, path := range strings.Fields(string(output)) {
		if strings.HasSuffix(path, ".sql") {
			files = append(files, path)
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("frozen migration inventory is empty")
	}

	for _, path := range files {
		contents := gitObject(t, aniRoot, path)
		hostPath := filepath.Join(t.TempDir(), filepath.Base(path))
		if err := os.WriteFile(hostPath, contents, 0o600); err != nil {
			t.Fatalf("materialize %s: %v", path, err)
		}
		containerPath := "/tmp/cp0-migrations/" + filepath.Base(path)
		if err := container.CopyFileToContainer(ctx, hostPath, containerPath, 0o600); err != nil {
			t.Fatalf("copy %s: %v", path, err)
		}
		exitCode, reader, err := container.Exec(ctx, []string{"psql", "-v", "ON_ERROR_STOP=1", "-U", migrationOwner, "-d", "ani", "-f", containerPath})
		combined, _ := io.ReadAll(reader)
		if err != nil || exitCode != 0 {
			t.Fatalf("replay %s: exit=%d err=%v output=%s", path, exitCode, err, combined)
		}
	}
	return files
}

func aniRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve integration test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", "ANI"))
}

func gitObject(t *testing.T, aniRoot, path string) []byte {
	t.Helper()
	cmd := exec.Command("git", "-C", aniRoot, "show", frozenANICommit+":"+path)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("read fixed Git object %s: %v", path, err)
	}
	return output
}

func openAdminPool(t *testing.T, ctx context.Context, dsn string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open isolated migration owner pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping isolated migration owner pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seedLegacyAuthRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO tenants (id, name, display_name, plan_id) VALUES
			($1, 'cp0-tenant-a', 'CP0 Tenant A', $3),
			($2, 'cp0-tenant-b', 'CP0 Tenant B', $3)
	`, tenantA, tenantB, starterPlan)
	if err != nil {
		t.Fatalf("seed isolated legacy tenants: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, tenant_id, username, email, status) VALUES
			($1, $2, 'tenant-a-user', 'a@example.test', 'active'),
			($3, $4, 'tenant-b-user', 'b@example.test', 'active'),
			($5, NULL, 'platform-user', 'platform@example.test', 'active')
	`, userA, tenantA, userB, tenantB, platformUser)
	if err != nil {
		t.Fatalf("seed isolated legacy auth rows: %v", err)
	}
}

func assertRestrictedRole(t *testing.T, role datapkg.LegacyRoleInfo) {
	t.Helper()
	if role.Name != runtimeRole || !role.CanLogin || role.Superuser || role.BypassRLS || role.CreateRole || role.CreateDB {
		t.Fatalf("runtime role is not restricted: %+v", role)
	}
}

func assertSuperuserRejected(t *testing.T, ctx context.Context, adminDSN string) {
	t.Helper()
	admin, err := datapkg.OpenLegacyPostgres(ctx, datapkg.LegacyPostgresConfig{DSN: adminDSN, ExpectedRole: migrationOwner})
	if admin != nil {
		admin.Close()
	}
	if !errors.Is(err, datapkg.ErrUnsafeRuntimeRole) {
		t.Fatalf("superuser OpenLegacyPostgres() error = %v", err)
	}
}

func testLegacyRLS(t *testing.T, ctx context.Context, postgres *datapkg.LegacyPostgres) {
	t.Helper()
	expiresAt := time.Now().Add(24 * time.Hour).UTC()
	err := postgres.WithTenantTx(ctx, tenantA, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO refresh_tokens (tenant_id, user_id, token_hash, roles, expires_at) VALUES ($1,$2,'refresh-a',ARRAY['user'],$3)`, tenantA, userA, expiresAt); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO api_keys (tenant_id, user_id, name, key_hash, key_prefix, scopes) VALUES ($1,$2,'key-a','hash-a','ani_dev_a',ARRAY['scope:models:read'])`, tenantA, userA)
		return err
	})
	if err != nil {
		t.Fatalf("allowed tenant operation: %v", err)
	}

	err = postgres.WithTenantTx(ctx, tenantA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO refresh_tokens (tenant_id, user_id, token_hash, roles, expires_at) VALUES ($1,$2,'cross-tenant',ARRAY['user'],$3)`, tenantB, userB, expiresAt)
		return err
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("cross-Tenant RLS error = %v", err)
	}

	assertTenantVisibleCount(t, ctx, postgres, tenantA, "refresh_tokens", 1)
	assertTenantVisibleCount(t, ctx, postgres, tenantB, "refresh_tokens", 0)
	assertTenantVisibleCount(t, ctx, postgres, tenantA, "api_keys", 1)
	assertTenantVisibleCount(t, ctx, postgres, tenantB, "api_keys", 0)

	err = postgres.WithPlatformTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO refresh_tokens (tenant_id, user_id, token_hash, roles, expires_at) VALUES (NULL,$1,'platform-refresh',ARRAY['platform-admin'],$2)`, platformUser, expiresAt)
		return err
	})
	if err != nil {
		t.Fatalf("platform refresh token operation: %v", err)
	}
}

func assertTenantVisibleCount(t *testing.T, ctx context.Context, postgres *datapkg.LegacyPostgres, tenantID, table string, want int) {
	t.Helper()
	if table != "refresh_tokens" && table != "api_keys" {
		t.Fatalf("test table not allowlisted: %s", table)
	}
	var got int
	err := postgres.WithTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&got)
	})
	if err != nil {
		t.Fatalf("count %s for tenant %s: %v", table, tenantID, err)
	}
	if got != want {
		t.Fatalf("count %s for tenant %s = %d, want %d", table, tenantID, got, want)
	}
}

func testLegacyRedisSemantics(t *testing.T, ctx context.Context, redisAdapter *datapkg.LegacyRedis) {
	t.Helper()
	if err := redisAdapter.SetOIDCState(ctx, "state-a", []byte(`{"tenant_name":"cp0-tenant-a"}`), 10*time.Minute); err != nil {
		t.Fatalf("SetOIDCState() error = %v", err)
	}
	assertTTLRange(t, ctx, redisAdapter, "oidc:state:state-a", 9*time.Minute, 10*time.Minute)
	value, err := redisAdapter.ConsumeOIDCState(ctx, "state-a")
	if err != nil || string(value) != `{"tenant_name":"cp0-tenant-a"}` {
		t.Fatalf("ConsumeOIDCState() = %q, %v", value, err)
	}
	if _, err := redisAdapter.ConsumeOIDCState(ctx, "state-a"); !errors.Is(err, datapkg.ErrCacheMiss) {
		t.Fatalf("second ConsumeOIDCState() error = %v", err)
	}

	if err := redisAdapter.SetJWTBlocklist(ctx, "redis-only", time.Hour); err != nil {
		t.Fatalf("SetJWTBlocklist() error = %v", err)
	}
	assertTTLRange(t, ctx, redisAdapter, "jwt:blocklist:redis-only", 59*time.Minute, time.Hour)
	for want := int64(1); want <= 2; want++ {
		got, err := redisAdapter.IncrementAPIKeyRate(ctx, "hash-a", time.Minute)
		if err != nil || got != want {
			t.Fatalf("IncrementAPIKeyRate() = %d, %v, want %d", got, err, want)
		}
	}
	assertTTLRange(t, ctx, redisAdapter, "api-key:rate:hash-a", 50*time.Second, time.Minute)
	keys, err := redisAdapter.NamespaceKeys(ctx)
	if err != nil {
		t.Fatalf("NamespaceKeys() error = %v", err)
	}
	sort.Strings(keys)
	want := []string{"api-key:rate:hash-a", "jwt:blocklist:redis-only"}
	if strings.Join(keys, "\n") != strings.Join(want, "\n") {
		t.Fatalf("namespace keys = %v, want %v", keys, want)
	}
}

func assertTTLRange(t *testing.T, ctx context.Context, redisAdapter *datapkg.LegacyRedis, key string, minimum, maximum time.Duration) {
	t.Helper()
	ttl, err := redisAdapter.TTL(ctx, key)
	if err != nil {
		t.Fatalf("TTL(%s) error = %v", key, err)
	}
	if ttl < minimum || ttl > maximum {
		t.Fatalf("TTL(%s) = %s, want %s..%s", key, ttl, minimum, maximum)
	}
}

func testCoordinatedRevocation(t *testing.T, ctx context.Context, adminPool *pgxpool.Pool, runtimeDSN, redisAddress, namespace string, postgres *datapkg.LegacyPostgres, redisAdapter *datapkg.LegacyRedis) {
	t.Helper()
	storage, err := datapkg.NewLegacyStorage(postgres, redisAdapter)
	if err != nil {
		t.Fatalf("NewLegacyStorage() error = %v", err)
	}
	if err := storage.RevokeToken(ctx, "coordinated-success", 5*time.Minute); err != nil {
		t.Fatalf("RevokeToken(success) error = %v", err)
	}
	assertRevocationState(t, ctx, adminPool, redisAdapter, "coordinated-success", true)

	closedRedis, err := datapkg.OpenLegacyRedis(ctx, datapkg.LegacyRedisConfig{Address: redisAddress, Namespace: namespace})
	if err != nil {
		t.Fatalf("open Redis failure probe: %v", err)
	}
	if err := closedRedis.Close(); err != nil {
		t.Fatalf("close Redis failure probe: %v", err)
	}
	redisFailureStorage, err := datapkg.NewLegacyStorage(postgres, closedRedis)
	if err != nil {
		t.Fatalf("NewLegacyStorage(redis failure) error = %v", err)
	}
	err = redisFailureStorage.RevokeToken(ctx, "redis-unavailable", time.Minute)
	if !errors.Is(err, datapkg.ErrDependencyUnavailable) || err.Error() != "legacy storage dependency unavailable: redis write key" {
		t.Fatalf("Redis unavailable error = %v", err)
	}
	assertRevocationState(t, ctx, adminPool, redisAdapter, "redis-unavailable", false)

	closedPostgres, err := datapkg.OpenLegacyPostgres(ctx, datapkg.LegacyPostgresConfig{DSN: runtimeDSN, ExpectedRole: runtimeRole})
	if err != nil {
		t.Fatalf("open PostgreSQL failure probe: %v", err)
	}
	closedPostgres.Close()
	postgresFailureStorage, err := datapkg.NewLegacyStorage(closedPostgres, redisAdapter)
	if err != nil {
		t.Fatalf("NewLegacyStorage(postgres failure) error = %v", err)
	}
	err = postgresFailureStorage.RevokeToken(ctx, "postgres-unavailable", time.Minute)
	if !errors.Is(err, datapkg.ErrDependencyUnavailable) || err.Error() != "legacy storage dependency unavailable: postgres begin transaction" {
		t.Fatalf("PostgreSQL unavailable error = %v", err)
	}
	assertRevocationState(t, ctx, adminPool, redisAdapter, "postgres-unavailable", false)
}

func assertRevocationState(t *testing.T, ctx context.Context, adminPool *pgxpool.Pool, redisAdapter *datapkg.LegacyRedis, jti string, want bool) {
	t.Helper()
	var postgresState bool
	if err := adminPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM jwt_blocklist WHERE jti=$1)`, jti).Scan(&postgresState); err != nil {
		t.Fatalf("query PostgreSQL revocation %s: %v", jti, err)
	}
	redisState, err := redisAdapter.IsJWTBlocked(ctx, jti)
	if err != nil {
		t.Fatalf("query Redis revocation %s: %v", jti, err)
	}
	if postgresState != want || redisState != want {
		t.Fatalf("revocation %s state: postgres=%t redis=%t want=%t", jti, postgresState, redisState, want)
	}
}
