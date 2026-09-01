# Spec: claim-handles

## Purpose

Define the opaque-only claim path that keeps bearer secrets out of agent context, logs, shell history, and handle files.

## Requirements

### Requirement: Release a control-plane-untouched implementation intent

`release-unit-claim` SHALL require exact workflow and unit CAS, the current
owner, and the exact latest live implementation handle while it remains in
`intent` state and the pending unit has no current evidence, review, or
unit-scoped verification, and no authoritative unit mutation/result activity
has been recorded after the latest implementation claim boundary. It SHALL
fail closed when those activity facts and their normalized rows are
inconsistent. It SHALL inspect only persisted control-plane facts;
it SHALL NOT inspect Git or assert repository cleanliness. Success SHALL in one
transaction revoke the handle, increment the pending unit and implementing
workflow revisions once, append one structured `unit_claim_release` artifact,
one implementing self-transition event, and one artifact-linked
`unit_claim_released` activity. It SHALL commit before best-effort handle-file
cleanup, return only sanitized result fields, and restore ordinary ready-unit
redispatch. Replay, expiry, wrong owner/target/purpose, stale CAS, current
implementation facts, and durable-write failure SHALL fail closed without
partial state or authority disclosure.
The durable release fact SHALL authorize exactly one immediate ordinary
`claim-unit`. Its subsequent `unit_claimed` activity SHALL consume that
authorization; revoking or expiring the new claim SHALL NOT make the older
release fact reusable, and further authority SHALL use the normal recovery path.

#### Scenario: Current intent owner releases for redispatch

- GIVEN a pending unit with its latest unexpired implementation intent and no current implementation facts
- WHEN its owner supplies both current revisions, the exact opaque handle path, and a bounded reason
- THEN authority SHALL be revoked atomically, both revisions SHALL advance once, one coalesced sanitized history occurrence SHALL remain navigable, and the unit SHALL become ready

#### Scenario: Durable activity prevents release despite a missing normalized row

- GIVEN a latest implementation intent followed by authoritative unit TDD, review, or completion activity whose normalized result row is missing
- WHEN release eligibility is evaluated
- THEN release SHALL fail closed without revoking authority or advancing revisions

#### Scenario: Immediate reclaim consumes release authorization

- GIVEN a released unit was ordinarily reclaimed once
- WHEN that newer claim is revoked or expires and another ordinary claim is attempted
- THEN the old release fact SHALL NOT authorize another claim
- AND the history and brief release predicate SHALL remain consumed

#### Scenario: Release never proves repository cleanliness

- GIVEN arbitrary editor, untracked, or working-tree changes outside PitCrew's durable facts
- WHEN release eligibility is evaluated
- THEN those changes SHALL neither be inspected nor represented as absent


### Requirement: Opaque production path

`claim-unit`, `recover-unit-claim`, and `recover-review` SHALL require `--workflow-id`, `--unit-id`, `--revision`, `--actor`, and caller-supplied `--handle-dir`; `recover-aggregate` uses its grouped/historical matrix below. Production orchestration SHALL select the resolved private `<data-home>/pitcrew/projects/<project-id>/handles/` root, with workflow-specific subdirectories as needed. Commands SHALL write a `0600` opaque handle inside a caller-owned `0700` directory. No production command, flag, template, or payload SHALL accept or emit a raw claim token; `--emit-plain-token` and `--claim-token` SHALL NOT exist.

#### Scenario: Production claim returns only a path

- GIVEN matching ids, revision, actor, and secure directory
- WHEN claim-unit succeeds
- THEN only an opaque handle path SHALL be returned

#### Scenario: Linked worktrees share durable handle placement

- GIVEN callers operating from a main checkout and its linked worktree
- WHEN Aion supplies their resolved handle directory
- THEN both SHALL place authority under the same private central project root

### Requirement: Handle document and validation

The caller-owned handle SHALL be one JSON document containing `version:1`, `state` (`intent|active`), `workflow_id`, `unit_id`, `claim_id` (`32hex`), `secret_hash` (SHA-256 hex), `issued_at`, and `expires_at` (UTC RFC3339). The plain secret SHALL NOT be stored. Reads SHALL reject symlinks, wrong ownership, wrong modes, mismatched explicit workflow/unit ids, or a handle whose persisted claim is not current.

#### Scenario: Unsafe handle is rejected

- GIVEN a symlinked, misowned, mismatched, or wrong-mode handle
- WHEN a unit command reads it
- THEN exit code 5 SHALL result without mutation

### Requirement: Capped purpose-aware lease (REQ-LEASE-001)

