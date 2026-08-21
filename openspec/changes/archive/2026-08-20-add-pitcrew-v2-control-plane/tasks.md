# Tasks: add-pitcrew-v2-control-plane

## Review Workload Forecast

Correction estimate: ≤400 lines/slice; 20 total PRs.

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|---|---|---|---|---|---|
| 16 | Unit-command atomicity | PR 16 | `go test ./internal/handles ./internal/cli -run 'Refresh|Atomic'` | failed-complete keeps expiry | handle/CLI transaction |
| 17 | TDD outcome semantics | PR 17 | `go test ./internal/evidence ./internal/cli -run 'Outcome|TDD'` | false outcomes rejected | evidence validation |
| 18 | Plan approval authority | PR 18 | `go test ./internal/plan ./internal/cli -run 'Approval|Ready|Claim|Exception'` | preapproval denied; exception persists | plan service/schema |
| 19 | State/transition contract | PR 19 | `go test ./internal/workflow ./internal/cli -run 'Transition|StateError'` | legal/invalid matrix | workflow errors/tests |
| 20 | Strict evidence markers | PR 20 | `awk '/✅ Written.*✅ Passed/{n++}END{exit n!=11}' openspec/changes/add-pitcrew-v2-control-plane/apply-progress.md` | N/A:evidence-only | `apply-progress.md` |

## Phase 1: Bootstrap and Contract Repair

- [x] 1.1 Keep existing maxims/OpenSpec artifacts.
- [x] 1.2 Reconcile command-count, lifecycle-state, and hand-off contradictions.
- [x] 1.3 Create Go 1.26 module + SQLite; build.

## Phase 2: Store and Domain

- [x] 2.1 RED-test IDs/randomness/timestamps/envelopes/codes.
- [x] 2.2 Implement `internal/ids` and `internal/envelope`.
- [x] 2.3 RED-test store PRAGMAs/schema/migrations/busy-timeout/CAS.
- [x] 2.4 Implement local single-connection network-free store.
- [x] 2.5 RED-test/implement transitions/events/abandonment.
- [x] 2.6 RED-test/implement plan dependencies/overlaps/budgets/exceptions/readiness.
- [x] 2.7 RED-test/implement evidence/reviews/corrections/revisions/completion-gates.

## Phase 3: Claim Security

- [x] 3.1 RED-test issuance symlinks/ownership/modes/hash-only/opaque-paths.
- [x] 3.2 Implement cryptographic/SHA-256/`0700`/`0600`/no-symlink/atomic-writes.
- [x] 3.3 RED-test promotion/refresh/five-minute-expiry/deletion/recovery/generation/debug-revocation/identity-collision.
- [x] 3.4 Implement lifecycle/recovery/authority/opaque-hand-off.

## Phase 4: CLI and Integration

- [x] 4.1 RED-test/implement principles/help/version: exact-text/JSON, epilogue.
- [x] 4.2 RED-test/implement new/show: long-flags, envelopes, stderr-errors/codes; forbid `daemon/RPC/TUI/Master/installer/raw-token/v1-migration`.
- [x] 4.3 RED-test flag/DTO matrices: explicit-id/revision/actor/claim flags; unknown/duplicate/short/missing flags; no-follow regular/readable UTF-8 single-strict-JSON inputs; usage-before-store/state classification.
- [x] 4.4 Wire append-only ordered artifact/plan/unit flows, workflow/unit CAS, atomic rows/events/transitions/finalization, actor collisions, and revoke-before-output debug envelope (one secret; dead handle); run `go test ./...`.

## Phase 5: Runtime Installation and Documentation

- [x] 5.1 RED-test POSIX/runtime-detection/idempotency/rollback, Master-overwrite, exact-maxims, eight-roles, prohibitions, hand-off.
- [x] 5.2 Implement installer/shell-tests/docs; run `sh scripts/tests/run.sh` and `go test ./...`.

## Phase 6: Verification Corrections

- [x] 6.1 RED: prove failed unit commands never refresh expiry; assert database and handle bytes unchanged.
- [x] 6.2 Move refresh into the unit mutation transaction; commit only after command success.
- [x] 6.3 RED: reject semantic RED-pass, GREEN-fail, and validation-fail outcomes without mutation.
- [x] 6.4 Enforce RED failure plus GREEN/validation success in `internal/evidence`.
- [x] 6.5 RED: deny readiness/claims before plan approval and require persisted approved exceptions.
- [x] 6.6 Gate readiness/claims on `plan_approved`; persist only explicitly approved exceptions atomically.
- [x] 6.7 RED: require state errors to name current+expected and execute every legal/invalid transition.
- [x] 6.8 Add structured expected-state errors; make the exhaustive runtime matrix pass.
- [x] 6.9 Repair every `apply-progress.md` row with evidence-backed `✅ Written` RED and `✅ Passed` GREEN markers.
- [x] 6.10 Run focused/full Go tests, shell tests, build/vet/format/diff checks; hand off readiness for independent strict verification.
