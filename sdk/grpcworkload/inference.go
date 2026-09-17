package grpcworkload

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

const InferenceAudience = "ani-inference-service"
const InferenceOperation = "inference.check_access"
const InferenceMethod = "/inference.control.v1.InferenceControl/CheckInferenceAccess"
const InferenceSourceOperation = "invokeInferenceChatCompletions"

// AuthorizeInferenceAccess is retained as an existing caller name. Target rules
// come exclusively from the supplied registry; new owners use AuthorizeForReceiver.
func (c *Client) AuthorizeInferenceAccess(ctx context.Context, r AuthorizationRequest) (Subject, error) {
	var selected WorkloadTarget
	for _, t := range c.cfg.Registry.Targets() {
		for _, source := range t.Sources {
			if source.Operation == r.SourceOperation && source.OwnerCheck == "receiver" {
				if selected != (WorkloadTarget{}) {
					return Subject{}, ErrConfiguration
				}
				selected = WorkloadTarget{Audience: t.Audience, Operation: t.Operation, RPCMethod: t.RPC}
			}
		}
	}
	return c.AuthorizeForReceiver(ctx, r, selected)
}

// InferenceReceiverInterceptor is a compatibility entry to the common receiver.
func (c *Client) InferenceReceiverInterceptor(target Target, check func(context.Context, proto.Message, Verified) error) (grpc.UnaryServerInterceptor, error) {
	return c.ReceiverInterceptorWithOwnerCheck([]Target{target}, check)
}
