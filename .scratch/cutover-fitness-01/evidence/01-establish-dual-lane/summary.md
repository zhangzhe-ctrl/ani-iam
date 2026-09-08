# CUTOVER-FITNESS-01 / issue 01 summary

`environment-established: fail`

The registry, storage, and network foundation gates passed. The run stopped before runtime deployment because the exact current ANI Auth image could not be built from commit `56a5f0b493c8404a024a92647d93f2ba2f7daf35`: its unchanged Dockerfile's `go build` reported that updates to `go.mod` were required and instructed `go mod tidy`. Modifying the ANI Dockerfile, module files, or detached source would violate the immutable-source and Allowed-path boundary, so no workaround was applied.

## Results

- Registry: `pass`; probe digest `sha256:427e694db951cb2f20c071515f59009e47ef188c5bfd30a837094f780ae6cf88` was pulled directly by all three Ready nodes without an imagePullSecret.
- Storage: `pass`; both fixed `ani-block` PVCs bound to `Retain` PVs and completed an actual write/read round trip.
- Network: `pass`; both namespaces have default-deny plus exact DNS/same-lane policy; same-lane TCP/UDP passed and both cross-lane directions failed as expected while DNS resolved.
- Target ani-iam local image build: `pass`; it was not pushed after the stop condition.
- Current Auth image build: `fail`.
- Current/target Gateway builds, runtime readiness, migration/seed, fixed smoke, and target exactly-one `CheckPermission`: `not_verified`.

## Safety and recovery

No Kubernetes Secret or runtime Credential was created, read, copied, printed, or recorded. No shared namespace, service, database, Redis, Dex, NATS, PVC, PV, registry configuration, traffic, Git remote, ANI checkout/ref/tracked file, or DP2-10 source file was modified. No namespace, PVC, PV, image, or cluster resource was deleted. The two storage Jobs have completed and no CF-01 write workload remains active. Foundation resources are retained for diagnosis.

The protected DP2-10 porcelain digest is unchanged at `02fe876c9e4a3e2f49c43a864864d62230a548e641fda790b7e5549752aedff6`; its tracked binary diff digest remains `e1387ff7898b606ceaf4f524346525fc2e3b6df0a0eb0ae064b7389882a4371c`.
DP2-10 was restored as the sole `claimed` ticket at `2026-09-08T11:27:14Z`; no DP2-10 implementation resumed in this Goal.

## Not verified

OIDC, Refresh/Logout/SwitchTenant, the full DP2-05 error matrix, API Key/Envoy, Service Token/Inference, Core lifecycle/NATS, UI/multi-tab, deployment rollback, HA, capacity, production readiness, both runtime lanes, and `CF01-HUMAN-PASSWORD-PROTECTED-READ-V1` remain `not_verified`.
