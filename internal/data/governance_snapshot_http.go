package data

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/google/uuid"
	governancev1 "github.com/zhangzhe-ctrl/ani-governance/api/governance/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/sdk/grpcworkload"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
)

type GovernanceSnapshotHTTPConfiguration struct {
	Origin, ServerName string
	ReaderID           uuid.UUID
	TLS                grpcworkload.TLSFiles
	Registry           *workloadregistry.Registry
	TokenSource        grpcworkload.HTTPWorkloadTokenSource
}

type governanceSnapshotHTTPClient struct {
	client *http.Client
	origin string
	reader uuid.UUID
}

func NewGovernanceSnapshotHTTPClient(c GovernanceSnapshotHTTPConfiguration) (biz.TenantSnapshotSource, func(), error) {
	if !ValidHTTPSOrigin(c.Origin) || c.ReaderID.Version() != 7 || c.ReaderID.Variant() != uuid.RFC4122 {
		return nil, nil, biz.ErrTenantLifecycleInvalid
	}
	for path, operation := range map[string]string{governancev1.SnapshotBeginPath: governancev1.SnapshotBeginOperation, governancev1.SnapshotPagePath: governancev1.SnapshotPageOperation} {
		target, err := grpcworkload.RegisteredHTTPWorkloadTarget(c.Registry, governancev1.Audience, http.MethodPost, path)
		if err != nil || target.Operation != operation {
			return nil, nil, biz.ErrTenantLifecycleInvalid
		}
	}
	client, err := grpcworkload.NewWorkloadHTTPClient(c.Origin, c.ServerName, governancev1.Audience, c.TLS, c.Registry, c.TokenSource)
	if err != nil {
		return nil, nil, err
	}
	return &governanceSnapshotHTTPClient{client: client, origin: c.Origin, reader: c.ReaderID}, client.CloseIdleConnections, nil
}

func (c *governanceSnapshotHTTPClient) request(ctx context.Context, path, key string, input, out any, required ...string) error {
	body, err := json.Marshal(input)
	if err != nil {
		return biz.ErrTenantLifecycleInvalid
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.origin+path, bytes.NewReader(body))
	if err != nil {
		return biz.ErrTenantLifecycleInvalid
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	response, err := c.client.Do(req)
	if err != nil {
		return biz.ErrPersistenceUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return biz.ErrWorkloadPermissionDenied
		case http.StatusGone:
			return biz.ErrTenantSnapshotExpired
		case http.StatusBadRequest:
			return biz.ErrTenantLifecycleInvalid
		case http.StatusConflict:
			return biz.ErrTenantLifecycleConflict
		default:
			return biz.ErrPersistenceUnavailable
		}
	}
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return biz.ErrTenantLifecycleInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, governancev1.MaxMessageBytes+1))
	if err != nil {
		return biz.ErrPersistenceUnavailable
	}
	if governancev1.DecodeStrict(raw, out) != nil {
		return biz.ErrTenantLifecycleInvalid
	}
	// Zero watermarks/counts and an empty final cursor are legitimate. Explicit
	// required-field presence cannot be inferred from their Go zero values.
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return biz.ErrTenantLifecycleInvalid
	}
	for _, field := range required {
		if _, ok := fields[field]; !ok {
			return biz.ErrTenantLifecycleInvalid
		}
	}
	return nil
}

func (c *governanceSnapshotHTTPClient) Begin(ctx context.Context, key string, size int) (biz.TenantSnapshotCursor, error) {
	if size == 0 {
		size = 100
	}
	if len(key) < 1 || len(key) > 128 || size < 1 || size > 100 {
		return biz.TenantSnapshotCursor{}, biz.ErrTenantLifecycleInvalid
	}
	for _, ch := range key {
		if ch < 33 || ch > 126 {
			return biz.TenantSnapshotCursor{}, biz.ErrTenantLifecycleInvalid
		}
	}
	var dto governancev1.SnapshotBeginResponse
	err := c.request(ctx, governancev1.SnapshotBeginPath, key, governancev1.SnapshotBeginRequest{PageSize: &size}, &dto, "watermark", "total_count")
	if err != nil {
		return biz.TenantSnapshotCursor{}, err
	}
	id, idErr := uuid.Parse(dto.SnapshotId)
	epoch, epochErr := uuid.Parse(dto.Epoch)
	cursor := biz.TenantSnapshotCursor{ID: id, Epoch: epoch, ReaderID: c.reader, Watermark: dto.Watermark, TotalCount: dto.TotalCount, PageSize: size, CapturedAt: dto.CapturedAt, ExpiresAt: dto.ExpiresAt, FirstToken: dto.Cursor}
	if idErr != nil || epochErr != nil || id.String() != dto.SnapshotId || epoch.String() != dto.Epoch || cursor.Validate() != nil || cursor.CapturedAt.After(time.Now()) {
		return biz.TenantSnapshotCursor{}, biz.ErrTenantLifecycleInvalid
	}
	if !time.Now().Before(cursor.ExpiresAt) {
		return biz.TenantSnapshotCursor{}, biz.ErrTenantSnapshotExpired
	}
	return cursor, nil
}

func (c *governanceSnapshotHTTPClient) Page(ctx context.Context, cursor biz.TenantSnapshotCursor, token string) (biz.TenantSnapshotPage, error) {
	if cursor.Validate() != nil || cursor.ReaderID != c.reader || !biz.ValidTenantSnapshotToken(token) {
		return biz.TenantSnapshotPage{}, biz.ErrTenantLifecycleInvalid
	}
	if !time.Now().Before(cursor.ExpiresAt) {
		return biz.TenantSnapshotPage{}, biz.ErrTenantSnapshotExpired
	}
	var dto governancev1.SnapshotPageResponse
	if err := c.request(ctx, governancev1.SnapshotPagePath, "", governancev1.SnapshotPageRequest{Cursor: token}, &dto, "watermark", "items", "next_cursor"); err != nil {
		return biz.TenantSnapshotPage{}, err
	}
	id, idErr := uuid.Parse(dto.SnapshotId)
	epoch, epochErr := uuid.Parse(dto.Epoch)
	if idErr != nil || epochErr != nil || id.String() != dto.SnapshotId || epoch.String() != dto.Epoch {
		return biz.TenantSnapshotPage{}, biz.ErrTenantLifecycleInvalid
	}
	page := biz.TenantSnapshotPage{SnapshotID: id, Epoch: epoch, Watermark: dto.Watermark, RequestToken: token, NextToken: dto.NextCursor}
	for _, item := range dto.Items {
		tenant, err := uuid.Parse(item.TenantId)
		if err != nil || tenant.String() != item.TenantId {
			return biz.TenantSnapshotPage{}, biz.ErrTenantLifecycleInvalid
		}
		page.Items = append(page.Items, biz.TenantSnapshotFact{TenantID: tenant, Version: item.TenantVersion, Status: string(item.BusinessStatus)})
	}
	return page, page.Validate(cursor)
}
