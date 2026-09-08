package server

import (
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

func TestObservabilityPublishesCredentialFreeAPIKeyOperationalGauges(t *testing.T) {
	observability, err := NewObservability("ani-iam-test", "test", NewReadiness())
	if err != nil {
		t.Fatalf("NewObservability() error = %v", err)
	}
	t.Cleanup(func() {
		if err := observability.Shutdown(t.Context()); err != nil {
			t.Errorf("Shutdown() error = %v", err)
		}
	})
	assertGaugeValues(t, observability, map[string]float64{
		"ani_iam_api_key_stale_non_expiring_count":                0,
		"ani_iam_service_principal_unusual_active_api_keys_count": 0,
		"ani_iam_api_key_operational_snapshot_timestamp_seconds":  0,
	})

	observability.SetAPIKeyOperationalSnapshot(biz.APIKeyOperationalSnapshot{
		StaleNonExpiringCount:        7,
		UnusualServicePrincipalCount: 3,
		ObservedAt:                   time.Date(2026, 9, 9, 1, 2, 3, 0, time.UTC),
	})

	assertGaugeValues(t, observability, map[string]float64{
		"ani_iam_api_key_stale_non_expiring_count":                7,
		"ani_iam_service_principal_unusual_active_api_keys_count": 3,
		"ani_iam_api_key_operational_snapshot_timestamp_seconds":  1788915723,
	})
}

func assertGaugeValues(t *testing.T, observability *Observability, want map[string]float64) {
	t.Helper()
	families, err := observability.Gatherer().Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	for name, wantValue := range want {
		family := findMetricFamily(families, name)
		if family == nil {
			t.Fatalf("metric %q not found", name)
		}
		if len(family.Metric) != 1 || family.Metric[0].Gauge == nil {
			t.Fatalf("metric %q = %#v, want one gauge", name, family.Metric)
		}
		if got := family.Metric[0].Gauge.GetValue(); got != wantValue {
			t.Fatalf("metric %q = %v, want %v", name, got, wantValue)
		}
		for _, label := range family.Metric[0].Label {
			switch label.GetName() {
			case "tenant_id", "principal_id", "credential_id", "api_key_id":
				t.Fatalf("metric %q exposes forbidden label %q", name, label.GetName())
			}
		}
	}
}

func findMetricFamily(families []*dto.MetricFamily, name string) *dto.MetricFamily {
	for _, family := range families {
		if family.GetName() == name {
			return family
		}
	}
	return nil
}
