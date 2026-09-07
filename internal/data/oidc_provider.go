package data

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"unicode"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

var ErrInvalidOIDCProviderConfiguration = errors.New("invalid OIDC provider configuration")

func LoadOIDCClientSecretFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read OIDC client secret: %w", err)
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return "", fmt.Errorf("read OIDC client secret: %w", err)
	}
	if len(contents) == 0 || len(contents) > 4096 {
		return "", errors.New("OIDC client secret must contain 1..4096 visible ASCII bytes")
	}
	for _, character := range contents {
		if character < 0x21 || character > 0x7e {
			return "", errors.New("OIDC client secret must contain only visible ASCII bytes")
		}
	}
	return string(contents), nil
}

type CoreOSOIDCProviderConfig struct {
	Name         string
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURIs []string
	HTTPClient   *http.Client
}

type coreOSOIDCProvider struct {
	name         string
	issuerURL    string
	httpClient   *http.Client
	oauth        oauth2.Config
	verifier     *oidc.IDTokenVerifier
	redirectURIs map[string]struct{}
}

func NewCoreOSOIDCProvider(ctx context.Context, config CoreOSOIDCProviderConfig) (biz.OIDCProvider, error) {
	name := strings.TrimSpace(config.Name)
	issuerURL := strings.TrimSpace(config.IssuerURL)
	clientID := strings.TrimSpace(config.ClientID)
	if ctx == nil || name == "" || name != config.Name || strings.IndexFunc(name, unicode.IsSpace) >= 0 ||
		clientID == "" || clientID != config.ClientID || strings.TrimSpace(config.ClientSecret) == "" ||
		config.HTTPClient == nil || config.HTTPClient.Timeout <= 0 || issuerURL != config.IssuerURL ||
		!validOIDCIssuerURL(issuerURL) {
		return nil, ErrInvalidOIDCProviderConfiguration
	}
	redirectURIs := make(map[string]struct{}, len(config.RedirectURIs))
	for _, redirectURI := range config.RedirectURIs {
		if !validOIDCRedirectURI(redirectURI) {
			return nil, ErrInvalidOIDCProviderConfiguration
		}
		redirectURIs[redirectURI] = struct{}{}
	}
	if len(redirectURIs) == 0 {
		return nil, ErrInvalidOIDCProviderConfiguration
	}
	discoveryContext := oidcClientContext(ctx, config.HTTPClient)
	provider, err := oidc.NewProvider(discoveryContext, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider: %w", err)
	}
	oauthConfig := oauth2.Config{
		ClientID: clientID, ClientSecret: config.ClientSecret, Endpoint: provider.Endpoint(),
		Scopes: []string{oidc.ScopeOpenID, "profile", "email"},
	}
	return &coreOSOIDCProvider{
		name: name, issuerURL: issuerURL, httpClient: config.HTTPClient, oauth: oauthConfig,
		verifier: provider.Verifier(&oidc.Config{ClientID: clientID}), redirectURIs: redirectURIs,
	}, nil
}

func (p *coreOSOIDCProvider) Name() string {
	return p.name
}

func (p *coreOSOIDCProvider) AuthorizationURL(request biz.OIDCAuthorizationRequest) (string, error) {
	if strings.TrimSpace(request.State) == "" || strings.TrimSpace(request.Nonce) == "" ||
		!validPKCEVerifier(request.CodeVerifier) || !p.redirectAllowed(request.RedirectURI) {
		return "", ErrInvalidOIDCProviderConfiguration
	}
	config := p.oauth
	config.RedirectURL = request.RedirectURI
	return config.AuthCodeURL(
		request.State,
		oidc.Nonce(request.Nonce),
		oauth2.S256ChallengeOption(request.CodeVerifier),
	), nil
}

