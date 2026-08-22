# Proposal: Redesign TUI Control-Plane History

## Intent

Make the read-only TUI a clear, versioned project-local control-plane history: named workflows by start time and an actor-attributed timeline linking each successful mutation to its result.

## Scope

### In Scope
- Establish canonical version `0.2.0`; future releases follow SemVer 2.0.0, with CLI and TUI sharing one source.
- Brand the responsive TUI header with large `PitCrew2`, small `Control Plane`, and an always-visible current version in a distinct color.
- Show a `created_at`-descending grid with start time, persisted name, and state; require names for new workflows and derive bounded fallbacks only for unnamed history.
- Show metadata and a chronological actor/timestamp/action timeline linked to exact results.
- Append one activity for every successful mutation within its domain transaction.
- Exclude reads and preserve read-only, project-local TUI behavior.

### Out of Scope
- Recording reads.
- Cross-project history, remote transport, TUI mutations, analytics, or invented historical interactions.
- Exposing claim or handle secrets.

## Capabilities

### New Capabilities
None.

### Modified Capabilities
- `cli-surface`: Require a short-name input and define canonical SemVer identity shared by CLI and TUI.
- `workflow-lifecycle`: Persist workflow names; historical fallback occurs only when read.
- `event-store`: Add forward-only workflow-name and append-only transactional activity persistence.
- `tui-inspection`: Add responsive versioned branding, datagrid, metadata, and linked chronological timeline.

## Approach

Use one canonical SemVer source beginning at `0.2.0`. Add nullable historical names and activities keyed by workflow, optional unit, action, actor, time, and safe subject identity. Commit each mutation and activity together; failures write neither. Keep transition events unchanged and derive fallback names only in read projections.

## Affected Areas

| Area | Impact | Description |
|---|---|---|
| Store and domain services | Modified | Names and atomic activities. |
| CLI and version source | Modified | Required name and shared SemVer. |
| History and TUI | Modified | Branding, grid, detail, timeline. |

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Activity divergence | Medium | Same-transaction writes. |
| Misleading legacy data | Medium | Mark fallbacks; invent nothing. |
| Sensitive links | Low | Typed safe identifiers only. |

## Rollback Plan

Revert writers, projections, CLI input, and presentation. Leave additive schema unused; never rewrite existing databases.

## Success Criteria

- [ ] New workflows reject missing names; historical rows receive deterministic fallbacks.
- [ ] CLI and TUI report the same canonical version, with `0.2.0` as baseline.
- [ ] The newest-by-start grid and detail expose metadata and complete chronological activity.
- [ ] Every successful mutation has one atomic navigable activity; failures and reads have none.
- [ ] TUI inspection remains read-only and project-local.
