package data

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRedisAPIKeyRuntimeControlsRejectIncompleteConfiguration(t *testing.T) {
	if _, err := NewRedisAPIKeyCreationLimiter(nil, RedisAPIKeyCreationLimiterConfig{
		Namespace: "test", Limit: 1, Window: time.Minute,
	}); err != ErrInvalidRedisAPIKeyCreationLimiterConfiguration {
		t.Fatalf("NewRedisAPIKeyCreationLimiter() error = %v", err)
	}
	if _, err := NewRedisAPIKeyUsageAggregator(nil, NewData(nil), RedisAPIKeyUsageConfig{
		Namespace: "test", BatchSize: 1,
	}); err != ErrInvalidRedisAPIKeyUsageConfiguration {
		t.Fatalf("NewRedisAPIKeyUsageAggregator() error = %v", err)
	}
}

func TestAPIKeyUsageMemberRoundTripsOnlyBoundaryIdentities(t *testing.T) {
	tenantID := uuid.MustParse("0199d080-5000-7001-9000-000000000001")
	keyID := uuid.MustParse("0199d080-5000-7001-9000-000000000002")
	member := apiKeyUsageMember(tenantID, keyID)
	gotTenantID, gotKeyID, err := parseAPIKeyUsageMember(member)
	if err != nil || gotTenantID != tenantID || gotKeyID != keyID {
		t.Fatalf("parseAPIKeyUsageMember(%q) = %s/%s/%v", member, gotTenantID, gotKeyID, err)
	}
	for _, invalid := range []string{"", tenantID.String(), tenantID.String() + "/not-a-key", "too/many/parts"} {
		if _, _, err := parseAPIKeyUsageMember(invalid); err == nil {
			t.Fatalf("parseAPIKeyUsageMember(%q) succeeded", invalid)
		}
	}
}
