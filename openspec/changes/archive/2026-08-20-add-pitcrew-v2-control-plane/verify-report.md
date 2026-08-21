```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:e9fb5dbacb45b4f8c4f54e346d9c1e529d73208f5fc03fbfd2f1f263b68704a5
verdict: pass_with_warnings
blockers: 0
critical_findings: 0
requirements: 41/41
scenarios: 41/41
test_command: "GOCACHE=/tmp/pitcrew2-reverify-gocache go test ./... -count=1 && sh scripts/tests/run.sh"
test_exit_code: 0
test_output_hash: sha256:bb875c51a1c93bf3d1f73d07b2149d5c33909844cdbe47be517459f39995ec11
build_command: "GOCACHE=/tmp/pitcrew2-reverify-gocache go build ./... && GOCACHE=/tmp/pitcrew2-reverify-gocache go vet ./... && test -z \"$(gofmt -d $(find cmd internal -name '*.go' -type f | sort) maxims_embed.go)\" && git diff --check"
build_exit_code: 0
build_output_hash: sha256:30729227dcf732d46569c8606f73b7dea5e1c67707189ecb777d70b4e4c32b9d
```

## Verification Report

**Change**: add-pitcrew-v2-control-plane
**Version**: N/A
**Mode**: Strict TDD

### Completeness
| Metric | Value |
|---|---:|
| Tasks total | 30 |
| Tasks complete | 30 |
| Tasks incomplete | 0 |
| Requirements implemented | 41/41 |
| Runtime-compliant scenarios | 41/41 |

The seven delta specs contain exactly 41 `### Requirement:` headings and 41 `#### Scenario:` headings. `tasks.md` contains 30 checked task items and no unchecked items.

### Build & Tests Execution

**Build/static checks**: ✅ Passed

```text
GOCACHE=/tmp/pitcrew2-reverify-gocache go build ./... && GOCACHE=/tmp/pitcrew2-reverify-gocache go vet ./... && test -z "$(gofmt -d $(find cmd internal -name '*.go' -type f | sort) maxims_embed.go)" && git diff --check
exit 0
output sha256:30729227dcf732d46569c8606f73b7dea5e1c67707189ecb777d70b4e4c32b9d
```

**Full tests**: ✅ 52 Go top-level tests plus the POSIX installer smoke suite passed

```text
GOCACHE=/tmp/pitcrew2-reverify-gocache go test ./... -count=1 && sh scripts/tests/run.sh
exit 0
output sha256:bb875c51a1c93bf3d1f73d07b2149d5c33909844cdbe47be517459f39995ec11
```

**Correction-focused evidence**:

| Prior critical finding | Command result | Output hash |
|---|---|---|
| Failed-command handle atomicity | ✅ `go test ./internal/handles ./internal/cli -run 'TestFailedUnitCommandsPreserveClaimAtomically\|TestExpiredHandleIsAtomicallyDeletedWithoutUnitMutation\|TestUsePromotesRefreshesAndEnforcesActorSeparation' -count=1 -v` | `sha256:e2a57417598f00d7dc11bc8b0541f42c4c983404fba304072e2ba16e4b3400ff` |
| TDD outcome semantics | ✅ `go test ./internal/evidence ./internal/cli -run 'TestTDDOutcomeSemanticsRequireRedFailureAndSuccessfulGreenValidation\|TestFalseTDDOutcomesAreRejectedWithoutMutation' -count=1 -v` | `sha256:0f19c35ed93e5e6616589bc4360d27b2466575a6b467be57fba50f29dc0395e2` |
| Plan approval, readiness, claims, persisted exceptions | ✅ `go test ./internal/plan ./internal/cli -run 'TestApproveExceptionsRequiresExplicitMatchingApproval\|TestPlanApprovalGatesReadinessClaimsAndPersistsExplicitExceptions' -count=1 -v` | `sha256:cc0a74448540eb9ee00a6da697fc2afb6e6f47291bf010af9a62e0bc728239ba` |
| Exact state errors and full transition matrix | ✅ `go test ./internal/workflow ./internal/cli -run 'TestTransitionMatrixExecutesEveryLegalAndRejectsEverySourceState\|TestStateErrorNamesCurrentAndExpectedState' -count=1 -v` | `sha256:ebfddf7ccd17657b84a69a7d2671007043477842524e477fbbc725f531d23d19` |
| Strict RED/GREEN markers | ✅ 16 rows, 0 malformed | `sha256:130c7b49867518980547ddbce802f720410194950ba48794b65c25a843560fb0` |

