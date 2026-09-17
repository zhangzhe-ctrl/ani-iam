package grpcworkload

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/url"
	"time"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const WorkloadHTTPTokenHeader = "X-ANI-Workload-Token"
const WorkloadHTTPRevisionHeader = "X-ANI-Target-Revision"

type HTTPWorkloadTarget struct{ Audience, Operation, Method, Path string }
type HTTPWorkloadTokenSource func(context.Context, HTTPWorkloadTarget) (string, error)

func RegisteredHTTPWorkloadTarget(r *workloadregistry.Registry, audience, method, path string) (HTTPWorkloadTarget, error) {
	t, ok := r.HTTP(audience, method, path)
	if !ok || !t.Enabled || t.Mechanism != workloadregistry.WorkloadOnly {
		return HTTPWorkloadTarget{}, ErrConfiguration
	}
	return HTTPWorkloadTarget{t.Audience, t.Operation, t.HTTPMethod, t.HTTPPath}, nil
}

func (t HTTPWorkloadTarget) validate(r *workloadregistry.Registry) error {
	expected, err := RegisteredHTTPWorkloadTarget(r, t.Audience, t.Method, t.Path)
	if err != nil || expected != t {
		return ErrConfiguration
	}
	return nil
}

func (c *WorkloadOnlyClient) HTTPTokenSource(ctx context.Context, target HTTPWorkloadTarget) (string, error) {
	if err := target.validate(c.client.cfg.Registry); err != nil {
		return "", err
	}
	call, cancel := c.client.deadline(ctx)
	defer cancel()
	issued, err := c.client.authentication.IssueWorkloadToken(call, &iamv1.IssueWorkloadTokenRequest{Audience: target.Audience, OperationId: target.Operation, TargetRevision: c.client.cfg.Registry.Revision(target.Audience, target.Operation)})
	if err != nil {
		return "", err
	}
	if issued.GetWorkloadToken() == "" || issued.GetExpiresAt() == nil || issued.GetExpiresAt().CheckValid() != nil || !time.Now().Before(issued.GetExpiresAt().AsTime()) {
		return "", status.Error(codes.Unauthenticated, "IAM returned no current Workload credential")
	}
	return issued.GetWorkloadToken(), nil
}

// CheckHTTP exercises the receiver's current Verify and same-audience Grants.
// Only the exact invalid-empty-credential response counts as a healthy probe.
func (c *WorkloadOnlyClient) CheckHTTP(ctx context.Context, target HTTPWorkloadTarget) error {
	if target.validate(c.client.cfg.Registry) != nil {
		return ErrConfiguration
	}
	call, cancel := c.client.deadline(ctx)
	defer cancel()
	reply, err := healthv1.NewHealthClient(c.client.conn).Check(call, &healthv1.HealthCheckRequest{})
	if err != nil {
		return err
	}
	if reply.GetStatus() != healthv1.HealthCheckResponse_SERVING {
		return status.Error(codes.Unavailable, "IAM is not ready")
	}
	return c.CheckHTTPAuthority(ctx, target)
}

// CheckHTTPAuthority validates the current receiver Grants without depending
// on global readiness. A dependency owner uses this during mutual startup,
// where its availability is itself one of IAM's readiness dependencies.
func (c *WorkloadOnlyClient) CheckHTTPAuthority(ctx context.Context, target HTTPWorkloadTarget) error {
	if target.validate(c.client.cfg.Registry) != nil {
		return ErrConfiguration
	}
	call, cancel := c.client.deadline(ctx)
	defer cancel()
	_, err := c.client.authorization.VerifyWorkloadCaller(call, &iamv1.VerifyWorkloadCallerRequest{
		Audience: target.Audience, OperationId: target.Operation, HttpMethod: target.Method, HttpPath: target.Path,
		TargetRevision: c.client.cfg.Registry.Revision(target.Audience, target.Operation),
		ObservedPeer:   &iamv1.WorkloadPeer{Environment: c.client.cfg.Environment, TrustDomain: c.client.cfg.TrustDomain},
	})
	if status.Code(err) == codes.Unauthenticated {
		for _, detail := range status.Convert(err).Details() {
			if info, ok := detail.(*errdetails.ErrorInfo); ok && info.Domain == "iam.ani.internal" && info.Reason == "CREDENTIAL_INVALID" {
				return nil
			}
		}
	}
	if err != nil {
		return err
	}
	return status.Error(codes.Unavailable, "IAM HTTP verification probe did not reject its empty credential")
}

