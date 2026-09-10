---
status: accepted
---

# Treat the September v1 release as a test-environment delivery

The September 30 `v1.0.0` milestone is an acceptance delivery for a test and demonstration environment with no real users; ANI does not yet have a production environment, which will be established only after the refactor. The version may be delivered without claiming production readiness. Missing production-scale HA, backup, failover, disaster-recovery rehearsal, soak, or other production evidence is recorded as `not_verified` and remains a blocker for any later production-readiness declaration, not for this test-environment milestone. This deferral does not waive an accepted work item's narrower functional-safety gates: the actual replacement/retirement items must verify their accepted rollback, trust-invalidation and recovery conditions before executing the corresponding actions. The user now places isolated API readiness (M1) before frontend integration and actual cutover; deployment-cutover and large unaccepted recovery mechanisms are not M1 prerequisites. The exact current scope is in the WR spec and decisions register.