func (p *coreOSOIDCProvider) ExchangeAndVerify(ctx context.Context, request biz.OIDCExchangeRequest) (biz.OIDCVerifiedIdentity, error) {
	if ctx == nil || strings.TrimSpace(request.Code) == "" || strings.TrimSpace(request.Nonce) == "" ||
		!validPKCEVerifier(request.CodeVerifier) || !p.redirectAllowed(request.RedirectURI) {
		return biz.OIDCVerifiedIdentity{}, errors.New("OIDC exchange request is invalid")
	}
	requestContext := oidcClientContext(ctx, p.httpClient)
	config := p.oauth
	config.RedirectURL = request.RedirectURI
	token, err := config.Exchange(requestContext, request.Code, oauth2.VerifierOption(request.CodeVerifier))
	if err != nil {
		if oidcTokenExchangeUnavailable(err) {
			return biz.OIDCVerifiedIdentity{}, oidcDependencyError("OIDC token endpoint is unavailable", requestContext, err)
		}
		return biz.OIDCVerifiedIdentity{}, errors.New("OIDC authorization code exchange was rejected")
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || strings.TrimSpace(rawIDToken) == "" {
		return biz.OIDCVerifiedIdentity{}, fmt.Errorf("OIDC token response omitted required ID token: %w", biz.ErrOIDCDependency)
	}
	idToken, err := p.verifier.Verify(requestContext, rawIDToken)
	if err != nil {
		if oidcVerificationUnavailable(requestContext, err) {
			return biz.OIDCVerifiedIdentity{}, oidcDependencyError("OIDC verification keys are unavailable", requestContext, err)
		}
		return biz.OIDCVerifiedIdentity{}, errors.New("OIDC ID token verification was rejected")
	}
	if idToken.Issuer != p.issuerURL || idToken.Nonce != request.Nonce {
		return biz.OIDCVerifiedIdentity{}, errors.New("OIDC ID token binding is invalid")
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return biz.OIDCVerifiedIdentity{}, errors.New("decode OIDC ID token claims")
	}
	return biz.OIDCVerifiedIdentity{
		Issuer: idToken.Issuer, Subject: idToken.Subject, Email: claims.Email, EmailVerified: claims.EmailVerified,
	}, nil
}

func oidcDependencyError(message string, ctx context.Context, err error) error {
	cause := error(biz.ErrOIDCDependency)
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		cause = errors.Join(cause, context.DeadlineExceeded)
	} else if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		cause = errors.Join(cause, context.Canceled)
	}
	return fmt.Errorf("%s: %w", message, cause)
}

func oidcTokenExchangeUnavailable(err error) bool {
	var retrieveError *oauth2.RetrieveError
	if !errors.As(err, &retrieveError) {
		return true
	}
	if retrieveError.Response == nil {
		return true
	}
	statusCode := retrieveError.Response.StatusCode
	return statusCode == http.StatusTooManyRequests || statusCode >= http.StatusInternalServerError ||
		retrieveError.ErrorCode == "temporarily_unavailable" || retrieveError.ErrorCode == "server_error"
}

func oidcVerificationUnavailable(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return true
	}
	message := err.Error()
	// coreos/go-oidc v3.20.0 flattens RemoteKeySet errors with %v. Every
	// network, non-200, response-read, and JWKS-decode failure is nevertheless
	// kept below the stable "fetching keys oidc:" prefix; signature rejection
	// does not use that prefix.
	return strings.Contains(message, "fetching keys oidc:") ||
		strings.Contains(message, "context deadline exceeded") || strings.Contains(message, "context canceled")
}

func (p *coreOSOIDCProvider) redirectAllowed(redirectURI string) bool {
	_, ok := p.redirectURIs[redirectURI]
	return ok
}

func oidcClientContext(ctx context.Context, client *http.Client) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, client)
}

func validPKCEVerifier(value string) bool {
	if len(value) < 43 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '.' || character == '_' || character == '~' {
			continue
		}
		return false
	}
	return true
}

func validOIDCIssuerURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	if parsed.Scheme != "http" {
		return false
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validOIDCRedirectURI(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.IsAbs() && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil &&
		parsed.RawQuery == "" && parsed.Fragment == "" && value == parsed.String()
}

var _ biz.OIDCProvider = (*coreOSOIDCProvider)(nil)
