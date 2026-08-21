# Apply Progress: add-pitcrew-v2-control-plane

## Delivery

- Strategy: `auto-chain`
- Chain strategy: `stacked-to-main`
- Planned boundaries: PR 1 contracts/IDs/envelopes; PR 2 store; PR 3 workflow;
  PR 4 plans; PR 5 evidence; PR 6 handle issuance/filesystem; PR 7 handle
  lifecycle/authority; PR 8 maxims/static CLI; PR 9 new/show/errors; PR 10
  inputs/artifacts; PR 11 plan/readiness; PR 12 claims/recovery; PR 13 TDD/review;
  PR 14 completion/revocation; PR 15 installer/docs; PR 16 failed-unit-command
  claim atomicity correction; PR 17 TDD outcome semantics correction; PR 18
  plan approval authority correction; PR 19 transition/state-error correction;
  PR 20 evidence audit and final correction validation.
- Current boundary: PR 20 / Work Unit 20 — strict evidence audit and verification hand-off.
- Mode: Strict TDD

The original 488-line CLI boundary was split into the current PR 8/PR 9 plan:

1. PR 8 — canonical maxims embedding/parsing plus global help/version and
   `principles` text/JSON representations.
2. PR 9 — project-local `workflow new/show`, success/failure envelopes, exit
   classification, and the real binary lifecycle harness.

## Completed Tasks

- [x] 1.1–1.3 Foundation preservation, contract repair, and Go bootstrap.
- [x] 2.1–2.2 IDs, timestamps, and envelopes.
- [x] 2.3–2.4 SQLite store, migrations, busy timeout, and CAS.
- [x] 2.5 Workflow transitions, events, and abandonment.
- [x] 2.6 Plan validation, admission, dependencies, and readiness.
- [x] 2.7 TDD/review evidence, corrections, revisioning, and completion gates.
- [x] 3.1–3.2 Claim-handle issuance and filesystem security.
- [x] 3.3–3.4 Claim-handle lifecycle, recovery, and authority.
- [x] 4.1 CLI/maxims RED tests.
- [x] 4.2 CLI new/show/error implementation and prohibited-surface checks.
- [x] 4.3 Exact flag/DTO/input-file RED matrices.
- [x] 4.4 Artifact, plan, claim, TDD, review, correction, and completion wiring.
- [x] 5.1 POSIX installer and runtime contract RED tests.
- [x] 5.2 Installer, role prompts, shared contract, and progressive documentation.
- [x] 6.1 Failed-unit-command claim immutability RED regression.
- [x] 6.2 Transactional claim promotion/refresh with unit mutation commit.
- [x] 6.3 False RED/GREEN/validation outcome immutability RED regression.
- [x] 6.4 Exit-status-based TDD outcome semantics.
- [x] 6.5 Preapproval readiness/claim denial and exception persistence RED regression.
- [x] 6.6 Atomic plan authority and explicit admission-exception persistence.
- [x] 6.7 Exhaustive legal/invalid transition and state-error RED matrix.
- [x] 6.8 Structured current/expected transition errors.
- [x] 6.9 Evidence-backed exact RED/GREEN markers on every cumulative row.
- [x] 6.10 Focused/full validation and independent-verification hand-off.

## Contract Reconciliation

- The command surface is 16 `workflow` subcommands plus `pitcrew principles`;
  `--version` and `--help` are global flags.
- Successful workflow commands use the standard envelope. `principles` emits
  byte-exact embedded text or a raw structured JSON array; global help/version
  remain plain text.
- The aggregate has nine ordinary states plus exceptional `abandoned`;
  `reviewing` is a work-unit state.
- Roles persist content directly and return revision-bearing one-line status.
- Events carry abandonment reasons and work units carry revisions.
- Handle files remain exactly the documented public shape. Runtime actor identity
  is stored only in SQLite to enforce Implementer/Reviewer separation and never
  crosses into the opaque handle file.
- Go's embed directive cannot reference a parent directory. A root-package
  `maxims_embed.go` carrier embeds the sole canonical root `MAXIMS.md`; the
  internal parser consumes those bytes without copying the document.

