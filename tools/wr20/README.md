# WR20 remote execution

Authority and evidence stay in the original IAM WR20 issue. Run the reviewed
command once with `python3 tools/wr20/remote-run.py --command-file <path>`.
Inputs include the verified complete WR19 ANI overlay, the fixed IAM commit,
and the fixed Notification source. Every file has a mode/hash/deletion record.
Only explicit scope changes can differ from initial source; generated artifacts
must return through a temporary directory and be checked against the input run.

One remote heavy task, GOMAXPROCS=2 and -p=2; no local build fallback. Raw process
logs and credentials stay in the remote private directory. An uncertain SSH
result must be inspected using the same run's PID, log, and exit record. Never
redispatch a command merely because the connection failed. No publication.


WR20 C adds an independent versioned AES-256-GCM outbox key file with JSON shape
`{"active":"v1","keys":{"v1":"<base64 of 32 random bytes>"}}`. Store it outside
source, readable only by the process. AAD binds the key version, outbox ID, action
ID and Human ID. Only the email snapshot is encrypted; the signed action token
is derived in memory at dispatch. Receipt/cancellation clears ciphertext and key
reference. Retain previous key versions until pending/attention rows are resolved.
No new key-management platform is included.

Migration 202609100005 refuses any existing notification outbox or password-action
completion records before replacing plaintext storage and adding the completion
fingerprint. It has only been authorized for fresh task databases. It does not
convert/delete shared data; a populated deployment needs a separate reviewed plan.
Completion fingerprints use the secret signed action capability as the HMAC key
and bind the new password; identical valid-token retries converge while changed
passwords conflict. Token expiry remains effective even for replay.

After RPC timeout, the dispatcher persists retry/receipt CAS with a separate
one-second deadline. A database failure still leaves the existing bounded lease
for recovery; external submission is idempotent. No cross-database transaction
or retry inside the Workload transport is added.
