# Current Workload authority revision

`VerifyWorkloadCallerResponse.authority_revision` (field 8) is an opaque proof of the current caller authority for a registry-declared pair of HTTP operations. It grants no authority and is not a credential. The receiver still verifies the one WAT against the actual method, path, operation, reviewed target revision and authenticated TLS peer on every request.

Owner declarations use `x-ani-authority-operations` on both Snapshot operations. The generated `workloadregistry.Target.authority_operations` must contain exactly two enabled `workload_only` HTTP operations, strictly sorted, including the target itself. Both members must declare the identical pair, audience and receiver operation. Callers cannot select a group or another subject. Undeclared targets retain their original encoding, target revision and verification behavior.

IAM's data adapter rechecks both caller and receiver identities, Binding and Principal versions, receiver Verify ingress and exact receiver Grant, both caller Grants and their registered target revisions in one read-only PostgreSQL REPEATABLE READ transaction. Its first query establishes the MVCC snapshot and verification linearization point. A revocation committed before that point is denied; a concurrent later revocation follows that ordering. IAM checks WAT expiry again after reading authority. Missing adapters, invalid or partial authority and dependency failure deny access. Runtime database write permissions are unchanged.

The proof is `wa1:` followed by lowercase SHA256 over UTF-8 `ani.workload-authority/v1` plus an actual newline, followed by compact JSON in this exact field order:

```json
{"audience":"AUDIENCE","principal_id":"UUID","principal_version":1,"binding_id":"UUID","binding_version":1,"targets":[{"operation":"OP1","target_revision":"HEX","grant_id":"UUID","grant_version":1},{"operation":"OP2","target_revision":"HEX","grant_id":"UUID","grant_version":1}]}
```

IDs are canonical UUIDv7; versions are positive integers; target revisions are lowercase SHA256; targets follow the declared operation order. Request operation, WAT ID, timestamps and expiry are excluded so that Begin and Page receive the same proof for the same authority. A Grant restored with a higher version produces a different proof.

SDK `WorkloadCaller.AuthorityRevision()` returns IAM's value unchanged. Grouped targets require `^wa1:[0-9a-f]{64}$`; ungrouped targets require an empty value. Governance stores this opaque revision with its Snapshot reader and compares it on every Page. There is no local hash or missing-proof fallback. The old cut cannot be reused after authority changes, even when authority is restored.

Notification keeps its original reviewed registry input and binary. Compatibility requires unchanged non-Snapshot target revisions and a new Tenant's complete real Notification/SMTP/invitation/activation/acceptance chain on the new IAM/Governance combination; protobuf compatibility alone is insufficient.
