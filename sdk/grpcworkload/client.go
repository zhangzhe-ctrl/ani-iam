package grpcworkload

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"os"
	"strings"
	"time"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// TLSFiles belong to the deployment owner. The CA is loaded at construction;
// leaf certificate renewal is picked up on new TLS handshakes.
type TLSFiles struct{ CertificateFile, PrivateKeyFile, CAFile string }

func (f TLSFiles) config(serverName string, server bool) (*tls.Config, error) {
	ca, err := os.ReadFile(f.CAFile)
	if err != nil {
		return nil, ErrConfiguration
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, ErrConfiguration
	}
	load := func() (*tls.Certificate, error) {
		c, err := tls.LoadX509KeyPair(f.CertificateFile, f.PrivateKeyFile)
		if err != nil {
			return nil, ErrConfiguration
		}
		return &c, nil
	}
	if _, err := load(); err != nil {
		return nil, err
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pool, ServerName: serverName}
	if server {
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
		cfg.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return load() }
	} else {
		if serverName == "" {
			return nil, ErrConfiguration
		}
		cfg.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) { return load() }
	}
	return cfg, nil
}

// ServerCredentials requires a verified client certificate for every receiver
// connection; a namespace, header, or shared bearer secret cannot substitute.
func (f TLSFiles) ServerCredentials() (credentials.TransportCredentials, error) {
	cfg, err := f.config("", true)
	if err != nil {
		return nil, err
	}
	return credentials.NewTLS(cfg), nil
}

type ClientConfig struct {
	Address, ServerName, Environment, TrustDomain, PolicyRevision string
	TLS                                                           TLSFiles
	Timeout                                                       time.Duration
}

type Client struct {
	conn           *grpc.ClientConn
	authentication iamv1.AuthenticationServiceClient
	authorization  iamv1.AuthorizationServiceClient
	cfg            ClientConfig
}

func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.Address == "" || cfg.Environment == "" || cfg.TrustDomain == "" || cfg.PolicyRevision == "" {
		return nil, ErrConfiguration
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 2 * time.Second
	}
	if cfg.Timeout <= 0 || cfg.Timeout > 2*time.Second {
		return nil, ErrConfiguration
	}
	tlsConfig, err := cfg.TLS.config(cfg.ServerName, false)
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(cfg.Address, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)), grpc.WithDisableRetry())
	if err != nil {
		return nil, ErrConfiguration
	}
	return &Client{conn: conn, authentication: iamv1.NewAuthenticationServiceClient(conn), authorization: iamv1.NewAuthorizationServiceClient(conn), cfg: cfg}, nil
}
func (c *Client) Close() error { return c.conn.Close() }
func (c *Client) deadline(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, c.cfg.Timeout)
}

type ResourceBoundary struct{ TenantID, ResourceID string }
type AuthorizationRequest struct {
	Credential, SourceOperation, TenantID, ResourceID string
	CheckResource                                     func(context.Context, ResourceBoundary) error
}

