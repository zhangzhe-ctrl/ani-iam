package service

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	governancev1 "github.com/zhangzhe-ctrl/ani-governance/api/governance/v1"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/sdk/tenantbootstrap"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestGovernanceBrokerConsumesOnlyOwnedSingleTargetContracts(t *testing.T) {
	id := func() string { return uuid.Must(uuid.NewV7()).String() }
	epoch, tenant, eventID := id(), id(), id()
	now := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	lifecycle := governancev1.LifecycleEvent{Revision: governancev1.Revision, Producer: governancev1.Producer, EventId: eventID, Epoch: epoch, Sequence: 1, TenantId: tenant, TenantVersion: 1, BusinessStatus: governancev1.Active, Reason: "created by owner", OccurredAt: now, EffectiveAt: now}
	raw, err := json.Marshal(lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	message := biz.CoreBrokerMessage{Subject: governancev1.LifecycleSubject, Payload: raw, Headers: map[string][]string{"Content-Type": {"application/json"}, "Nats-Msg-Id": {eventID}}}
	decoder := NewGovernanceBrokerDecoder()
	decoded, err := decoder.Decode(message)
	if err != nil || decoded.Lifecycle == nil || decoded.Bootstrap != nil || decoded.Heartbeat != nil || decoded.Sequence != 1 || decoded.Epoch.String() != epoch || !bytes.Equal(decoded.RawPayload, raw) {
		t.Fatal(decoded, err)
	}
	heartbeat := governancev1.Heartbeat{Revision: governancev1.Revision, Producer: governancev1.Producer, HeartbeatId: id(), Epoch: epoch, CommittedSequence: 0, PublishedSequence: 0, ObservedAt: now}
	raw, err = json.Marshal(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err = decoder.Decode(biz.CoreBrokerMessage{Subject: governancev1.HeartbeatSubject, Payload: raw})
	if err != nil || decoded.Heartbeat == nil || decoded.Sequence != 0 || decoded.TenantID != uuid.Nil || decoded.Lifecycle != nil {
		t.Fatal("heartbeat became Tenant or stream event", decoded, err)
	}
	dto := &iamv1.TenantBootstrapRequested{SchemaRevision: tenantbootstrap.Revision, EventId: id(), Producer: governancev1.Producer, SourceEpoch: epoch, LifecycleSequence: 1, TenantId: tenant, OperationId: id(), OccurredAt: timestamppb.New(now), IntendedAdministrator: &iamv1.TenantBootstrapAdministrator{NormalizedEmail: "admin@example.com", Locale: "zh-CN"}}
	dto.PayloadFingerprint, err = tenantbootstrap.Fingerprint(dto.TenantId, dto.OperationId, dto.IntendedAdministrator.NormalizedEmail, dto.IntendedAdministrator.Locale)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = tenantbootstrap.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err = decoder.Decode(biz.CoreBrokerMessage{Subject: tenantbootstrap.Subject, Payload: raw})
	if err != nil || decoded.Bootstrap == nil || decoded.Sequence != 1 || decoded.Lifecycle != nil || decoded.Heartbeat != nil || decoded.TenantID.String() != tenant {
		t.Fatal("bootstrap became Lifecycle slot", decoded, err)
	}
	for _, subject := range []string{"ani.integration.tenant.lifecycle.v1", "ani.integration.tenant.iam-bootstrap.v1", "ani.integration.tenant.lifecycle-heartbeat.v1", governancev1.LifecycleSubject} {
		if _, err := decoder.Decode(biz.CoreBrokerMessage{Subject: subject, Payload: raw}); err == nil {
			t.Fatal("wrong contract or legacy alias accepted", subject)
		}
	}
	lifecycleRaw, _ := json.Marshal(lifecycle)
	for _, raw := range []string{
		strings.Replace(string(lifecycleRaw), `"sequence":1`, `"sequence":"1"`, 1),
		strings.Replace(string(lifecycleRaw), `"revision":`, `"revision":"gov-tenant-v1","revision":`, 1),
		strings.Replace(string(lifecycleRaw), `"producer":"ani-governance"`, `"producer":"ani-core-control"`, 1),
	} {
		if _, err := decoder.Decode(biz.CoreBrokerMessage{Subject: governancev1.LifecycleSubject, Payload: []byte(raw)}); err == nil {
			t.Fatal("ambiguous message accepted")
		}
	}
	for _, headers := range []map[string][]string{
		{"Nats-Msg-Id": {id()}}, {"Nats-Msg-Id": {eventID, eventID}}, {"Principal-Id": {id()}}, {"Content-Type": {"text/plain"}},
	} {
		if _, err := decoder.Decode(biz.CoreBrokerMessage{Subject: governancev1.LifecycleSubject, Payload: lifecycleRaw, Headers: headers}); err == nil {
			t.Fatal("untrusted header accepted")
		}
	}
}
