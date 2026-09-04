# DP2-03 contract-first RED

Result: `fail`

Command:

```text
env GOTMPDIR=/home/chabking/.cache/ani-direct-p2-01-05-go-tmp go test ./tests/contracts -count=1
```

Observed before adding any target contract or generated artifact:

```text
TestIAMDescriptorHasOnlyTargetServices: missing api/iam/v1/iam_descriptor.pb
TestCoreDescriptorOwnsLifecycleBootstrapAndSnapshot: missing tests/contracts/artifacts/core_tenant_integration_v1_descriptor.pb
TestProducerConsumerFixturesRoundTripStrictly: missing IAM/Core descriptors
TestBootstrapPayloadFingerprint: missing core_tenant_iam_bootstrap_requested.v1.json
TestStableErrorInfoContracts: missing IAM and Core ErrorInfo fixtures
TestImmutableContractPins: missing tests/contracts/contract_pins.json
fail
```

The pre-ticket repositories contained neither the target three-service IAM contract nor a canonical Core Lifecycle/Bootstrap/Snapshot artifact. The test established exact service/method inventories, required message fields, strict producer-consumer fixture decoding, payload fingerprint verification, stable gRPC/ErrorInfo mapping, immutable artifact pins, and the existing `biz` import boundary before any target source or generated file was added.

No generated file, contract source, runtime code, external system, publisher, NATS infrastructure or consumer pin was changed by this RED run.
