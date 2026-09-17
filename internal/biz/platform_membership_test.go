package biz

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"testing"
)

type platformGuardTestTx struct {
	PlatformAdministrationTransaction
	candidates []PlatformAdministratorCandidate
}

func (t platformGuardTestTx) AdministratorCandidates(context.Context, PlatformCapability, PlatformAdminLoginPolicy) ([]PlatformAdministratorCandidate, error) {
	return t.candidates, nil
}
func TestPlatformLastAdministratorCountsOnlyLoginCapableTarget(t *testing.T) {
	u := &PlatformAdministrationUsecase{}
	for _, tc := range []struct {
		target bool
		count  int64
		denied bool
	}{{true, 1, true}, {true, 2, false}, {false, 1, false}, {false, 0, false}} {

		target := uuid.Must(uuid.NewV7())
		candidates := []PlatformAdministratorCandidate{}
		for i := int64(0); i < tc.count; i++ {
			id := uuid.Must(uuid.NewV7())
			if i == 0 && tc.target {
				id = target
			}
			candidates = append(candidates, PlatformAdministratorCandidate{MembershipID: id, LoginCapable: true})
		}
		candidates = append(candidates, PlatformAdministratorCandidate{MembershipID: uuid.Must(uuid.NewV7()), LoginCapable: false})
		err := u.protectPlatformAdministrator(context.Background(), platformGuardTestTx{candidates: candidates}, PlatformCapability{}, target)

		if errors.Is(err, ErrLastPlatformAdministrator) != tc.denied {
			t.Fatalf("target=%v count=%d guard differs", tc.target, tc.count)
		}
	}
}
