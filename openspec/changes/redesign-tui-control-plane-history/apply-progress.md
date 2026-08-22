# Apply Progress: Redesign TUI Control-Plane History

## Completed Tasks

- [x] 1.1 Canonical SemVer, migration, and workflow-name RED coverage.
- [x] 1.2 Canonical version source, additive schema, bounded names, fallback, and CLI wiring.
- [x] 1.3 Centralized validation and completed focused/runtime verification.

## TDD Cycle Evidence

| Task | Test file | Layer | Safety net | RED | GREEN | TRIANGULATE | REFACTOR |
|---|---|---|---|---|---|---|---|
| 1.1 | `internal/version/version_test.go`, `internal/store/store_test.go`, `internal/workflow/workflow_test.go`, `internal/cli/cli_test.go`, `cmd/pitcrew/main_test.go` | Unit + integration | Four existing packages passed; version package was new | Exit 1: version/name/schema contracts missing | Five focused packages passed | Valid/invalid SemVer, persisted/fallback names, exact/unsafe ALTER, valid/missing/blank/over-limit CLI input | Shared regex and exact ALTER parser |
| 1.2 | Same focused files | Unit + integration | Covered by 1.1 | Covered by 1.1 | Full focused suite passed | V1 row preserved through v2; new workflow returns persisted name | `NormalizeName` and `DisplayName` centralize policy |
| 1.3 | Same focused files | Integration + runtime | Full suite passed before final validation | Exact-ALTER suffix case failed before parser tightening | Full suite, vet, diff check, and runtime harness passed | Plain build reports `0.2.0`; valid name persists; invalid name creates no store | No further production changes required |

## Work Unit Evidence

| Evidence | Result |
|---|---|
| Focused test | `go test ./internal/version ./internal/store ./internal/workflow ./internal/cli ./cmd/pitcrew -run 'Version\|Name\|Migration' -count=1` → exit 0, five packages passed |
| Runtime harness | Plain `go build`; `pitcrew --version` returned `pitcrew 0.2.0`; named creation returned exact non-derived name; blank name exited 2 without creating `.pitcrew`; v1→v2 integration test passed |
| Rollback boundary | Revert version/name/CLI code and tests; migration 2 remains additive and can be left unused without rewriting prior rows |

## Remaining Tasks

- [ ] Phase 2 transactional activities.
- [ ] Phase 3 typed history.
- [ ] Phase 4 TUI and documentation.

## Deviations

The TUI model/header consumption of `internal/version.Current` remains intentionally assigned to Phase 4; Unit 1 establishes the canonical source and CLI identity only.
