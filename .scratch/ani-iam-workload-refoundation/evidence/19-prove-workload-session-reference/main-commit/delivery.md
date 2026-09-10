# IAM main delivery

The user explicitly requested committing IAM to main, then added a remote push request. This supersedes the earlier WR19 Goal's no-Git-publication restriction only for IAM. No ANI/Session changes, module tags, images, deployment or cutover are included.

The commit combines the already accepted WR18 and WR19 implementation because both remained uncommitted at cd38cd90bca3e9d83af09a381051b895d82ae94f. All 215 final source files and five required deletions match the recorded remote-tested snapshot, alongside current architecture/issue documents and selected readable evidence. No additional product changes were made for this delivery.

Heavy tests are not rerun for identical code. The final remote real-resource chain, 58 affected regression results, SDK/build/vet/race, and generation/repository gates remain the source-specific evidence. Only diff, staged payload, SHA/mode, generated-output, path and sensitive-data checks are performed locally.

The API/SDK candidate v0.1.0-rc.1 is still unpublished; use the documented controlled go.work for development. A main push does not create module tags or establish production readiness. Existing local main commits 13e709f4 and cd38cd90 are also included in the requested normal fast-forward push, preserving history.

Large archives, private inputs, full review diff bundles and historical raw logs stay local. The selected text evidence references those local-only artifacts for full audit; a fresh clone contains code/contracts/result summaries, not the complete test-run archive. Unrelated DP2-11 evidence and original WR18/WR19 worktrees remain untouched.

The exact commit and verified remote SHA are recorded after publication in the local main-commit/publication-receipt.json. Its commit cannot self-embed its own SHA. No force push is authorized.
