package grpcworkload

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func httpRegistryFixture(t *testing.T) *workloadregistry.Registry {
	t.Helper()
	d := workloadregistry.Document{Schema: workloadregistry.Schema, Targets: []workloadregistry.Target{
		{Audience: "snapshot-owner", Operation: "snapshot.receive", Mechanism: workloadregistry.Receiver, GrantScope: "receiver", Enabled: true},
		{Audience: "snapshot-owner", Operation: "snapshot.read", HTTPMethod: "GET", HTTPPath: "/api/v1/internal/snapshot", Mechanism: workloadregistry.WorkloadOnly, GrantScope: "snapshot_read", ReceiverOperation: "snapshot.receive", Enabled: true},
	}}
	raw, _ := json.Marshal(d)
	digest := sha256.Sum256(raw)
	r, err := workloadregistry.Parse(raw, hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Certificates are fresh test identities and are exercised through an actual
// TLS handshake. This unit test fakes only the online IAM client, not the peer.
func httpTLSFixture(t *testing.T) (TLSFiles, TLSFiles) {
	t.Helper()
	dir := t.TempDir()
	now := time.Now()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	raw, err := x509.CreateCertificate(rand.Reader, ca, ca, public, private)
	if err != nil {
		t.Fatal(err)
	}
	caPath := filepath.Join(dir, "ca.pem")
	if err = os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}), 0600); err != nil {
		t.Fatal(err)
	}
	leaf := func(name string, serial int64, usage x509.ExtKeyUsage) TLSFiles {
		pub, key, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			t.Fatal(e)
		}
		cert := &x509.Certificate{SerialNumber: big.NewInt(serial), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), DNSNames: []string{name}, ExtKeyUsage: []x509.ExtKeyUsage{usage}, KeyUsage: x509.KeyUsageDigitalSignature}
		der, e := x509.CreateCertificate(rand.Reader, cert, ca, pub, private)
		if e != nil {
			t.Fatal(e)
		}
		keyDER, e := x509.MarshalPKCS8PrivateKey(key)
		if e != nil {
			t.Fatal(e)
		}
		f := TLSFiles{CertificateFile: filepath.Join(dir, name+".pem"), PrivateKeyFile: filepath.Join(dir, name+".key"), CAFile: caPath}
		if e = os.WriteFile(f.CertificateFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(f.PrivateKeyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600); e != nil {
			t.Fatal(e)
		}
		return f
	}
	return leaf("snapshot.test", 2, x509.ExtKeyUsageServerAuth), leaf("reader.test", 3, x509.ExtKeyUsageClientAuth)
}

type httpVerifierFake struct {
	iamv1.AuthorizationServiceClient
	calls      atomic.Int64
	failure    atomic.Int32
	wrongReply atomic.Bool
	t          *testing.T
	revision   string
}

func (f *httpVerifierFake) VerifyWorkloadCaller(_ context.Context, r *iamv1.VerifyWorkloadCallerRequest, _ ...grpc.CallOption) (*iamv1.VerifyWorkloadCallerResponse, error) {
	f.calls.Add(1)
	if r.GetRpcMethod() != "" || r.GetHttpMethod() != "GET" || r.GetHttpPath() != "/api/v1/internal/snapshot" || r.GetTargetRevision() != f.revision || r.GetObservedPeer().GetIdentityValue() != "reader.test" || r.GetWorkloadToken() != "only-workload" {
		f.t.Error("incorrect online HTTP proof")
	}
	if c := codes.Code(f.failure.Load()); c != codes.OK {
		return nil, status.Error(c, "online refusal")
	}
	reply := &iamv1.VerifyWorkloadCallerResponse{Caller: &iamv1.DirectWorkloadCaller{PrincipalId: "verified-reader", BindingId: "verified-binding", PrincipalVersion: 1, BindingVersion: 1, GrantVersion: 1, Peer: r.ObservedPeer}, Audience: r.Audience, OperationId: r.OperationId, HttpMethod: r.HttpMethod, HttpPath: r.HttpPath, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}
	if f.wrongReply.Load() {
		reply.HttpMethod = "POST"
	}
	return reply, nil
}

