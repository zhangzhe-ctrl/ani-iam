package data

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jws"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

const workloadJWTType = "ANI-WORKLOAD+JWT"
const delegationJWTType = "ANI-DELEGATION+JWT"
const continuationJWTType = "ANI-CONTINUATION+JWT"
const invocationContextClaim = "ani_invocation_v1"

func (c *JWXAccessTokenCodec) IssueWorkload(ctx context.Context, claims biz.WorkloadTokenClaims) (string, error) {
	if !validWorkloadClaims(claims) {
		return "", biz.ErrInvocationCredentialInvalid
	}
	return c.signInvocation(ctx, workloadJWTType, claims.ID, claims.Caller.Identity.PrincipalID, claims.IssuedAt, claims.ExpiresAt, claims)
}

func (c *JWXAccessTokenCodec) VerifyWorkload(ctx context.Context, raw string) (biz.WorkloadTokenClaims, error) {
	var claims biz.WorkloadTokenClaims
	id, subject, issued, expires, err := c.parseInvocation(ctx, raw, workloadJWTType, biz.WorkloadTokenMaxTTL, &claims)
	if err != nil || !validWorkloadClaims(claims) || claims.ID != id || claims.Caller.Identity.PrincipalID != subject || !claims.IssuedAt.Equal(issued) || !claims.ExpiresAt.Equal(expires) {
		return biz.WorkloadTokenClaims{}, biz.ErrInvocationCredentialInvalid
	}
	return claims, nil
}

func (c *JWXAccessTokenCodec) IssueDelegation(ctx context.Context, claims biz.DelegationClaims) (string, error) {
	if !validDelegationClaims(claims) {
		return "", biz.ErrInvocationCredentialInvalid
	}
	return c.signInvocation(ctx, delegationJWTType, claims.ID, claims.Subject.Principal.ID, claims.IssuedAt, claims.ExpiresAt, claims)
}

func (c *JWXAccessTokenCodec) VerifyDelegation(ctx context.Context, raw string) (biz.DelegationClaims, error) {
	var claims biz.DelegationClaims
	id, subject, issued, expires, err := c.parseInvocation(ctx, raw, delegationJWTType, biz.DelegationMaxTTL, &claims)
	if err != nil || !validDelegationClaims(claims) || claims.ID != id || claims.Subject.Principal.ID != subject || !claims.IssuedAt.Equal(issued) || !claims.ExpiresAt.Equal(expires) {
		return biz.DelegationClaims{}, biz.ErrInvocationCredentialInvalid
	}
	return claims, nil
}

func (c *JWXAccessTokenCodec) signInvocation(ctx context.Context, kind string, id, subject uuid.UUID, issued, expires time.Time, payload any) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// A JSON string preserves exact int64 revisions across JWT implementations.
	// Only IAM decodes this versioned private claim; adapters never parse tokens.
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", biz.ErrInvocationCredentialInvalid
	}
	token, err := jwt.NewBuilder().Issuer(c.issuer).Subject(subject.String()).Audience([]string{biz.SessionInvocationAudience}).JwtID(id.String()).IssuedAt(issued).Expiration(expires).Claim(invocationContextClaim, string(encoded)).Build()
	if err != nil {
		return "", biz.ErrInvocationCredentialInvalid
	}
	headers := jws.NewHeaders()
	if headers.Set(jws.KeyIDKey, c.activeKeyID) != nil || headers.Set(jws.TypeKey, kind) != nil {
		return "", biz.ErrInvocationCredentialInvalid
	}
	signed, err := jwt.Sign(token, jwt.WithKey(jwa.EdDSA(), c.privateKey, jws.WithProtectedHeaders(headers)))
	if err != nil {
		return "", biz.ErrInvocationCredentialInvalid
	}
	return string(signed), nil
}

func (c *JWXAccessTokenCodec) parseInvocation(ctx context.Context, raw, kind string, ttl time.Duration, payload any) (uuid.UUID, uuid.UUID, time.Time, time.Time, error) {
	deny := func() (uuid.UUID, uuid.UUID, time.Time, time.Time, error) {
		return uuid.Nil, uuid.Nil, time.Time{}, time.Time{}, biz.ErrInvocationCredentialInvalid
	}
	if ctx.Err() != nil || len(raw) == 0 || len(raw) > 32768 {
		return deny()
	}
	message, err := jws.Parse([]byte(raw), jws.WithCompact())
	if err != nil || len(message.Signatures()) != 1 {
		return deny()
	}
	h := message.Signatures()[0].ProtectedHeaders()
	alg, ok := h.Algorithm()
	if !ok || alg != jwa.EdDSA() {
		return deny()
	}
	typ, ok := h.Type()
	if !ok || typ != kind {
		return deny()
	}
	kid, ok := h.KeyID()
	if !ok {
		return deny()
	}
	key, ok := c.verificationKey[kid]
	if !ok {
		return deny()
	}
	token, err := jwt.Parse([]byte(raw), jwt.WithKey(jwa.EdDSA(), key), jwt.WithClock(jwt.ClockFunc(c.clock.Now)), jwt.WithIssuer(c.issuer), jwt.WithAudience(biz.SessionInvocationAudience),
		jwt.WithRequiredClaim(jwt.SubjectKey), jwt.WithRequiredClaim(jwt.AudienceKey), jwt.WithRequiredClaim(jwt.JwtIDKey), jwt.WithRequiredClaim(jwt.IssuedAtKey), jwt.WithRequiredClaim(jwt.ExpirationKey), jwt.WithRequiredClaim(invocationContextClaim))
	if err != nil {
		return deny()
	}
	audiences, _ := token.Audience()
	if len(audiences) != 1 || audiences[0] != biz.SessionInvocationAudience {
		return deny()
	}
	idText, _ := token.JwtID()
	id, err := uuid.Parse(idText)
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 {
		return deny()
	}
	subjectText, _ := token.Subject()
	subject, err := uuid.Parse(subjectText)
	if err != nil || subject == uuid.Nil {
		return deny()
	}
	issued, _ := token.IssuedAt()
	expires, _ := token.Expiration()
	now := c.clock.Now().UTC()
	if issued.IsZero() || issued.After(now) || !expires.After(issued) || expires.Sub(issued) > ttl || !now.Before(expires) {
		return deny()
	}
	var rawContext string
	if token.Get(invocationContextClaim, &rawContext) != nil {
		return deny()
	}
	decoder := json.NewDecoder(bytes.NewBufferString(rawContext))
	decoder.DisallowUnknownFields()
	if decoder.Decode(payload) != nil {
		return deny()
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return deny()
	}
	return id, subject, issued, expires, nil
}

