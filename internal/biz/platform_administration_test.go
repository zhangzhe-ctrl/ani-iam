package biz

import (
	"context"
	"testing"
)

func TestPlatformAdministrationCannotManufactureCapability(t *testing.T) {
	if _, _, err := (PlatformCapability{}).CredentialBinding(); err == nil {
		t.Fatal("zero Platform capability accepted")
	}
	u, _, _, c := platformAuthTestSetup(t)
	if _, err := u.AuthorizeAdministration(context.Background(), c, "ListPlatformMemberships", "PLATFORM_ADMIN_READ"); err == nil {
		t.Fatal("caller-free permission check manufactured management capability")
	}
	admin := NewPlatformAdministrationUsecase(nil, u.registry, nil, platformTestIDs{}, u.clock)
	if _, err := admin.ListRoles(context.Background(), PlatformCapability{}, "", 1); err == nil {
		t.Fatal("zero capability reached administration")
	}
}
