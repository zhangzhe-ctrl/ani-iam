package biz

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/google/uuid"
)

var ErrSessionCursorInvalid = errors.New("session cursor is invalid")

type SessionListItem struct {
	Session Session
	Grants  []SessionListGrant
}
type SessionListGrant struct {
	TenantID uuid.UUID
	Grant    SessionGrant
}
type ListSessionsCommand struct {
	Credential, Cursor string
	Limit              int
}
type ListSessionsResult struct {
	Sessions   []SessionListItem
	NextCursor string
}

// Sessions belong to the Human globally. Grant summaries are limited to the
// authenticated Tenant; listing does not create authority in another Tenant.
type SessionListReader interface {
	ListOwnedSessions(context.Context, uuid.UUID, uuid.UUID, int) ([]Session, error)
	ListOwnedSessionGrants(context.Context, TenantScope, uuid.UUID, uuid.UUID) ([]SessionGrant, error)
}

func (u *AuthenticationUsecase) ListSessions(ctx context.Context, c ListSessionsCommand) (ListSessionsResult, error) {
	if c.Limit == 0 {
		c.Limit = 50
	}
	if c.Limit < 1 || c.Limit > 100 {
		return ListSessionsResult{}, ErrSessionCursorInvalid
	}
	after := uuid.Nil
	if c.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(c.Cursor)
		if err != nil || !strings.HasPrefix(string(raw), "v1:") {
			return ListSessionsResult{}, ErrSessionCursorInvalid
		}
		after, err = uuid.Parse(string(raw[3:]))
		if err != nil || after == uuid.Nil {
			return ListSessionsResult{}, ErrSessionCursorInvalid
		}
	}
	claims, err := u.tokens.Verify(ctx, c.Credential)
	if err != nil {
		if errors.Is(err, ErrAuthenticationDependency) || errors.Is(err, ErrAuthorizationDependency) {
			return ListSessionsResult{}, err
		}
		return ListSessionsResult{}, errors.Join(ErrInvalidCredential, err)
	}
	now := u.clock.Now().UTC()
	if claims.Subject == uuid.Nil || claims.SessionID == uuid.Nil || claims.GrantID == uuid.Nil || claims.TenantID == uuid.Nil || claims.GrantVersion <= 0 || !now.Before(claims.ExpiresAt) || !containsHumanAuthenticationMethod(claims.AuthnMethods) {
		return ListSessionsResult{}, ErrInvalidCredential
	}
	scope, err := NewTenantScope(claims.TenantID)
	if err != nil {
		return ListSessionsResult{}, ErrInvalidCredential
	}
	state, err := u.reader.LookupTenantSwitch(ctx, scope, claims)
	if err != nil {
		return ListSessionsResult{}, err
	}
	if err = validateTenantSwitchState(state, claims, claims.TenantID, now); err != nil {
		return ListSessionsResult{}, err
	}
	reader, ok := u.reader.(SessionListReader)
	if !ok {
		return ListSessionsResult{}, ErrAuthenticationDependency
	}
	rows, err := reader.ListOwnedSessions(ctx, claims.Subject, after, c.Limit+1)
	if err != nil {
		return ListSessionsResult{}, err
	}
	result := ListSessionsResult{Sessions: make([]SessionListItem, 0, len(rows))}
	if len(rows) > c.Limit {
		rows = rows[:c.Limit]
		result.NextCursor = base64.RawURLEncoding.EncodeToString([]byte("v1:" + rows[len(rows)-1].ID.String()))
	}
	for _, s := range rows {
		grants, err := reader.ListOwnedSessionGrants(ctx, scope, claims.Subject, s.ID)
		if err != nil {
			return ListSessionsResult{}, err
		}
		item := SessionListItem{Session: s, Grants: make([]SessionListGrant, 0, len(grants))}
		for _, g := range grants {
			item.Grants = append(item.Grants, SessionListGrant{TenantID: claims.TenantID, Grant: g})
		}
		result.Sessions = append(result.Sessions, item)
	}
	return result, nil
}
