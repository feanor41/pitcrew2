# Apply Progress: Redesign TUI Control-Plane History

## Completed Tasks

- [x] 1.1 Canonical SemVer, migration, and workflow-name RED coverage.
- [x] 1.2 Canonical version source, additive schema, bounded names, fallback, and CLI wiring.
- [x] 1.3 Centralized validation and completed focused/runtime verification.
- [x] 2.1 Typed actions, safe subjects, zero-write reads/failures, and rollback RED coverage.
- [x] 2.2 Transactional activity wiring across workflow, plan, evidence, and handles.
- [x] 2.3 Shared constructors, exact-link matrix, full validation, and secret exclusion.
- [x] 3.1 Grid/timeline ordering, legacy schema, complete record, and collision-safe resolution RED coverage.
- [x] 3.2 Typed name metadata, authoritative activity timeline, honest legacy rows, and migration-free v1/v2 projections.
- [x] 3.3 Consolidated workflow scanners and completed focused, full-suite, and dual-fixture verification.

## TDD Cycle Evidence

| Task | Test file | Layer | Safety net | RED | GREEN | TRIANGULATE | REFACTOR |
|---|---|---|---|---|---|---|---|
| 1.1 | `internal/version/version_test.go`, `internal/store/store_test.go`, `internal/workflow/workflow_test.go`, `internal/cli/cli_test.go`, `cmd/pitcrew/main_test.go` | Unit + integration | Four existing packages passed; version package was new | Exit 1: version/name/schema contracts missing | Five focused packages passed | Valid/invalid SemVer, persisted/fallback names, exact/unsafe ALTER, valid/missing/blank/over-limit CLI input | Shared regex and exact ALTER parser |
| 1.2 | Same focused files | Unit + integration | Covered by 1.1 | Covered by 1.1 | Full focused suite passed | V1 row preserved through v2; new workflow returns persisted name | `NormalizeName` and `DisplayName` centralize policy |
| 1.3 | Same focused files | Integration + runtime | Full suite passed before final validation | Exact-ALTER suffix case failed before parser tightening | Full suite, vet, diff check, and runtime harness passed | Plain build reports `0.2.0`; valid name persists; invalid name creates no store | No further production changes required |
| 2.1 | `internal/activity/activity_test.go`, `internal/cli/flows_test.go`, `internal/handles/handles_test.go` | Unit + integration | Five affected packages passed | Missing package/actions and absent rollback made focused tests fail | Typed package and CLI mutation matrix passed | Safe/unsafe subjects, success/read/failure, ledger/commit rollback, handle removal/restoration | Failure trigger helpers centralized |
| 2.2 | Same focused files | Integration | Covered by 2.1 | Full lifecycle initially returned zero activities | Every action committed exactly once with exact public subject | Artifact IDs, event revisions, plan/work-unit/evidence/review identities and recovery | One transaction-scoped `AppendTx` seam |
| 2.3 | Same focused files | Integration + runtime | Full affected suite passed | N/A: approval coverage protected behavior | Focused, affected, full, vet, and diff checks passed | Debug/recovery paths and automatic ready transition emit no secret or duplicate activity | Shared typed subject constructors and timestamps |
| 3.1 | `internal/history/service_test.go` | Integration | Existing history package passed | Exit 1: typed names, activity timeline, and exact activity resolution were undefined; revision 2 exposed same-time legacy suppression | New v1/v2 projections passed | Persisted/derived names, every durable subject kind, collisions, chronological order, marked legacy rows, and distinct same-actor/time records | Covered by 3.3 |
| 3.2 | `internal/history/service.go` | Integration | Covered by 3.1 | Covered by 3.1 | Full history package passed | Authoritative activities plus only actor/time-supported legacy records; v1 without name/activity schema | Schema-aware typed projection only; no migration |
| 3.3 | Same focused files | Integration + runtime | Full suite passed | N/A: behavior protected by 3.1 | Focused, full, vet, build, diff, and compiled harness passed | Compiled harness opened real v1/v2 SQLite fixtures read-only and resolved all durable kinds | Shared workflow scanner and schema probes |

## Work Unit Evidence

| Evidence | Result |
|---|---|
| Focused test | `go test ./internal/version ./internal/store ./internal/workflow ./internal/cli ./cmd/pitcrew -run 'Version\|Name\|Migration' -count=1` → exit 0, five packages passed |
| Runtime harness | Plain `go build`; `pitcrew --version` returned `pitcrew 0.2.0`; named creation returned exact non-derived name; blank name exited 2 without creating `.pitcrew`; v1→v2 integration test passed |
| Rollback boundary | Revert version/name/CLI code and tests; migration 2 remains additive and can be left unused without rewriting prior rows |
| Unit 2 focused test | `go test ./internal/activity ./internal/workflow ./internal/plan ./internal/evidence ./internal/handles -run 'Activity\|Rollback' -count=1` → exit 0 |
| Unit 2 runtime harness | CLI full lifecycle emitted the exact 12-action sequence; reads/failures emitted zero; ledger/commit failures rolled back DB and handle bytes |
| Unit 2 rollback boundary | Revert activity writers/package/tests; leave additive `activities` table unused |
| Unit 3 focused test | `go test ./internal/history -run 'Name\|Timeline\|Legacy\|Order\|Resolve' -count=1` → exit 0 |
| Unit 3 runtime harness | Compiled `internal/history` test binary opened v1/v2 SQLite fixtures through `OpenReadOnly`; names, chronological timeline, exact resolution, and unchanged legacy schema passed |
| Unit 3 rollback boundary | Revert `internal/history/service.go` and `service_test.go`; schema and mutation writers remain untouched |
| Unit 3 correction | Revision 2 RED proved `artifact:1` activity suppressed distinct legacy `artifact:2` sharing actor/time; exact typed RecordID deduplication fixed it; focused/full/vet/build passed at 399 changed lines |

## Remaining Tasks

- [ ] Phase 4 TUI and documentation.

## Deviations

The TUI model/header consumption of `internal/version.Current` remains intentionally assigned to Phase 4; Unit 1 establishes the canonical source and CLI identity only.
