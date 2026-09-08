# DP2-10 baseline resume — 2026-09-09

## Human decision

- `pass`: the user explicitly accepted IAM descriptor SHA-256 `66552fe0a53a1c4956f6ee7943498af1b5309602fec28eb47c1b51f4276f5d9a` as the DP2-10–12 baseline.
- The accepted additive descriptor change was committed by DP2-06 as `74d7e441435d35dc66a733c8bf4a93129b26de3f`: `PasswordLoginRequest.source_ip` was added and the immutable contract pin was updated.
- DP2-10 does not modify `api/**`; the Core descriptor and explicitly frozen IAM Admin/Authorization Proto sources remain unchanged.

## Recovery audit

- IAM branch: `codex/direct-p2-06-14`.
- IAM HEAD: `38a28b3a4d4136e78cb86e4dc1ac8f797d1dc1cd`.
- IAM tree: `20f2920d5bd9d3d681d7b71a1c6e6839672f26e7`.
- `pass`: tracked DP2-10 binary diff SHA-256, excluding the checkpoint and issue, is `e1387ff7898b606ceaf4f524346525fc2e3b6df0a0eb0ae064b7389882a4371c`.
- `pass`: reconstructed pre-transition untracked checksum manifest SHA-256 is `bf3b9c89d1d0afd49cde3f814e6385e91bf138f084e0be84d2e31c10490b1af4`.
- `pass`: the dedicated ANI worktree is clean at commit `4ff73e09c16df706af2aacdff6d763ed4aeaa873`, tree `bc0ddb3ba1a449bc30b7815f9047aeb4390e8eff`.
- `pass`: DP2-03, DP2-04, and DP2-09 are `resolved`; DP2-10 is the sole `claimed` Direct P2 ticket; DP2-11, DP2-12, and DP2-13 remain `ready-for-agent`.

## Frozen artifacts

| Artifact | SHA-256 | Result |
| --- | --- | --- |
| IAM descriptor | `66552fe0a53a1c4956f6ee7943498af1b5309602fec28eb47c1b51f4276f5d9a` | `pass` |
| Core descriptor | `7dd40f9053b7c1c0c8905decab0f81b07173d0b25651113147bde9a5370d352a` | `pass` |
| `iam_admin_service.proto` | `332dc8ad82bdc9e7c07618808028316a0c9ab735d0b71177095cb7d2f3a7d316` | `pass` |
| `authorization_service.proto` | `7799bef6830dcdd6de8cf19f40b6d4a79361a35c06664e078b8b0ac61cf22050` | `pass` |
| operation registry source | `742147f0b370b565667748a0c8194a49f79677aa192fc3262a24c2de8eae6f80` | `pass` |
| policy revision | `sha256:1d5c80b83635e9a152c0edd9e8d1c9b66f5f8962e84cdd4f4701ecc486dd969c` | `pass` |

No push, PR, deployment, cutover, Credential invalidation, shared-infrastructure mutation, or deletion was performed during this resume audit.