## TDD Cycle Evidence

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|---|---|---|---|---|---|---|---|
| 1.2 | OpenSpec artifacts | Contract | N/A | ✅ Written — recovered-artifact contradiction inventory recorded command-count, aggregate-state, and hand-off conflicts before reconciliation | ✅ Passed — proposal/spec/design cross-check confirms the closed 16-command surface, work-unit-only `reviewing`, and direct role-to-control-plane hand-off | Three contradictions resolved | Terminology normalized |
| 1.3 | `go.mod` | Build | N/A | ✅ Written — structural bootstrap requirement recorded before the Go module/build surface existed | ✅ Passed — audited `GOCACHE=/tmp/pitcrew2-pr20-audit go build ./...`, exit 0 | Module/dependency checked | `go mod tidy` |
| 2.1–2.2 | `internal/ids/ids_test.go`, `internal/envelope/envelope_test.go` | Unit | N/A (new) | ✅ Written — tests initially failed on undefined ID/envelope symbols, exit 1 | ✅ Passed — audited `go test ./internal/ids ./internal/envelope -count=1` (`0.002s`, `0.002s`) | Formats, entropy, UTC, success/failure documents, codes | Shared helpers/tables |
| 2.3–2.4 | `internal/store/store_test.go` | Integration | N/A (new) | ✅ Written — tests initially failed on undefined store symbols and exposed partial invalid migration behavior | ✅ Passed — audited `go test ./internal/store -count=1` (`4.246s`); busy-timeout retries also passed 3/3 | PRAGMAs, schema, safe/destructive order, CAS, lock timeout | Batch preflight made atomic |
| 2.5 | `internal/workflow/workflow_test.go` | Integration | N/A (new) | ✅ Written — tests initially failed on undefined workflow symbols and exposed empty abandonment reason acceptance | ✅ Passed — audited `go test ./internal/workflow -count=1` (`0.050s`) | Lifecycle, invalid state, CAS, retention, JSON | Atomic row counts |
| 2.6 | `internal/plan/plan_test.go` | Unit | N/A (new) | ✅ Written — tests initially failed on undefined plan symbols and exposed accepted repository traversal | ✅ Passed — audited `go test ./internal/plan -count=1` (`0.002s`) | Cycles, references, overlap, path threats, budgets, readiness | Segment-aware safe prefixes |
| 2.7 | `internal/evidence/evidence_test.go` | Integration | N/A (new) | ✅ Written — tests initially failed on undefined evidence symbols; outside-plan signal/wire-shape cases were added RED | ✅ Passed — audited `go test ./internal/evidence -count=1` (`0.008s`) | Fields, corrections, revisions, review/handle gates | Explicit review outcome |
| 3.1–3.4 | `internal/handles/handles_test.go` | Integration/security | Store/evidence baseline passed | ✅ Written — tests initially failed on undefined handle API/schema, then exposed missing revocation and empty-actor rejection | ✅ Passed — audited `go test ./internal/handles -count=1` (`0.015s`) | Symlinks, ownership/modes, secret non-persistence, promotion, refresh, expiry, recovery, generation, revocation, identity collision | Atomic files, no-follow open, explicit revoke |
| 4.1–4.2 | `internal/maxims/maxims_test.go`, `internal/cli/cli_test.go`, `cmd/pitcrew/main_test.go` | Unit/integration/runtime | Existing domain suite passed | ✅ Written — tests initially failed on undefined maxims API, CLI dispatcher, and entrypoint, exit 1 | ✅ Passed — audited focused packages (`0.002s`, `0.070s`, `0.002s`) | Exact bytes/JSON, all exit classes, long-only flags, stderr isolation, local workflow lifecycle, forbidden help surfaces | Shared parser/error classifier and root embed carrier |
| 4.3–4.4 | `internal/cli/input_test.go`, `internal/cli/flows_test.go` plus domain tests | Integration/runtime/security | Six affected packages passed (`cli`, `workflow`, `plan`, `evidence`, `handles`, `store`) | ✅ Written — lifecycle tests rejected the old two-command parser; DTO/path cases then failed before their production guards | ✅ Passed — audited CLI/workflow/plan/evidence/handles/store packages (`0.073s`, `0.053s`, `0.002s`, `0.008s`, `0.015s`, `4.512s`) | All 16 flag matrices, symlink/FIFO/directory/UTF-8/strict JSON, artifact ordering, both CAS scopes, correction/recovery, actor collision, debug one-shot, dead handles, finalization | Exact parser/DTO helpers, domain services, atomic completion authority |
| 5.1–5.2 | `scripts/tests/run.sh` | POSIX shell/runtime/docs | N/A (new installer/docs) | ✅ Written — suite first failed because the installer was absent, then documentation assertions failed on missing root guides | ✅ Passed — audited `sh scripts/tests/run.sh`, `installer_smoke_tests=passed` | Four runtime targets, byte-exact maxims, nine fragments, idempotency, customized Master refusal/overwrite, ordinary fragment refresh, new/existing-file rollback, prohibitions, hand-off, exact CLI docs | Transactional staging/manifest rollback and progressive-disclosure docs |
| 6.1–6.2 | `internal/cli/flows_test.go` | Integration/runtime | ✅ Existing `internal/handles` and `internal/cli` suites passed | ✅ Written — three failed unit-command cases reproduced file-byte and database-expiry mutation | ✅ Passed — focused handles/CLI suites passed after transaction coordination | ✅ TDD, review, and completion domain failures preserve both claim representations | ✅ Shared transaction-aware evidence seams; claim file update/removal is coordinated with commit |
| 6.3–6.4 | `internal/evidence/evidence_test.go`, `internal/cli/flows_test.go` | Unit/integration/runtime | ✅ Existing `internal/evidence` and `internal/cli` suites passed | ✅ Written — semantic RED-pass, GREEN-fail, validation-fail, and arbitrary-prose outcomes were accepted and mutated unit/evidence/claim state | ✅ Passed — focused evidence/CLI outcome suites passed | ✅ Five invalid semantic cases, three CLI immutability cases, and canonical `exit N: detail` formatting | ✅ Small exit-status parser keeps domain validation independent of arbitrary prose |
| 6.5–6.6 | `internal/cli/flows_test.go` | Integration/runtime | ✅ Existing `internal/plan` and `internal/cli` suites passed | ✅ Written — submitted overlap metadata exposed two ready units before approval and permitted preapproval claim issuance | ✅ Passed — focused plan/CLI authority suites passed | ✅ Preapproval readiness/claim denial, explicit oversized approval persistence, unrelated metadata left unapproved, and postapproval readiness/claim | ✅ Plan load, exception markers, workflow transition, and event now share the approval transaction |
| 6.7–6.8 | `internal/workflow/workflow_test.go`, `internal/cli/cli_test.go` | Integration/runtime | ✅ Existing `internal/workflow` and `internal/cli` suites passed | ✅ Written — `TransitionError` was absent and the CLI reported `draft cannot complete` without `expected ready_to_complete` | ✅ Passed — focused workflow/CLI transition suites passed | ✅ 19 legal transitions including all non-terminal abandonment paths, 10 invalid source states, and a real CLI envelope | ✅ One transition table now drives next-state resolution and deterministic expected-source errors |
| 6.9–6.10 | `apply-progress.md` plus all listed suites | Evidence audit/full validation | ✅ All 14 referenced test/build/shell paths were present before edits | ✅ Written — independent verification documented 11 historical rows missing the mandatory exact RED/GREEN markers | ✅ Passed — each row was matched to recorded historical RED evidence and a current executable GREEN audit; the final focused/full/shell/build/static suite passed | ✅ Marker audit covers all 16 cumulative rows rather than only the 11 historical rows | ➖ Evidence-only correction; no production code changed |