**External-binary runtime probe**: ✅ Passed. A newly built `pitcrew` binary proved exact current/expected state errors, preapproval readiness and claim denial, exact persisted exception approval, immutable rejection of false TDD outcomes, and immutable claim state after failed completion. Output hash: `sha256:6009c032b659eaaa4083c4cb877002a9b264c7b4d50d6c52665416df4bd8f4d7`.

**Coverage**: A successful retry of `go test ./... -coverpkg=./... -coverprofile=/tmp/pitcrew2-reverify/cover-retry.out -count=1` produced 78.9% aggregate statement coverage; no threshold is configured. Output hash: `sha256:40549607ca606e0dfa18e339191b65788d75524900699de5aa3aa5cf33d75a47`.

### Spec Compliance Matrix
| Capability | Requirement / scenario | Runtime evidence | Result |
|---|---|---|---|
| cli-surface | Closed command and input contract / Every command enforces its row | `internal/cli/input_test.go`, full suite | ✅ COMPLIANT |
| cli-surface | Representations and exits / Success and failure representations | CLI tests and external state-error probe | ✅ COMPLIANT |
| cli-surface | Declarative actor metadata / Actor does not authorize | CLI lifecycle tests | ✅ COMPLIANT |
| cli-surface | Role and hand-off contract / Role map is advisory | CLI lifecycle and installer tests | ✅ COMPLIANT |
| workflow-lifecycle | Aggregate and states / Workflow creation | `TestWorkflowNewAndShowUseEnvelopeAndProjectLocalStore` | ✅ COMPLIANT |
| workflow-lifecycle | Transition table / Each transition is enforced | 19 legal and 10 invalid matrix cases | ✅ COMPLIANT |
| workflow-lifecycle | Stage artifact input and retention / All stage artifacts persist | CLI lifecycle test | ✅ COMPLIANT |
| workflow-lifecycle | Inspection / Show retrieves durable artifacts | CLI lifecycle test | ✅ COMPLIANT |
| workflow-lifecycle | Events, CAS, and abandonment / Abandonment is retained | Workflow and CLI CAS/abandon tests | ✅ COMPLIANT |
| plan-and-work-units | Plan input / Valid plan is preserved | Plan JSON and CLI lifecycle tests | ✅ COMPLIANT |
| plan-and-work-units | Prefixes and dependencies / Invalid graph is rejected | Plan validation tests and approval-gate probe | ✅ COMPLIANT |
| plan-and-work-units | Admission and approval / Oversized unit requires approval | Plan/CLI focused tests and persisted-marker probe | ✅ COMPLIANT |
| plan-and-work-units | Readiness and parallelism / Ready units honor capacity | Plan readiness tests | ✅ COMPLIANT |
| plan-and-work-units | Stacked delivery / Stacking remains external | Readiness tests and source inspection | ✅ COMPLIANT |
| tdd-and-review | TDD input / Complete evidence is accepted | Evidence and CLI lifecycle tests | ✅ COMPLIANT |
| tdd-and-review | Review input / Correction fields are required | CLI correction tests | ✅ COMPLIANT |
| tdd-and-review | Declarative independence check / Same actor is rejected | CLI and handle collision tests | ✅ COMPLIANT |
| tdd-and-review | Correction outcomes / Inside correction increments revision | Evidence and CLI correction tests | ✅ COMPLIANT |
| tdd-and-review | Approval and completion / Approved unit completes | Evidence and CLI lifecycle tests | ✅ COMPLIANT |
| tdd-and-review | Trivial-work exception / Trivial bypass is external | Source inspection; no triviality classifier exists | ✅ COMPLIANT |
| claim-handles | Opaque production path / Production claim returns only a path | Handle and CLI lifecycle tests | ✅ COMPLIANT |
| claim-handles | Handle document and validation / Unsafe handle is rejected | Handle security tests | ✅ COMPLIANT |
| claim-handles | Lifecycle / Expired claim is removed | Expiry and failed-command atomicity tests | ✅ COMPLIANT |
| claim-handles | Actor collision metadata / Same actor cannot review | Handle and CLI collision tests | ✅ COMPLIANT |
| claim-handles | Recovery / Recovery is blocked after evidence | Handle recovery tests | ✅ COMPLIANT |
| claim-handles | One-shot operator escape / Debug secret is one-shot | Handle and CLI debug tests | ✅ COMPLIANT |
| claim-handles | Hand-off / Hand-off contains no secret | Opaque claim and installer tests | ✅ COMPLIANT |
| event-store | Local single-process store / Projects remain isolated | Store path and CLI project-isolation tests | ✅ COMPLIANT |
| event-store | Connection policy / Busy writer times out | Store busy-timeout test passed in the full suite and three focused retries | ✅ COMPLIANT |
| event-store | Durable schema / Artifacts remain durable | Store schema and artifact lifecycle tests | ✅ COMPLIANT |
| event-store | Revision compare-and-swap / Unit CAS mismatch | Store and CLI CAS tests | ✅ COMPLIANT |
| event-store | Migrations / Destructive migration is rejected | Store migration tests | ✅ COMPLIANT |
| runtime-install | POSIX shell / Repeated installation is idempotent | Installer smoke suite | ✅ COMPLIANT |
| runtime-install | Roll back partial writes / Partial failure rolls back | Installer smoke suite | ✅ COMPLIANT |
| runtime-install | One prompt fragment per role / Every role fragment is written | Installer smoke suite | ✅ COMPLIANT |
| runtime-install | Agent-contract fragment / Prohibitions are installed | Installer smoke suite | ✅ COMPLIANT |
| runtime-install | Master fragment overwrite protection / Master overwrite is refused | Installer smoke suite | ✅ COMPLIANT |
| runtime-install | Reading MAXIMS.md / Installed maxims are exact | Installer smoke suite | ✅ COMPLIANT |
| runtime-install | Hand-off reminder / Reminder is present | Installer smoke suite | ✅ COMPLIANT |
| runtime-install | Supported runtimes / Unsupported runtime fails | Installer smoke suite | ✅ COMPLIANT |
| runtime-install | Smoke tests / Installer contracts are exercised | `sh scripts/tests/run.sh` | ✅ COMPLIANT |

