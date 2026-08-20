<!--
Reconstructed from Engram observations on 2026-08-20.
Source observations:
  - pitcrew2/foundation-change (id 4441)
  - pitcrew2/agent-roles (id 4442)
  - pitcrew2/orchestration-model (id 4444)
NOT byte-identical to the originals. Original was ~95-110 lines. The
TDD evidence contract, the same-identity prohibition, and the review
verdict are reconstructed from the documented narrative.
-->

# Spec: tdd-and-review

## Purpose

Define the evidence contract for work-unit completion (RED, GREEN,
refactor, validation, changed paths) and the independent review
contract that gates unit completion. The default path is strict TDD;
trivial work may skip the harness per Maxim III.

## Requirements

### R1 — TDD evidence contract

Every call to `workflow unit-tdd` SHALL record, for the current unit
revision:

- `red_command` (the command run, with exit code)
- `red_outcome` (observed failure)
- `green_command` (the command run, with exit code)
- `green_outcome` (observed success)
- `refactor_summary` (short text or `""` if no refactor)
- `validation_command` (broader test/lint/build command)
- `validation_outcome` (observed success)
- `changed_paths` (comma-separated repository-relative paths)

A TDD record SHALL be accepted only if all four commands have a
recorded command string and a recorded outcome.

### R2 — Trivial-work exception (Maxim III)

The Master MAY skip the harness for trivial work (typo fixes,
one-line edits, well-understood local refactors). The Master SHALL
document the trivial-work decision in the user-facing reply. The CLI
SHALL NOT enforce trivial-work detection; it is a Master concern.

### R3 — Review verdict

`workflow unit-review` SHALL record one verdict per unit revision:

- `approved` — the evidence is sufficient; the unit MAY be completed.
- `corrections` — the evidence is insufficient; the unit SHALL NOT
  be completed. The verdict SHALL include `findings` and
  `plan_impact` (`inside` or `outside` the original plan).

### R4 — Same-identity prohibition

The Implementer and the Reviewer SHALL be distinct identities. The
runtime SHALL configure them as separate subagents so the same agent
does not implement and review its own work. The CLI SHALL reject
`workflow unit-review` for a unit whose active handle's claim was
issued in the same runtime identity as the Implementer that ran
`workflow unit-tdd` for the same revision.

### R5 — Inside-plan correction

WHEN a review returns `corrections` with `plan_impact: inside`,
THEN the unit SHALL return to state `pending` with the recorded
findings. The Implementer MAY re-claim the unit, run a new TDD cycle,
and re-submit for review. The unit revision SHALL increment on each
re-submission.

### R6 — Outside-plan correction

WHEN a review returns `corrections` with `plan_impact: outside`,
THEN the unit SHALL return to state `pending` AND the workflow
SHALL surface the correction to the Master for a revised-plan
decision. Outside-plan corrections require a new OpenSpec change
before execution resumes.

### R7 — Unit completion

`workflow unit-complete` SHALL succeed only when:

- the unit is in state `reviewing`,
- the latest review verdict for the unit's current revision is
  `approved`,
- the active handle is valid and unexpired.

On success, the unit SHALL transition to `done`, the handle SHALL be
revoked, and the workflow's `ready_to_complete` flag SHALL be
re-evaluated.

## Scenarios

### S1 — Strict TDD happy path

> WHEN the Implementer runs RED (fails), GREEN (passes), REFACTOR,
> and VALIDATION,
> THEN `workflow unit-tdd` SHALL record all six fields and the
> unit SHALL transition to `reviewing`.

### S2 — Missing red outcome

> WHEN `workflow unit-tdd` is invoked without a recorded
> `red_outcome`,
> THEN the CLI SHALL reject the record with exit code `3` and a
> message naming the missing field.

### S3 — Review approves

> WHEN the Reviewer invokes `workflow unit-review --verdict approved`,
> THEN the unit SHALL remain in `reviewing` with verdict
> `approved`. The Implementer MAY then invoke
> `workflow unit-complete`.

### S4 — Review requests corrections inside the plan

> WHEN the Reviewer invokes
> `workflow unit-review --verdict corrections --plan-impact inside`,
> THEN the unit SHALL return to `pending` with the recorded
> findings. The Implementer MAY re-claim and re-submit.

### S5 — Same-identity rejection

> WHEN the same runtime identity invokes both
> `workflow unit-tdd` and `workflow unit-review` for the same
> unit revision,
> THEN the CLI SHALL reject the review with exit code `3`.
