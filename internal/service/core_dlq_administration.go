package service

import (
	"context"
	"github.com/google/uuid"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"google.golang.org/protobuf/types/known/timestamppb"
	"math"
	"sort"
)

func coreDLQEntryDTO(v biz.CoreDLQEntry) *iamv1.CoreDLQEntry {
	return &iamv1.CoreDLQEntry{EntryId: v.ID.String(), ConsumerId: v.ConsumerID.String(), BrokerName: v.Message.BrokerName, Account: v.Message.Account, Stream: v.Message.Stream, Subject: v.Message.Subject, BrokerSequence: v.Message.BrokerSequence, DeliveryCount: v.Message.DeliveryCount, PublishedAt: timestamppb.New(v.Message.PublishedAt), QuarantinedAt: timestamppb.New(v.QuarantinedAt), RawSha256: v.RawSHA256, LastError: v.LastError, LastAttempt: uint64(v.LastAttempt), ProvenanceAvailable: v.ProvenanceAvailable}
}
func (s *IAMAdminService) ListCoreDLQEntries(ctx context.Context, r *iamv1.ListCoreDLQEntriesRequest) (*iamv1.ListCoreDLQEntriesResponse, error) {
	const op = "listCoreIAMDLQEntries"
	cap, err := s.platformCapability(ctx, r.GetCredential(), "ListCoreDLQEntries", op, r.GetConsumerId())
	if err != nil {
		return nil, err
	}
	consumer, err := requiredUUID(r.GetConsumerId(), "consumer_id")
	if err != nil {
		return nil, err
	}
	v, err := s.platform.ListCoreDLQEntries(ctx, cap, consumer, r.GetPage().GetCursor(), r.GetPage().GetPageSize())
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: op, ResourceID: consumer.String()})
	}
	out := &iamv1.ListCoreDLQEntriesResponse{Entries: []*iamv1.CoreDLQEntry{}, NextCursor: v.NextCursor}
	for _, entry := range v.Entries {
		out.Entries = append(out.Entries, coreDLQEntryDTO(entry))
	}
	return out, nil
}
func (s *IAMAdminService) GetCoreDLQEntry(ctx context.Context, r *iamv1.GetCoreDLQEntryRequest) (*iamv1.GetCoreDLQEntryResponse, error) {
	const op = "getCoreIAMDLQEntry"
	cap, err := s.platformCapability(ctx, r.GetCredential(), "GetCoreDLQEntry", op, r.GetEntryId())
	if err != nil {
		return nil, err
	}
	consumer, err := requiredUUID(r.GetConsumerId(), "consumer_id")
	if err != nil {
		return nil, err
	}
	entry, err := requiredUUID(r.GetEntryId(), "entry_id")
	if err != nil {
		return nil, err
	}
	v, err := s.platform.GetCoreDLQEntry(ctx, cap, consumer, entry, r.GetPage().GetCursor(), r.GetPage().GetPageSize())
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: op, ResourceID: entry.String()})
	}
	out := &iamv1.GetCoreDLQEntryResponse{Entry: coreDLQEntryDTO(v.Entry), RawPayload: v.Entry.Message.Payload, Headers: []*iamv1.CoreDLQHeader{}, Attempts: []*iamv1.CoreDLQAttempt{}, NextCursor: v.NextCursor}
	names := make([]string, 0, len(v.Entry.Message.Headers))
	for name := range v.Entry.Message.Headers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		out.Headers = append(out.Headers, &iamv1.CoreDLQHeader{Name: name, Values: v.Entry.Message.Headers[name]})
	}
	for _, a := range v.Attempts {
		out.Attempts = append(out.Attempts, &iamv1.CoreDLQAttempt{AttemptId: a.ID.String(), AuditId: a.AuditID.String(), AttemptNumber: uint64(a.Number), Outcome: a.Outcome, ProjectionOutcome: a.ProjectionOutcome, ErrorCode: a.ErrorCode, ReasonCode: a.ReasonCode, AuthoritySha256: a.AuthoritySHA256, CreatedAt: timestamppb.New(a.CreatedAt)})
	}
	return out, nil
}
func (s *IAMAdminService) ReplayCoreDLQEntry(ctx context.Context, r *iamv1.ReplayCoreDLQEntryRequest) (*iamv1.ReplayCoreDLQEntryResponse, error) {
	const op = "replayCoreIAMDLQEntry"
	cap, err := s.platformCapabilityForReason(ctx, r.GetCredential(), "ReplayCoreDLQEntry", op, r.GetEntryId(), r.GetReasonCode())
	if err != nil {
		return nil, err
	}
	consumer, err := requiredUUID(r.GetConsumerId(), "consumer_id")
	if err != nil {
		return nil, err
	}
	entry, err := requiredUUID(r.GetEntryId(), "entry_id")
	if err != nil {
		return nil, err
	}
	if r.GetExpectedAttempt() >= math.MaxInt64 {
		return nil, invalidArgumentStatus("expected_attempt", "DLQ attempt precondition is invalid")
	}
	v, err := s.platform.ReplayCoreDLQEntry(ctx, cap, biz.ReplayCoreDLQCommand{ConsumerID: consumer, EntryID: entry, ExpectedRawSHA256: r.GetExpectedRawSha256(), ExpectedAttempt: int64(r.GetExpectedAttempt()), ReasonCode: r.GetReasonCode(), IdempotencyKey: r.GetIdempotencyKey()})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: op, ResourceID: entry.String(), IdempotencyKey: r.GetIdempotencyKey()})
	}
	out := &iamv1.ReplayCoreDLQEntryResponse{ConsumerId: v.ConsumerID.String(), EntryId: v.EntryID.String(), AttemptId: v.AttemptID.String(), AuditId: v.AuditID.String(), AttemptNumber: uint64(v.AttemptNumber), RawSha256: v.RawSHA256, Outcome: v.Outcome, ProjectionOutcome: v.ProjectionOutcome, AuthoritySha256: v.AuthoritySHA256, EventId: v.EventID.String(), SourceSequence: v.SourceSequence, Replayed: v.Replayed}
	if v.TenantID != uuid.Nil {
		out.TenantId = v.TenantID.String()
	}
	if v.OperationID != uuid.Nil {
		out.OperationId = v.OperationID.String()
	}
	return out, nil
}