An implementation or review claim SHALL start as `intent`, become `active` on the first successful matching unit command, and expire fifteen minutes after issue. This includes fresh implementation claims issued by aggregate recovery. The issue path SHALL select the lease by claim purpose and cap every current purpose at fifteen minutes. Unit commands SHALL NOT renew the lease, and orchestration SHALL NOT use heartbeat polling or background renewal. An expired handle SHALL return exit code `5`, be atomically deleted, and cause no workflow mutation. Successful `unit-complete` and successful review consumption SHALL revoke their respective handles.

#### Scenario: Successful use does not renew a lease (SCN-LEASE-001)

- GIVEN an implementation or review handle issued with its purpose-aware fifteen-minute expiry
- WHEN one or more matching unit commands succeed before that expiry
- THEN the persisted expiry SHALL remain capped at fifteen minutes after issue
- AND use at or after that expiry SHALL atomically delete the handle and return exit code 5 without workflow mutation

#### Scenario: Aggregate-recovery authority expires at the exact boundary (SCN-LEASE-002)

- GIVEN aggregate recovery issued a fresh implementation handle fifteen minutes earlier
- WHEN its assigned implementer attempts the first matching unit command exactly at `expires_at`
- THEN the handle SHALL be atomically revoked and deleted
- AND the unit and workflow SHALL remain unchanged

### Requirement: Actor collision metadata

The claim SHALL persist `--actor` as store-only declarative metadata; it SHALL NOT appear in the handle file and SHALL NOT authenticate or authorize. `unit-tdd` SHALL require the same actor label and associate it with the unit revision. `unit-review` SHALL reject that label for the same unit revision with exit code `3`.

#### Scenario: Same actor cannot review

- GIVEN actor agent-a claimed and recorded TDD
- WHEN unit-review uses actor agent-a
- THEN exit code 3 SHALL result without a review

### Requirement: Recovery

Recovery SHALL issue a fresh secret and increment `claim_generation` only when the unit is not `reviewing` and has no TDD evidence for its current unit revision. The prior claim SHALL be revoked.

#### Scenario: Recovery is blocked after evidence

- GIVEN TDD evidence exists for the current unit revision
- WHEN recover-unit-claim is invoked
- THEN exit code 3 SHALL result and no claim SHALL issue

### Requirement: Aggregate-correction recovery

For policy-aware plans, `recover-aggregate` SHALL require `--input-file` with exact blocker revision, bounded causal groups, and one actor assignment per selected done unit; it SHALL reject `--unit-id`. Historical plans MAY accept either the grouped input or exactly one grandfathered `--unit-id`, never both. One successful command stages exclusive `0600` actor-bound handles under a caller-owned non-symlink `0700` directory and commits their rows, unit/workflow changes, artifact, and activity atomically; every ordinary failure removes staged/final files, and an orphan without DB authority is invalid. Output SHALL contain only `handles[{unit_id,unit_revision,actor,handle_path}]`, never secrets.

#### Scenario: Aggregate recovery is exactly one fresh authority

- GIVEN a matching `ready_to_complete` workflow whose latest aggregate review requests corrections and one completed unit
- WHEN `recover-aggregate` names that one unit at the current revision
- THEN prior records remain available and only a fresh opaque implementation handle for the incremented unit revision is returned

#### Scenario: Aggregate recovery rejects invalid selection or authority

- GIVEN a stale, terminal, no-corrections, non-done, or duplicate-unit recovery request
- WHEN `recover-aggregate` is invoked
- THEN it SHALL fail without issuing a handle or reopening any unit

### Requirement: One-shot operator escape

`claim-unit` MAY accept hidden `--print-claim-handle-secret-once`. Success SHALL still emit the standard workflow envelope, place the secret exactly once at `data.claim_secret`, and revoke the new handle in the same process before output. The secret SHALL occur nowhere else in the envelope or persisted state. The flag SHALL be absent from help and templates; the returned handle SHALL fail subsequent use with exit code `5`.

#### Scenario: Debug secret is one-shot

- GIVEN the hidden flag on a valid claim request
- WHEN claim succeeds
- THEN data.claim_secret SHALL be its sole plaintext occurrence and the claim SHALL be revoked

### Requirement: Hand-off

The Implementer SHALL return only the handle path to Aion. For selected unit review, Aion SHALL create reviewer authority and pass only the resulting opaque review handle path to the Reviewer.

#### Scenario: Hand-off contains no secret

- GIVEN a successful production claim
- WHEN the Implementer reports to Aion
- THEN only the handle path SHALL be handed off
