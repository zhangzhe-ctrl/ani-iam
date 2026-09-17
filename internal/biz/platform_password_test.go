package biz

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"net/netip"
	"testing"
	"time"
)

type platformPasswordTestStore struct {
	observed, current      PlatformPasswordState
	readErr                error
	saveErr                error
	failureErr             error
	lastFailure            LoginFailureMutation
	saved, reset, failures int
}

func (s *platformPasswordTestStore) ReadPlatformPassword(context.Context, string) (PlatformPasswordState, error) {
	return s.observed, s.readErr
}
func (s *platformPasswordTestStore) WithinPlatformPassword(ctx context.Context, f func(PlatformPasswordTransaction) error) error {
	return f(s)
}
func (s *platformPasswordTestStore) LockPassword(context.Context, string) (PlatformPasswordState, error) {
	return s.current, nil
}
func (s *platformPasswordTestStore) ResetPasswordFailures(context.Context, uuid.UUID, int64, time.Time) error {
	s.reset++
	return nil
}
func (s *platformPasswordTestStore) RecordPasswordFailure(_ context.Context, m LoginFailureMutation) error {
	s.failures++
	s.lastFailure = m
	return s.failureErr
}
func (s *platformPasswordTestStore) SaveLogin(context.Context, PlatformLoginMutation) error {
	s.saved++
	return s.saveErr
}
func TestPlatformPasswordRechecksCredentialAndCurrentAuthority(t *testing.T) {
	now := time.Date(2026, 9, 13, 1, 0, 0, 0, time.UTC)
	base := PlatformPasswordState{Login: PlatformLoginState{IdentityID: uuid.Must(uuid.NewV7()), IdentityActive: true, Principal: Principal{ID: uuid.Must(uuid.NewV7()), Status: PrincipalStatusActive}, MembershipID: uuid.Must(uuid.NewV7()), MembershipStatus: MembershipStatusActive}, PasswordHash: "unit-hash", CredentialVersion: 1}
	for name, mutate := range map[string]func(*PlatformPasswordState){
		"rotated Credential":   func(s *PlatformPasswordState) { s.CredentialVersion++ },
		"changed hash":         func(s *PlatformPasswordState) { s.PasswordHash = "replacement" },
		"disabled identity":    func(s *PlatformPasswordState) { s.Login.IdentityActive = false },
		"disabled Human":       func(s *PlatformPasswordState) { s.Login.Principal.Status = PrincipalStatusDisabled },
		"suspended Membership": func(s *PlatformPasswordState) { s.Login.MembershipStatus = MembershipStatusSuspended },
		"removed Membership":   func(s *PlatformPasswordState) { s.Login.MembershipID = uuid.Nil },
		"locked Credential":    func(s *PlatformPasswordState) { s.LockedUntil = now.Add(time.Minute) },
	} {
		t.Run(name, func(t *testing.T) {
			repo := &platformPasswordTestStore{observed: base, current: base}
			mutate(&repo.current)
			u, err := NewPlatformPasswordUsecase("ani-iam", repo, repo, acceptingPasswordVerifier{}, allowingLoginThrottle{}, platformTestSigner{}, platformTestSecrets{}, platformTestIDs{}, fixedAuthClock{now: now})
			if err != nil {
				t.Fatal(err)
			}
			r, err := u.PasswordLogin(context.Background(), PasswordLoginCommand{Account: "admin@example.test", Password: "unit", Audience: AudienceBoss, SourceIP: netip.MustParseAddr("192.0.2.1"), IdempotencyKey: "test"})
			if err == nil || r.AccessToken != "" || repo.saved != 0 || repo.reset != 0 {
				t.Fatal("changed current identity state signed a Session")
			}
		})
	}
}
func TestPlatformPasswordNeverReturnsCredentialAfterAuditFailure(t *testing.T) {
	now := time.Now().UTC()
	s := PlatformPasswordState{Login: PlatformLoginState{IdentityID: uuid.Must(uuid.NewV7()), IdentityActive: true, Principal: Principal{ID: uuid.Must(uuid.NewV7()), Status: PrincipalStatusActive}, MembershipID: uuid.Must(uuid.NewV7()), MembershipStatus: MembershipStatusActive}, PasswordHash: "unit", CredentialVersion: 1}
	repo := &platformPasswordTestStore{observed: s, current: s, saveErr: ErrPersistenceUnavailable}
	u, _ := NewPlatformPasswordUsecase("ani-iam", repo, repo, acceptingPasswordVerifier{}, allowingLoginThrottle{}, platformTestSigner{}, platformTestSecrets{}, platformTestIDs{}, fixedAuthClock{now: now})
	result, err := u.PasswordLogin(context.Background(), PasswordLoginCommand{Account: "admin@example.test", Password: "unit", Audience: AudienceBoss, SourceIP: netip.MustParseAddr("192.0.2.1"), IdempotencyKey: "test"})
	if !errors.Is(err, ErrPersistenceUnavailable) || result.AccessToken != "" || result.RefreshToken != "" {
		t.Fatal("failed transactional Audit leaked issued credentials")
	}
}

