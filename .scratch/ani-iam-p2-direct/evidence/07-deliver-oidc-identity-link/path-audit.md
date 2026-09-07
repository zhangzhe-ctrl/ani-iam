# DP2-07 exact path audit

Fixed baseline:
`b52907dc4cb919dbbbe68768734e0a453b36b954`.

The exact feature payload below is commit
`33097ae02985ed569eba0f8176f184ce9f6ff849`.

Ticket-native allowed roots are `internal/biz/**`, `internal/data/**`,
`internal/service/**`, `migrations/**`, `configs/**`, `tests/**`, this ticket,
and this evidence directory. The Owner additionally approved exactly:

```text
go.mod
go.sum
internal/conf/conf.proto
internal/conf/conf.pb.go
internal/conf/validate.go
internal/conf/validate_test.go
cmd/server/app.go
cmd/server/app_test.go
internal/server/workload_identity.go
internal/server/workload_identity_test.go
```

The feature payload contains only:

```text
.scratch/ani-iam-p2-direct/issues/07-deliver-oidc-identity-link.md
.scratch/ani-iam-p2-direct/evidence/07-deliver-oidc-identity-link/bom.cdx.json
.scratch/ani-iam-p2-direct/evidence/07-deliver-oidc-identity-link/green.md
.scratch/ani-iam-p2-direct/evidence/07-deliver-oidc-identity-link/path-audit.md
.scratch/ani-iam-p2-direct/evidence/07-deliver-oidc-identity-link/red.md
.scratch/ani-iam-p2-direct/evidence/07-deliver-oidc-identity-link/review.md
.scratch/ani-iam-p2-direct/evidence/07-deliver-oidc-identity-link/supply-chain.md
.scratch/ani-iam-p2-direct/evidence/07-deliver-oidc-identity-link/verification.md
cmd/server/app.go
cmd/server/app_test.go
configs/config.yaml
go.mod
go.sum
internal/biz/authentication.go
internal/biz/authentication_test.go
internal/biz/oidc.go
internal/biz/oidc_test.go
internal/conf/conf.pb.go
internal/conf/conf.proto
internal/conf/validate.go
internal/conf/validate_test.go
internal/data/oidc_postgres.go
internal/data/oidc_provider.go
internal/data/oidc_provider_test.go
internal/data/oidc_redis.go
internal/data/queries/persistence.sql
internal/data/sqlcgen/models.go
internal/data/sqlcgen/persistence.sql.go
internal/data/sqlcgen/querier.go
internal/data/target_slice.go
internal/server/workload_identity.go
internal/server/workload_identity_test.go
internal/service/authentication.go
internal/service/authentication_test.go
internal/service/authorization.go
migrations/202609070001_oidc_identity.sql
migrations/atlas.sum
tests/integration/oidc_dex_test.go
tests/integration/oidc_persistence_test.go
tests/integration/oidc_state_test.go
tests/integration/password_action_test.go
tests/integration/vertical_slice_test.go
```

No `api/**`, `deploy/**`, ANI worktree, DP2-08+, push, deployment, live traffic,
destructive data operation, reset, stash, rebase, amend or force operation is in
the payload. Generated files are paired with their authorized source inputs and
were reproduced with fixed generators. The exact index will be compared to
this list before commit; staging uses explicit paths, never `git add -A`.
