// Package workloadregistry reads reviewed, finite synchronous target declarations.
// A registration describes a target; it never authenticates or grants authority.
package workloadregistry

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
)

const Schema = "ani.workload-targets/v1"
const MaxBytes = 1 << 20

type Mechanism string

const (
	Direct       Mechanism = "direct"
	WorkloadOnly Mechanism = "workload_only"
	Delegated    Mechanism = "delegated"
	Receiver     Mechanism = "receiver"
)

var ErrInvalid = errors.New("invalid Workload target registration")

// Source binds the existing authorization operation to one opaque owner mode.
// Permission and owner obligations are declarations, never executable policy.
type Source struct {
	Operation       string   `json:"operation"`
	Mode            string   `json:"mode"`
	Resource        string   `json:"resource"`
	Action          string   `json:"action"`
	PrincipalKinds  []string `json:"principal_kinds"`
	CredentialKinds []string `json:"credential_kinds"`
	OwnerHandler    string   `json:"owner_handler"`
	OwnerCheck      string   `json:"owner_check"` // caller or receiver
}

type Target struct {
	Audience            string    `json:"audience"`
	Operation           string    `json:"operation"`
	RPC                 string    `json:"rpc"`
	HTTPMethod          string    `json:"http_method,omitempty"`
	HTTPPath            string    `json:"http_path,omitempty"`
	Mechanism           Mechanism `json:"mechanism"`
	GrantScope          string    `json:"grant_scope"`
	Enabled             bool      `json:"enabled"`
	ReceiverOperation   string    `json:"receiver_operation,omitempty"`
	Continuation        bool      `json:"continuation,omitempty"`
	Sources             []Source  `json:"sources,omitempty"`
	AuthorityOperations []string  `json:"authority_operations,omitempty"`
}

type Document struct {
	Schema  string   `json:"schema"`
	Targets []Target `json:"targets"`
}

// Registry is immutable; every returned target is an independent copy.
type Registry struct {
	digest      string
	targets     map[string]Target
	methods     map[string]string
	httpTargets map[string]string
	revisions   map[string]string
}

