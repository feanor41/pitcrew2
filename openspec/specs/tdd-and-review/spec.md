# Spec: tdd-and-review

## Purpose

Define test evidence, selective unit review, corrections, unit completion, and authoritative aggregate review.

## Requirements


### Requirement: TDD input

`unit-tdd --input-file` SHALL read exactly one JSON object with string fields `red_command`, `red_outcome`, `green_command`, `green_outcome`, `refactor_summary`, `validation_command`, `validation_outcome`, and `changed_paths`, plus optional structured `verification_runs` and `scenario_results`. Every string field except `refactor_summary` SHALL be non-empty; outcomes SHALL record RED failure and GREEN/validation success; changed paths SHALL be comma-separated normalized repository-relative paths. The explicit `--revision` SHALL identify the current unit revision. Accepted evidence SHALL persist with workflow/unit ids and recording time and move the unit from `pending` to `reviewing`.

#### Scenario: Complete evidence is accepted

- GIVEN a pending unit and complete TDD JSON
- WHEN unit-tdd targets its current revision
- THEN evidence SHALL persist and state SHALL become reviewing

### Requirement: Current linked scenario coverage

For a unit with declared structured coverage, current-revision TDD evidence SHALL include one successful result for every declared scenario ID. Each result SHALL reference a current verification record whose scenario set contains that ID. Missing, duplicate, failed, or unlinked results SHALL be rejected before evidence or state mutation and the error SHALL identify the missing scenario. Legacy opaque units without declared coverage SHALL retain the existing evidence contract and SHALL NOT gain inferred IDs.

#### Scenario: Missing current result is rejected

- GIVEN a structured unit declares `SCN-COV-001`
- WHEN current TDD evidence omits that scenario result
- THEN evidence admission SHALL fail naming `SCN-COV-001` without mutation

### Requirement: Tiered immutable verification

Every structured verification record SHALL contain an immutable ID, one tier from `focused`, `affected_package`, `aggregate_full`, or `publication_full`, the command as inert evidence text, an exit-bearing outcome, repository fingerprint, covered scenario IDs, actor, and recording time. PitCrew SHALL NOT execute stored command strings. Structured unit evidence SHALL include successful current `focused` and `affected_package` records.

A reuse record SHALL name an existing immutable successful record and SHALL be accepted only when tier, command, repository fingerprint, and normalized scenario set match exactly. Missing, failed, or stale source evidence SHALL be rejected without mutation. A risk decision MAY still require a fresh run.

#### Scenario: Stale reuse is rejected

- GIVEN an immutable success recorded for one repository fingerprint
- WHEN a reuse record supplies another fingerprint
- THEN the reuse SHALL fail and current verification SHALL remain required

### Requirement: Review input

`unit-review --input-file` SHALL read exactly one JSON object with `verdict` (`approved|corrections`), `summary` (string), `findings` (string), and `plan_impact` (`inside|outside`). For `corrections`, findings SHALL be non-empty and plan impact SHALL be present; for `approved`, findings MAY be empty and plan impact SHALL be omitted. The review SHALL bind to the explicit workflow id, unit id, and current unit revision rather than accepting those identifiers from the payload.

#### Scenario: Correction fields are required

- GIVEN a corrections verdict missing findings or plan impact
- WHEN unit-review reads it
- THEN exit code 3 SHALL result without a review

### Requirement: Declarative independence check

The accepted TDD actor and review `--actor` SHALL be non-empty declarative labels. The CLI SHALL reject equal labels for the same unit revision with exit code `3`; it SHALL NOT treat either label as authentication or authorization.

#### Scenario: Same actor is rejected

- GIVEN actor agent-a recorded TDD
- WHEN unit-review uses actor agent-a
- THEN exit code 3 SHALL result without a review

### Requirement: Correction outcomes

An `inside` correction SHALL record the verdict/findings, return the unit to `pending`, and increment its revision for re-claim and new evidence. An `outside` correction SHALL do the same and set a response signal requiring Aion plan revision and a new OpenSpec change before execution resumes.

#### Scenario: Inside correction increments revision

- GIVEN an inside-plan correction at revision n
- WHEN the review succeeds
- THEN the unit SHALL become pending at revision n+1

### Requirement: Selective unit review and completion

An approved review SHALL leave the unit `reviewing`. `unit-complete` SHALL require current TDD evidence, the `reviewing` state, and a valid active owner `--claim-handle`; it SHALL accept zero or one approval. A corrections verdict SHALL continue to reopen the unit and require fresh evidence. Completion SHALL move the unit to `done`, revoke the handle, and move the aggregate to `ready_to_complete` atomically when all units are done.

#### Scenario: Approved unit completes

- GIVEN current evidence with zero or one approval and a valid active handle
- WHEN unit-complete succeeds
- THEN the unit SHALL become done and the handle SHALL be revoked

### Requirement: Independent aggregate review

`workflow complete --input-file` SHALL accept an existing review verdict shape with `approved|corrections`, summary, and findings, plus structured aggregate verification and a reviewed-result checkpoint when the workflow has structured coverage. It SHALL reject an actor matching implementation evidence for any current unit revision and reject repeated completion while an aggregate blocker is unresolved, without mutation. The reviewer SHALL validate the repository result and tests against requirements, all specification/design amendments, plan/tasks, current evidence, unit reviews, and the declared correction policy. Approval with no blocker SHALL append an `aggregate_review` artifact and atomically complete the workflow. Corrections SHALL append it once, advance CAS in `ready_to_complete`, and return `workflow recover-aggregate` when authority exists or `user authorization required` when exhausted; the verdict itself consumes no round.