## Test Summary

- Total tests: 52 top-level test functions plus table-driven cases.
- Layers: unit tests for IDs, envelopes, plans, maxims, and dispatch;
  temporary-SQLite/directory integration tests for store, workflow, evidence,
  handles, and CLI lifecycle; real-binary harnesses; POSIX shell installer tests.
- Claim threat cases: final-file and directory symlinks, mode/owner mismatch,
  plaintext secret or actor leakage, tampered file/store mismatch, expired handles,
  forbidden recovery, stale generations, empty or colliding actors, and revoked
  handle reuse.
- The store busy-timeout assertion was stabilized at a four-second lower bound
  after modernc SQLite returned `SQLITE_BUSY` at 4.336s while retaining the
  configured `busy_timeout=5000`; production behavior was unchanged.

## Work Unit Evidence

### PR 1 — Contracts, IDs, envelopes

| Evidence | Result |
|---|---|
| Focused tests | `go test ./internal/ids ./internal/envelope` passed. |
| Runtime/build | `go build ./...` passed. |
| Rollback | Contract reconciliation, module files, IDs, and envelopes. |

### PR 2 — SQLite store

| Evidence | Result |
|---|---|
| Focused/runtime | `go test ./internal/store` passed with temporary DB, PRAGMAs, schema, migration, CAS, and busy-timeout coverage. |
| Rollback | `internal/store` and its event-store schema contract. |

