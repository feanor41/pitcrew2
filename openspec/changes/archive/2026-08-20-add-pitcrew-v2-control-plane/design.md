# Design: add-pitcrew-v2-control-plane

## Technical Approach

Keep the local Go/SQLite architecture and closed 16-command surface. CLI owns flags/file decoding; domain services own semantics; mutations commit atomically before envelope output. Unchanged baseline remains binding: project-local WAL store and PRAGMAs, `crypto/rand` identities, opaque `0600` handles in `0700` directories, embedded canonical `MAXIMS.md`, external POSIX installer, eight advisory roles, and no daemon, network, TUI, `internal/master`, raw production token, or v1 migration.

## Architecture Decisions

| Decision | Choice | Alternative / rationale |
|---|---|---|
| Large inputs | Required `--input-file` JSON for artifacts, plans, TDD, and reviews | Inline JSON/stdin make quoting fragile. |
| Actor | Required non-empty `--actor`, persisted as declarative metadata | It is collision/audit context, never authentication, authorization, or command routing. |
| Artifacts | Append-only rows; `show` orders by revision then row id | Workflow columns would overwrite history. |
| CAS | Workflow commands compare workflow revision; unit commands compare unit revision | Avoid unrelated unit contention; final completion guards the current workflow row in its transaction. |
| Debug claim | Revoke and commit before returning the secret; then emit `data.claim_secret` once | Output-before-commit could expose authority; output failure safely loses it. |

## Data Flow

```text
argv -> exact command FlagSet -> open/validate input file -> strict DTO decode
     -> domain validation -> SQLite transaction (CAS + rows + event) -> envelope

show -> workflow row + artifacts ORDER BY accepted_revision,id -> data
debug claim -> create handle -> revoke/delete -> COMMIT -> data.claim_secret
```

`--input-file` is opened read-only with no symlink following; both path metadata and opened descriptor must be regular. Reject directory, FIFO/device, symlink, unreadable file, invalid UTF-8 content, malformed/trailing JSON, multiple documents, and unknown fields as usage (`2`) before opening the store. Domain-invalid but well-formed bodies return state (`3`).

## Interfaces / Contracts

```go
type StageArtifactInput struct { Content string `json:"content"` }
type PlanInput struct {
    Summary string `json:"summary"`; Scope string `json:"scope"`
    MaxParallelUnits int `json:"max_parallel_units"`
    WorkUnits []plan.WorkUnit `json:"work_units"`
    OverlapApprovals []plan.OverlapApproval `json:"overlap_approvals,omitempty"`
}
type TDDInput = evidence.TDDRecord
type ReviewInput struct {
    Verdict evidence.Verdict `json:"verdict"`; Summary string `json:"summary"`
    Findings string `json:"findings"`; PlanImpact *evidence.PlanImpact `json:"plan_impact,omitempty"`
}
type Artifact struct {
    Kind, Content, Actor string; Revision int64; RecordedAt string
}
```

Command parsers enforce the spec matrix exactly: `new` uses goal/actor; `show` and readiness use workflow id; workflow mutations add workflow revision/actor and command-specific file, reason, or repeatable exception; claim/recovery use workflow id, unit id, unit revision, actor, handle directory; unit operations use those identities plus handle path and, for TDD/review, input file. Unknown, duplicate, short, or missing flags are usage errors.

`artifacts(id INTEGER PRIMARY KEY AUTOINCREMENT, workflow_id, kind, content, actor, accepted_revision, recorded_at)` enters the unreleased initial migration. Evidence/review gain actor columns there. Artifact insert at the resulting workflow revision, transition, event, and CAS share one transaction.

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/cli/cli.go`, `internal/cli/input.go` | Modify/Create | Dispatch, flags, DTO decoding, envelopes. |
| `internal/store/store.go` | Modify | Schema and busy classification. |
| `internal/workflow/artifact.go` | Create | Append/list repository and stage transaction. |
| `internal/workflow/workflow.go` | Modify | Artifact-aware inspection/transitions. |
| `internal/plan/plan.go` | Modify | Exact plan and overlap approvals. |
| `internal/evidence/evidence.go` | Modify | Actors, unit CAS, final aggregate update. |
| `internal/handles/handles.go` | Modify | Unit CAS and one-shot revocation. |
| Matching `*_test.go` files | Modify/Create | RED, integration, and golden tests. |

## Error Classification

`1`: unexpected I/O/SQLite/encoding; `2`: flags or file/JSON shape; `3`: domain/state/not-found/busy timeout/actor collision; `4`: relevant workflow or unit CAS; `5`: handle path, ownership, mode, expiry, mismatch, or revocation. Failures are one stderr line with empty stdout and no mutation.

## Testing Strategy

| Layer | Coverage |
|---|---|
| Unit | Flag matrices, DTO exactness, payload semantics, artifact ordering, actor collision, both CAS scopes. |
| SQLite integration | Atomic artifact/event transition, append-only history, concurrent CAS, final-unit aggregate update, committed debug revocation. |
| CLI/golden | All 16 happy/failure envelopes; file adversarial cases; secret occurs once and returned handle fails with `5`. |

Run `go test ./...` with stdlib tests and `modernc.org/sqlite`.

## Threat Matrix

| Boundary | Applicability |
|---|---|
| Documentation-like paths | N/A — inputs are data-only JSON and are never classified or executed. |
| Git repository selection | N/A — no Git invocation or repository selector. |
| Commit state | N/A — no VCS operation. |
| Push state | N/A — no VCS operation. |
| PR commands | N/A — no PR or subprocess composition. |

## Migration / Rollout

v2 remains unreleased and has no migration from v1; amend migration 1, then implement through the agreed stacked delivery slices. Existing development databases may be deleted and recreated.

## Open Questions

None.
