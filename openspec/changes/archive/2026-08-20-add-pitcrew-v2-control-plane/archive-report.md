# Archive Report: add-pitcrew-v2-control-plane

## Final State

- Schema: spec-driven
- Artifact mode: OpenSpec
- Archive date: 2026-08-20
- Tasks: 30/30 complete
- Apply: all done
- Verification: PASS WITH WARNINGS
- Requirements: 41/41 compliant
- Scenarios: 41/41 compliant
- Blockers: 0
- Critical findings: 0
- Canonical verification report SHA-256: `21e062072d04ee0cad3d0ecfd9c50f5301cdd107e93e5971b2f65a1c5d6ded7a`
- Review gate: structurally absent; archived under ordinary repository policy because receipt-driven mode was never enabled

## Specification Sync

The seven ADDED-only delta specifications were synchronized into previously
missing main specification paths. The synchronization preserved every byte
except for the required OpenSpec header normalization from
`## ADDED Requirements` to `## Requirements`:

| Domain | Action | Requirements |
|---|---|---:|
| claim-handles | Created | 7 |
| cli-surface | Created | 4 |
| event-store | Created | 5 |
| plan-and-work-units | Created | 5 |
| runtime-install | Created | 9 |
| tdd-and-review | Created | 6 |
| workflow-lifecycle | Created | 5 |

Every normalized temporary copy matched its installed main specification
byte-for-byte before the destination was accepted. The archived delta
specifications remain unchanged.

## Archive Move

The complete active change tree was copied to a temporary recursive snapshot, moved mechanically to `openspec/changes/archive/2026-08-20-add-pitcrew-v2-control-plane`, and compared with `diff -r`. The readback completed successfully with empty output. This report was added after that comparison and is therefore intentionally excluded from the pre-move snapshot comparison.

## Archive Contents

- `proposal.md`
- `design.md`
- `tasks.md`
- `apply-progress.md`
- `verify-report.md`
- `specs/claim-handles/spec.md`
- `specs/cli-surface/spec.md`
- `specs/event-store/spec.md`
- `specs/plan-and-work-units/spec.md`
- `specs/runtime-install/spec.md`
- `specs/tdd-and-review/spec.md`
- `specs/workflow-lifecycle/spec.md`

## Non-blocking Warnings

The canonical verification report records these non-blocking limitations:

1. Seven changed production Go files are below 80% statement coverage; no coverage threshold is configured.
2. The first coverage-instrumented full run hit a timing-sensitive busy-timeout assertion; three focused retries and the coverage retry passed without code changes.
3. `go build` could not write the read-only user module stat cache despite exiting successfully.
4. `shellcheck`, dash, posh, and BusyBox were unavailable for additional portability checks.
5. Three contract/bootstrap tasks use artifact or structural evidence rather than dedicated executable tests.

The repository has no `openspec/config.yaml`, so there were no project-specific `rules.archive` directives to apply.

## Mechanical Readback

All seven normalized temporary-copy versus main-specification readbacks
produced empty `diff` output. The pre-move snapshot versus archived tree
readback also produced empty `diff -r` output.

## Outcome

The change is fully planned, implemented, independently verified, synchronized into the main specifications, and archived. No incomplete tasks, blockers, critical findings, or review receipts remain to reconcile.
