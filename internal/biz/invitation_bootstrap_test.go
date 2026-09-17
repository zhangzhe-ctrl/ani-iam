package biz

import (
	"errors"
	"github.com/google/uuid"
	"testing"
)

type invitationBootstrapCatalog []Permission

func (c invitationBootstrapCatalog) Contains(p Permission) bool {
	for _, v := range c {
		if v == p {
			return true
		}
	}
	return false
}
func (c invitationBootstrapCatalog) Permissions(PermissionScope) []Permission {
	return append([]Permission(nil), c...)
}
func TestInvitationBootstrapRequiresExactOriginalAndAdministratorDefinition(t *testing.T) {
	role := uuid.Must(uuid.NewV7())
	catalog := invitationBootstrapCatalog{{Scope: PermissionScopeTenant, Resource: "instances", Action: "read"}, {Scope: PermissionScopeTenant, Resource: "instances", Action: "create"}}
	good := InvitationBootstrapState{ID: uuid.Must(uuid.NewV7()), Version: 2, AccessVersion: 1, Source: "core", Status: "waiting_for_principal_verification", IntendedEmail: "invited@example.test", RoleID: role, RoleSystem: true, RoleDefinitionVersion: 1, Permissions: catalog, LoginCapable: true}
	if validateInvitationBootstrap(&good, good.IntendedEmail, []uuid.UUID{role}, catalog) != nil {
		t.Fatal("exact waiting Bootstrap rejected")
	}
	for _, change := range []func(*InvitationBootstrapState){
		func(s *InvitationBootstrapState) { s.Source = "recovery" }, func(s *InvitationBootstrapState) { s.Status = "pending" }, func(s *InvitationBootstrapState) { s.Status = "attention_required" }, func(s *InvitationBootstrapState) { s.Superseded = true }, func(s *InvitationBootstrapState) { s.IntendedEmail = "other@example.test" }, func(s *InvitationBootstrapState) { s.LoginCapable = false }, func(s *InvitationBootstrapState) { s.RoleSystem = false }, func(s *InvitationBootstrapState) { s.RoleDefinitionVersion = 2 }, func(s *InvitationBootstrapState) { s.Permissions = s.Permissions[:1] }, func(s *InvitationBootstrapState) { s.Permissions = []Permission{catalog[0], catalog[0]} }, func(s *InvitationBootstrapState) { s.RoleID = uuid.Must(uuid.NewV7()) },
	} {
		s := good
		change(&s)
		if !errors.Is(validateInvitationBootstrap(&s, good.IntendedEmail, []uuid.UUID{role}, catalog), ErrInvitationConflict) {
			t.Fatal("inexact Bootstrap accepted")
		}
	}
	if validateInvitationBootstrap(nil, "", nil, nil) != nil {
		t.Fatal("ordinary Invitation unexpectedly needs bootstrap configuration")
	}
}
