# Spec: tdd-and-review

## Purpose

Define test evidence, independent review, corrections, and completion for one work-unit revision.

## ADDED Requirements


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

An `inside` correction SHALL record the verdict/findings, return the unit to `pending`, and increment its revision for re-claim and new evidence. An `outside` correction SHALL do the same and set a response signal requiring Master plan revision and a new OpenSpec change before execution resumes.

#### Scenario: Inside correction increments revision

- GIVEN an inside-plan correction at revision n
- WHEN the review succeeds
- THEN the unit SHALL become pending at revision n+1

### Requirement: Approval and completion

An approved review SHALL leave the unit `reviewing`. `unit-complete` SHALL require that state, exactly one latest approved verdict for the explicit unit revision, and a valid active `--claim-handle`. It SHALL move the unit to `done`, revoke the handle, and move the aggregate to `ready_to_complete` atomically when all units are done.

#### Scenario: Approved unit completes

- GIVEN latest approval and a valid active handle
- WHEN unit-complete succeeds
- THEN the unit SHALL become done and the handle SHALL be revoked

### Requirement: Trivial-work exception

The Master MAY skip the harness for trivial work and SHALL disclose that choice to the user. The CLI SHALL NOT classify triviality.

#### Scenario: Trivial bypass is external

- GIVEN the Master discloses a trivial-work bypass
- WHEN no workflow command runs
- THEN the CLI SHALL impose no triviality check
