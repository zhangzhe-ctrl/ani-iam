> Superseded before execution: use k8s-proposal-v2.json and k8s-proposal-v2-summary.json. The 16-object revision has two single-replica Deployments and an independent named-Deployment read identity for Gateway. No objects from this older draft were created.

# WR-19 test Kubernetes resources

User explicitly selected SSH `ani` and CF-01's recorded test environment. The live cluster UID is `f5cafbf1-5246-4f8f-b9da-742182f37528`; CF target namespace UID is `86a86f83-71e2-4c75-bb50-d81d83a73b90`. Existing resources are recorded in cluster-preflight.json and protected. No new kind cluster.

Exact proposed create payload: [k8s-proposal.json](k8s-proposal.json), 11 objects:
- `ani-cutover-target/ServiceAccount/wr19-session`, independent from existing CF accounts.
- Two new namespaces `ani-tenant-01993000-0019-7000-8000-000000000001` and `ani-tenant-01993000-0019-7000-8000-000000000002`. Each contains one BusyBox fixture Pod `wr19-exec-a/b`, one default-deny NetworkPolicy, and a `wr19-session-exec` Role/RoleBinding. They are absent in the live preflight. Kubernetes may automatically create each namespace's default service account/root CA ConfigMap; those are owned by the new namespace.

Session's resource namespace remains derived from Tenant ID. No arbitrary namespace override is introduced to fit CF names. The Role grants only pods get/list and pods/exec create in those two namespaces. The fixture has no service account token, privileged capabilities, host mounts, network allowance, Service, Ingress, PVC or published application port. Pod image is fixed by digest. Total requests 20m CPU/32Mi, limits 200m CPU/128Mi, emptyDir size up to 16Mi total.

IAM/Gateway/Session binaries and their new dedicated PG/Redis dependencies run on ubuntu, loopback only. Session gets a newly issued token for this task's service account (1 hour, renewed only for this same task if necessary). It never receives a pre-existing admin kubeconfig or credential. The control-plane address observed is `https://10.10.1.66:6443`; use authenticated SSH forwarding with only loopback listeners if direct routing is unavailable. Retain TLS verification against the cluster's public CA and original server identity through forwarding. The public CA is not a private credential. Newly generated credentials stay in private task-owned remote files and are never printed or saved in evidence.

Authorized test operations requested: create the 11 new objects, issue that bounded service-account token, perform real exec in only these two fixture Pods, test negative access/tenant checks and connection closure, then clean up only task-owned resources after checking recorded UIDs. Deleting any existing CF namespace/PVC/Deployment/SA or changing its config/selector/credential is outside this proposal. No shared database use, application deployment, cutover or image publication.

Before creation, recheck cluster/namespace UIDs, proposed-name absence, exact manifest SHA and activity. Any collision, UID drift, unexpected image pull error, missing capacity or requirement to change existing resources pauses the environment-dependent path. Failed/new resources are identified by name+UID+goal labels; recovery only deletes or stops those listed additions and task-owned SSH tunnels/processes. No global prune, default context change or automatic cluster cleanup.

Status: awaiting acceptance for the exact new resource operations above. The user's host/environment authorization is already accepted; this confirmation is specifically required by the Goal's exact Kubernetes topology checkpoint, because CF namespaces cannot directly hold Tenant-derived resource Pods.
