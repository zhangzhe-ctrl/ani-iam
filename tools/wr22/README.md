# WR22 isolated execution

The sole claimed item and evidence are in the original `ani-iam` worktree at
`.scratch/ani-iam-workload-refoundation/`. This candidate inherits the complete
WR21 source manifests, including uncommitted changes. Do not apply WR19/20 again.

`remote-run.py --command-file tools/wr22/roles-check.sh` freezes and uploads the
exact allowed source into a new `ubuntu` run and dispatches once. All builds,
generators, databases and tests execute under the shared heavy-task lock with
two Go workers. Only individually registered task resources may be stopped.

`collect.py <run_id> --generated` imports repeated generation only after source
and output checks. Raw logs and generated test credentials remain under the
remote run's private directory; public results contain statuses and metadata.

The evidence `progress.md` identifies the passing run for each administration,
recovery, audit and Invitation acceptance gate. `invitation-password-check.sh`
proves first Membership creation without an existing Session. It uses private
encrypted delivery fixtures and does not claim SMTP acceptance.

`notification-formal.sh` separately starts the actual IAM, Gateway and
Notification executables, restricted PostgreSQL roles, Redis, isolated IdPs and
an SMTP sink. New recipients obtain both Invitation tokens and verification
codes exclusively from their correlated sink messages. It proves activation
without Membership, explicit acceptance, administrator-to-member invitation,
exact Workload authority, receipt-loss retry and controlled resend. The Core
Tenant is an explicit prerequisite; real Core creation/bootstrap is WR23.

Notification input stays read-only at the accepted WR20 commit/tree. A third
manifest captures the independently allowlisted `ani-notification-service-wr22`
candidate. Its fresh database epoch is only applied to a new task database.

The authoritative issue and completion evidence determine WR22's state. WR23
may be claimed only after WR22 is resolved and its complete candidate checkpoint
is fixed. No publication or shared-environment action is authorized.
