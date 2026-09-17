package grpcworkload

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type httpHumanIAM struct {
	iamv1.AuthorizationServiceClient
	mu                            sync.Mutex
	target                        HTTPWorkloadTarget
	revision                      string
	decision                      *iamv1.AuthorizationDecision
	callerFailure, subjectFailure codes.Code
	callerCalls, subjectCalls     int
	permission                    *iamv1.CheckPermissionRequest
}

func (f *httpHumanIAM) VerifyWorkloadCaller(_ context.Context, r *iamv1.VerifyWorkloadCallerRequest, _ ...grpc.CallOption) (*iamv1.VerifyWorkloadCallerResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callerCalls++
	if r.GetWorkloadToken() != "direct-caller-wat" || r.GetAudience() != f.target.Audience || r.GetOperationId() != f.target.Operation ||
		r.GetTargetRevision() != f.revision || r.GetRpcMethod() != "" || r.GetHttpMethod() != f.target.Method || r.GetHttpPath() != f.target.Path ||
		r.GetObservedPeer().GetIdentityValue() != "reader.test" {
		return nil, status.Error(codes.PermissionDenied, "incorrect direct caller evidence")
	}
	if f.callerFailure != codes.OK {
		return nil, status.Error(f.callerFailure, "caller denied")
	}
	return &iamv1.VerifyWorkloadCallerResponse{
		Caller:   &iamv1.DirectWorkloadCaller{PrincipalId: "verified-workload", BindingId: "verified-binding", PrincipalVersion: 1, BindingVersion: 1, GrantVersion: 1, Peer: r.ObservedPeer},
		Audience: r.Audience, OperationId: r.OperationId, HttpMethod: r.HttpMethod, HttpPath: r.HttpPath, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)),
	}, nil
}

func (f *httpHumanIAM) CheckPermission(_ context.Context, r *iamv1.CheckPermissionRequest, _ ...grpc.CallOption) (*iamv1.CheckPermissionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subjectCalls++
	f.permission = proto.Clone(r).(*iamv1.CheckPermissionRequest)
	if r.GetCredential().GetValue() != "human-access" || r.GetOperationId() != "ReadOwnerResource" || r.GetPolicyRevision() != "policy" {
		return nil, status.Error(codes.PermissionDenied, "incorrect Human authorization request")
	}
	if f.subjectFailure != codes.OK {
		return nil, status.Error(f.subjectFailure, "subject denied")
	}
	return &iamv1.CheckPermissionResponse{Decision: proto.Clone(f.decision).(*iamv1.AuthorizationDecision)}, nil
}

func humanHTTPDecision(tenant string) *iamv1.AuthorizationDecision {
	boundary := &iamv1.Boundary{Boundary: &iamv1.Boundary_Platform{Platform: &iamv1.PlatformBoundary{}}}
	if tenant != "" {
		boundary = &iamv1.Boundary{Boundary: &iamv1.Boundary_Tenant{Tenant: &iamv1.TenantBoundary{TenantId: tenant}}}
	}
	return &iamv1.AuthorizationDecision{Allowed: true, DecisionId: uuid.NewString(), PolicyRevision: "policy", Principal: &iamv1.PrincipalContext{
		PrincipalId: uuid.NewString(), PrincipalType: iamv1.PrincipalType_PRINCIPAL_TYPE_HUMAN, PrincipalStatus: iamv1.PrincipalStatus_PRINCIPAL_STATUS_ACTIVE,
		SessionId: uuid.NewString(), GrantId: uuid.NewString(), Boundary: boundary,
	}}
}

