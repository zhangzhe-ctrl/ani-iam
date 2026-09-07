package contracts_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

const (
	iamDescriptorRelative  = "api/iam/v1/iam_descriptor.pb"
	coreDescriptorRelative = "tests/contracts/artifacts/core_tenant_integration_v1_descriptor.pb"
)

var iamServices = map[string][]string{
	"iam.v1.AuthenticationService": {
		"BeginOIDCIdentityLink", "BeginOIDCLogin", "CompleteOIDCIdentityLink",
		"CompleteOIDCLogin", "CompletePasswordAction", "IssueServiceToken",
		"ListSessions", "LogoutSession", "PasswordLogin", "RefreshSession",
		"RequestPasswordAction", "RevokeAllSessions", "RevokeSession",
		"SwitchTenant", "ValidatePrincipal",
	},
	"iam.v1.AuthorizationService": {"CheckPermission"},
	"iam.v1.IAMAdminService": {
		"AcceptPlatformInvitation", "AcceptTenantInvitation", "ApproveRecoveryBootstrap",
		"ApproveRestoreTenantAdmin", "BindPlatformRole", "BindTenantRole",
		"CancelPlatformInvitation", "CancelTenantInvitation", "CreateAPIKey",
		"CreatePlatformInvitation", "CreatePlatformRole", "CreateServicePrincipal",
		"CreateTenantInvitation", "CreateTenantRole", "DeletePlatformRole",
		"DeleteTenantRole", "ExecuteRecoveryBootstrap", "ExecuteRestoreTenantAdmin",
		"GetAuditEvent", "GetPlatformAuditEvent", "GetPlatformInvitation",
		"GetPlatformMembership", "GetPlatformRole", "GetServicePrincipal",
		"GetTenantAccess", "GetTenantInvitation", "GetTenantMembership", "GetTenantRole",
		"ListAPIKeys", "ListAuditEvents", "ListPlatformAuditEvents",
		"ListPlatformInvitations", "ListPlatformMemberships", "ListPlatformRoles",
		"ListServicePrincipals", "ListTenantInvitations", "ListTenantMemberships",
		"ListTenantRoles", "RemovePlatformMembership", "RemoveTenantMembership",
		"RequestRecoveryBootstrap", "RequestRestoreTenantAdmin", "ResendPlatformInvitation",
		"ResendTenantInvitation", "RevokeAPIKey", "UnbindPlatformRole", "UnbindTenantRole",
		"UpdatePlatformMembership", "UpdatePlatformRole", "UpdateServicePrincipal",
		"UpdateTenantAccess", "UpdateTenantMembership", "UpdateTenantRole",
	},
}

var coreServices = map[string][]string{
	"tenant.integration.v1.TenantIAMIntegrationService": {
		"BeginTenantLifecycleSnapshot", "ListTenantLifecycleSnapshotPage",
	},
}

var expectedFixtureNames = []string{
	"core_error_contract.v1.json",
	"core_tenant_iam_bootstrap_requested.v1.json",
	"core_tenant_lifecycle_changed.v1.json",
	"core_tenant_lifecycle_heartbeat.v1.json",
	"core_tenant_lifecycle_snapshot_page.v1.json",
	"iam_check_permission.v1.json",
	"iam_error_contract.v1.json",
	"iam_password_login.v1.json",
}

type errorContract struct {
	Domain  string      `json:"domain"`
	Reasons []errorRule `json:"reasons"`
}

type errorRule struct {
	Reason           string   `json:"reason"`
	GRPCCode         string   `json:"grpc_code"`
	RequiredMetadata []string `json:"required_metadata"`
}

type contractPins struct {
	SchemaVersion  string             `json:"schema_version"`
	IAMStartCommit string             `json:"iam_start_commit"`
	ANIStartCommit string             `json:"ani_start_commit"`
	PolicyRevision string             `json:"policy_revision"`
	Notification   notificationPin    `json:"notification"`
	Toolchain      map[string]toolPin `json:"toolchain"`
	Artifacts      map[string]string  `json:"artifacts"`
	Fixtures       map[string]string  `json:"fixtures"`
}

