package data

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jws"
	"github.com/lestrrat-go/jwx/v3/jwt"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

func TestJWXAccessTokenCodecRoundTripsFrozenClaims(t *testing.T) {
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x42}, ed25519.SeedSize))
	publicKey := privateKey.Public().(ed25519.PublicKey)
	now := time.Date(2026, 9, 4, 8, 0, 0, 0, time.UTC)
	codec, err := NewJWXAccessTokenCodec(
		"dp2-05-test-key",
		privateKey,
		map[string]ed25519.PublicKey{"dp2-05-test-key": publicKey},
		"ani-iam",
		fixedDataClock{now: now},
	)
	if err != nil {
		t.Fatalf("NewJWXAccessTokenCodec() error = %v", err)
	}
	want := biz.AccessTokenClaims{
		Issuer:       "ani-iam",
		Subject:      uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17"),
		Audience:     biz.AudienceConsole,
		TokenID:      uuid.MustParse("0198f062-b76d-7001-9000-000000000005"),
		SessionID:    uuid.MustParse("0198f062-b76d-7001-9000-000000000001"),
		GrantID:      uuid.MustParse("0198f062-b76d-7001-9000-000000000002"),
		GrantVersion: 1,
		TenantID:     uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70"),
		IssuedAt:     now,
		ExpiresAt:    now.Add(15 * time.Minute),
		AuthnMethods: []biz.AuditAuthenticationMethod{biz.AuditAuthenticationMethodPassword},
	}

	signed, err := codec.Issue(context.Background(), want)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	message, err := jws.Parse([]byte(signed), jws.WithCompact())
	if err != nil {
		t.Fatalf("jws.Parse() error = %v", err)
	}
	if len(message.Signatures()) != 1 {
		t.Fatalf("signature count = %d, want 1", len(message.Signatures()))
	}
	headers := message.Signatures()[0].ProtectedHeaders()
	algorithm, ok := headers.Algorithm()
	if !ok || algorithm != jwa.EdDSA() {
		t.Fatalf("protected alg = %q, %v, want EdDSA", algorithm, ok)
	}
	keyID, ok := headers.KeyID()
	if !ok || keyID != "dp2-05-test-key" {
		t.Fatalf("protected kid = %q, %v", keyID, ok)
	}
	tokenType, ok := headers.Type()
	if !ok || tokenType != "JWT" {
		t.Fatalf("protected typ = %q, %v", tokenType, ok)
	}

	got, err := codec.Verify(context.Background(), signed)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if got.Issuer != want.Issuer || got.Subject != want.Subject || got.Audience != want.Audience ||
		got.TokenID != want.TokenID || got.SessionID != want.SessionID || got.GrantID != want.GrantID ||
		got.GrantVersion != want.GrantVersion || got.TenantID != want.TenantID ||
		!got.IssuedAt.Equal(want.IssuedAt) || !got.ExpiresAt.Equal(want.ExpiresAt) ||
		len(got.AuthnMethods) != 1 || got.AuthnMethods[0] != biz.AuditAuthenticationMethodPassword {
		t.Fatalf("Verify() claims = %#v, want %#v", got, want)
	}
}