### PR 3–5 — Workflow, plans, evidence

| Evidence | Result |
|---|---|
| Focused tests | `go test ./internal/workflow ./internal/plan ./internal/evidence -count=1` passed (`0.008s`, `0.002s`, `0.007s`). |
| Runtime harness | Temporary-DB lifecycle test passed (`0.004s`). |
| Rollback | Each of `internal/workflow`, `internal/plan`, and `internal/evidence` is autonomous with its named schema seam. |

### PR 6–7 — Claim handles

| Evidence | Result |
|---|---|
| Focused command | `GOCACHE=/tmp/pitcrew2-pr6-gocache go test ./internal/handles -count=1` passed in `0.013s`. |
| Runtime harness | Claim promotion/refresh, expiry/delete, and recovery/generation scenarios passed in a temporary directory (`0.007s`). |
| Broad validation | `go test ./...`, `go build ./...`, `go vet ./...`, and `git diff --check` exited 0. |
| Rollback | PR 6 owns issuance/filesystem behavior; PR 7 owns lifecycle/authority behavior and their minimal schema seams. |

### PR 8–9 — Static CLI and new/show/errors

| Evidence | Result |
|---|---|
| RED | `go test ./internal/maxims ./internal/cli ./cmd/pitcrew -count=1` exited 1 with undefined maxims, dispatcher, and entrypoint symbols. |
| Focused GREEN | `GOCACHE=/tmp/pitcrew2-pr8-gocache go test ./internal/maxims ./internal/cli ./cmd/pitcrew -count=1` passed in `0.002s`, `0.004s`, and `0.002s`. |
| Runtime harness | Built `/tmp/pitcrew-pr8`; in `/tmp/pitcrew-pr8-runtime.f29gyq`, `principles` matched root `MAXIMS.md` byte-for-byte (2665 bytes), JSON parsed, `workflow new/show` persisted `wf-c93a8453c6bd812ad03e5bca`, and `-goal` returned exit 2 with empty stdout and one-line stderr. |
| Broad validation | `go test ./...`, `go build ./...`, `go vet ./...`, and `git diff --check` exited 0; full tests included store `4.291s`, handles `0.014s`, workflow `0.009s`, and CLI `0.005s`. |
| Prohibited surfaces | At this boundary, production scan found no daemon/RPC/TUI/Master/installer/raw-token/v1-migration surface and no exposed debug flag; later PR 10–14 slices intentionally wired the reserved unit flows. |
| Rollback | Revert `maxims_embed.go`, `internal/maxims`, `internal/cli`, `cmd/pitcrew`, and the CLI representation/embed clarifications. Earlier domain packages remain intact. |

### PR 10–14 — Artifact, plan, and unit flows

