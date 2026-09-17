package data

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/redis/go-redis/v9"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"net/netip"
	"strings"
	"time"
)

type invitedAccountLimiter struct {
	client    redis.UniversalClient
	namespace string
}

func NewInvitedAccountLimiter(client redis.UniversalClient, namespace string) (biz.InvitedAccountLimiter, error) {
	namespace = strings.Trim(strings.TrimSpace(namespace), ":")
	if client == nil || namespace == "" {
		return nil, biz.ErrAuthenticationDependency
	}
	return &invitedAccountLimiter{client: client, namespace: namespace}, nil
}

// All dimensions are checked and counted in one Redis script. Rejected traffic
// does not reset or extend the original window.
var acquireInvitedAccountLimit = redis.NewScript(`
local wait=0
for i,key in ipairs(KEYS) do
 local count=tonumber(redis.call('GET',key) or '0')
 local ttl=redis.call('PTTL',key)
 if count>0 and ttl<1 then return {-1,0} end
 if count>=tonumber(ARGV[i+1]) then wait=math.max(wait,ttl) end
end
if wait>0 then return {1,wait} end
for _,key in ipairs(KEYS) do
 if redis.call('INCR',key)==1 then redis.call('PEXPIRE',key,ARGV[1]) end
end
return {0,0}
`)

func (l *invitedAccountLimiter) acquire(ctx context.Context, scope string, window time.Duration, keys []string, limits ...int) error {
	args := []any{window.Milliseconds()}
	for _, n := range limits {
		args = append(args, n)
	}
	r, err := acquireInvitedAccountLimit.Run(ctx, l.client, keys, args...).Int64Slice()
	if err != nil || len(r) != 2 || r[0] < 0 {
		return biz.ErrAuthenticationDependency
	}
	if r[0] == 0 {
		return nil
	}
	if r[0] != 1 || r[1] < 1 {
		return biz.ErrAuthenticationDependency
	}
	return &biz.AuthenticationRateLimitError{LimitScope: scope, RetryAfter: time.Duration(r[1]) * time.Millisecond}
}
func (l *invitedAccountLimiter) AcquireVerificationRequest(ctx context.Context, email string, ip netip.Addr) error {
	if email == "" || !ip.IsValid() {
		return biz.ErrInvitedAccountInvalid
	}
	prefix := l.namespace + ":invited-account:request:"
	return l.acquire(ctx, "invited_account_verification", 15*time.Minute, []string{fmt.Sprintf("%semail:%x", prefix, sha256.Sum256([]byte(email))), fmt.Sprintf("%sip:%x", prefix, sha256.Sum256([]byte(ip.Unmap().String())))}, 3, 20)
}
func (l *invitedAccountLimiter) AcquireAccountCompletion(ctx context.Context, ip netip.Addr) error {
	if !ip.IsValid() {
		return biz.ErrInvitedAccountInvalid
	}
	return l.acquire(ctx, "invited_account_completion", time.Minute, []string{fmt.Sprintf("%s:invited-account:complete:ip:%x", l.namespace, sha256.Sum256([]byte(ip.Unmap().String())))}, 20)
}