var name = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
var rpc = regexp.MustCompile(`^/[A-Za-z][A-Za-z0-9_.]{0,191}/[A-Za-z][A-Za-z0-9_]{0,63}$`)
var audience = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,127}$`)

func key(a, o string) string   { return a + "\x00" + o }
func digest(raw []byte) string { v := sha256.Sum256(raw); return hex.EncodeToString(v[:]) }

// Load binds the exact file bytes to an independently reviewed SHA256.
func Load(path, expectedSHA256 string) (*Registry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrInvalid
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxBytes {
		return nil, ErrInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil {
		return nil, ErrInvalid
	}
	return Parse(raw, expectedSHA256)
}

func Parse(raw []byte, expectedSHA256 string) (*Registry, error) {
	if len(raw) == 0 || len(raw) > MaxBytes || len(expectedSHA256) != 64 || digest(raw) != expectedSHA256 {
		return nil, ErrInvalid
	}
	// encoding/json normally accepts duplicate object keys. Reject them before
	// typed decoding so the reviewed input has exactly one interpretation.
	d := json.NewDecoder(bytes.NewReader(raw))
	if uniqueJSON(d, 0) != nil {
		return nil, ErrInvalid
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var doc Document
	if d.Decode(&doc) != nil || doc.Schema != Schema || len(doc.Targets) == 0 || len(doc.Targets) > 512 {
		return nil, ErrInvalid
	}
	r := &Registry{digest: expectedSHA256, targets: map[string]Target{}, methods: map[string]string{}, httpTargets: map[string]string{}, revisions: map[string]string{}}
	for _, t := range doc.Targets {
		if err := validate(t); err != nil {
			return nil, err
		}
		k := key(t.Audience, t.Operation)
		if _, exists := r.targets[k]; exists {
			return nil, fmt.Errorf("%w: duplicate target", ErrInvalid)
		}
		if t.RPC != "" {
			if _, exists := r.methods[t.RPC]; exists {
				return nil, fmt.Errorf("%w: duplicate RPC", ErrInvalid)
			}
			r.methods[t.RPC] = k
		}
		if t.HTTPMethod != "" {
			endpoint := key(t.Audience, key(t.HTTPMethod, t.HTTPPath))
			if _, exists := r.httpTargets[endpoint]; exists {
				return nil, fmt.Errorf("%w: duplicate HTTP endpoint", ErrInvalid)
			}
			r.httpTargets[endpoint] = k
		}
		t.AuthorityOperations = slices.Clone(t.AuthorityOperations)
		t.Sources = slices.Clone(t.Sources)
		for i := range t.Sources {
			t.Sources[i].PrincipalKinds = slices.Clone(t.Sources[i].PrincipalKinds)
			t.Sources[i].CredentialKinds = slices.Clone(t.Sources[i].CredentialKinds)
			sort.Strings(t.Sources[i].PrincipalKinds)
			sort.Strings(t.Sources[i].CredentialKinds)
		}
		sort.Slice(t.Sources, func(i, j int) bool {
			return key(t.Sources[i].Operation, t.Sources[i].Mode) < key(t.Sources[j].Operation, t.Sources[j].Mode)
		})
		encoded, err := json.Marshal(t)
		if err != nil {
			return nil, ErrInvalid
		}
		r.targets[k] = t
		r.revisions[k] = digest(encoded)
	}
	// A receiver grant must name this exact audience, and cannot recursively
	// claim a verifier or a caller target as receiver authority.
	for _, t := range r.targets {
		if t.ReceiverOperation != "" {
			receiver, ok := r.targets[key(t.Audience, t.ReceiverOperation)]
			if !ok || receiver.Mechanism != Receiver || (t.Enabled && !receiver.Enabled) {
				return nil, fmt.Errorf("%w: receiver association", ErrInvalid)
			}
		}
		for _, operation := range t.AuthorityOperations {
			member, ok := r.targets[key(t.Audience, operation)]
			if !ok || !member.Enabled || member.Mechanism != WorkloadOnly || member.HTTPPath == "" || member.ReceiverOperation != t.ReceiverOperation || !slices.Equal(member.AuthorityOperations, t.AuthorityOperations) {
				return nil, fmt.Errorf("%w: authority operation association", ErrInvalid)
			}
		}
	}
	return r, nil
}

func validate(t Target) error {
	bad := func() error { return fmt.Errorf("%w: target constraints", ErrInvalid) }
	if !audience.MatchString(t.Audience) || !name.MatchString(t.GrantScope) {
		return bad()
	}
	if len(t.AuthorityOperations) != 0 {
		if len(t.AuthorityOperations) != 2 || !t.Enabled || t.Mechanism != WorkloadOnly || t.HTTPPath == "" || !slices.Contains(t.AuthorityOperations, t.Operation) || t.AuthorityOperations[0] >= t.AuthorityOperations[1] || !name.MatchString(t.AuthorityOperations[0]) || !name.MatchString(t.AuthorityOperations[1]) {
			return bad()
		}
	}
	if t.Mechanism == Direct {
		if t.HTTPMethod != "" || t.HTTPPath != "" {
			return bad()
		}
		if !rpc.MatchString(t.RPC) || t.Operation != t.RPC || len(t.Sources) != 0 || t.ReceiverOperation != "" || t.Continuation {
			return bad()
		}
		return nil
	}
	if !name.MatchString(t.Operation) {
		return bad()
	}
	if t.Mechanism == Receiver {
		if t.HTTPMethod != "" || t.HTTPPath != "" || t.RPC != "" || t.ReceiverOperation != "" || len(t.Sources) != 0 || t.Continuation {
			return bad()
		}
		return nil
	}
	httpTarget := t.HTTPMethod != "" || t.HTTPPath != ""
	if httpTarget {
		if t.Mechanism != WorkloadOnly || t.RPC != "" || (t.HTTPMethod != "GET" && t.HTTPMethod != "POST") ||
			len(t.HTTPPath) > 256 || !strings.HasPrefix(t.HTTPPath, "/") || t.HTTPPath == "/" ||
			path.Clean(t.HTTPPath) != t.HTTPPath || strings.ContainsAny(t.HTTPPath, "%?#{ }*\\\r\n\t") {
			return bad()
		}
		for _, c := range t.HTTPPath {
			if c > 127 || c < 33 {
				return bad()
			}
		}
	} else if !rpc.MatchString(t.RPC) {
		return bad()
	}
	if !name.MatchString(t.ReceiverOperation) || t.ReceiverOperation == t.Operation {
		return bad()
	}
	if t.Mechanism == WorkloadOnly {
		if len(t.Sources) != 0 || t.Continuation {
			return bad()
		}
		return nil
	}
	if t.Mechanism != Delegated || len(t.Sources) == 0 || len(t.Sources) > 16 {
		return bad()
	}
	seen := map[string]bool{}
	for _, s := range t.Sources {
		if !name.MatchString(s.Operation) || !name.MatchString(s.Mode) || !name.MatchString(s.Resource) || !name.MatchString(s.Action) || !name.MatchString(s.OwnerHandler) || (s.OwnerCheck != "caller" && s.OwnerCheck != "receiver") || seen[s.Operation] {
			return bad()
		}
		seen[s.Operation] = true
		if !setWithin(s.PrincipalKinds, []string{"human", "workload"}) || !setWithin(s.CredentialKinds, []string{"access_token", "api_key", "workload_token"}) {
			return bad()
		}
		// The currently supported delegated credentials are Human Session access
		// and Tenant Workload API Key. A policy may also advertise workload_token
		// for its direct path, but it cannot become a delegated subject credential.
		if slices.Contains(s.PrincipalKinds, "human") != slices.Contains(s.CredentialKinds, "access_token") || slices.Contains(s.PrincipalKinds, "workload") != slices.Contains(s.CredentialKinds, "api_key") {
			return bad()
		}
	}
	return nil
}

func setWithin(values, allowed []string) bool {
	if len(values) == 0 || len(values) > len(allowed) {
		return false
	}
	seen := map[string]bool{}
	for _, v := range values {
		if seen[v] || !slices.Contains(allowed, v) {
			return false
		}
		seen[v] = true
	}
	return true
}

func uniqueJSON(d *json.Decoder, depth int) error {
	if depth > 16 {
		return ErrInvalid
	}
	t, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			t, err := d.Token()
			if err != nil {
				return err
			}
			k, ok := t.(string)
			if !ok || seen[k] {
				return ErrInvalid
			}
			seen[k] = true
			if err := uniqueJSON(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := uniqueJSON(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return ErrInvalid
	}
	_, err = d.Token()
	return err
}

func (r *Registry) Digest() string {
	if r == nil {
		return ""
	}
	return r.digest
}
func (r *Registry) Lookup(a, o string) (Target, bool) {
	if r == nil {
		return Target{}, false
	}
	t, ok := r.targets[key(a, o)]
	if !ok {
		return Target{}, false
	}
	t.Sources = slices.Clone(t.Sources)
	t.AuthorityOperations = slices.Clone(t.AuthorityOperations)
	for i := range t.Sources {
		t.Sources[i].PrincipalKinds = slices.Clone(t.Sources[i].PrincipalKinds)
		t.Sources[i].CredentialKinds = slices.Clone(t.Sources[i].CredentialKinds)
	}
	return t, true
}
func (r *Registry) Method(method string) (Target, bool) {
	if r == nil {
		return Target{}, false
	}
	t, ok := r.targets[r.methods[method]]
	if !ok {
		return Target{}, false
	}
	return r.Lookup(t.Audience, t.Operation)
}
func (r *Registry) Revision(a, o string) string {
	if r == nil {
		return ""
	}
	return r.revisions[key(a, o)]
}
func (r *Registry) Targets() []Target {
	if r == nil {
		return nil
	}
	result := make([]Target, 0, len(r.targets))
	for _, t := range r.targets {
		copy, _ := r.Lookup(t.Audience, t.Operation)
		result = append(result, copy)
	}
	sort.Slice(result, func(i, j int) bool {
		return key(result[i].Audience, result[i].Operation) < key(result[j].Audience, result[j].Operation)
	})
	return result
}
func (t Target) Source(operation, mode string) (Source, bool) {
	for _, s := range t.Sources {
		if s.Operation == operation && s.Mode == mode {
			return s, true
		}
	}
	return Source{}, false
}
func (s Source) AllowsSubject(principal, credential string) bool {
	return ((principal == "human" && credential == "access_token") || (principal == "workload" && credential == "api_key")) && slices.Contains(s.PrincipalKinds, principal) && slices.Contains(s.CredentialKinds, credential)
}

// HTTP resolves only the reviewed, exact wire method and path. No templates,
// decoding, slash cleaning, verb fallback or owner-specific names are accepted.
func (r *Registry) HTTP(audience, method, path string) (Target, bool) {
	if r == nil {
		return Target{}, false
	}
	k, ok := r.httpTargets[key(audience, key(method, path))]
	if !ok {
		return Target{}, false
	}
	t := r.targets[k]
	return r.Lookup(t.Audience, t.Operation)
}
