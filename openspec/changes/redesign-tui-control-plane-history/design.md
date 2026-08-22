# Design: TUI Control-Plane History Redesign

## Technical Approach

Add a canonical compile-time version, an additive database migration, and a transaction-scoped activity writer. Project migrated and legacy databases through typed history models. The same-process, project-local, read-only TUI probes legacy schema instead of migrating. Existing Bubble Tea/Lip Gloss render Operate mode without another visual dependency.

## Architecture Decisions

| Decision | Choice and rationale | Rejected alternative |
|---|---|---|
| Canonical version | New `internal/version/version.go` exports only `const Current = "0.2.0"`. `internal/cli` reads it directly for `--version`; `tui.Model.Version()` returns it and the header renders that method. Removing `cmd/pitcrew`'s `dev` variable and `Dependencies.Version` makes divergence impossible in production. A test-only SemVer 2.0.0 regex validates every release value without a dependency. | A `VERSION` file needs generation/embed plumbing; `-ldflags -X` is ad hoc and can produce mismatched builds. |
| Workflow name | Migration 2 adds nullable `workflows.name`; creation requires a trimmed, non-empty name of at most 80 runes. Legacy reads derive the first non-empty goal line, collapse whitespace, bound it, fall back to `Untitled workflow`, and expose `NameDerived=true` without writing. | Backfill invents history. |
| Compatible migration | Permit only parsed `ALTER TABLE workflows ADD COLUMN name TEXT`; all other ALTER remains rejected. Migration 2 also creates `activities` and indexes. Writable commands migrate before creation. | Rebuilding risks data loss. |
| Activity boundary | New `internal/activity` owns enums, labels, safe subjects, and `AppendTx`. Each domain service appends before its transaction commits; any failure rolls back both. | CLI-side logging can diverge. |
| Safe links | Subjects use only public record IDs: workflow, event revision, artifact rowid, plan, work-unit ID, or evidence/review unit+revision. Claim IDs, hashes, paths, and handle contents are forbidden. | Generic strings risk leaks and dead links. |
| Legacy projection | `history.Service` probes columns/tables. Activities are authoritative; earlier events/artifacts/evidence/reviews with real actor/time become marked fallback timeline rows. Untimed data remains only in records. | Required migration breaks old read-only stores. |

## Activity Taxonomy

| Actions | UI labels / subjects |
|---|---|
| `workflow_created` | Created workflow / workflow |
| `exploration_recorded`, `specification_recorded`, `design_recorded` | Recorded exploration/specification/design / artifact |
| `plan_submitted`, `plan_approved` | Submitted/Approved plan / plan |
| `implementation_started`, `workflow_completed`, `workflow_abandoned` | Started/Completed/Abandoned workflow / event |
| `unit_claimed`, `unit_claim_recovered`, `unit_completed` | Claimed/Recovered/Completed work unit / work unit |
| `unit_tdd_recorded`, `unit_review_recorded` | Recorded TDD/review / evidence/review |

Reads, failures, and rollbacks append nothing. Automatic `ready_to_complete` is part of `unit_completed`, not another activity.

## Data and UI Flow

```text
CLI mutation -> service transaction -> domain rows + activity -> commit
OpenReadOnly -> schema probe -> typed grid/detail/timeline -> model/view
```

Wide mode gives `PitCrew2` highest visual weight, `Control Plane` a smaller subtitle, and `v` plus `Model.Version()` a distinct accent. Narrow mode reflows all three onto a compact line. Minimum-size mode keeps that identity/version line above guidance, so version remains visible without relying on color.

History is a marked `created_at DESC, id ASC` grid: `Started | Work | Status`; narrow rows stack those labels. Detail shows name/fallback, ID/revision, times, state, goal, then chronological actor/action rows and exact result drill-down. Wide mode uses timeline/result panes; narrow mode pushes the result screen. Arrow/Vim keys remain equivalent outside input. Resize preserves workflow, subject, and valid evidence position.

## File Changes

| Files | Change |
|---|---|
| `internal/version/*`, `cmd/pitcrew/*`, `internal/cli/*` | Canonical SemVer and shared CLI output; remove injected/ad-hoc version. |
| `internal/store/*`, `internal/activity/*` | Migration 2 and typed ledger. |
| `internal/workflow/*`, `internal/plan/*`, `internal/evidence/*`, `internal/handles/*` | Required name and transactional activities. |
| `internal/history/*` | Schema-aware grid, records, timeline, resolution. |
| `internal/tui/model.go`, `view.go`, `styles.go`, tests/goldens | Shared version access and responsive Operate UI. |
| `AGENTS.md`, `openspec/AGENTS.md`, `docs/*`, build/release scripts | Name/history/version contracts; ordinary builds without version ldflags. |

## Testing Strategy

RED tests cover exact `0.2.0`, SemVer validity, identical CLI/model/header values, and wide/narrow/minimum headers. Also cover migration preservation, fallback honesty, every activity, zero rows for reads/failures, exact links, ordering, Arrow/Vim parity, resize focus, and read-only invariance. Inject activity/commit failures per transaction family; assert database rollback and handle-file restoration.

## Threat Matrix

| Boundary | Applicability and RED proof |
|---|---|
| CLI input routing | Missing/blank/over-limit name fails before mutation; valid input creates once. |
| Handle filesystem + DB transaction | Injected ledger/commit failure leaves no domain/activity divergence or orphaned file. |
| Documentation paths, Git selection, commit/push/PR | N/A: no classifier, shell, VCS, or delivery execution changes. |

## Migration / Release

Roll forward additively; rollback leaves nullable name and activities unused. Existing installed binaries and databases keep working unchanged. Newly built/installed binaries embed `0.2.0` through normal `go build`; future releases make one reviewed `Current` edit to a valid SemVer 2.0.0 value, with no linker override.

## Open Questions

None.
