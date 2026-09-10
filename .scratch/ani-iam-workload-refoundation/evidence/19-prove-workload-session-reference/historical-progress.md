> Historical progress snapshots. Current status and acceptance are in README.md. Earlier pending/block statements are superseded by the recorded user acceptance.

# WR-19 execution evidence

Status: **in progress**. Authoritative issue19 remains claimed in the original IAM repository. The public SDK and independently running example have passed their recorded gates. The complete WR-19 reference chain and Kubernetes resource connection remain **not_verified**.

## Verified slice

Bootstrap is implemented in the dedicated `/home/chabking/workspace/ani-iam-wr19` worktree through the formal IAM `provision-workloads` command. It uses an independently authenticated provisioner role, a reviewed manifest digest and pinned CA/environment, single consumption per environment, exact Workload/Binding/Grant creation, an immutable receipt and attributable Audit in one transaction. It cannot update an existing initialization or grant itself runtime authority.

- WR18 restoration: 174 source files verified, five renamed/deleted old files remain absent. Current 67 authoritative documents copied as source snapshots only. See worktree-restoration.json.
- Bootstrap SQL generator: two runs produced identical outputs. Initial success tests exposed an additional WR17 contract requirement during source review: a different manifest ID must not permit a second initialization of the same environment. A database unique constraint and environment lock now enforce that requirement; concurrent different-intent and no-state-on-failure tests were added. Earlier run results are retained as historical subsets, not final acceptance.
- Final bootstrap slice: run `wr19-20260910T032113Z-4dab8b8b`, exact source archive in its source.json; all root unit tests and vet passed. Targeted PostgreSQL/formal-process integration under race passed, including 23 named results, concurrent same/different intent, expired/new and consumed retry, audit rollback, runtime/provisioner separation, immutable recovery receipt, WR18 formal runtime and no-RLS relations. Six newly registered dependency containers were terminated. See bootstrap-final-results.json.
- Prior affected Human/Session/Tenant regression and unit race also passed; see bootstrap-regression-results.json. These results apply to the recorded bootstrap snapshot, not to later unfinished invocation changes.

## Current implementation

The IAM invocation slice is frozen in implementation-scope.json and invocation-contract.md. Public API and SDK modules use unpublished candidate `v0.1.0-rc.1`; SDK depends only on the public API, gRPC/protobuf and standard libraries. No absolute replacement is present in delivery modules. Controlled remote go.work files bind the candidate versions to exact source snapshots. For `go mod tidy`, which ignores workspace replacements, a disposable remote modfile supplies source overrides; those overrides are removed before reviewed dependency locks return.

- Formal IAM bootstrap/login/WAT/delegation/receiver verification passed 28 named results in `wr19-20260910T041332Z-370860ce`: distinct caller and subject; missing/forged/type-confused credentials; issuer/audience codec gates in earlier unit tests; peer, target, Tenant, subject, resource, source/mode and digest substitution; current Workload/Binding/Grant, receiver own verification Grant, permission and Human Session revocation; binding-version renewal; mint audit failure without credential release. See invocation-results.json. Two dependency containers terminated.
- SDK standalone module normal/race tests, vet and no-IAM-internal dependency graph passed; see sdk-standalone-results.json.
- Public example caller and receiver binaries ran against the formal bootstrapped IAM in `wr19-20260910T041950Z-de92a694`; example module build/vet and 29 formal test results passed. It verifies an example-owned inventory and returns a harmless acknowledgement, not a terminal Session. See example-runtime-results.json. This is independent of the required formal ANI/Session resource proof.
- Session worktree `/home/chabking/workspace/ani-session-gateway-wr19` created from the fixed clean baseline. Exact 14-path creation-adapter slice is frozen in session-implementation-scope.json, with 82 explicit baseline transfer inputs in session-source-inputs.json. SDK target mapping, protected handler boundary and isolated Kubernetes config loader passed targeted race tests/vet and binary build in wr19-20260910T042451Z-f723f173 (session-adapter-results.json); formal protected runtime is not activated while the lifecycle decision is pending.
- ANI WR19 worktree created from fixed clean baseline; its 32-path reference slice is frozen in ani-implementation-scope.json. Public IAM SDK authorization, authenticated normalized Session calling, matching four-operation registry and Session-owned idempotency retry are implemented. Targeted adapter/Gateway tests, vet and formal binary build passed in `wr19-20260910T044533Z-ec1ad5e6`; see ani-adapter-results.json. The formal three-process resource chain remains unverified. All original worktrees and WR18 source remain protected; no staging, commits, pushes, tags, PRs or publication occurred.

## Environment and decisions

- Heavy work only on SSH ubuntu, serial GOMAXPROCS=2 / -p=2, fixed tools. A later Network race overlapped the end of one short generation/module-check run. That run completed; the runner now rejects a new task command with exit 76 when another Go build/test is active, in addition to its IAM lock and memory check. No Network/kc062 resources were changed.
- User selected SSH ani for real Kubernetes. Cluster UID and both CF01 namespace UIDs match the documented test environment; cluster-preflight.json records 25 protected existing resources. No cluster writes have been performed. The old kind proposal is superseded and must not execute.
- Exact proposed additions are in k8s-proposal.json / k8s-environment-proposal.md: 11 objects for two new Tenant namespaces/exec Pods and one restricted Session service account. Confirmation for those precise operations is pending; host/environment use is already accepted.
- Established-connection policy is still pending user response. The proposed bounded periodic check is not accepted by elapsed time and has no enabled RPC or grant. D01/D02 admission/online verification remain accepted.