The Reviewer SHALL inspect the complete acceptance matrix and batch all material findings into one verdict. A correction re-review SHALL re-review the full scenario matrix and touched invariants rather than checking only the previous findings. At a selective unit gate, Aion SHALL allow one correction and one verification re-review; another corrections result SHALL stop orchestration rather than begin an unbounded loop. These are orchestration limits and SHALL NOT add a review command, state, or counter.

#### Scenario: Review findings are finite and complete

- GIVEN an initial or correction review of a structured result
- WHEN the Reviewer records its verdict
- THEN it SHALL cover the complete acceptance matrix and batch all material findings
- AND a correction verification SHALL also cover touched invariants beyond the prior findings

`authorize-correction` SHALL accept strict `{aggregate_review_revision,reason,user_direction_confirmed:true}` only for the exact current exhausted blocker. It SHALL append one authorization artifact/activity and a same-state CAS event atomically. Premature, mismatched, repeated-unconsumed, or terminal requests SHALL fail without mutation; the actor and confirmation are audited assertions, not authentication.

#### Scenario: Aggregate corrections preserve flow

- GIVEN all units are done and an independent reviewer reports corrections
- WHEN workflow complete succeeds
- THEN the review SHALL persist, workflow CAS SHALL advance, state SHALL remain ready_to_complete, and the response SHALL return an executable correction path

### Requirement: Aggregate verification bundle and reviewed checkpoint

Structured aggregate approval SHALL require successful current `focused` and `affected_package` records for every structured unit revision, at least one successful `aggregate_full` record, and an identifiable reviewed-result checkpoint. The checkpoint SHALL atomically persist project ID, canonical checkout/worktree, base revision, reviewed HEAD, result digest, dirty flag, optional commit reference, optional delivery ID, aggregate revision, and recording time. A dirty or unpublished result MAY complete; an empty, relative, or otherwise unidentified checkpoint SHALL fail without workflow, review, verification, or checkpoint mutation.

`publication_full` SHALL remain a separately recordable tier and SHALL NOT be an aggregate completion prerequisite. Publication policy MAY require it later at a clean delivery boundary without rewriting aggregate evidence.

#### Scenario: Dirty identifiable result completes without publication

- GIVEN complete unit and aggregate verification for an identifiable dirty worktree
- WHEN an independent reviewer approves with its checkpoint and no publication record
- THEN the review, aggregate verification, checkpoint, and completed CAS transition SHALL persist atomically

### Requirement: Aggregate recovery starts fresh evidence

`recover-aggregate` after corrections SHALL return fresh actor-bound opaque implementation authority for every selected reopened done unit. A subsequent `unit-tdd` SHALL use its incremented unit revision and preserve prior evidence; expired, revoked, one-shot, or mismatched authority SHALL fail with exit `5`. Recovery SHALL not expose a secret or substitute an actor label for authority.

#### Scenario: Recovered unit requires fresh TDD

- GIVEN a successful aggregate recovery
- WHEN its returned handle is used for `unit-tdd`
- THEN new evidence SHALL be recorded at the new revision while prior evidence remains inspectable

### Requirement: Proportional external routing

Aion SHALL contextually select the least-demanding route that fully satisfies
the outcome, material risks, and constraints. Aion SHALL select `direct_inline`
for safe, well-understood coordinator work; `delegated_direct` when a bounded
specialist handoff materially helps straightforward work; and `full_workflow`
when durable exploration, specification, design, planning, evidence, or
independent aggregate assurance is materially necessary. File count MAY inform
the judgment but SHALL NOT determine it. When selecting a stronger route, Aion
SHALL record the protected constraint and why the simpler route is materially
insufficient. The CLI SHALL record the selected route and rationale and SHALL
NOT classify work. Aion SHALL coordinate corrections and fresh aggregate
review without blindly retrying unchanged state or CAS failures.

#### Scenario: Equal file counts permit different routes

- GIVEN two changes affect the same number of files
- AND one is safe, mechanical, and already decided while the other carries material uncertainty
- WHEN Aion selects their routes
- THEN Aion MAY select a direct route for the first and `full_workflow` for the second
- AND the stronger selection SHALL name the uncertainty and why direct work is insufficient

#### Scenario: Larger mechanical work remains direct

- GIVEN an already-decided mechanical change affects a larger set of files
- AND no material risk, uncertainty, durable reasoning need, or independent aggregate assurance requires a stronger route
- WHEN Aion selects the least-demanding sufficient route
- THEN Aion MAY keep the change direct regardless of file count

#### Scenario: Bounded handoff materially helps

- GIVEN straightforward work would materially benefit from one bounded specialist handoff
- AND durable full-workflow reasoning and aggregate assurance are unnecessary
- WHEN Aion selects the route
- THEN Aion MAY select `delegated_direct` and record why inline work is insufficient

#### Scenario: Trivial bypass is external

- GIVEN Aion selects a valid route outside a full workflow
- WHEN no workflow command runs
- THEN the CLI SHALL impose no triviality check