type toolPin struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type notificationPin struct {
	Repository       string `json:"repository"`
	Commit           string `json:"commit"`
	RuntimeCommit    string `json:"runtime_commit"`
	ModuleVersion    string `json:"module_version"`
	ModuleSum        string `json:"module_sum"`
	ProtoSHA256      string `json:"proto_sha256"`
	DescriptorSHA256 string `json:"descriptor_sha256"`
}

func TestIAMDescriptorHasOnlyTargetServices(t *testing.T) {
	set := loadDescriptorSet(t, iamDescriptorRelative)
	assertServiceInventory(t, set, iamServices)
	for _, file := range set.File {
		for _, service := range file.Service {
			fullName := file.GetPackage() + "." + service.GetName()
			if fullName == "auth.v1.AuthService" {
				t.Fatal("legacy auth.v1.AuthService leaked into the target descriptor")
			}
		}
	}
	assertMessageFields(t, set, "iam.v1.CheckPermissionRequest", []string{
		"credential", "operation_id", "policy_revision", "target",
	})
	assertMessageFields(t, set, "iam.v1.AuthorizationDecision", []string{
		"allowed", "decision_id", "obligations", "policy_revision", "principal", "reason",
	})
}

func TestCoreDescriptorOwnsLifecycleBootstrapAndSnapshot(t *testing.T) {
	set := loadDescriptorSet(t, coreDescriptorRelative)
	assertServiceInventory(t, set, coreServices)
	assertMessageFields(t, set, "tenant.integration.v1.IntegrationEnvelope", []string{
		"aggregate_version", "causation_id", "correlation_id", "event_id", "occurred_at",
		"operation_id", "producer", "schema_major", "tenant_id", "traceparent",
	})
	assertMessageFields(t, set, "tenant.integration.v1.TenantLifecycleChanged", []string{
		"effective_at", "envelope", "reason", "status",
	})
	assertMessageFields(t, set, "tenant.integration.v1.TenantIAMBootstrapRequested", []string{
		"envelope", "intended_administrator", "payload_fingerprint",
	})
	assertMessageFields(t, set, "tenant.integration.v1.TenantLifecycleSnapshotPage", []string{
		"cursor", "items", "next_page_token", "snapshot_version",
	})
	for _, banned := range []string{"UpdateTenantLifecycle", "CreateTenant", "DeleteTenant"} {
		if hasMethod(set, banned) {
			t.Fatalf("IAM-facing Core contract exposes forbidden lifecycle writer %s", banned)
		}
	}
}

func TestProducerConsumerFixturesRoundTripStrictly(t *testing.T) {
	iam := loadDescriptorSet(t, iamDescriptorRelative)
	core := loadDescriptorSet(t, coreDescriptorRelative)
	cases := []struct {
		file       string
		message    protoreflect.FullName
		descriptor *descriptorpb.FileDescriptorSet
	}{
		{"iam_password_login.v1.json", "iam.v1.PasswordLoginRequest", iam},
		{"iam_check_permission.v1.json", "iam.v1.CheckPermissionRequest", iam},
		{"core_tenant_lifecycle_changed.v1.json", "tenant.integration.v1.TenantLifecycleChanged", core},
		{"core_tenant_iam_bootstrap_requested.v1.json", "tenant.integration.v1.TenantIAMBootstrapRequested", core},
		{"core_tenant_lifecycle_heartbeat.v1.json", "tenant.integration.v1.TenantLifecycleHeartbeat", core},
		{"core_tenant_lifecycle_snapshot_page.v1.json", "tenant.integration.v1.TenantLifecycleSnapshotPage", core},
	}
	for _, test := range cases {
		t.Run(test.file, func(t *testing.T) {
			data := readFile(t, filepath.Join("tests/contracts/fixtures", test.file))
			files, err := protodesc.NewFiles(test.descriptor)
			if err != nil {
				t.Fatalf("build descriptor registry: %v", err)
			}
			descriptor, err := files.FindDescriptorByName(test.message)
			if err != nil {
				t.Fatalf("find %s: %v", test.message, err)
			}
			messageDescriptor, ok := descriptor.(protoreflect.MessageDescriptor)
			if !ok {
				t.Fatalf("%s is not a message", test.message)
			}
			message := dynamicpb.NewMessage(messageDescriptor)
			if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(data, message); err != nil {
				t.Fatalf("strictly decode fixture: %v", err)
			}
			rendered, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(message)
			if err != nil || len(rendered) == 0 {
				t.Fatalf("round-trip fixture: bytes=%d error=%v", len(rendered), err)
			}
		})
	}
}

