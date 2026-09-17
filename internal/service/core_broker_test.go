package service

import (
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"testing"
)

func TestFormerCoreBrokerContractsAreRejected(t *testing.T) {
	decoder := NewGovernanceBrokerDecoder()
	for _, subject := range []string{"ani.integration.tenant.lifecycle.v1", "ani.integration.tenant.iam-bootstrap.v1", "ani.integration.tenant.lifecycle-heartbeat.v1"} {
		if _, err := decoder.Decode(biz.CoreBrokerMessage{Subject: subject, Payload: []byte(`{"envelope":{"schema_major":1,"producer":"core-control-service"}}`)}); err == nil {
			t.Fatal("legacy subject accepted", subject)
		}
	}
}
