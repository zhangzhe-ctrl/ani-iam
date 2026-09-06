package data

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jws"
	"github.com/lestrrat-go/jwx/v3/jwt"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

const (
	accessTokenPrincipalTypeClaim = "principal_type"
	accessTokenBoundaryTypeClaim  = "boundary_type"
	accessTokenTenantIDClaim      = "tenant_id"
	accessTokenSessionIDClaim     = "session_id"
	accessTokenGrantIDClaim       = "grant_id"
	accessTokenGrantVersionClaim  = "grant_version"
	accessTokenAuthnMethodsClaim  = "authn_methods"

	accessTokenPrincipalTypeHuman = "human"
	accessTokenBoundaryTypeTenant = "tenant"
)

var ErrInvalidAccessTokenConfiguration = errors.New("invalid access-token configuration")

var ErrInvalidAccessTokenClaims = errors.New("invalid access-token claims")

type JWXAccessTokenCodec struct {
	activeKeyID     string
	privateKey      ed25519.PrivateKey
	verificationKey map[string]ed25519.PublicKey
	issuer          string
	clock           biz.Clock
}

func NewJWXAccessTokenCodec(
	activeKeyID string,
	privateKey ed25519.PrivateKey,
	verificationKeys map[string]ed25519.PublicKey,
	issuer string,
	clock biz.Clock,
) (*JWXAccessTokenCodec, error) {
	activeKeyID = strings.TrimSpace(activeKeyID)
	issuer = strings.TrimSpace(issuer)
	if activeKeyID == "" || issuer == "" || len(privateKey) != ed25519.PrivateKeySize || clock == nil {
		return nil, ErrInvalidAccessTokenConfiguration
	}
	keys := make(map[string]ed25519.PublicKey, len(verificationKeys))
	for keyID, key := range verificationKeys {
		keyID = strings.TrimSpace(keyID)
		if keyID == "" || len(key) != ed25519.PublicKeySize {
			return nil, ErrInvalidAccessTokenConfiguration
		}
		keys[keyID] = bytes.Clone(key)
	}
	activePublicKey, ok := keys[activeKeyID]
	if !ok || !bytes.Equal(activePublicKey, privateKey.Public().(ed25519.PublicKey)) {
		return nil, ErrInvalidAccessTokenConfiguration
	}
	return &JWXAccessTokenCodec{
		activeKeyID:     activeKeyID,
		privateKey:      bytes.Clone(privateKey),
		verificationKey: keys,
		issuer:          issuer,
		clock:           clock,
	}, nil
}

func (c *JWXAccessTokenCodec) Issue(ctx context.Context, claims biz.AccessTokenClaims) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if claims.Audience != biz.AudienceConsole && claims.Audience != biz.AudienceBoss {
		return "", errors.New("access-token audience is not supported")
	}
	if err := c.validateClaims(claims); err != nil {
		return "", err
	}
	authnMethods := make([]string, len(claims.AuthnMethods))
	for index, method := range claims.AuthnMethods {
		authnMethods[index] = string(method)
	}
	token, err := jwt.NewBuilder().
		Issuer(claims.Issuer).
		Subject(claims.Subject.String()).
		Audience([]string{string(claims.Audience)}).
		JwtID(claims.TokenID.String()).
		IssuedAt(claims.IssuedAt.UTC()).
		Expiration(claims.ExpiresAt.UTC()).
		Claim(accessTokenPrincipalTypeClaim, accessTokenPrincipalTypeHuman).
		Claim(accessTokenBoundaryTypeClaim, accessTokenBoundaryTypeTenant).
		Claim(accessTokenTenantIDClaim, claims.TenantID.String()).
		Claim(accessTokenSessionIDClaim, claims.SessionID.String()).
		Claim(accessTokenGrantIDClaim, claims.GrantID.String()).
		Claim(accessTokenGrantVersionClaim, claims.GrantVersion).
		Claim(accessTokenAuthnMethodsClaim, authnMethods).
		Build()
	if err != nil {
		return "", fmt.Errorf("build access token: %w", err)
	}
	headers := jws.NewHeaders()
	if err := headers.Set(jws.KeyIDKey, c.activeKeyID); err != nil {
		return "", fmt.Errorf("set access-token key ID: %w", err)
	}
	if err := headers.Set(jws.TypeKey, "JWT"); err != nil {
		return "", fmt.Errorf("set access-token type: %w", err)
	}
	signed, err := jwt.Sign(token, jwt.WithKey(jwa.EdDSA(), c.privateKey, jws.WithProtectedHeaders(headers)))
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}
	return string(signed), nil
}