**Compliance summary**: 41/41 requirements and 41/41 scenarios are compliant.

### Correctness of Prior Corrections
| Finding | Status | Evidence |
|---|---|---|
| Failed unit commands refreshed claims | ✅ Fixed | Unit-command mutation and refresh share one transaction; three failure modes preserve handle bytes and database expiry. |
| Arbitrary TDD outcomes were accepted | ✅ Fixed | Canonical `exit N[: detail]` parsing requires RED nonzero and GREEN/validation zero; invalid cases preserve state. |
| Readiness and claims worked before approval | ✅ Fixed | Both require `plan_approved` or `implementing`; external probe returned state errors before approval. |
| Approved admission exceptions were not persisted | ✅ Fixed | Approval writes only explicitly required `admission_exception_approved` markers in its transaction. |
| State errors omitted expected states | ✅ Fixed | `TransitionError` carries current and all valid expected source states while preserving exit-code classification. |
| Transition coverage was incomplete | ✅ Fixed | Runtime matrix executes 11 ordinary transitions, abandonment from eight non-terminal states, and invalid transitions from all ten aggregate states. |
| Strict markers were absent | ✅ Fixed | All 16 cumulative TDD rows contain exact `✅ Written` and `✅ Passed` markers. |

### Coherence (Design)
| Decision | Followed? | Notes |
|---|---|---|
| Required `--input-file` JSON | ✅ Yes | Strict no-follow decoding precedes store opening. |
| Declarative actor metadata | ✅ Yes | Actor provides collision/audit context only. |
| Append-only artifacts | ✅ Yes | Artifacts are ordered by accepted revision and row id. |
| Workflow versus unit CAS | ✅ Yes | Unit operations compare unit revision; aggregate finalization is atomic. |
| Commit-before-output debug claim | ✅ Yes | Debug claims are revoked and committed before the one-shot secret envelope. |
| Atomic unit-command mutation | ✅ Yes | Claim refresh/removal and evidence/review/completion commit together. |

### TDD Compliance
| Check | Result | Details |
|---|---|---|
| TDD evidence reported | ✅ | `apply-progress.md` contains 16 grouped evidence rows. |
| Task evidence coverage | ⚠️ | All 30 tasks map to evidence rows; 27 implementation/build tasks have executable checks, while three contract/bootstrap items use artifact or structural evidence rather than dedicated tests. |
| RED confirmed | ✅ | Every row contains `✅ Written`; all 14 cited test/build/shell paths exist. |
| GREEN confirmed | ✅ | Every row contains `✅ Passed`; all focused suites and the full suite pass now. |
| Triangulation adequate | ✅ | Table-driven cases cover input matrices, persistence, handles, outcomes, approval, and all transitions. |
| Safety net documented | ✅ | Every grouped row records an existing-suite baseline or `N/A (new)` boundary. |

