package biz

import (
	"errors"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestTenantInvitationEmailUsesGlobalNormalizationWithoutAliases(t *testing.T) {
	for input, want := range map[string]string{" Alice.Smith+tag@Example.TEST ": "alice.smith+tag@example.test", "用户@例子.测试": "用户@xn--fsqu00a.xn--0zwm56d"} {
		got, err := normalizeInvitationEmail(input)
		if err != nil || got != want {
			t.Fatal("invitation mailbox normalization changed identity")
		}
	}
	for _, input := range []string{"", "a@@example.test", "display <a@example.test>", "a\x00@example.test", strings.Repeat("a", 321) + "@example.test"} {
		if _, err := normalizeInvitationEmail(input); !errors.Is(err, ErrInvitationInvalid) {
			t.Fatal("invalid mailbox accepted")
		}
	}
}
func TestTenantInvitationRolesAreCanonicalAndBounded(t *testing.T) {
	a, b := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	got, err := invitationRoleIDs([]uuid.UUID{b, a, b})
	if err != nil || len(got) != 2 || strings.Compare(got[0].String(), got[1].String()) >= 0 {
		t.Fatal("role set is not canonical")
	}
	for _, input := range [][]uuid.UUID{nil, {uuid.Nil}, {uuid.New()}, make([]uuid.UUID, 101)} {
		if _, err := invitationRoleIDs(input); !errors.Is(err, ErrInvitationInvalid) {
			t.Fatal("invalid invitation role set accepted")
		}
	}
}