func TestJWXAccessTokenCodecRoundTripsDomainSeparatedPasswordAction(t *testing.T) {
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x51}, ed25519.SeedSize))
	publicKey := privateKey.Public().(ed25519.PublicKey)
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	codec, err := NewJWXAccessTokenCodec(
		"dp2-06-test-key",
		privateKey,
		map[string]ed25519.PublicKey{"dp2-06-test-key": publicKey},
		"ani-iam",
		fixedDataClock{now: now},
	)
	if err != nil {
		t.Fatalf("NewJWXAccessTokenCodec() error = %v", err)
	}
	want := biz.PasswordActionTokenClaims{
		Issuer:      "ani-iam",
		PrincipalID: uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17"),
		OperationID: uuid.MustParse("0198f062-b76d-7001-9000-000000000051"),
		Purpose:     biz.PasswordActionPurposeReset,
		IssuedAt:    now,
		ExpiresAt:   now.Add(30 * time.Minute),
	}

	signed, err := codec.IssuePasswordAction(context.Background(), want)
	if err != nil {
		t.Fatalf("IssuePasswordAction() error = %v", err)
	}
	message, err := jws.Parse([]byte(signed), jws.WithCompact())
	if err != nil {
		t.Fatalf("parse password-action JWS: %v", err)
	}
	if len(message.Signatures()) != 1 {
		t.Fatalf("password-action signature count = %d, want 1", len(message.Signatures()))
	}
	tokenType, ok := message.Signatures()[0].ProtectedHeaders().Type()
	if !ok || tokenType != "ANI-PASSWORD-ACTION+JWT" {
		t.Fatalf("password-action protected typ = %q, %v", tokenType, ok)
	}

	got, err := codec.VerifyPasswordAction(context.Background(), signed)
	if err != nil {
		t.Fatalf("VerifyPasswordAction() error = %v", err)
	}
	if got != want {
		t.Fatalf("VerifyPasswordAction() claims = %#v, want %#v", got, want)
	}
	if _, err := codec.Verify(context.Background(), signed); err == nil {
		t.Fatal("Verify(access token) accepted a password-action token")
	}

	accessToken, err := codec.Issue(context.Background(), validJWXAccessTokenClaims(now))
	if err != nil {
		t.Fatalf("Issue(access token) error = %v", err)
	}
	if _, err := codec.VerifyPasswordAction(context.Background(), accessToken); err == nil {
		t.Fatal("VerifyPasswordAction() accepted an access token")
	}
}

func TestJWXAccessTokenCodecPasswordActionIssueIsDeterministicForDispatchRetry(t *testing.T) {
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x52}, ed25519.SeedSize))
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	codec, err := NewJWXAccessTokenCodec(
		"dp2-06-test-key",
		privateKey,
		map[string]ed25519.PublicKey{"dp2-06-test-key": privateKey.Public().(ed25519.PublicKey)},
		"ani-iam",
		fixedDataClock{now: now.Add(5 * time.Minute)},
	)
	if err != nil {
		t.Fatalf("NewJWXAccessTokenCodec() error = %v", err)
	}
	claims := biz.PasswordActionTokenClaims{
		Issuer:      "ani-iam",
		PrincipalID: uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17"),
		OperationID: uuid.MustParse("0198f062-b76d-7001-9000-000000000052"),
		Purpose:     biz.PasswordActionPurposeReset,
		IssuedAt:    now,
		ExpiresAt:   now.Add(30 * time.Minute),
	}

	first, err := codec.IssuePasswordAction(context.Background(), claims)
	if err != nil {
		t.Fatalf("first IssuePasswordAction() error = %v", err)
	}
	second, err := codec.IssuePasswordAction(context.Background(), claims)
	if err != nil {
		t.Fatalf("retry IssuePasswordAction() error = %v", err)
	}
	if first != second {
		t.Fatal("IssuePasswordAction() changed the token for the same durable action claims")
	}
}

func TestJWXAccessTokenCodecRejectsNonTargetAudience(t *testing.T) {
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x24}, ed25519.SeedSize))
	codec, err := NewJWXAccessTokenCodec(
		"dp2-05-test-key",
		privateKey,
		map[string]ed25519.PublicKey{"dp2-05-test-key": privateKey.Public().(ed25519.PublicKey)},
		"ani-iam",
		fixedDataClock{now: time.Date(2026, 9, 4, 8, 0, 0, 0, time.UTC)},
	)
	if err != nil {
		t.Fatalf("NewJWXAccessTokenCodec() error = %v", err)
	}
	now := time.Date(2026, 9, 4, 8, 0, 0, 0, time.UTC)
	_, err = codec.Issue(context.Background(), biz.AccessTokenClaims{
		Issuer:       "ani-iam",
		Subject:      uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17"),
		Audience:     biz.Audience("unregistered"),
		TokenID:      uuid.MustParse("0198f062-b76d-7001-9000-000000000005"),
		SessionID:    uuid.MustParse("0198f062-b76d-7001-9000-000000000001"),
		GrantID:      uuid.MustParse("0198f062-b76d-7001-9000-000000000002"),
		GrantVersion: 1,
		TenantID:     uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70"),
		IssuedAt:     now,
		ExpiresAt:    now.Add(15 * time.Minute),
		AuthnMethods: []biz.AuditAuthenticationMethod{biz.AuditAuthenticationMethodPassword},
	})
	if err == nil {
		t.Fatal("Issue(non-target audience) error = nil")
	}
}

