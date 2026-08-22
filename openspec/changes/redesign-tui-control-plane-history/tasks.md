# Tasks: Redesign TUI Control-Plane History

## Review Workload Forecast

| Field | Value |
|---|---|
| Estimated lines | 1,180–1,480; 260–400 per PR |
| Split | PR1 → PR2 → PR3 → PR4 → tracker → main |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: feature-branch-chain
400-line budget risk: High

First create the sole aggregate PR `feat/tui-history-redesign` → `main` (`48a1eaa`). Branch PR 1 from the tracker; each successor starts from and targets its predecessor. Merge PR 1 into the tracker; retarget/rebase each successor onto the updated tracker, require a unit-only diff, and merge. Merge the tracker last.

### Suggested Work Units

| Unit | Goal / estimate | Focused test | Runtime harness | Rollback boundary |
|---|---|---|---|---|
| 1 | Version/names/migration/CLI, 280–360 | `go test ./internal/version ./internal/store ./internal/workflow ./internal/cli ./cmd/pitcrew -run 'Version|Name|Migration'` | Plain build; verify `0.2.0`, names, and v1 DB | Revert version/name/CLI; schema unused |
| 2 | Atomic activities, 340–400 | `go test ./internal/activity ./internal/workflow ./internal/plan ./internal/evidence ./internal/handles -run 'Activity|Rollback'` | Run mutations, reads, failures; query ledger | Revert writers; leave ledger schema |
| 3 | Typed history, 260–340 | `go test ./internal/history -run 'Name|Timeline|Legacy|Order|Resolve'` | Read v1/v2; resolve | Revert projections |
| 4 | TUI/drill-down/docs, 300–400 | `go test ./internal/tui ./internal/cli ./cmd/pitcrew -run 'Header|Version|Grid|Timeline|Resize|TUI'` | PTY wide↔minimum, Arrow/Vim, drill-down, `q`; snapshot DB | Revert TUI/docs |

## Phase 1: Version, Names, and Migration

- [x] 1.1 **RED:** Validate canonical `0.2.0` with a dependency-free SemVer 2.0.0 regex and identical CLI/model values; prove v1→v2 preservation, allowed name ALTER, and valid/invalid `--name` behavior.
- [x] 1.2 **GREEN:** Add `internal/version.Current`, remove `Dependencies.Version`, `cmd/pitcrew`'s ad-hoc value, and version ldflags; add migration 2, ≤80-rune names, marked fallback, and CLI wiring.
- [x] 1.3 **REFACTOR:** Centralize validation; run focused/runtime tests and plain `go build` rollback compatibility.

## Phase 2: Transactional Activities

- [ ] 2.1 **RED:** Test every action/safe subject; prove reads/failures add zero and injected ledger/commit failures roll back every transaction family, remove new handles, and restore prior bytes.
- [ ] 2.2 **GREEN:** Implement typed `internal/activity.AppendTx`; wire workflow, plan, evidence, and handles so each successful mutation atomically commits exactly one linked activity.
- [ ] 2.3 **REFACTOR:** Deduplicate construction; run matrices and exclude claim material or second activities.

## Phase 3: Typed History

- [ ] 3.1 **RED:** Prove grid/timeline order, schema probing, honest legacy rows, complete records, and exact resolution despite collisions.
- [ ] 3.2 **GREEN:** Add name metadata, chronological typed timeline, actor/time-only legacy rows, and migration-free v1/v2 reads to `internal/history/service.go`.
- [ ] 3.3 **REFACTOR:** Consolidate scanners; run focused tests and dual-fixture read-only harness.

## Phase 4: TUI and Documentation

- [ ] 4.1 **RED:** Add model/view/goldens for `PitCrew2` / `Control Plane` with canonical version always visible in accent color, including minimum layout; cover grid, drill-down, Arrow/Vim, focus, resize, and immutable snapshots.
- [ ] 4.2 **GREEN:** Render `Model.Version()` in responsive headers and implement grid/detail/result screens in `internal/tui/{model,view,styles}.go`; update docs/agent contracts without dependencies or mutation.
- [ ] 4.3 **REFACTOR:** Run focused tests, `go test ./...`, `go vet ./...`, `go build ./...`, and PTY harness; exclude deterministic goldens only from authored-line counts.
