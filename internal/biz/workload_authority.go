package biz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
)

// WorkloadAuthorityReader rechecks the receiver, observed caller and both
// registry-derived Grants in one current database snapshot. Its arguments are
// verified invocation context, never a caller-selected subject or permission set.
type WorkloadAuthorityReader interface {
	ReadCurrentAuthority(context.Context, DirectCaller, DirectCaller, VerifiedWorkloadPeer) (WorkloadAuthoritySnapshot, error)
}

type WorkloadAuthoritySnapshot struct {
	Identity WorkloadIdentity
	Grants   []WorkloadAuthorityGrant
}

type WorkloadAuthorityGrant struct {
	Operation      string
	TargetRevision string
	ID             uuid.UUID
	Version        int64
}

func (u *WorkloadInvocation) WithWorkloadAuthorityReader(reader WorkloadAuthorityReader) *WorkloadInvocation {
	u.authority = reader
	return u
}

// Authority revisions deliberately exclude token/request time and the selected
// operation: Begin and Page must bind the same current authority to one cut.
func workloadAuthorityRevision(registry *workloadregistry.Registry, caller DirectCaller, current WorkloadAuthoritySnapshot) (string, error) {
	target, ok := registry.Lookup(caller.Target.Audience, caller.Target.Operation)
	if !ok || !target.Enabled || len(target.AuthorityOperations) != 2 || len(current.Grants) != 2 || current.Identity != caller.Identity || caller.TargetRevision != registry.Revision(caller.Target.Audience, caller.Target.Operation) {
		return "", ErrWorkloadPermissionDenied
	}
	i := current.Identity
	if i.PrincipalID.Version() != 7 || i.PrincipalID.Variant() != uuid.RFC4122 || i.BindingID.Version() != 7 || i.BindingID.Variant() != uuid.RFC4122 || i.PrincipalVersion < 1 || i.BindingVersion < 1 {
		return "", ErrWorkloadIdentityInvalid
	}
	type grant struct {
		Operation      string `json:"operation"`
		TargetRevision string `json:"target_revision"`
		ID             string `json:"grant_id"`
		Version        int64  `json:"grant_version"`
	}
	value := struct {
		Audience         string  `json:"audience"`
		PrincipalID      string  `json:"principal_id"`
		PrincipalVersion int64   `json:"principal_version"`
		BindingID        string  `json:"binding_id"`
		BindingVersion   int64   `json:"binding_version"`
		Targets          []grant `json:"targets"`
	}{Audience: caller.Target.Audience, PrincipalID: i.PrincipalID.String(), PrincipalVersion: i.PrincipalVersion, BindingID: i.BindingID.String(), BindingVersion: i.BindingVersion}
	seen := map[uuid.UUID]bool{}
	for index, g := range current.Grants {
		want := registry.Revision(caller.Target.Audience, target.AuthorityOperations[index])
		decoded, err := hex.DecodeString(g.TargetRevision)
		if g.Operation != target.AuthorityOperations[index] || g.TargetRevision != want || err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != g.TargetRevision || g.ID.Version() != 7 || g.ID.Variant() != uuid.RFC4122 || seen[g.ID] || g.Version < 1 || (g.Operation == caller.Target.Operation && g.Version != caller.GrantVersion) {
			return "", ErrWorkloadPermissionDenied
		}
		seen[g.ID] = true
		value.Targets = append(value.Targets, grant{g.Operation, g.TargetRevision, g.ID.String(), g.Version})
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", ErrInvalidPersistenceState
	}
	digest := sha256.Sum256(append([]byte("ani.workload-authority/v1\n"), raw...))
	return fmt.Sprintf("wa1:%x", digest), nil
}
