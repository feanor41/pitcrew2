# Knowledge promotion

PitCrew treats accumulated experience as a source of candidates, not as an
implicit runtime dependency. Valuable observations may originate in Engram, a
review, an incident, or a conversation, but an agent may rely on them only
after they have been explicitly promoted into a versioned PitCrew surface.

The governing flow is:

```text
personal or external memory -> reviewed candidate -> versioned PitCrew authority
                                                    -> bounded agent brief
```

Engram is therefore an inbox and provenance source. It is not PitCrew's source
of truth, and `pitcrew agent brief` must not query a personal Engram store.
This keeps PitCrew portable, deterministic, reviewable, available offline, and
independent of one operator's private memory.

## What is worth promoting

Promote an observation when it is durable, reusable, and expected to change a
future agent decision. Classify it before choosing its destination:

| Candidate type | Authoritative destination | Typical audience |
|---|---|---|
| Harness-wide invariant | `MAXIMS.md`, through its dedicated change process | Every role |
| Stable role behavior | The role contract and its executable specification | One coordinating or specialist role |
| Project operating rule | A versioned project playbook, usually `docs/contributing.md` | Contributors and relevant coordinators |
| Reusable operation | A versioned playbook with tests for material invariants | Roles executing that operation |
| Delivery-specific fact | The accepted delivery or workflow projection | Only roles participating in that delivery |
| Defect or missing capability | A test and implementation issue | The delivery that owns the fix |
| Personal preference | Personal memory unless explicitly generalized | The operator and conversational layer |
| Historical record | Issue, release, commit, or archive | Auditors; not ordinary briefs |

Do not promote session summaries, repeated passive captures, obsolete status,
one-time authorization, completed release hashes, or a full incident narrative
when a short causal rule is sufficient. If a rule is already authoritative in
code, tests, a specification, or a role contract, retain the memory only as
provenance instead of duplicating the rule.

## Promotion procedure

1. Extract the general rule from the event that produced it.
2. State the scope, intended roles, applicability conditions, and protected
   constraint.
3. Check the repository and issue tracker for an existing authority or
   conflicting rule.
4. Select exactly one authoritative destination from the table above.
5. Review and merge the change through the repository's ordinary delivery
   path. Add tests only for invariants whose drift would cause incorrect agent
   behavior.
6. Record provenance, review date, and supersession when the destination
   supports them.
7. Keep the original memory as history; do not make agents retrieve it at
   runtime.

Promotion is explicit. Automatic import would allow private, stale, duplicated,
or contradictory observations to become operational policy without review.

## Distribution through agent briefs

An agent brief remains layered and bounded:

1. global maxims for every role;
2. the stable contract for the requested role;
3. only the applicable operating-knowledge keys;
4. current delivery or workflow state and executable authority.

The brief must never dump the complete knowledge collection. Until a dedicated
knowledge manifest exists, promoted knowledge reaches agents through the
existing authoritative surface: maxims, role contracts, project playbooks,
specifications, tests, and current control-plane state.

When PitCrew has roughly five to ten durable operating rules that are hard to
discover through those surfaces, it may add a small versioned manifest. Each
entry should contain:

- a stable key;
- a type and concise assertion;
- applicable roles and conditions;
- repository evidence or source provenance;
- active, superseded, or retired state;
- a review date and optional `supersedes` key.

Aion should select applicable keys qualitatively when admitting a delivery.
The selected keys may then be stored with that delivery or workflow and exposed
only to relevant roles. This selection must not become a semantic-search
service, deterministic classifier, daemon, network synchronization process, or
new personal-preference database.

## Existing surfaces and boundaries

`project context` is not the operating-knowledge collection. Its schema records
bounded, evidence-backed facts in exactly six categories: stack, runtime,
deployment, architecture, documentation, and SDD. It has no model for audience,
applicability, preference, supersession, or review lifecycle, and ordinary role
briefs do not consume it. Keep it focused on factual project initialization and
deployment evidence.

Likewise, delivery progress is descriptive state, not durable policy. A useful
fact discovered during a delivery becomes generally authoritative only through
the promotion procedure above.

## Initial promotions

The first audit identified three concrete outcomes:

- Meaningful, truthful progress communication is already part of Daimon's
  stable contract. Do not duplicate it as a new maxim.
- Aion owns contextual, qualitative selection among direct, delegated-direct,
  and full-workflow routes. File count can inform judgment but must not act as a
  deterministic classifier. This belongs in Aion's stable role policy and its
  executable specification.
- Quick and official releases are distinct project operations. Their canonical
  playbook belongs in `docs/contributing.md`, not in the global maxims.

No fifth maxim is needed. These rules have narrower audiences and promoting
them at the correct scope avoids drift and unnecessary prompt weight.

## Delivery phases

### Phase 0: use existing surfaces

Promote the Aion routing rule and the release-mode playbook. Treat communication
behavior as already promoted. No new persistence, command, or brief schema is
required. The remaining normative and runtime-contract reconciliation is tracked
by [GitHub issue #175](https://github.com/feanor41/pitcrew2/issues/175).

### Phase 1: introduce a manifest only after demonstrated pressure

Once the threshold above is reached, define the manifest, validation rules,
brief projection, and supersession behavior as one bounded feature. Existing
authoritative files remain the source material; Engram remains optional. This
activation-gated work is tracked by
[GitHub issue #176](https://github.com/feanor41/pitcrew2/issues/176).

### Phase 2: persist per-delivery selection only if it proves useful

Allow Aion to attach selected stable keys to a delivery or workflow so scoped
briefs can project them consistently. Require revision checks and preserve
terminal immutability. Do not add runtime Engram access or background services.