func TestJWXAccessTokenCodecVerifyRejectsNonTargetAudience(t *testing.T) {
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x36}, ed25519.SeedSize))
	now := time.Date(2026, 9, 4, 8, 0, 0, 0, time.UTC)
	codec, err := NewJWXAccessTokenCodec(
		"dp2-05-test-key",
		privateKey,
		map[string]ed25519.PublicKey{"dp2-05-test-key": privateKey.Public().(ed25519.PublicKey)},
		"ani-iam",
		fixedDataClock{now: now},
	)
	if err != nil {
		t.Fatalf("NewJWXAccessTokenCodec() error = %v", err)
	}
	signed := signDataAccessTokenFixture(t, privateKey, "dp2-05-test-key", "ani-iam", "unregistered", now, now.Add(15*time.Minute))

	if _, err := codec.Verify(context.Background(), signed); err == nil {
		t.Fatal("Verify(non-target audience) error = nil")
	}
}

func TestJWXAccessTokenCodecIssueRejectsInvalidClaims(t *testing.T) {
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x18}, ed25519.SeedSize))
	now := time.Date(2026, 9, 4, 8, 0, 0, 0, time.UTC)
	codec, err := NewJWXAccessTokenCodec(
		"dp2-05-test-key",
		privateKey,
		map[string]ed25519.PublicKey{"dp2-05-test-key": privateKey.Public().(ed25519.PublicKey)},
		"ani-iam",
		fixedDataClock{now: now},
	)
	if err != nil {
		t.Fatalf("NewJWXAccessTokenCodec() error = %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*biz.AccessTokenClaims)
	}{
		{name: "issuer", mutate: func(claims *biz.AccessTokenClaims) { claims.Issuer = "other-issuer" }},
		{name: "subject", mutate: func(claims *biz.AccessTokenClaims) { claims.Subject = uuid.Nil }},
		{name: "token ID", mutate: func(claims *biz.AccessTokenClaims) { claims.TokenID = uuid.Nil }},
		{name: "session ID", mutate: func(claims *biz.AccessTokenClaims) { claims.SessionID = uuid.Nil }},
		{name: "grant ID", mutate: func(claims *biz.AccessTokenClaims) { claims.GrantID = uuid.Nil }},
		{name: "tenant ID", mutate: func(claims *biz.AccessTokenClaims) { claims.TenantID = uuid.Nil }},
		{name: "grant version", mutate: func(claims *biz.AccessTokenClaims) { claims.GrantVersion = 0 }},
		{name: "issued in future", mutate: func(claims *biz.AccessTokenClaims) { claims.IssuedAt = now.Add(time.Second) }},
		{name: "expired", mutate: func(claims *biz.AccessTokenClaims) { claims.ExpiresAt = now }},
		{name: "lifetime", mutate: func(claims *biz.AccessTokenClaims) { claims.ExpiresAt = now.Add(15*time.Minute + time.Second) }},
		{name: "authn methods", mutate: func(claims *biz.AccessTokenClaims) { claims.AuthnMethods = nil }},
		{name: "unsupported authn method", mutate: func(claims *biz.AccessTokenClaims) {
			claims.AuthnMethods = []biz.AuditAuthenticationMethod{biz.AuditAuthenticationMethodInternal}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			claims := validJWXAccessTokenClaims(now)
			test.mutate(&claims)
			if _, err := codec.Issue(context.Background(), claims); err == nil {
				t.Fatal("Issue(invalid claims) error = nil")
			}
		})
	}
}