func (c *JWXAccessTokenCodec) Verify(ctx context.Context, rawCredential string) (biz.AccessTokenClaims, error) {
	if err := ctx.Err(); err != nil {
		return biz.AccessTokenClaims{}, err
	}
	message, err := jws.Parse([]byte(rawCredential), jws.WithCompact())
	if err != nil {
		return biz.AccessTokenClaims{}, fmt.Errorf("parse access-token JWS: %w", err)
	}
	if len(message.Signatures()) != 1 {
		return biz.AccessTokenClaims{}, errors.New("access token must contain exactly one signature")
	}
	headers := message.Signatures()[0].ProtectedHeaders()
	algorithm, ok := headers.Algorithm()
	if !ok || algorithm != jwa.EdDSA() {
		return biz.AccessTokenClaims{}, errors.New("access token must use EdDSA")
	}
	keyID, ok := headers.KeyID()
	if !ok {
		return biz.AccessTokenClaims{}, errors.New("access token key ID is required")
	}
	publicKey, ok := c.verificationKey[keyID]
	if !ok {
		return biz.AccessTokenClaims{}, errors.New("access token key ID is not recognized")
	}
	tokenType, ok := headers.Type()
	if !ok || tokenType != "JWT" {
		return biz.AccessTokenClaims{}, errors.New("access token type must be JWT")
	}
	token, err := jwt.Parse(
		[]byte(rawCredential),
		jwt.WithKey(jwa.EdDSA(), publicKey),
		jwt.WithClock(jwt.ClockFunc(c.clock.Now)),
		jwt.WithIssuer(c.issuer),
		jwt.WithRequiredClaim(jwt.SubjectKey),
		jwt.WithRequiredClaim(jwt.AudienceKey),
		jwt.WithRequiredClaim(jwt.JwtIDKey),
		jwt.WithRequiredClaim(jwt.IssuedAtKey),
		jwt.WithRequiredClaim(jwt.ExpirationKey),
		jwt.WithRequiredClaim(accessTokenPrincipalTypeClaim),
		jwt.WithRequiredClaim(accessTokenBoundaryTypeClaim),
		jwt.WithRequiredClaim(accessTokenTenantIDClaim),
		jwt.WithRequiredClaim(accessTokenSessionIDClaim),
		jwt.WithRequiredClaim(accessTokenGrantIDClaim),
		jwt.WithRequiredClaim(accessTokenGrantVersionClaim),
		jwt.WithRequiredClaim(accessTokenAuthnMethodsClaim),
	)
	if err != nil {
		return biz.AccessTokenClaims{}, fmt.Errorf("verify access token: %w", err)
	}
	claims, err := accessTokenClaims(token)
	if err != nil {
		return biz.AccessTokenClaims{}, err
	}
	if err := c.validateClaims(claims); err != nil {
		return biz.AccessTokenClaims{}, err
	}
	return claims, nil
}

func (c *JWXAccessTokenCodec) validateClaims(claims biz.AccessTokenClaims) error {
	if claims.Issuer != c.issuer {
		return fmt.Errorf("%w: issuer", ErrInvalidAccessTokenClaims)
	}
	if claims.Audience != biz.AudienceConsole && claims.Audience != biz.AudienceBoss {
		return fmt.Errorf("%w: audience", ErrInvalidAccessTokenClaims)
	}
	for name, id := range map[string]uuid.UUID{
		"subject":    claims.Subject,
		"token_id":   claims.TokenID,
		"session_id": claims.SessionID,
		"grant_id":   claims.GrantID,
	} {
		if id == uuid.Nil || id.Version() != 7 {
			return fmt.Errorf("%w: %s", ErrInvalidAccessTokenClaims, name)
		}
	}
	if claims.TenantID == uuid.Nil {
		return fmt.Errorf("%w: tenant_id", ErrInvalidAccessTokenClaims)
	}
	if claims.GrantVersion < 1 {
		return fmt.Errorf("%w: grant_version", ErrInvalidAccessTokenClaims)
	}
	now := c.clock.Now().UTC()
	issuedAt := claims.IssuedAt.UTC()
	expiresAt := claims.ExpiresAt.UTC()
	if issuedAt.IsZero() || expiresAt.IsZero() || issuedAt.After(now) || !expiresAt.After(now) || !expiresAt.After(issuedAt) {
		return fmt.Errorf("%w: token lifetime", ErrInvalidAccessTokenClaims)
	}
	maximumLifetime := 15 * time.Minute
	if claims.Audience == biz.AudienceBoss {
		maximumLifetime = 10 * time.Minute
	}
	if expiresAt.Sub(issuedAt) > maximumLifetime {
		return fmt.Errorf("%w: token lifetime exceeds audience maximum", ErrInvalidAccessTokenClaims)
	}
	if len(claims.AuthnMethods) == 0 {
		return fmt.Errorf("%w: authn_methods", ErrInvalidAccessTokenClaims)
	}
	for _, method := range claims.AuthnMethods {
		if method != biz.AuditAuthenticationMethodPassword && method != biz.AuditAuthenticationMethodOIDC {
			return fmt.Errorf("%w: authn_methods", ErrInvalidAccessTokenClaims)
		}
	}
	return nil
}

