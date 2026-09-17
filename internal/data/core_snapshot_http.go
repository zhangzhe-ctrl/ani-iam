package data

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/sdk/grpcworkload"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
)

const coreSnapshotHTTPPath = "/api/v1/internal/tenant-lifecycle/snapshots"

type CoreSnapshotHTTPConfiguration struct {
	Origin, ServerName, Producer string
	ConsumerID                   uuid.UUID
	TLS                          grpcworkload.TLSFiles
	Registry                     *workloadregistry.Registry
	TokenSource                  grpcworkload.HTTPWorkloadTokenSource
}

type coreSnapshotHTTPClient struct {
	client           *http.Client
	origin, producer string
	consumer         uuid.UUID
}

func NewCoreSnapshotHTTPClient(c CoreSnapshotHTTPConfiguration) (biz.CoreSnapshotSource, func(), error) {
	if !biz.ValidCoreProjectionProducer(c.Producer) || c.ConsumerID.Version() != 7 {
		return nil, nil, biz.ErrCoreProjectionInvalid
	}
	for method, operation := range map[string]string{"POST": "core.snapshot.begin", "GET": "core.snapshot.page"} {
		target, err := grpcworkload.RegisteredHTTPWorkloadTarget(c.Registry, "ani-core-control", method, coreSnapshotHTTPPath)
		if err != nil || target.Operation != operation {
			return nil, nil, biz.ErrCoreProjectionInvalid
		}
	}
	client, err := grpcworkload.NewWorkloadHTTPClient(c.Origin, c.ServerName, "ani-core-control", c.TLS, c.Registry, c.TokenSource)
	if err != nil {
		return nil, nil, err
	}
	return &coreSnapshotHTTPClient{client, c.Origin, c.Producer, c.ConsumerID}, client.CloseIdleConnections, nil
}

type coreSnapshotCursorDocument struct {
	ID             string    `json:"id"`
	SourceSequence int64     `json:"source_sequence"`
	BrokerSequence int64     `json:"broker_sequence"`
	PageSize       int32     `json:"page_size"`
	ExpiresAt      time.Time `json:"expires_at"`
}

func (c *coreSnapshotHTTPClient) cursor(d coreSnapshotCursorDocument) (biz.CoreSnapshotCursor, error) {
	id, err := uuid.Parse(d.ID)
	result := biz.CoreSnapshotCursor{ID: id, ConsumerID: c.consumer, Producer: c.producer, SourceCut: d.SourceSequence, BrokerAfter: d.BrokerSequence, PageSize: d.PageSize, ExpiresAt: d.ExpiresAt}
	if err != nil || id.String() != d.ID || result.Validate() != nil {
		return biz.CoreSnapshotCursor{}, biz.ErrCoreProjectionInvalid
	}
	if !time.Now().Before(result.ExpiresAt) {
		return biz.CoreSnapshotCursor{}, biz.ErrCoreSnapshotExpired
	}
	return result, nil
}
func (c *coreSnapshotHTTPClient) request(ctx context.Context, method, path string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.origin+path, bytes.NewReader(body))
	if err != nil {
		return biz.ErrCoreProjectionInvalid
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	response, err := c.client.Do(req)
	if err != nil {
		return biz.ErrPersistenceUnavailable
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil || len(raw) > 1<<20 {
		return biz.ErrPersistenceUnavailable
	}
	if response.StatusCode != http.StatusOK {
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return biz.ErrWorkloadPermissionDenied
		case http.StatusBadRequest:
			return biz.ErrCoreProjectionInvalid
		case http.StatusConflict:
			var e struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(raw, &e)
			if e.Code == "SNAPSHOT_EXPIRED" {
				return biz.ErrCoreSnapshotExpired
			}
			return biz.ErrCoreProjectionConflict
		default:
			return biz.ErrPersistenceUnavailable
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return biz.ErrCoreProjectionInvalid
	}
	return nil
}
func (c *coreSnapshotHTTPClient) Begin(ctx context.Context, key string, size int32) (biz.CoreSnapshotCursor, error) {
	if size == 0 {
		size = 100
	}
	id, parseErr := uuid.Parse(key)
	if parseErr != nil || id == uuid.Nil || id.String() != key || size < 1 || size > 500 {
		return biz.CoreSnapshotCursor{}, biz.ErrCoreProjectionInvalid
	}
	raw, _ := json.Marshal(map[string]any{"idempotency_key": key, "page_size": size})
	var doc coreSnapshotCursorDocument
	if err := c.request(ctx, "POST", coreSnapshotHTTPPath, raw, &doc); err != nil {
		return biz.CoreSnapshotCursor{}, err
	}
	cursor, err := c.cursor(doc)
	if err == nil && cursor.PageSize != size {
		err = biz.ErrCoreProjectionConflict
	}
	return cursor, err
}
func (c *coreSnapshotHTTPClient) Page(ctx context.Context, cursor biz.CoreSnapshotCursor, token string) (biz.CoreSnapshotPage, error) {
	empty := biz.CoreSnapshotPage{}
	if cursor.Validate() != nil || cursor.Producer != c.producer || cursor.ConsumerID != c.consumer {
		return empty, biz.ErrCoreProjectionInvalid
	}
	if !time.Now().Before(cursor.ExpiresAt) {
		return empty, biz.ErrCoreSnapshotExpired
	}
	if token != "" {
		id, err := uuid.Parse(token)
		if err != nil || id == uuid.Nil || id.String() != token {
			return empty, biz.ErrCoreProjectionInvalid
		}
	}
	query := url.Values{"cursor_id": {cursor.ID.String()}}
	if token != "" {
		query.Set("page_token", token)
	}
	var doc struct {
		Cursor coreSnapshotCursorDocument `json:"cursor"`
		Items  []struct {
			TenantID string `json:"tenant_id"`
			Status   string `json:"status"`
			Version  int64  `json:"lifecycle_version"`
		} `json:"items"`
		Next string `json:"next_page_token"`
	}
	if err := c.request(ctx, "GET", coreSnapshotHTTPPath+"?"+query.Encode(), nil, &doc); err != nil {
		return empty, err
	}
	actual, err := c.cursor(doc.Cursor)
	if err != nil {
		return empty, err
	}
	if actual.ID != cursor.ID || actual.SourceCut != cursor.SourceCut || actual.BrokerAfter != cursor.BrokerAfter || actual.PageSize != cursor.PageSize || !actual.ExpiresAt.Equal(cursor.ExpiresAt) {
		return empty, biz.ErrCoreProjectionConflict
	}
	page := biz.CoreSnapshotPage{Cursor: actual, RequestToken: token, NextToken: doc.Next, Items: make([]biz.CoreSnapshotFact, 0, len(doc.Items))}
	for _, f := range doc.Items {
		id, err := uuid.Parse(f.TenantID)
		if err != nil || id.String() != f.TenantID {
			return empty, biz.ErrCoreProjectionInvalid
		}
		page.Items = append(page.Items, biz.CoreSnapshotFact{TenantID: id, Version: f.Version, Status: f.Status})
	}
	return page, page.Validate()
}