func TestHTTPHumanRealTLSCurrentCallerAndSubject(t *testing.T) {
	for _, scope := range []string{"platform", "tenant"} {
		t.Run(scope, func(t *testing.T) {
			registry := httpRegistryFixture(t)
			target, _ := RegisteredHTTPWorkloadTarget(registry, "snapshot-owner", "GET", "/api/v1/internal/snapshot")
			tenant := ""
			if scope == "tenant" {
				tenant = uuid.NewString()
			}
			f := &httpHumanIAM{target: target, revision: registry.Revision(target.Audience, target.Operation), decision: humanHTTPDecision(tenant)}
			c := &Client{authorization: f, cfg: ClientConfig{Registry: registry, Environment: "isolated", TrustDomain: "test", PolicyRevision: "policy", Timeout: time.Second}}
			serverFiles, clientFiles := httpTLSFixture(t)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var clean *http.Request
				var caller WorkloadCaller
				var subject Subject
				var err error
				if scope == "platform" {
					clean, caller, subject, err = c.AuthorizeHTTPPlatformHuman(r, target, "ReadOwnerResource")
				} else {
					clean, caller, subject, err = c.AuthorizeHTTPTenantHuman(r, target, AuthorizationRequest{TenantID: tenant, SourceOperation: "ReadOwnerResource"})
				}
				if err != nil {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				if clean.Header.Get("Authorization") != "" || clean.Header.Get(WorkloadHTTPTokenHeader) != "" || clean.Header.Get(WorkloadHTTPRevisionHeader) != "" ||
					r.Header.Get("Authorization") != "Bearer human-access" || caller.PrincipalID() != "verified-workload" || subject.DecisionID() == "" {
					t.Error("provenance or cloned credential boundary broken")
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			var err error
			server.TLS, err = serverFiles.ServerTLSConfig()
			if err != nil {
				t.Fatal(err)
			}
			server.StartTLS()
			defer server.Close()
			tlsConfig, err := clientFiles.config("snapshot.test", false)
			if err != nil {
				t.Fatal(err)
			}
			transport := &http.Transport{TLSClientConfig: tlsConfig}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: time.Second}
			request := func(change func(*http.Request)) int {
				t.Helper()
				r, _ := http.NewRequest("GET", server.URL+target.Path, nil)
				r.Header.Set("Authorization", "Bearer human-access")
				r.Header.Set(WorkloadHTTPTokenHeader, "direct-caller-wat")
				r.Header.Set(WorkloadHTTPRevisionHeader, f.revision)
				if change != nil {
					change(r)
				}
				response, err := client.Do(r)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				return response.StatusCode
			}
			if request(nil) != 204 {
				t.Fatal("authorized real TLS request refused")
			}
			f.mu.Lock()
			if f.permission.GetTarget().GetTenantId() != tenant {
				t.Error("explicit subject boundary changed")
			}
			f.mu.Unlock()
			for _, tc := range []struct {
				name   string
				change func(*http.Request)
			}{
				{"no Human", func(r *http.Request) { r.Header.Del("Authorization") }},
				{"duplicate Human", func(r *http.Request) { r.Header.Add("Authorization", "Bearer other") }},
				{"joined Human", func(r *http.Request) { r.Header.Set("Authorization", "Bearer human-access,Bearer other") }},
				{"whitespace", func(r *http.Request) { r.Header.Set("Authorization", "Bearer human access") }},
				{"wrong scheme", func(r *http.Request) { r.Header.Set("Authorization", "Basic human-access") }},
				{"cookie", func(r *http.Request) { r.Header.Set("Cookie", "session=other") }},
				{"proxy", func(r *http.Request) { r.Header.Set("Proxy-Authorization", "Bearer other") }},
				{"delegation", func(r *http.Request) { r.Header.Set(delegationMetadata, "other") }},
				{"no Workload", func(r *http.Request) { r.Header.Del(WorkloadHTTPTokenHeader) }},
				{"duplicate Workload", func(r *http.Request) { r.Header.Add(WorkloadHTTPTokenHeader, "other") }},
				{"revision", func(r *http.Request) { r.Header.Set(WorkloadHTTPRevisionHeader, "other") }},
				{"unknown route", func(r *http.Request) { r.URL.Path += "/other" }},
				{"encoded alias", func(r *http.Request) { r.URL.RawPath = "/api/v1/internal/%73napshot" }},
			} {
				f.mu.Lock()
				before := f.subjectCalls
				f.mu.Unlock()
				if request(tc.change) != 403 {
					t.Fatal("invalid request admitted", tc.name)
				}
				f.mu.Lock()
				after := f.subjectCalls
				f.mu.Unlock()
				if after != before {
					t.Fatal("invalid request reached subject authorization", tc.name)
				}
			}
			for _, code := range []codes.Code{codes.PermissionDenied, codes.Unauthenticated, codes.Unavailable} {
				f.mu.Lock()
				f.callerFailure = code
				before := f.subjectCalls
				f.mu.Unlock()
				if request(nil) != 403 {
					t.Fatal("current caller refusal admitted", code)
				}
				f.mu.Lock()
				after := f.subjectCalls
				f.mu.Unlock()
				if after != before {
					t.Fatal("caller refusal reached Human check")
				}
			}
			f.mu.Lock()
			f.callerFailure = codes.OK
			f.subjectFailure = codes.PermissionDenied
			f.mu.Unlock()
			if request(nil) != 403 {
				t.Fatal("current subject revocation admitted")
			}
			f.mu.Lock()
			f.subjectFailure = codes.OK
			f.decision = humanHTTPDecision(uuid.NewString())
			f.mu.Unlock()
			if request(nil) != 403 {
				t.Fatal("wrong or cross-Tenant subject boundary admitted")
			}
			for _, change := range []func(*iamv1.AuthorizationDecision){
				func(d *iamv1.AuthorizationDecision) {
					d.Principal.PrincipalType = iamv1.PrincipalType_PRINCIPAL_TYPE_WORKLOAD
				},
				func(d *iamv1.AuthorizationDecision) {
					d.Principal.PrincipalStatus = iamv1.PrincipalStatus_PRINCIPAL_STATUS_DISABLED
				},
				func(d *iamv1.AuthorizationDecision) { d.Principal.SessionId = "" },
				func(d *iamv1.AuthorizationDecision) { d.DecisionId = "" },
				func(d *iamv1.AuthorizationDecision) { d.PolicyRevision = "old" },
			} {
				f.mu.Lock()
				f.decision = humanHTTPDecision(tenant)
				change(f.decision)
				f.mu.Unlock()
				if request(nil) != 403 {
					t.Fatal("invalid current Human decision admitted")
				}
			}
			f.mu.Lock()
			f.decision = humanHTTPDecision(tenant)
			f.mu.Unlock()
			if request(nil) != 204 {
				t.Fatal("recovery requires fresh current authorization")
			}
		})
	}
}

func TestHTTPTenantHumanRequiresExplicitBoundary(t *testing.T) {
	c := &Client{}
	for _, request := range []AuthorizationRequest{
		{}, {TenantID: "not-an-id"}, {TenantID: uuid.Nil.String()},
		{TenantID: uuid.NewString(), Credential: "injected"},
	} {
		if _, _, _, err := c.AuthorizeHTTPTenantHuman(nil, HTTPWorkloadTarget{}, request); status.Code(err) != codes.InvalidArgument {
			t.Fatal("missing or caller-selected authority accepted")
		}
	}
}