func TestBootstrapPayloadFingerprint(t *testing.T) {
	var fixture struct {
		Envelope struct {
			EventID          string `json:"event_id"`
			SchemaMajor      uint32 `json:"schema_major"`
			Producer         string `json:"producer"`
			TenantID         string `json:"tenant_id"`
			AggregateVersion string `json:"aggregate_version"`
			OccurredAt       string `json:"occurred_at"`
			CorrelationID    string `json:"correlation_id"`
			CausationID      string `json:"causation_id"`
			OperationID      string `json:"operation_id"`
			Traceparent      string `json:"traceparent"`
		} `json:"envelope"`
		IntendedAdministrator struct {
			NormalizedEmail string `json:"normalized_email"`
			Locale          string `json:"locale"`
		} `json:"intended_administrator"`
		PayloadFingerprint string `json:"payload_fingerprint"`
	}
	decodeJSON(t, "tests/contracts/fixtures/core_tenant_iam_bootstrap_requested.v1.json", &fixture)
	payload := struct {
		Locale          string `json:"locale"`
		NormalizedEmail string `json:"normalized_email"`
		OperationID     string `json:"operation_id"`
		TenantID        string `json:"tenant_id"`
	}{fixture.IntendedAdministrator.Locale, fixture.IntendedAdministrator.NormalizedEmail, fixture.Envelope.OperationID, fixture.Envelope.TenantID}
	canonical, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	want := "sha256:" + sha256Hex(canonical)
	if fixture.PayloadFingerprint != want {
		t.Fatalf("payload fingerprint = %s, want %s", fixture.PayloadFingerprint, want)
	}
}