| Evidence | Result |
|---|---|
| RED | `go test ./internal/cli -run 'Test(Input|Exact|Artifact|CAS)' -count=1` failed: old parser rejected the new actor contract, accepted missing actor, and had no remaining command dispatch. Follow-up DTO/path RED cases failed by opening the store or reaching handle validation. |
| Focused GREEN | `GOCACHE=/tmp/pitcrew2-pr10-gocache go test ./internal/cli ./internal/workflow ./internal/plan ./internal/evidence ./internal/handles ./internal/store -count=1` passed; store integration completed in `5.012s`. |
| Runtime harness | Built `/tmp/pitcrew-pr10`; `/tmp/pitcrew-pr10-e2e.svXoH7` completed new → three artifacts → plan/approve/begin → ready/claim → TDD/review/unit-complete → workflow complete. Handle was revoked, three artifacts remained ordered, final state was completed, state failure exited 3, and usage failure exited 2. |
| Security/contract | Input files use `O_NOFOLLOW`, descriptor regular-file verification, UTF-8, size, one-document, trailing-data, and unknown-field checks before store open. Debug claim commits revocation before emitting one `data.claim_secret`; no handle file or plaintext persistence remains. |
| Broad validation | Focused PR10 tests passed in `0.026s`; final `go test ./...`, `go build ./...`, `go vet ./...`, gofmt, `git diff --check`, and prohibited-surface checks exited 0. Full tests included CLI `0.032s`, store `4.963s`, handles `0.017s`, and workflow `0.012s`. |
| Rollback | Revert `internal/cli` PR10 handlers/input tests, `internal/workflow/artifact.go`, `internal/plan/service.go` and overlap/DTO seams, evidence actors/completion seam, handle CAS/revocation seams, and initial-schema artifact/actor columns. PR8/PR9 static and new/show behavior remains. |

The original PR 10 work unit exceeded the 400-line review budget: entirely new
files alone contained 773 lines before substantial CLI/domain modifications.
The planner therefore promoted its autonomous behavior splits to PR 10–14:
strict inputs/artifacts, plans/readiness, claims/recovery, TDD/review, and
completion/authority revocation.

### PR 15 — Installer and documentation

| Evidence | Result |
|---|---|
| RED | `sh scripts/tests/run.sh` failed because the installer did not exist; after installer GREEN, documentation assertions failed because root `AGENTS.md` and guides did not exist. |
| Focused GREEN | `sh scripts/tests/run.sh` exited 0 with `installer_smoke_tests=passed`: Codex, OpenCode, Claude Code, and Pi installed; reinstall was byte-identical; customized Master refusal/overwrite, ordinary-fragment refresh, rollback, exact maxims, nine fragments, prohibitions, and hand-off passed. |
| Runtime harness | `/tmp/pitcrew-pr15-runtime.Vj0voA` used an isolated `HOME`: nine files installed, reinstall checksums matched, simulated failure exited 1, and customized Master/Explorer bytes were restored. |
| POSIX/static | `sh -n` and a local bashism scan passed for installer/tests. `shellcheck`, `dash`, `posh`, and BusyBox were unavailable locally. |
| Broad validation | `go test ./...`, `go build ./...`, `go vet ./...`, gofmt, and `git diff --check` exited 0. Full tests included CLI `0.034s`, store `4.814s`, handles `0.017s`, workflow `0.012s`. |
| Rollback | Revert `scripts/`, root `AGENTS.md`, and `docs/`; no Go/runtime-state behavior is removed. |

PR 15 contains 462 authored lines and exceeds the 400-line budget. Its clean
split is PR 15a installer plus smoke/runtime tests (233 lines), followed by PR
15b root agent guide and CLI/contributor documentation (229 lines).

### PR 16 — Failed unit-command claim atomicity correction

| Evidence | Result |
|---|---|
| RED | ✅ Written — `GOCACHE=/tmp/pitcrew2-pr16-red go test ./internal/cli -run TestFailedUnitCommandsPreserveClaimAtomically -count=1` failed all three TDD/review/completion cases: handle bytes changed and expiry advanced from `15:05` to `15:06`. |
| Focused GREEN | ✅ Passed — `GOCACHE=/tmp/pitcrew2-pr16-final go test ./internal/handles ./internal/cli -run 'Refresh|Atomic' -count=1` passed (`0.007s`, `0.019s`). |
| Runtime harness | Built `/tmp/pitcrew-pr16`; `/tmp/pitcrew-pr16-runtime.9dBz4D` executed a real lifecycle through TDD, then failed `unit-complete` without review at exit 3. Stdout was empty, stderr was one line, handle SHA-256 remained `9bf2cbd7ab784a2df221a4967ef63830872a1ed2fef09239e09e87fdf0d6aa02`, and the database claim remained `active\|2026-08-21T01:56:51.935Z`. |
| Broad validation | `go test ./...`, `go build ./...`, `go vet ./...`, gofmt, and `git diff --check` exited 0. Full tests included CLI `0.052s`, store `4.989s`, handles `0.018s`, and workflow `0.012s`. The known read-only module stat-cache warning remained non-blocking. |
| Rollback | Revert the transaction-aware seams in `internal/handles/handles.go` and `internal/evidence/evidence.go`, their CLI coordination in `internal/cli/cli.go`, and the PR16 regression in `internal/cli/flows_test.go`. |