func TestJWXAccessTokenCodecVerifyFailsClosed(t *testing.T) {
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x66}, ed25519.SeedSize))
	now := time.Date(2026, 9, 4, 8, 0, 0, 0, time.UTC)
	codec, err := NewJWXAccessTokenCodec(
		"dp2-05-test-key",
		privateKey,
		map[string]ed25519.PublicKey{"dp2-05-test-key": privateKey.Public().(ed25519.PublicKey)},
		"ani-iam",
		fixedDataClock{now: now},
	)
	if err != nil {
		t.Fatalf("NewJWXAccessTokenCodec() error = %v", err)
	}
	valid := signDataAccessTokenFixture(t, privateKey, "dp2-05-test-key", "ani-iam", "console", now, now.Add(15*time.Minute))
	parts := strings.Split(valid, ".")
	if len(parts) != 3 || len(parts[2]) == 0 {
		t.Fatalf("valid fixture is not a compact JWS: %q", valid)
	}
	if parts[2][0] == 'A' {
		parts[2] = "B" + parts[2][1:]
	} else {
		parts[2] = "A" + parts[2][1:]
	}
	tampered := strings.Join(parts, ".")

	tests := []struct {
		name string
		raw  string
	}{
		{name: "tampered signature", raw: tampered},
		{name: "unknown key ID", raw: signDataAccessTokenFixture(t, privateKey, "unknown-key", "ani-iam", "console", now, now.Add(15*time.Minute))},
		{name: "wrong issuer", raw: signDataAccessTokenFixture(t, privateKey, "dp2-05-test-key", "other-issuer", "console", now, now.Add(15*time.Minute))},
		{name: "expired", raw: signDataAccessTokenFixture(t, privateKey, "dp2-05-test-key", "ani-iam", "console", now.Add(-time.Hour), now.Add(-time.Second))},
		{name: "future issued-at", raw: signDataAccessTokenFixture(t, privateKey, "dp2-05-test-key", "ani-iam", "console", now.Add(time.Second), now.Add(10*time.Minute))},
		{name: "audience lifetime exceeded", raw: signDataAccessTokenFixture(t, privateKey, "dp2-05-test-key", "ani-iam", "console", now, now.Add(15*time.Minute+time.Second))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := codec.Verify(context.Background(), test.raw); err == nil {
				t.Fatal("Verify(invalid token) error = nil")
			}
		})
	}
}

func validJWXAccessTokenClaims(now time.Time) biz.AccessTokenClaims {
	return biz.AccessTokenClaims{
		Issuer:       "ani-iam",
		Subject:      uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17"),
		Audience:     biz.AudienceConsole,
		TokenID:      uuid.MustParse("0198f062-b76d-7001-9000-000000000005"),
		SessionID:    uuid.MustParse("0198f062-b76d-7001-9000-000000000001"),
		GrantID:      uuid.MustParse("0198f062-b76d-7001-9000-000000000002"),
		GrantVersion: 1,
		TenantID:     uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70"),
		IssuedAt:     now,
		ExpiresAt:    now.Add(15 * time.Minute),
		AuthnMethods: []biz.AuditAuthenticationMethod{biz.AuditAuthenticationMethodPassword},
	}
}

func signDataAccessTokenFixture(
	t *testing.T,
	privateKey ed25519.PrivateKey,
	keyID string,
	issuer string,
	audience string,
	issuedAt time.Time,
	expiresAt time.Time,
) string {
	t.Helper()
	token, err := jwt.NewBuilder().
		Issuer(issuer).
		Subject("0198f062-b76d-77da-98fa-65f26fc01e17").
		Audience([]string{audience}).
		JwtID("0198f062-b76d-7001-9000-000000000005").
		IssuedAt(issuedAt).
		Expiration(expiresAt).
		Claim(accessTokenPrincipalTypeClaim, accessTokenPrincipalTypeHuman).
		Claim(accessTokenBoundaryTypeClaim, accessTokenBoundaryTypeTenant).
		Claim(accessTokenTenantIDClaim, "0198f062-b76d-7f2a-b0ad-50a417bf1f70").
		Claim(accessTokenSessionIDClaim, "0198f062-b76d-7001-9000-000000000001").
		Claim(accessTokenGrantIDClaim, "0198f062-b76d-7001-9000-000000000002").
		Claim(accessTokenGrantVersionClaim, int64(1)).
		Claim(accessTokenAuthnMethodsClaim, []string{string(biz.AuditAuthenticationMethodPassword)}).
		Build()
	if err != nil {
		t.Fatalf("build token fixture: %v", err)
	}
	headers := jws.NewHeaders()
	if err := headers.Set(jws.KeyIDKey, keyID); err != nil {
		t.Fatalf("set token fixture kid: %v", err)
	}
	if err := headers.Set(jws.TypeKey, "JWT"); err != nil {
		t.Fatalf("set token fixture typ: %v", err)
	}
	signed, err := jwt.Sign(token, jwt.WithKey(jwa.EdDSA(), privateKey, jws.WithProtectedHeaders(headers)))
	if err != nil {
		t.Fatalf("sign token fixture: %v", err)
	}
	return string(signed)
}

type fixedDataClock struct{ now time.Time }

func (c fixedDataClock) Now() time.Time { return c.now }
