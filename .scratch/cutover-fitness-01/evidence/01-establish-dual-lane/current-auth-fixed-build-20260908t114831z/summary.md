# Current Auth fixed-source image build

`result: pass`

Issue 01 already fixes ANI `main` at commit `56a5f0b493c8404a024a92647d93f2ba2f7daf35`, tree `4ba6a15ad0cddf0db66a25d695b082d47346aff1`. The remote `main` still resolved to the same commit when verified, so no moving branch reference was used.

The image was built from a detached archive of that commit with the task-owned `deploy/cutover-fitness/current-auth.Dockerfile`. The build creates an ephemeral Go workspace in the build stage and does not modify `go.mod`, `go.sum`, the ANI checkout, or any ANI Git ref.

Immutable image reference:

`docker.changqingyun.cn/ani/cf01-auth-service-current@sha256:8e5d4d11662d7fdfab92f3732b07e9b0999c2de2b4bcf50af52a86cd83d1c018`

The registry manifest read, source revision label, `linux/amd64` manifest, entrypoint, and non-root user were verified. Runtime deployment and behavior are outside this user-directed follow-up and remain `not_verified`.