## Recovery and continuation

All dependency containers of completed bootstrap tests were stopped by registered IDs. Source snapshots, sanitized logs/results and private remote fixture files are retained. Never copy private fixture file values into evidence. SSH uncertainty requires checking the same run's PID/log/exit; a known nonzero exit can be repaired in a new recorded run. Pending environment or lifecycle decisions pause only their dependent paths. Continue implementation without lowering the complete WR19 acceptance matrix or claiming this slice completes M1.

## Kubernetes topology refinement

The unapplied 11-object proposal is superseded by [v2](k8s-proposal-v2.json), 16 objects. Gateway owns Deployment-backed metadata and has a running read-only status observer; bare Pod resource references are unsupported. Each fixture is therefore a single-replica Deployment. Gateway gets its own `wr19-gateway-reader` SA with only named Deployment get in the two new Tenant namespaces. Session alone gets pod list/get and exec create. Resource limits are unchanged. Direct ubuntu→10.10.1.66:6443 TCP connectivity passed; authenticated API behavior remains not_verified. Exact operations are awaiting user acceptance; no cluster mutation has occurred.

## Independent regression checkpoint

After the public API/SDK and Gateway adapter integration, `wr19-20260910T045819Z-886165e6` passed IAM root unit tests/vet, SDK race/vet, the independently executed public example, 50 named real PG/Redis/formal IAM results (no failures/skips), and all Session module unit tests/vet. See independent-regression-results.json. All seven registered dependency containers terminated and were verified absent. Example caller/receiver PID, binary hash and zero exit codes are now recorded.

The preceding run failed two legacy password-action positive tests because their hand-built X.509 fixture had zero validity dates. The test-only allowed path was recorded before correcting the fixture. Runtime current-chain validation and its expired-connection negative test remain unchanged. The successful source archive is `24d347c260f65f5a2bc4d7f8946f543e642a1786c80c539e1a5735762086c119`.

Formal resource/owner-schema/request inputs are pinned in formal-chain-inputs.json. They have not been executed. Remaining dependent work: user acceptance of the exact v2 cluster operations and existing-connection policy, Session formal mTLS/lifecycle wiring, the three-process real exec harness, accepted lifecycle/rotation/replay/outage gates and final required repository checks. No WR20 work has started.

## Required ANI owner gate preparation

The explicit read-only ANI input list now includes the unchanged sources/docs required by `make test`, architecture, documentation-entrypoint and Services boundary checks. Product writes remain limited to the same 32 paths. The remote validator uses a fresh uncommitted Git object database containing only 14 individually hashed public commit/tree/OpenAPI objects from its two existing pins (`0cedae8...` and `bde4ea7...`); no history, branch, index, credentials, add/commit or publication is transferred/performed.

The first archive preparation exposed Git's quoted Chinese filename output; the runner now uses NUL-delimited path lists. Its next run (`wr19-20260910T050640Z-a8f3d830`) reached the unchanged generation gate and failed because a second fixed historical input was missing. That input was subsequently added to the allowlist; no validator or policy assertion was weakened.

ANI owner gates then passed in `wr19-20260910T050810Z-73666686`: `make test` (architecture/auth/generation + 49 Go packages + Python syntax), `make validate-doc-entrypoints`, Services boundary checker with its four existing accepted warnings, and WR19 registry --check. Only the deliberately overridden remote go.work changed; no source, module lock or generated file drift was found. See ani-owner-gates-results.json. Full `make validate-services` was not run: no Services business/API source is changed by this Core Gateway slice.

## Historical user checkpoint (resolved by the following user reply)

The same two explicit decisions remain pending across three consecutive Goal turns. The independent implementation/tests and required ANI owner gate work have been preserved; all last verified runners/processes/registered containers are absent. Goal execution is blocked awaiting exact Kubernetes v2 operations and the established-connection policy. The full objective is unchanged; WR19 remains claimed, never resolved. See blocked-audit.json for the revalidated state and dependent restart steps.

## Current continuation after user acceptance

The user accepted the exact v2 cluster operations and the 30-second / 2-second connection policy, and requested retention of reusable fixtures. See user-acceptance-20260910.json. The prior blocked-audit is historical and resolved; WR19 remains claimed while its full-chain acceptance is executed.

All 16 declared objects were created after cluster/namespace UID and name-absence checks. Both Deployment pods are Running/Ready; declared and controller-created UIDs are recorded in k8s-create-events.jsonl and k8s-retained-resources.json. Reusable namespaces/Deployments/NetworkPolicies/RBAC/ServiceAccounts are retained. Temporary Kubernetes access uses newly minted one-hour tokens, held privately on ubuntu.

The earlier direct TCP probe is no longer current: direct ubuntu→API traffic now times out. A task-owned loopback SSH SOCKS route through ani preserves the original API URL and CA validation. Authenticated allowed/denied checks passed for both restricted ServiceAccounts; see k8s-access-preflight.json and k8s-tunnel.json. This is transport plumbing, not IAM identity or full business-chain evidence.

The accepted lifecycle implementation passed the source-specific gates in continuation-results.json: root IAM tests/vet, SDK race/vet, 37 named real PostgreSQL/Redis/formal IAM tests, all Session component tests/vet, targeted manager/connection/gRPC race, and formal Session build. The real Kubernetes three-process harness and later protected-constructor/admin-loopback refinements are being validated in subsequent runs. Do not promote this intermediate result to full WR19 completion.