func TestStableErrorInfoContracts(t *testing.T) {
	tests := map[string]struct {
		domain     string
		descriptor *descriptorpb.FileDescriptorSet
		enum       protoreflect.FullName
		prefix     string
	}{
		"iam_error_contract.v1.json": {
			domain:     "iam.ani.internal",
			descriptor: loadDescriptorSet(t, iamDescriptorRelative),
			enum:       "iam.v1.IAMErrorReason",
			prefix:     "IAM_ERROR_REASON_",
		},
		"core_error_contract.v1.json": {
			domain:     "core.ani.internal",
			descriptor: loadDescriptorSet(t, coreDescriptorRelative),
			enum:       "tenant.integration.v1.CoreIntegrationErrorReason",
			prefix:     "CORE_INTEGRATION_ERROR_REASON_",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var contract errorContract
			decodeJSON(t, filepath.Join("tests/contracts/fixtures", name), &contract)
			if contract.Domain != test.domain || len(contract.Reasons) == 0 {
				t.Fatalf("domain=%q reasons=%d", contract.Domain, len(contract.Reasons))
			}
			declaredReasons := enumReasons(t, test.descriptor, test.enum, test.prefix)
			seen := map[string]struct{}{}
			for _, rule := range contract.Reasons {
				if _, duplicate := seen[rule.Reason]; duplicate {
					t.Fatalf("duplicate reason %s", rule.Reason)
				}
				seen[rule.Reason] = struct{}{}
				if _, declared := declaredReasons[rule.Reason]; !declared {
					t.Fatalf("reason %s is absent from %s", rule.Reason, test.enum)
				}
				code, ok := grpcCode(rule.GRPCCode)
				if !ok {
					t.Fatalf("unknown gRPC code %q", rule.GRPCCode)
				}
				metadata := make(map[string]string, len(rule.RequiredMetadata))
				for _, key := range rule.RequiredMetadata {
					if strings.TrimSpace(key) == "" {
						t.Fatalf("empty metadata key for %s", rule.Reason)
					}
					metadata[key] = "fixture"
				}
				withDetails, err := status.New(code, rule.Reason).WithDetails(&errdetails.ErrorInfo{
					Reason: rule.Reason, Domain: contract.Domain, Metadata: metadata,
				})
				if err != nil {
					t.Fatalf("attach ErrorInfo: %v", err)
				}
				roundTrip := status.FromProto(withDetails.Proto())
				if roundTrip.Code() != code || len(roundTrip.Details()) != 1 {
					t.Fatalf("round-trip status code=%s details=%d", roundTrip.Code(), len(roundTrip.Details()))
				}
				info, ok := roundTrip.Details()[0].(*errdetails.ErrorInfo)
				if !ok || info.GetReason() != rule.Reason || info.GetDomain() != contract.Domain {
					t.Fatalf("round-trip ErrorInfo = %#v", roundTrip.Details()[0])
				}
				for _, key := range rule.RequiredMetadata {
					if info.Metadata[key] == "" {
						t.Fatalf("missing %s metadata %q", rule.Reason, key)
					}
				}
			}
			if len(seen) != len(declaredReasons) {
				t.Fatalf("fixture reasons = %v, descriptor reasons = %v", sortedKeys(seen), sortedKeys(declaredReasons))
			}
		})
	}
}