### PR 17 — TDD outcome semantics correction

| Evidence | Result |
|---|---|
| RED | ✅ Written — `GOCACHE=/tmp/pitcrew2-pr17-red go test ./internal/evidence ./internal/cli -run 'Outcome|TDD' -count=1` failed five domain cases and all three CLI cases because RED-pass, GREEN-fail, validation-fail, and arbitrary prose were accepted; CLI responses incorrectly exited 0 and moved units to `reviewing`. |
| Focused GREEN | ✅ Passed — `GOCACHE=/tmp/pitcrew2-pr17-final go test ./internal/evidence ./internal/cli -run 'Outcome|TDD' -count=1` passed (`0.002s`, `0.017s`). |
| Runtime harness | Built `/tmp/pitcrew-pr17`; `/tmp/pitcrew-pr16-runtime.bheszK` executed a real lifecycle through claim, then submitted RED=`exit 0`, GREEN=`exit 1`, validation=`exit 2`. It exited 3 with empty stdout and one stderr line; handle SHA-256 remained `060c20b94ffcb82af55fd1af9e26808788b8c583edb5ecde416e819bb1fa0a5b`; database state remained `intent\|2026-08-21T02:02:57.431Z\|pending\|0` (claim state/expiry, unit state, evidence count). |
| Contract formatting | Outcomes accept trimmed `exit N` and documented `exit N: detail`; RED requires nonzero N while GREEN and validation require zero. Arbitrary prose is rejected rather than heuristically classified. |
| Broad validation | `go test ./...`, `go build ./...`, `go vet ./...`, gofmt, and `git diff --check` exited 0. Full tests included CLI `0.061s`, evidence `0.016s`, store `4.513s`, handles `0.016s`, and workflow `0.025s`. The known read-only module stat-cache warning remained non-blocking. |
| Rollback | Revert outcome semantic validation/parser in `internal/evidence/evidence.go` and PR17 regressions in `internal/evidence/evidence_test.go` and `internal/cli/flows_test.go`. |

### PR 18 — Plan approval authority correction

| Evidence | Result |
|---|---|
| RED | ✅ Written — `GOCACHE=/tmp/pitcrew2-pr18-red go test ./internal/plan ./internal/cli -run 'Approval|Ready|Claim|Exception' -count=1` failed because a submitted overlap approval returned both units as ready while the workflow was still `planning`; the same boundary also permitted claim issuance and had no durable approval marker. |
| Focused GREEN | ✅ Passed — `GOCACHE=/tmp/pitcrew2-pr18-final2 go test ./internal/plan ./internal/cli -run 'Approval|Ready|Claim|Exception' -count=1` passed (`0.003s`, `0.040s`). |
| Runtime harness | Built `/tmp/pitcrew-pr18`; `/tmp/pitcrew-pr18-runtime.XSCe0I` submitted overlapping units plus one required and one unnecessary admission-exception declaration. Preapproval readiness and claim exited 3 with no handle directory. Approval without the required flag exited 3 and left both markers `0`; explicit approval moved the workflow to `plan_approved` with markers `wu-...001:1,wu-...002:0`; two units became ready and postapproval claim created a handle. |
| Atomic authority | `Service.Approve` now loads and validates the persisted plan, records only explicit required exception approvals, transitions the workflow, appends its event, and commits all effects in one transaction. Readiness and issuance require `plan_approved` or `implementing`. |
| Broad validation | `go test ./...`, `go build ./...`, `go vet ./...`, gofmt, and `git diff --check` exited 0. Full tests included CLI `0.070s`, plan `0.013s`, store `4.913s`, handles `0.017s`, and workflow `0.013s`. The known read-only module stat-cache warning remained non-blocking. |
| Rollback | Revert `admission_exception_approved` in `internal/store/store.go`, transactional approval/readiness gating in `internal/plan/service.go`, issuance gating in `internal/handles/handles.go`, and the PR18 CLI regression in `internal/cli/flows_test.go`. |