**TDD compliance**: 5 checks passed and 1 non-blocking evidence limitation is documented.

### Test Layer Distribution
| Layer | Tests | Files | Tools |
|---|---:|---:|---|
| Unit | 13 | 5 | Go `testing` |
| Integration/runtime/security | 39 | 7 | Go `testing`, temporary SQLite/filesystems, CLI runner |
| E2E | 1 shell suite | 1 | `/bin/sh`, real filesystem/runtime targets |
| **Total** | **52 Go top-level tests + 1 shell suite** | **13** | |

### Changed File Coverage
| File | Statement % | Branch % | Uncovered lines | Rating |
|---|---:|---:|---|---|
| `cmd/pitcrew/main.go` | 20.0% | N/A | 13-18 | ⚠️ Low |
| `internal/cli/cli.go` | 90.6% | N/A | error branches throughout 50-514 | ⚠️ Acceptable |
| `internal/cli/input.go` | 90.0% | N/A | 44-46, 51-53, 62-64, 87-89, 103-115 | ⚠️ Acceptable |
| `internal/envelope/envelope.go` | 100.0% | N/A | — | ✅ Excellent |
| `internal/evidence/evidence.go` | 72.5% | N/A | error/rollback branches throughout 95-364 | ⚠️ Low |
| `internal/handles/handles.go` | 67.3% | N/A | error/rollback and filesystem branches throughout 102-480 | ⚠️ Low |
| `internal/ids/ids.go` | 92.9% | N/A | 22-24 | ⚠️ Acceptable |
| `internal/maxims/maxims.go` | 84.0% | N/A | 28-49 | ⚠️ Acceptable |
| `internal/plan/plan.go` | 88.3% | N/A | validation/error branches throughout 66-320 | ⚠️ Acceptable |
| `internal/plan/service.go` | 70.2% | N/A | error/rollback branches throughout 29-205 | ⚠️ Low |
| `internal/store/store.go` | 76.1% | N/A | 30-117 | ⚠️ Low |
| `internal/workflow/artifact.go` | 69.6% | N/A | 22-87 | ⚠️ Low |
| `internal/workflow/workflow.go` | 78.6% | N/A | 73-188 | ⚠️ Low |

**Aggregate changed Go statement coverage**: 78.9%. Seven production files are below 80%; no coverage threshold is configured.

### Assertion Quality

✅ Assertions invoke production behavior and verify observable return values, streams, persisted rows, file bytes, revisions, events, handle expiry, and state. No tautological, ghost-loop, or mock-only tests were found.

### Quality Metrics

- **Vet**: ✅ `go vet ./...` passed.
- **Formatter**: ✅ `gofmt -d` was empty.
- **Diff hygiene**: ✅ `git diff --check` passed.
- **POSIX execution**: ✅ `sh -n` coverage is included in the installer suite and `/bin/sh` smoke tests passed.
- **Unavailable tools**: `shellcheck`, dash, posh, and BusyBox were not installed, so additional shell portability checks were not run.

### Issues Found

**CRITICAL**

None.

**WARNING**

1. Seven changed production Go files are below 80% statement coverage; no threshold is configured, so this does not block the specified change.
2. The first coverage-instrumented full run hit the known timing-sensitive busy-timeout assertion at 3.929 seconds versus its 4-second test lower bound (`sha256:54f8770643eeccb82934a66749499b948e8bb3c6ba501d946ccfeabdb0ce2a53`). Three focused retries passed at 4.75-4.90 seconds and the coverage retry passed at 4.99 seconds. No production or test code was changed during verification.
3. `go build` exited 0 but could not write the user module stat cache because that path is read-only.
4. Alternate POSIX shells and `shellcheck` were unavailable.

**SUGGESTION**

- Stabilize the busy-timeout timing assertion and increase coverage of transactional rollback/error branches.

### Verdict

**PASS WITH WARNINGS**

All 41 requirements and 41 scenarios have passing runtime evidence. Every previously critical defect is independently closed; the remaining findings are non-blocking coverage, timing-test, environment-cache, and optional portability-tool warnings.
