## Exploration: TUI Control-Plane History Redesign

### Current State
The header is `PITCREW WORKFLOW FLIGHT RECORDER`, not the requested `PitCrew2` / `Control Plane` identity.

Workflows already persist `created_at`, `updated_at`, `goal`, and `state`. History currently orders them by `updated_at DESC, id ASC` and renders a status rail with state, workflow ID, revision, and goal rather than a newest-by-start datagrid. A distinct short workflow name is not persisted.

Detail queries already load created/updated timestamps but the view omits them. The evidence canvas chronologically unions lifecycle events, artifacts, plans, work units, TDD evidence, and reviews. Full specification content, including Gherkin, is retained and navigable. Events contain actor and timestamp, but history flattens actor into content rather than exposing typed timeline fields.

Stage events can be linked to artifacts by workflow plus matching `revision_after` / `accepted_revision`. Other interactions are incomplete: plans and work units lack their own audit timestamps; TDD and reviews are records but not lifecycle events; claims, recoveries, and non-final unit completion lack a complete navigable audit entry. Read commands correctly create nothing.

The confirmed product decision is to persist an explicit short workflow name at creation. Historical rows require a deterministic goal-derived fallback only; new rows must not depend on inference.

### Affected Areas
- `internal/store/store.go` — migration for workflow short name and append-only activities.
- `internal/workflow`, `internal/plan`, `internal/evidence`, `internal/handles` — transactional activity insertion beside successful mutations.
- `internal/history/service.go` — typed grid/detail/timeline projections, ordering, actor, and stable subject links.
- `internal/tui/model.go`, `internal/tui/view.go` — branded header, datagrid, metadata detail, and linked timeline navigation.
- `internal/cli/cli.go` — creation input for the explicit short name; reads remain non-mutating.

### Approaches
1. **Synthesize history from current tables** — infer timeline entries and derive names from goals.
   - Pros: No migration and minimal write-path change.
   - Cons: Cannot recover missing interactions/timestamps, cannot guarantee links, and conflicts with the confirmed explicit-name requirement.
   - Effort: Medium

2. **Append-only activity ledger with explicit workflow name** — add a forward-only ledger keyed to workflow, optional unit, action, actor, timestamp, and stable subject kind/ID; add persisted short name with historical fallback.
   - Pros: Complete navigable history, atomic artifact/result links, typed timeline data, and preserved lifecycle-event semantics.
   - Cons: Requires a migration and coordinated domain-service changes.
   - Effort: High

### Recommendation
Use approach 2. Keep `events` as workflow state transitions and add an append-only project-local activity ledger for every successful mutating control-plane interaction. Insert each activity in the same transaction as workflow creation/transitions, artifacts, plan submission/approval, claims/recoveries, TDD, reviews, unit completion, and abandonment. Link it through stable subject kind/ID values to the artifact or result that the timeline opens.

Persist the short workflow name during `workflow new`; use a deterministic bounded first-line projection of `goal` only when reading historical rows without a name. Order the grid by `created_at DESC, id ASC`. Project detail metadata explicitly and expose timeline `Actor`, `At`, and `LinkedRecordID` fields rather than concatenating them into content.

Keep `tui`, `workflow show`, and `workflow list-ready-units` unrecorded. This preserves the TUI's read-only, migration-free runtime and project-local database boundary.

### Risks
- Activity insertion outside the owning transaction would allow timeline/domain divergence.
- Historical activity reconstruction is necessarily partial because several existing rows have no interaction timestamp.
- Stable subject links must never expose handle secrets or unsafe filesystem paths.
- Adding the required creation input needs a deliberate CLI compatibility decision for existing callers.

### Ready for Proposal
Yes. The proposal should define the explicit short-name creation contract and historical fallback, forward-only transactional activity coverage, stable navigable links, newest-by-start ordering, branded grid/detail presentation, and the invariant that reads remain unrecorded and the TUI remains read-only.
