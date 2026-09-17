package data

import "testing"

func TestDeploymentEndpoints(t *testing.T) {
	for _, value := range []string{"127.0.0.1:9443", "[::1]:9443", "10.96.10.3:9443", "notification.iam-gov.svc.cluster.local:9443", "iam:9443"} {
		if !ValidDeploymentAddress(value) {
			t.Fatalf("valid deployment endpoint rejected: %s", value)
		}
	}
	for _, value := range []string{"0.0.0.0:9443", "[::]:9443", "224.0.0.1:9443", "[ff02::1]:9443", ":9443", "dns:///iam:9443", "iam:0", "iam:65536", "iam:+443", "iam:0443", "iam:https", "*.svc:443", "a..svc:443", "-iam:443", "iam.:443", "IAM:443", "[fe80::1%eth0]:443", "iam:443/path"} {
		if ValidDeploymentAddress(value) {
			t.Fatalf("invalid endpoint accepted: %s", value)
		}
	}
	for _, value := range []string{"https://governance.iam-gov.svc:9443", "https://governance", "https://[::1]:9443"} {
		if !ValidHTTPSOrigin(value) {
			t.Fatalf("origin rejected: %s", value)
		}
	}
	for _, value := range []string{"http://governance:9443", "https://user@governance:9443", "https://governance:9443/", "https://governance:9443?", "https://governance:9443?a=b", "https://governance:9443#x", "https://0.0.0.0:443", "https://governance:0"} {
		if ValidHTTPSOrigin(value) {
			t.Fatalf("invalid origin accepted: %s", value)
		}
	}
}
