---
status: accepted
date: 2026-09-17
---

# Link modes are one body with a linkMode value

Hardlink mode and reflink mode share one `processLinkMode` body in `internal/services/crossseed/link_mode.go`. Every difference between the two modes is a field on the `linkMode` struct, and each mode lists its fields in one literal. The create-error split stays: when the plan build or the link tree creation fails, hardlink mode falls back to regular mode (with a full recheck after a creation failure) and reflink mode refuses regular fallback in both cases. Both were written in #1912. The split is a kept decision, not drift. Issue #2746.

## Considered options

- **Two bodies, the status quo.** Rejected: the bodies were 507 and 521 lines with seven behavioral differences. A rule fix had to land twice, and the bodies drifted on the below-threshold status note and on the pooled partial completion wiring (#2356).
- **A materializer interface with `materialize` and `rollback`.** Rejected: rollback is already shared code, and only the materialize call varies. A struct with one function field replaces the interface.
- **A mode enum with branches in the body.** Rejected: an enum scatters the asymmetries through the body. The struct lists them in one place per mode.

## Consequences

- A new link mode is one more `linkMode` literal.
- A change that moves an asymmetry out of the struct into a branch on the mode name, or that changes the create-error split, reverses this decision and needs a new ADR.
- `recheck_policy.go` is gone. `linkModeRecheckPolicy` lives in `link_mode.go`, and its table test in `relaxed_structure_recheck_test.go` still pins the rule.