func TestImmutableContractPins(t *testing.T) {
	var pins contractPins
	decodeJSON(t, "tests/contracts/contract_pins.json", &pins)
	if pins.SchemaVersion != "ani.contract-pins/v1" {
		t.Fatalf("schema version = %q", pins.SchemaVersion)
	}
	if pins.IAMStartCommit != "5ff9f3cfe083b3b911bb076450abbbb967e82a37" {
		t.Fatalf("IAM start commit = %q", pins.IAMStartCommit)
	}
	if pins.ANIStartCommit != "a221a7b50c2cfdb13f04c13f154338d836a48af3" {
		t.Fatalf("ANI start commit = %q", pins.ANIStartCommit)
	}
	if pins.PolicyRevision != "sha256:f222e2c6d3cd6442449cd722389d3d4fbfcdc7a0fee950c9d28385d3c264affa" {
		t.Fatalf("policy revision = %q", pins.PolicyRevision)
	}
	wantNotification := notificationPin{
		Repository:       "github.com/zhangzhe-ctrl/ani-notification-service",
		Commit:           "0e3f0a2b47fcc1fa96fa926cae2b9ab55bd25d84",
		RuntimeCommit:    "a477a38280c8626b0fdf6664e7afb049d22c2a58",
		ModuleVersion:    "v0.0.0-20260907002920-0e3f0a2b47fc",
		ModuleSum:        "h1:i0sqGu+qg4M18cdGJ00sQgp8Pd+jx8JmVn9s+OmoNJ8=",
		ProtoSHA256:      "af226602b76ddd7b0cb312456f1845f67d1968eb0228da64fc1cced13a2671b5",
		DescriptorSHA256: "7be0a2fa062229717a311af952fc8b3bb7f58c1ef21cde2de741bcbbb4dfc195",
	}
	if pins.Notification != wantNotification {
		t.Fatalf("Notification pin = %#v, want %#v", pins.Notification, wantNotification)
	}
	goMod := string(readFile(t, "go.mod"))
	if !strings.Contains(goMod, "github.com/zhangzhe-ctrl/ani-notification-service "+wantNotification.ModuleVersion) {
		t.Fatalf("go.mod does not require frozen Notification module %s", wantNotification.ModuleVersion)
	}
	wantTools := map[string]toolPin{
		"buf":                {Version: "1.72.0", SHA256: "8720830e26a733da55bb89bcd3cb44849c0965fc0c44fb5d691cccdc64dca5af"},
		"protoc-gen-go":      {Version: "v1.36.12", SHA256: "7475078ca943fa552b4755a0b5dd84f4387905a08cb09a47696fd3683cc1c010"},
		"protoc-gen-go-grpc": {Version: "1.6.2", SHA256: "aa1fabbfc27b12d81182864a3f90b47aee907bced808e17e275c5b18c9602b08"},
	}
	if len(pins.Toolchain) != len(wantTools) {
		t.Fatalf("toolchain pins = %#v", pins.Toolchain)
	}
	for name, want := range wantTools {
		if got := pins.Toolchain[name]; got != want {
			t.Fatalf("toolchain %s = %#v, want %#v", name, got, want)
		}
	}
	assertFixtureInventory(t, pins.Fixtures, "tests/contracts/fixtures")
	paths := map[string]string{
		"iam_descriptor":  iamDescriptorRelative,
		"core_descriptor": coreDescriptorRelative,
	}
	for name, path := range paths {
		want, ok := pins.Artifacts[name]
		if !ok {
			t.Fatalf("missing artifact pin %s", name)
		}
		if got := sha256Hex(readFile(t, path)); got != want {
			t.Fatalf("artifact %s digest = %s, want %s", name, got, want)
		}
	}
	for name, want := range pins.Fixtures {
		if got := sha256Hex(readFile(t, filepath.Join("tests/contracts/fixtures", name))); got != want {
			t.Fatalf("fixture %s digest = %s, want %s", name, got, want)
		}
	}
}

func assertFixtureInventory(t *testing.T, pins map[string]string, relativeDir string) {
	t.Helper()
	want := append([]string(nil), expectedFixtureNames...)
	sort.Strings(want)
	if got := sortedKeys(pins); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("pinned fixtures = %v, want %v", got, want)
	}
	entries, err := os.ReadDir(filepath.Join(repoRoot(t), relativeDir))
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			got = append(got, entry.Name())
		}
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("fixture files = %v, want %v", got, want)
	}
}