// Authorize performs the source operation's current IAM permission decision.
// TenantID is a requested boundary, never trusted authority. If absent, IAM
// resolves the credential's boundary; this adapter never parses a user token.
func (c *Client) Authorize(ctx context.Context, r AuthorizationRequest) (Subject, error) {
	if strings.TrimSpace(r.Credential) == "" || r.SourceOperation == "" {
		return Subject{}, status.Error(codes.Unauthenticated, "subject credential is required")
	}
	r.Credential = strings.TrimSpace(r.Credential)
	if r.TenantID == "" {
		call, cancel := c.deadline(ctx)
		reply, err := c.authentication.ValidatePrincipal(call, &iamv1.ValidatePrincipalRequest{Credential: &iamv1.BearerCredential{Value: r.Credential}, OperationId: r.SourceOperation, PolicyRevision: c.cfg.PolicyRevision})
		cancel()
		if err != nil {
			return Subject{}, err
		}
		if reply.GetPrincipal().GetBoundary().GetTenant() == nil {
			return Subject{}, status.Error(codes.PermissionDenied, "subject has no Tenant boundary")
		}
		r.TenantID = reply.GetPrincipal().GetBoundary().GetTenant().GetTenantId()
	}
	call, cancel := c.deadline(ctx)
	defer cancel()
	reply, err := c.authorization.CheckPermission(call, &iamv1.CheckPermissionRequest{Credential: &iamv1.BearerCredential{Value: r.Credential}, OperationId: r.SourceOperation, PolicyRevision: c.cfg.PolicyRevision, Target: &iamv1.AuthorizationTarget{TenantId: r.TenantID, ResourceId: r.ResourceID}})
	if err != nil {
		return Subject{}, err
	}
	d := reply.GetDecision()
	if !d.GetAllowed() || d.GetPolicyRevision() != c.cfg.PolicyRevision || d.GetPrincipal().GetPrincipalId() == "" || d.GetPrincipal().GetBoundary().GetTenant().GetTenantId() != r.TenantID {
		return Subject{}, status.Error(codes.PermissionDenied, "subject is not authorized")
	}
	for _, obligation := range d.GetObligations() {
		if obligation.GetType() != iamv1.AuthorizationObligationType_AUTHORIZATION_OBLIGATION_TYPE_RESOURCE_TENANT_MATCH || obligation.GetResourceId() != r.ResourceID || obligation.GetExpectedTenantId() != r.TenantID || r.CheckResource == nil {
			return Subject{}, status.Error(codes.PermissionDenied, "resource owner check is required")
		}
		if err := r.CheckResource(ctx, ResourceBoundary{TenantID: r.TenantID, ResourceID: r.ResourceID}); err != nil {
			return Subject{}, status.Error(codes.PermissionDenied, "resource owner check denied")
		}
	}
	return Subject{principal: proto.Clone(d.GetPrincipal()).(*iamv1.PrincipalContext), credential: r.Credential, sourceOperation: r.SourceOperation, policyRevision: c.cfg.PolicyRevision, resourceID: r.ResourceID, decisionID: d.GetDecisionId()}, nil
}

func verifiedPeer(ctx context.Context, environment, domain string) (*iamv1.WorkloadPeer, error) {
	p, ok := peer.FromContext(ctx)
	if !ok || p == nil {
		return nil, status.Error(codes.Unauthenticated, "verified Workload peer is required")
	}
	info, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(info.State.VerifiedChains) == 0 || len(info.State.PeerCertificates) == 0 {
		return nil, status.Error(codes.Unauthenticated, "verified Workload TLS is required")
	}
	leaf := info.State.PeerCertificates[0]
	now := time.Now()
	if len(leaf.DNSNames) != 1 || len(leaf.IPAddresses) != 0 || len(leaf.URIs) != 0 || len(leaf.EmailAddresses) != 0 {
		return nil, status.Error(codes.Unauthenticated, "Workload identity is ambiguous")
	}
	clientUsage := false
	for _, usage := range leaf.ExtKeyUsage {
		if usage == x509.ExtKeyUsageClientAuth {
			clientUsage = true
		}
	}
	validChain := false
	for _, chain := range info.State.VerifiedChains {
		if len(chain) == 0 || !chain[0].Equal(leaf) {
			continue
		}
		current := true
		for _, cert := range chain {
			if now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) {
				current = false
				break
			}
		}
		if current {
			validChain = true
			break
		}
	}
	if !clientUsage || !validChain {
		return nil, status.Error(codes.Unauthenticated, "Workload certificate is not current")
	}
	identity := leaf.DNSNames[0]
	if identity == "" || identity != strings.ToLower(strings.TrimSpace(identity)) || strings.ContainsAny(identity, " */\r\n\t") {
		return nil, status.Error(codes.Unauthenticated, "Workload identity is invalid")
	}
	return &iamv1.WorkloadPeer{Environment: environment, TrustDomain: domain, IdentityKind: "x509_dns", IdentityValue: identity}, nil
}
