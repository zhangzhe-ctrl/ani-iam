package grpcworkload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
	"google.golang.org/grpc"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type authorityVerifierFake struct {
	*httpVerifierFake
	proof atomic.Value
}

func (f *authorityVerifierFake) VerifyWorkloadCaller(ctx context.Context, r *iamv1.VerifyWorkloadCallerRequest, o ...grpc.CallOption) (*iamv1.VerifyWorkloadCallerResponse, error) {
	reply, err := f.httpVerifierFake.VerifyWorkloadCaller(ctx, r, o...)
	if err == nil {
		reply.AuthorityRevision = f.proof.Load().(string)
	}
	return reply, err
}
func TestHTTPAuthorityProofRequiredAndOpaque(t *testing.T) {
	for _, grouped := range []bool{false, true} {
		t.Run(map[bool]string{true: "grouped", false: "ungrouped"}[grouped], func(t *testing.T) {
			registry := httpRegistryFixture(t)
			if grouped {
				targets := registry.Targets()
				for i := range targets {
					if targets[i].Operation == "snapshot.read" {
						targets[i].AuthorityOperations = []string{"snapshot.page", "snapshot.read"}
					}
				}
				targets = append(targets, workloadregistry.Target{Audience: "snapshot-owner", Operation: "snapshot.page", HTTPMethod: "POST", HTTPPath: "/api/v1/internal/page", Mechanism: workloadregistry.WorkloadOnly, GrantScope: "snapshot_read", ReceiverOperation: "snapshot.receive", Enabled: true, AuthorityOperations: []string{"snapshot.page", "snapshot.read"}})
				raw, _ := json.Marshal(workloadregistry.Document{Schema: workloadregistry.Schema, Targets: targets})
				sum := sha256.Sum256(raw)
				var e error
				registry, e = workloadregistry.Parse(raw, hex.EncodeToString(sum[:]))
				if e != nil {
					t.Fatal(e)
				}
			}
			target, _ := RegisteredHTTPWorkloadTarget(registry, "snapshot-owner", "GET", "/api/v1/internal/snapshot")
			fake := &authorityVerifierFake{httpVerifierFake: &httpVerifierFake{t: t, revision: registry.Revision(target.Audience, target.Operation)}}
			fake.proof.Store("")
			verifier := &WorkloadOnlyClient{client: &Client{authorization: fake, cfg: ClientConfig{Registry: registry, Environment: "isolated", TrustDomain: "test", Timeout: time.Second}}}
			serverFiles, clientFiles := httpTLSFixture(t)
			var observed atomic.Value
			observed.Store("")
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, caller, e := verifier.VerifyHTTPCaller(r, target)
				if e != nil {
					w.WriteHeader(403)
					return
				}
				observed.Store(caller.AuthorityRevision())
				w.WriteHeader(204)
			}))
			var e error
			server.TLS, e = serverFiles.ServerTLSConfig()
			if e != nil {
				t.Fatal(e)
			}
			server.StartTLS()
			defer server.Close()
			client, e := NewWorkloadHTTPClient(server.URL, "snapshot.test", "snapshot-owner", clientFiles, registry, func(context.Context, HTTPWorkloadTarget) (string, error) { return "only-workload", nil })
			if e != nil {
				t.Fatal(e)
			}
			defer client.CloseIdleConnections()
			for _, proof := range []string{"", "wa1:" + strings.Repeat("a", 64), "wa1:" + strings.Repeat("A", 64), "wa2:" + strings.Repeat("a", 64), "wa1:" + strings.Repeat("a", 63), "wa1:" + strings.Repeat("g", 64), "wa1:" + strings.Repeat("a", 64) + "\n"} {
				fake.proof.Store(proof)
				r, _ := http.NewRequest("GET", server.URL+target.Path, nil)
				resp, e := client.Do(r)
				if e != nil {
					t.Fatal(e)
				}
				resp.Body.Close()
				accepted := (!grouped && proof == "") || (grouped && proof == "wa1:"+strings.Repeat("a", 64))
				if (resp.StatusCode == 204) != accepted {
					t.Fatalf("proof accepted=%v want=%v", resp.StatusCode == 204, accepted)
				}
				if accepted && observed.Load().(string) != proof {
					t.Fatal("IAM proof changed at SDK boundary")
				}
			}
		})
	}
}