// VerifyHTTPCaller accepts only a real net/http TLS connection state. HTTP
// headers never substitute for the peer, and no gRPC peer context is created.
// The returned clone removes credentials before it reaches the owner handler.
func (c *WorkloadOnlyClient) VerifyHTTPCaller(r *http.Request, target HTTPWorkloadTarget) (*http.Request, WorkloadCaller, error) {
	if r == nil || r.URL == nil || target.validate(c.client.cfg.Registry) != nil || r.Method != target.Method || r.URL.Path != target.Path || r.URL.RawPath != "" || r.URL.Opaque != "" {
		return nil, WorkloadCaller{}, status.Error(codes.PermissionDenied, "HTTP target is not registered")
	}
	if r.TLS == nil || !r.TLS.HandshakeComplete || r.TLS.Version < tls.VersionTLS13 {
		return nil, WorkloadCaller{}, status.Error(codes.Unauthenticated, "verified Workload TLS is required")
	}
	observed, err := verifiedTLSState(r.TLS, c.client.cfg.Environment, c.client.cfg.TrustDomain)
	if err != nil {
		return nil, WorkloadCaller{}, err
	}
	wat, revision := r.Header.Values(WorkloadHTTPTokenHeader), r.Header.Values(WorkloadHTTPRevisionHeader)
	if len(wat) != 1 || wat[0] == "" || len(revision) != 1 || revision[0] != c.client.cfg.Registry.Revision(target.Audience, target.Operation) {
		return nil, WorkloadCaller{}, status.Error(codes.Unauthenticated, "one versioned Workload credential is required")
	}
	for _, key := range []string{"Authorization", "Proxy-Authorization", "Cookie", delegationMetadata} {
		if len(r.Header.Values(key)) != 0 {
			return nil, WorkloadCaller{}, status.Error(codes.Unauthenticated, "Workload-only credentials are required")
		}
	}
	call, cancel := c.client.deadline(r.Context())
	defer cancel()
	reply, err := c.client.authorization.VerifyWorkloadCaller(call, &iamv1.VerifyWorkloadCallerRequest{WorkloadToken: wat[0], Audience: target.Audience, OperationId: target.Operation, TargetRevision: revision[0], HttpMethod: r.Method, HttpPath: r.URL.Path, ObservedPeer: observed})
	if err != nil {
		return nil, WorkloadCaller{}, err
	}
	caller := reply.GetCaller()
	if caller.GetPrincipalId() == "" || caller.GetBindingId() == "" || caller.GetPrincipalVersion() <= 0 || caller.GetBindingVersion() <= 0 || caller.GetGrantVersion() <= 0 || !proto.Equal(caller.GetPeer(), observed) || reply.GetAudience() != target.Audience || reply.GetOperationId() != target.Operation || reply.GetRpcMethod() != "" || reply.GetHttpMethod() != target.Method || reply.GetHttpPath() != target.Path || reply.GetExpiresAt() == nil || reply.GetExpiresAt().CheckValid() != nil || !time.Now().Before(reply.GetExpiresAt().AsTime()) {
		return nil, WorkloadCaller{}, status.Error(codes.PermissionDenied, "IAM verification did not match the HTTP call")
	}
	registration, _ := c.client.cfg.Registry.Lookup(target.Audience, target.Operation)
	authority := reply.GetAuthorityRevision()
	if (len(registration.AuthorityOperations) != 0 && !validAuthorityRevision(authority)) || (len(registration.AuthorityOperations) == 0 && authority != "") {
		return nil, WorkloadCaller{}, status.Error(codes.PermissionDenied, "IAM verification did not match the declared authority scope")
	}
	clean := r.Clone(r.Context())
	clean.Header.Del(WorkloadHTTPTokenHeader)
	clean.Header.Del(WorkloadHTTPRevisionHeader)
	return clean, WorkloadCaller{principalID: caller.GetPrincipalId(), authorityRevision: authority}, nil
}

func validAuthorityRevision(value string) bool {
	if len(value) != 68 || value[:4] != "wa1:" {
		return false
	}
	for _, c := range value[4:] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

type workloadHTTPTransport struct {
	origin    string
	audience  string
	transport *http.Transport
	registry  *workloadregistry.Registry
	source    HTTPWorkloadTokenSource
}

// NewWorkloadHTTPClient fixes one HTTPS origin and reviewed exact targets. It
// neither follows redirects nor retries a business request after an error.
func NewWorkloadHTTPClient(origin, serverName, audience string, files TLSFiles, registry *workloadregistry.Registry, source HTTPWorkloadTokenSource) (*http.Client, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || registry == nil || source == nil || audience == "" {
		return nil, ErrConfiguration
	}
	cfg, err := files.config(serverName, false)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{TLSClientConfig: cfg, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second, DisableKeepAlives: true}
	return &http.Client{Transport: &workloadHTTPTransport{origin: u.Host, audience: audience, transport: transport, registry: registry, source: source}, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

func (t *workloadHTTPTransport) CloseIdleConnections() { t.transport.CloseIdleConnections() }
func (t *workloadHTTPTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != t.origin || r.URL.User != nil || r.URL.Opaque != "" || r.URL.Fragment != "" || r.URL.RawPath != "" {
		return nil, ErrConfiguration
	}
	target, err := RegisteredHTTPWorkloadTarget(t.registry, t.audience, r.Method, r.URL.Path)
	if err != nil {
		return nil, err
	}
	call, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	token, err := t.source(call, target)
	cancel()
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, status.Error(codes.Unauthenticated, "Workload credential is required")
	}
	clean := r.Clone(r.Context())
	for _, key := range []string{"Authorization", "Proxy-Authorization", "Cookie", delegationMetadata, WorkloadHTTPTokenHeader, WorkloadHTTPRevisionHeader} {
		clean.Header.Del(key)
	}
	clean.Header.Set(WorkloadHTTPTokenHeader, token)
	clean.Header.Set(WorkloadHTTPRevisionHeader, t.registry.Revision(target.Audience, target.Operation))
	// Keep-alive reuse is disabled to avoid transport retries of read requests.
	clean.GetBody = nil
	return t.transport.RoundTrip(clean)
}