func TestPlatformPasswordDenialRequiresAuditWithoutChangingFailureCounter(t *testing.T) {
	now := time.Now().UTC()
	s := PlatformPasswordState{Login: PlatformLoginState{IdentityID: uuid.Must(uuid.NewV7()), IdentityActive: true, Principal: Principal{ID: uuid.Must(uuid.NewV7()), Status: PrincipalStatusActive}}, PasswordHash: "unit", CredentialVersion: 1}
	repo := &platformPasswordTestStore{observed: s, current: s, failureErr: ErrPersistenceUnavailable}
	u, _ := NewPlatformPasswordUsecase("ani-iam", repo, repo, acceptingPasswordVerifier{}, allowingLoginThrottle{}, platformTestSigner{}, platformTestSecrets{}, platformTestIDs{}, fixedAuthClock{now: now})
	_, err := u.PasswordLogin(context.Background(), PasswordLoginCommand{Account: "admin@example.test", Password: "unit", Audience: AudienceBoss, SourceIP: netip.MustParseAddr("192.0.2.1"), IdempotencyKey: "test"})
	if !errors.Is(err, ErrAuthenticationDependency) || repo.failures != 1 || repo.lastFailure.PrincipalID != uuid.Nil || repo.lastFailure.Audit.Reason != AuditReasonMembershipInactive || repo.lastFailure.Audit.Result != AuditResultDenied || repo.reset != 0 || repo.saved != 0 {
		t.Fatal("denial Audit was optional or changed authentication state")
	}
}

func TestPlatformPasswordConcurrentFailuresStillCountForUnchangedCredential(t *testing.T) {
	now := time.Now().UTC()
	s := PlatformPasswordState{Login: PlatformLoginState{IdentityID: uuid.Must(uuid.NewV7()), IdentityActive: true, Principal: Principal{ID: uuid.Must(uuid.NewV7()), Status: PrincipalStatusActive}}, PasswordHash: "unit", CredentialVersion: 1}
	for _, rotated := range []bool{false, true} {
		repo := &platformPasswordTestStore{observed: s, current: s}
		repo.current.CredentialVersion++
		if rotated {
			repo.current.PasswordHash = "replacement"
		}
		u, _ := NewPlatformPasswordUsecase("ani-iam", repo, repo, rejectingPasswordVerifier{}, allowingLoginThrottle{}, platformTestSigner{}, platformTestSecrets{}, platformTestIDs{}, fixedAuthClock{now: now})
		_, err := u.PasswordLogin(context.Background(), PasswordLoginCommand{Account: "admin@example.test", Password: "wrong", Audience: AudienceBoss, SourceIP: netip.MustParseAddr("192.0.2.1"), IdempotencyKey: "test"})
		if !errors.Is(err, ErrInvalidCredential) || repo.failures != 1 || (repo.lastFailure.PrincipalID != uuid.Nil) == rotated {
			t.Fatal("concurrent counter update was confused with password rotation")
		}
	}
}
