package biz

import (
	"context"
	"encoding/base64"
	"github.com/google/uuid"
)

type PlatformSessionListReader interface {
	ListOwnedSessions(context.Context, uuid.UUID, uuid.UUID, int) ([]Session, error)
	ListOwnedPlatformSessionGrants(context.Context, uuid.UUID, uuid.UUID) ([]SessionGrant, error)
}

func (u *AuthenticationUsecase) listPlatformSessions(ctx context.Context, c ListSessionsCommand, claims AccessTokenClaims, after uuid.UUID) (ListSessionsResult, error) {
	now := u.clock.Now().UTC()
	if claims.Boundary != AccessBoundaryPlatform || claims.Audience != AudienceBoss || claims.TenantID != uuid.Nil || claims.Subject == uuid.Nil || claims.SessionID == uuid.Nil || claims.GrantID == uuid.Nil || claims.GrantVersion < 1 || !now.Before(claims.ExpiresAt) || !containsHumanAuthenticationMethod(claims.AuthnMethods) {
		return ListSessionsResult{}, ErrInvalidCredential
	}
	if u.platformAuthorization == nil {
		return ListSessionsResult{}, ErrAuthenticationDependency
	}
	// This endpoint reads only the authenticated Human's own Sessions. It
	// checks current authority but grants no Platform management Permission.
	state, err := u.platformAuthorization.reader.LookupPlatformAuthorization(ctx, claims, "", nil)
	if err != nil {
		return ListSessionsResult{}, err
	}
	if platformAuthorizationDenial(state, claims, now) != "" {
		return ListSessionsResult{}, ErrInvalidCredential
	}
	reader, ok := u.reader.(PlatformSessionListReader)
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
	for _, session := range rows {
		grants, err := reader.ListOwnedPlatformSessionGrants(ctx, claims.Subject, session.ID)
		if err != nil {
			return ListSessionsResult{}, err
		}
		item := SessionListItem{Session: session, Grants: make([]SessionListGrant, 0, len(grants))}
		for _, grant := range grants {
			item.Grants = append(item.Grants, SessionListGrant{Boundary: AccessBoundaryPlatform, Grant: grant})
		}
		result.Sessions = append(result.Sessions, item)
	}
	return result, nil
}