func enumReasons(t *testing.T, set *descriptorpb.FileDescriptorSet, fullName protoreflect.FullName, prefix string) map[string]struct{} {
	t.Helper()
	files, err := protodesc.NewFiles(set)
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := files.FindDescriptorByName(fullName)
	if err != nil {
		t.Fatal(err)
	}
	enum, ok := descriptor.(protoreflect.EnumDescriptor)
	if !ok {
		t.Fatalf("%s is not an enum", fullName)
	}
	reasons := map[string]struct{}{}
	for index := 0; index < enum.Values().Len(); index++ {
		name := string(enum.Values().Get(index).Name())
		if strings.HasSuffix(name, "_UNSPECIFIED") {
			continue
		}
		if !strings.HasPrefix(name, prefix) {
			t.Fatalf("enum value %s lacks prefix %s", name, prefix)
		}
		reasons[strings.TrimPrefix(name, prefix)] = struct{}{}
	}
	return reasons
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func TestBizImportBoundary(t *testing.T) {
	root := repoRoot(t)
	forbidden := []string{
		"/api/", "github.com/go-kratos/", "/internal/data", "/internal/service",
		"github.com/jackc/pgx", "github.com/redis/go-redis", "github.com/nats-io/",
		"google.golang.org/grpc",
	}
	err := filepath.WalkDir(filepath.Join(root, "internal/biz"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range file.Imports {
			value, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			for _, banned := range forbidden {
				if strings.Contains(value, banned) {
					t.Errorf("biz import boundary: %s imports %s", path, value)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func loadDescriptorSet(t *testing.T, relative string) *descriptorpb.FileDescriptorSet {
	t.Helper()
	data := readFile(t, relative)
	set := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(data, set); err != nil {
		t.Fatalf("decode %s: %v", relative, err)
	}
	return set
}

func assertServiceInventory(t *testing.T, set *descriptorpb.FileDescriptorSet, expected map[string][]string) {
	t.Helper()
	got := map[string][]string{}
	for _, file := range set.File {
		for _, service := range file.Service {
			fullName := file.GetPackage() + "." + service.GetName()
			if _, tracked := expected[fullName]; !tracked && (file.GetPackage() == "iam.v1" || file.GetPackage() == "tenant.integration.v1") {
				t.Fatalf("unexpected target service %s", fullName)
			}
			methods := make([]string, 0, len(service.Method))
			for _, method := range service.Method {
				methods = append(methods, method.GetName())
			}
			sort.Strings(methods)
			got[fullName] = methods
		}
	}
	if len(got) != len(expected) {
		t.Fatalf("service inventory = %#v, want %#v", got, expected)
	}
	for name, want := range expected {
		want = append([]string(nil), want...)
		sort.Strings(want)
		if strings.Join(got[name], ",") != strings.Join(want, ",") {
			t.Fatalf("methods for %s = %v, want %v", name, got[name], want)
		}
	}
}

func assertMessageFields(t *testing.T, set *descriptorpb.FileDescriptorSet, fullName string, expected []string) {
	t.Helper()
	files, err := protodesc.NewFiles(set)
	if err != nil {
		t.Fatalf("build descriptor registry: %v", err)
	}
	descriptor, err := files.FindDescriptorByName(protoreflect.FullName(fullName))
	if err != nil {
		t.Fatalf("find %s: %v", fullName, err)
	}
	message, ok := descriptor.(protoreflect.MessageDescriptor)
	if !ok {
		t.Fatalf("%s is not a message", fullName)
	}
	got := make([]string, 0, message.Fields().Len())
	for index := 0; index < message.Fields().Len(); index++ {
		got = append(got, string(message.Fields().Get(index).Name()))
	}
	sort.Strings(got)
	want := append([]string(nil), expected...)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("fields for %s = %v, want %v", fullName, got, want)
	}
}

func hasMethod(set *descriptorpb.FileDescriptorSet, name string) bool {
	for _, file := range set.File {
		for _, service := range file.Service {
			for _, method := range service.Method {
				if method.GetName() == name {
					return true
				}
			}
		}
	}
	return false
}

func decodeJSON(t *testing.T, relative string, value any) {
	t.Helper()
	data := readFile(t, relative)
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		t.Fatalf("decode %s: %v", relative, err)
	}
}

func readFile(t *testing.T, relative string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), relative))
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	return data
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(current), "../.."))
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func grpcCode(name string) (codes.Code, bool) {
	values := map[string]codes.Code{
		"INVALID_ARGUMENT":    codes.InvalidArgument,
		"UNAUTHENTICATED":     codes.Unauthenticated,
		"PERMISSION_DENIED":   codes.PermissionDenied,
		"NOT_FOUND":           codes.NotFound,
		"ALREADY_EXISTS":      codes.AlreadyExists,
		"ABORTED":             codes.Aborted,
		"FAILED_PRECONDITION": codes.FailedPrecondition,
		"RESOURCE_EXHAUSTED":  codes.ResourceExhausted,
		"UNAVAILABLE":         codes.Unavailable,
		"DEADLINE_EXCEEDED":   codes.DeadlineExceeded,
	}
	code, ok := values[name]
	return code, ok
}
