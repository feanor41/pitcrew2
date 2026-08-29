# Spec: tdd-and-review

## Purpose

Define test evidence, selective unit review, corrections, unit completion, and authoritative aggregate review.

## Requirements


### Requirement: TDD input

`unit-tdd --input-file` SHALL read exactly one JSON object with string fields `red_command`, `red_outcome`, `green_command`, `green_outcome`, `refactor_summary`, `validation_command`, `validation_outcome`, and `changed_paths`. Every field except `refactor_summary` SHALL be non-empty; outcomes SHALL record RED failure and GREEN/validation success; changed paths SHALL be comma-separated normalized repository-relative paths. The explicit `--revision` SHALL identify the current unit revision. Accepted evidence SHALL persist with workflow/unit ids and recording time and move the unit from `pending` to `reviewing`.

#### Scenario: Complete evidence is accepted

- GIVEN a pending unit and complete TDD JSON
- WHEN unit-tdd targets its current revision
- THEN evidence SHALL persist and state SHALL become reviewing

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

`workflow complete --input-file` SHALL accept an existing review verdict shape with `approved|corrections`, summary, and findings. It SHALL reject an actor matching implementation evidence for any current unit revision and reject repeated completion while an aggregate blocker is unresolved, without mutation. The reviewer SHALL validate the repository result and tests against requirements, all specification/design amendments, plan/tasks, current evidence, unit reviews, and the declared correction policy. Approval with no blocker SHALL append an `aggregate_review` artifact and atomically complete the workflow. Corrections SHALL append it once, advance CAS in `ready_to_complete`, and return `workflow recover-aggregate` when authority exists or `user authorization required` when exhausted; the verdict itself consumes no round.

`authorize-correction` SHALL accept strict `{aggregate_review_revision,reason,user_direction_confirmed:true}` only for the exact current exhausted blocker. It SHALL append one authorization artifact/activity and a same-state CAS event atomically. Premature, mismatched, repeated-unconsumed, or terminal requests SHALL fail without mutation; the actor and confirmation are audited assertions, not authentication.

#### Scenario: Aggregate corrections preserve flow

- GIVEN all units are done and an independent reviewer reports corrections
- WHEN workflow complete succeeds
- THEN the review SHALL persist, workflow CAS SHALL advance, state SHALL remain ready_to_complete, and the response SHALL return an executable correction path

### Requirement: Aggregate recovery starts fresh evidence

`recover-aggregate` after corrections SHALL return fresh actor-bound opaque implementation authority for every selected reopened done unit. A subsequent `unit-tdd` SHALL use its incremented unit revision and preserve prior evidence; expired, revoked, one-shot, or mismatched authority SHALL fail with exit `5`. Recovery SHALL not expose a secret or substitute an actor label for authority.

#### Scenario: Recovered unit requires fresh TDD

- GIVEN a successful aggregate recovery
- WHEN its returned handle is used for `unit-tdd`
- THEN new evidence SHALL be recorded at the new revision while prior evidence remains inspectable

### Requirement: Proportional external routing

Aion SHALL directly implement and verify well-understood low-risk work affecting at most three files without claiming independent approval. Simple work affecting four or more files SHALL use direct delegation to pc2-implementer followed by one independent complete-change review. Complexity, impact, requirements, architecture, security, migrations, persistence, irreversibility, or uncertainty SHALL require the full workflow regardless of size. The CLI SHALL NOT classify routes. Aion SHALL coordinate corrections and fresh aggregate review without blindly retrying unchanged state or CAS failures.

#### Scenario: Trivial bypass is external

- GIVEN Aion selects a valid route outside a full workflow
- WHEN no workflow command runs
- THEN the CLI SHALL impose no triviality check