### PR 19 — Transition and state-error correction

| Evidence | Result |
|---|---|
| RED | ✅ Written — `GOCACHE=/tmp/pitcrew2-pr19-red go test ./internal/workflow ./internal/cli -run 'Transition|StateError' -count=1` failed to compile because structured `TransitionError` did not exist; the CLI case also failed because `draft cannot complete` omitted `expected ready_to_complete`. |
| Focused GREEN | ✅ Passed — `GOCACHE=/tmp/pitcrew2-pr19-final go test ./internal/workflow ./internal/cli -run 'Transition|StateError' -count=1` passed (`0.049s`, `0.005s`). |
| Runtime matrix | `GOCACHE=/tmp/pitcrew2-pr19-matrix-gocache go test ./internal/workflow -run TestTransitionMatrixExecutesEveryLegalAndRejectsEverySourceState -count=1 -v` passed 29 temporary-SQLite subtests in `0.045s`: all 11 ordinary legal transitions, abandonment from all eight non-terminal states, and one invalid transition from every ten source states. |
| Real binary harness | Built `/tmp/pitcrew-pr19`; `/tmp/pitcrew-pr19-runtime.M66dtc` exercised invalid commands from all ten aggregate states. Every command exited 3 with empty stdout, named its exact current and expected source states, and preserved state, revision 1, and the single creation event. |
| Classification | `TransitionError.Unwrap` retains `ErrInvalidTransition`, so structured details do not weaken the existing state exit-code classification. Stage-artifact transitions use the same error constructor. |
| Broad validation | `go test ./...`, `go build ./...`, `go vet ./...`, gofmt, and `git diff --check` exited 0. Full tests included CLI `0.076s`, workflow `0.055s`, store `4.913s`, handles `0.027s`, and plan `0.030s`. The known read-only module stat-cache warning remained non-blocking. |
| Rollback | Revert the transition table/error structure in `internal/workflow/workflow.go`, shared artifact error use in `internal/workflow/artifact.go`, and PR19 matrices in `internal/workflow/workflow_test.go` and `internal/cli/cli_test.go`. |

### PR 20 — Strict evidence audit and final correction validation

| Evidence | Result |
|---|---|
| RED audit | ✅ Written — the independent `verify-report.md` recorded that all 11 historical rows lacked exact `✅ Written` and `✅ Passed` markers. Every cited path was present, and the existing row text supplied the historical RED observation; no missing history was invented. |
| GREEN audit | ✅ Passed — all original focused packages and `scripts/tests/run.sh` passed current execution. Each historical row now names its recorded RED and current audited GREEN command/result. A table audit reports `tdd_rows=16 bad_rows=0`. |
| Correction-focused suite | Claim atomicity: handles `0.006s`, CLI `0.019s`; TDD outcomes: evidence `0.003s`, CLI `0.017s`; plan authority: plan `0.002s`, CLI `0.040s`; transitions: workflow `0.048s`, CLI `0.005s`. |
| Full test/shell suite | `GOCACHE=/tmp/pitcrew2-pr20-final go test ./... -count=1` passed 52 top-level tests plus cases; `sh scripts/tests/run.sh` returned `installer_smoke_tests=passed`. Full timings included CLI `0.074s`, store `4.413s`, workflow `0.062s`, and handles `0.017s`. |
| Build/static | `go build ./...`, `go vet ./...`, gofmt-diff, and `git diff --check` exited 0. The known read-only Go module stat-cache warning remained non-blocking. |
| Audit warning | The first broad audit observed the timing-sensitive busy-timeout assertion at `3.956s` versus its `4s` test lower bound. No production/test code was changed: three immediate focused retries passed (`4.707s`, `4.806s`, `5.007s`), the six-package retry passed (`4.512s`), and the final full suite passed (`4.413s`). Independent verification should retain this visibility. |
| Rollback | Revert only PR20 evidence-marker and audit updates in `apply-progress.md` and task checkboxes; no implementation behavior belongs to this slice. |

## Remaining Tasks

None. All tasks are marked complete; the corrected change is ready for independent `sdd-verify`.
