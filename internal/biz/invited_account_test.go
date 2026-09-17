package biz

import (
	"net/netip"
	"testing"
)

func TestInvitedAccountInputAndCodeCannotBeInvitationCredential(t *testing.T) {
	ip := netip.MustParseAddr("192.0.2.20")
	if email, err := invitedAccountInput("Invited@Example.test", "key", ip); err != nil || email != "invited@example.test" {
		t.Fatal("normalized email rejected")
	}
	for _, code := range []string{"", "12345", "1234567", "abcdef", "12345 ", "ani_inv_t.x", "１２３４５６"} {
		if validEmailVerificationCode(code) {
			t.Fatal("non-email-verification code accepted")
		}
	}
	if !validEmailVerificationCode("000001") {
		t.Fatal("leading zero code rejected")
	}
	if _, err := invitedAccountInput("Name <invited@example.test>", "key", ip); err == nil {
		t.Fatal("display-name account accepted")
	}
	if _, err := invitedAccountInput("invited@example.test", " key", ip); err == nil {
		t.Fatal("noncanonical key accepted")
	}
	if _, err := invitedAccountInput("invited@example.test", "key", netip.Addr{}); err == nil {
		t.Fatal("missing trusted IP accepted")
	}
}