func validProofCaller(c biz.DirectCaller) bool {
	i := c.Identity
	return i.PrincipalID != uuid.Nil && i.BindingID != uuid.Nil && i.PrincipalVersion > 0 && i.BindingVersion > 0 && c.GrantVersion > 0 &&
		i.Peer.Environment != "" && i.Peer.TrustDomain != "" && i.Peer.IdentityKind == "x509_dns" && i.Peer.IdentityValue != "" &&
		c.Target == (biz.WorkloadTarget{Audience: biz.SessionInvocationAudience, Operation: biz.SessionInvocationOperation})
}

func validProofTime(id uuid.UUID, issued, expires time.Time, ttl time.Duration) bool {
	return id.Version() == 7 && id.Variant() == uuid.RFC4122 && !issued.IsZero() && expires.After(issued) && expires.Sub(issued) <= ttl && issued.Nanosecond() == 0 && expires.Nanosecond() == 0
}

func validWorkloadClaims(c biz.WorkloadTokenClaims) bool {
	return validProofCaller(c.Caller) && validProofTime(c.ID, c.IssuedAt, c.ExpiresAt, biz.WorkloadTokenMaxTTL)
}

func validDelegationClaims(c biz.DelegationClaims) bool {
	if !validProofCaller(c.Caller) || !validProofTime(c.ID, c.IssuedAt, c.ExpiresAt, biz.DelegationMaxTTL) || c.WorkloadTokenID == uuid.Nil || c.Binding.Validate() != nil || c.Subject.Principal.ID != c.Binding.SubjectID || c.Subject.Principal.TenantID != c.Binding.TenantID {
		return false
	}
	return validDelegatedSubject(c.Subject)
}

func validDelegatedSubject(s biz.DelegatedSubject) bool {
	if s.Principal.Type == biz.PrincipalTypeHuman {
		return s.Principal.SessionID != uuid.Nil && s.Principal.GrantID != uuid.Nil && s.GrantVersion > 0 && s.APIKeyID == uuid.Nil && s.APIKeyVersion == 0
	}
	return s.Principal.Type == biz.PrincipalTypeWorkload && s.Principal.SessionID == uuid.Nil && s.Principal.GrantID == uuid.Nil && s.GrantVersion == 0 && s.APIKeyID != uuid.Nil && s.APIKeyVersion > 0
}

func (c *JWXAccessTokenCodec) IssueContinuation(ctx context.Context, claims biz.SessionContinuationClaims) (string, error) {
	if !validContinuationClaims(claims) {
		return "", biz.ErrInvocationCredentialInvalid
	}
	return c.signInvocation(ctx, continuationJWTType, claims.ID, claims.Subject.Principal.ID, claims.IssuedAt, claims.ExpiresAt, claims)
}
func (c *JWXAccessTokenCodec) VerifyContinuation(ctx context.Context, raw string) (biz.SessionContinuationClaims, error) {
	var claims biz.SessionContinuationClaims
	id, subject, issued, expires, err := c.parseInvocation(ctx, raw, continuationJWTType, biz.SessionContinuationMaxTTL, &claims)
	if err != nil || !validContinuationClaims(claims) || claims.ID != id || claims.Subject.Principal.ID != subject || !claims.IssuedAt.Equal(issued) || !claims.ExpiresAt.Equal(expires) {
		return biz.SessionContinuationClaims{}, biz.ErrInvocationCredentialInvalid
	}
	return claims, nil
}
func validContinuationClaims(c biz.SessionContinuationClaims) bool {
	receiver := c.Receiver
	// Validate the receiver's identity/version shape independently of its IAM target.
	shape := receiver
	shape.Target = c.Caller.Target
	return validProofCaller(c.Caller) && validProofCaller(shape) && receiver.Target == (biz.WorkloadTarget{Audience: "ani-iam", Operation: "/iam.v1.AuthorizationService/VerifyWorkloadInvocation"}) &&
		receiver.Identity.Peer.Environment == c.Caller.Identity.Peer.Environment && receiver.Identity.Peer.TrustDomain == c.Caller.Identity.Peer.TrustDomain &&
		validProofTime(c.ID, c.IssuedAt, c.ExpiresAt, biz.SessionContinuationMaxTTL) && c.Binding.Validate() == nil && validDelegatedSubject(c.Subject) && c.Subject.Principal.ID == c.Binding.SubjectID && c.Subject.Principal.TenantID == c.Binding.TenantID &&
		(c.Subject.CredentialExpiresAt.IsZero() || !c.ExpiresAt.After(c.Subject.CredentialExpiresAt)) && (c.Subject.Principal.Type != biz.PrincipalTypeHuman || !c.Subject.CredentialExpiresAt.IsZero())
}