func accessTokenClaims(token jwt.Token) (biz.AccessTokenClaims, error) {
	issuer, _ := token.Issuer()
	subject, _ := token.Subject()
	audiences, _ := token.Audience()
	tokenID, _ := token.JwtID()
	issuedAt, _ := token.IssuedAt()
	expiresAt, _ := token.Expiration()
	if len(audiences) != 1 {
		return biz.AccessTokenClaims{}, errors.New("access token must have one audience")
	}
	if biz.Audience(audiences[0]) != biz.AudienceConsole && biz.Audience(audiences[0]) != biz.AudienceBoss {
		return biz.AccessTokenClaims{}, errors.New("access-token audience is not supported")
	}

	var principalType, boundaryType, tenantID, sessionID, grantID string
	var rawGrantVersion float64
	var rawAuthnMethods []any
	for name, destination := range map[string]any{
		accessTokenPrincipalTypeClaim: &principalType,
		accessTokenBoundaryTypeClaim:  &boundaryType,
		accessTokenTenantIDClaim:      &tenantID,
		accessTokenSessionIDClaim:     &sessionID,
		accessTokenGrantIDClaim:       &grantID,
		accessTokenGrantVersionClaim:  &rawGrantVersion,
		accessTokenAuthnMethodsClaim:  &rawAuthnMethods,
	} {
		if err := token.Get(name, destination); err != nil {
			return biz.AccessTokenClaims{}, fmt.Errorf("read access-token claim %s: %w", name, err)
		}
	}
	if principalType != accessTokenPrincipalTypeHuman || boundaryType != accessTokenBoundaryTypeTenant {
		return biz.AccessTokenClaims{}, errors.New("access token principal or boundary type is invalid")
	}
	if rawGrantVersion < 1 || rawGrantVersion > math.MaxInt64 || math.Trunc(rawGrantVersion) != rawGrantVersion {
		return biz.AccessTokenClaims{}, errors.New("access token grant version is invalid")
	}
	grantVersion := int64(rawGrantVersion)
	parsedSubject, err := uuid.Parse(subject)
	if err != nil {
		return biz.AccessTokenClaims{}, fmt.Errorf("parse access-token subject: %w", err)
	}
	parsedTokenID, err := uuid.Parse(tokenID)
	if err != nil {
		return biz.AccessTokenClaims{}, fmt.Errorf("parse access-token ID: %w", err)
	}
	parsedTenantID, err := uuid.Parse(tenantID)
	if err != nil {
		return biz.AccessTokenClaims{}, fmt.Errorf("parse access-token tenant ID: %w", err)
	}
	parsedSessionID, err := uuid.Parse(sessionID)
	if err != nil {
		return biz.AccessTokenClaims{}, fmt.Errorf("parse access-token session ID: %w", err)
	}
	parsedGrantID, err := uuid.Parse(grantID)
	if err != nil {
		return biz.AccessTokenClaims{}, fmt.Errorf("parse access-token grant ID: %w", err)
	}
	authnMethods := make([]biz.AuditAuthenticationMethod, len(rawAuthnMethods))
	for index, rawMethod := range rawAuthnMethods {
		method, ok := rawMethod.(string)
		if !ok {
			return biz.AccessTokenClaims{}, errors.New("access token authentication method is invalid")
		}
		authnMethods[index] = biz.AuditAuthenticationMethod(method)
	}
	return biz.AccessTokenClaims{
		Issuer:       issuer,
		Subject:      parsedSubject,
		Audience:     biz.Audience(audiences[0]),
		TokenID:      parsedTokenID,
		SessionID:    parsedSessionID,
		GrantID:      parsedGrantID,
		GrantVersion: grantVersion,
		TenantID:     parsedTenantID,
		IssuedAt:     issuedAt.UTC(),
		ExpiresAt:    expiresAt.UTC(),
		AuthnMethods: authnMethods,
	}, nil
}

var (
	_ biz.AccessTokenIssuer        = (*JWXAccessTokenCodec)(nil)
	_ biz.AccessCredentialVerifier = (*JWXAccessTokenCodec)(nil)
)