func TestHTTPWorkloadRealTLSAndOnlineBoundary(t *testing.T) {
	registry := httpRegistryFixture(t)
	target, _ := RegisteredHTTPWorkloadTarget(registry, "snapshot-owner", "GET", "/api/v1/internal/snapshot")
	fake := &httpVerifierFake{t: t, revision: registry.Revision(target.Audience, target.Operation)}
	verifier := &WorkloadOnlyClient{client: &Client{authorization: fake, cfg: ClientConfig{Registry: registry, Environment: "isolated", TrustDomain: "test", Timeout: time.Second}}}
	serverFiles, clientFiles := httpTLSFixture(t)
	var dispatched atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// These are negative protocol cases over a real authenticated connection.
		switch r.URL.Query().Get("attack") {
		case "duplicate":
			r.Header.Add(WorkloadHTTPTokenHeader, "extra")
		case "revision":
			r.Header.Set(WorkloadHTTPRevisionHeader, "wrong")
		case "human":
			r.Header.Set("Authorization", "Bearer human")
		case "cookie":
			r.Header.Set("Cookie", "session=human")
		case "no-tls":
			r = r.Clone(r.Context())
			r.TLS = nil
		case "alias":
			r.URL.RawPath = "/api/v1/internal/%73napshot"
		}
		clean, caller, err := verifier.VerifyHTTPCaller(r, target)
		if err != nil {
			http.Error(w, "refused", http.StatusForbidden)
			return
		}
		if clean.Header.Get(WorkloadHTTPTokenHeader) != "" || clean.Header.Get(WorkloadHTTPRevisionHeader) != "" || caller.PrincipalID() != "verified-reader" {
			t.Error("credential escaped or wrong owner identity")
		}
		dispatched.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	var err error
	server.TLS, err = serverFiles.ServerTLSConfig()
	if err != nil {
		t.Fatal(err)
	}
	server.StartTLS()
	defer server.Close()
	client, err := NewWorkloadHTTPClient(server.URL, "snapshot.test", "snapshot-owner", clientFiles, registry, func(context.Context, HTTPWorkloadTarget) (string, error) { return "only-workload", nil })
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	request := func(suffix string) int {
		t.Helper()
		r, _ := http.NewRequest("GET", server.URL+target.Path+suffix, nil)
		r.Header.Set("Authorization", "ambient-human")
		resp, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if request("") != 204 || dispatched.Load() != 1 || fake.calls.Load() != 1 {
		t.Fatal("real TLS HTTP call failed")
	}
	for _, attack := range []string{"duplicate", "revision", "human", "cookie", "no-tls", "alias"} {
		before := fake.calls.Load()
		if request("?attack="+attack) != 403 || fake.calls.Load() != before {
			t.Fatal("invalid HTTP reached IAM", attack)
		}
	}
	for _, code := range []codes.Code{codes.PermissionDenied, codes.Unauthenticated, codes.Unavailable} {
		fake.failure.Store(int32(code))
		if request("") != 403 {
			t.Fatal("online refusal admitted")
		}
	}
	fake.failure.Store(0)
	fake.wrongReply.Store(true)
	if request("") != 403 {
		t.Fatal("mismatched reply admitted")
	}
	fake.wrongReply.Store(false)
	if request("") != 204 || dispatched.Load() != 2 {
		t.Fatal("recovery failed or unauthorized handler ran")
	}
	// Header identity cannot replace a client certificate at the TLS listener.
	plain := server.Client()
	resp, e := plain.Get(server.URL + target.Path)
	if e == nil {
		resp.Body.Close()
		t.Fatal("missing client certificate admitted")
	}
	before := fake.calls.Load()
	r, _ := http.NewRequest("POST", server.URL+target.Path, nil)
	if resp, e = client.Do(r); e == nil {
		resp.Body.Close()
		t.Fatal("unregistered verb sent")
	}
	if fake.calls.Load() != before {
		t.Fatal("unknown target verified")
	}
}
