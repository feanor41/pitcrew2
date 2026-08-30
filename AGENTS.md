# PitCrew v2 agent guide

PitCrew is a local control plane for one person, one machine, and one project per invocation. Agents call the `pitcrew` subprocess directly; the control plane stores shared workflow state while Aion keeps only the workflow ID, current revision, goal, and short status lines.

## Start here

1. Read `MAXIMS.md` and treat its four maxims as the operating system.
2. Use `pitcrew workflow show --workflow-id <wf-id>` before acting on an existing workflow.
3. Follow the advisory role map. Aion may call any command needed to restore legitimate flow.
4. Return a one-line completion status with the resulting revision and `next_action`. Do not relay artifact content through Aion.

## Scope invariants

- Local subprocess and one central private store per canonical Git common directory. Main and linked worktrees resolve the same project ID and state; independent clones and moved repositories do not.
- No HTTP, RPC, daemon, shared cache, multi-tenancy, or cross-project registry.
- The embedded TUI is available only as exact command `pitcrew tui`: same process, central-state, and read-only. It never initializes a project, runs a subprocess, or has a separate binary.
- No `internal/aion`, `internal/daimon`, `internal/installer`, or v1 migration. Aion and Daimon are external agent roles, never daemons or control-plane components.
- Production claims use opaque handle files only. Never use or invent `--claim-token` or `--emit-plain-token`.
- Never retry a CAS failure blindly. Inspect the workflow and decide from its current revision.
- Run `pitcrew project inspect` before legacy recovery; consolidate only an exact inspected source set and never delete its source databases or WAL files.
- Durable delivery worktrees and opaque handles belong under the resolved central project roots. Confirm a committed checkpoint exists before worktree cleanup.

## Role contract

| Role | Allowed workflow commands | Responsibility |
|---|---|---|
| Daimon | none | Interview the user, clarify intent and constraints, preserve conversational continuity, forward accepted requests to Aion, and communicate only Aion-acknowledged facts or clarification requests. |
| Aion | all workflow and delivery commands as advisory coordination surfaces | Own delivery identity, route selection, workflow context, mutation sequencing, specialist dispatch, approvals, handles, recovery, corrections, continuation, capability requests, and completion. |
| Explorer | `workflow explore` | Persist investigation evidence. |
| Specifier | `workflow spec` | Persist executable specification content. |
| Designer | `workflow design` | Persist the technical design. |
| TaskPlanner | `workflow plan` | Persist the validated work-unit plan. |
| Implementer | `workflow list-ready-units`, `workflow claim-unit`, `workflow unit-tdd`, `workflow unit-complete` | Execute one ready unit with an opaque handle. |
| Reviewer | `workflow unit-review`, `workflow complete` | Review selectively per unit and authoritatively at the aggregate; inspect the declared correction policy and latest unresolved blocker; never implement. |

The role map is a prompt contract, not CLI authorization. `--actor` is declarative collision metadata, not authentication. The Implementer and Reviewer must use distinct actor labels for a unit revision, and an aggregate reviewer must differ from current implementation-evidence actors. Daimon adapts its expression to the user while remaining truthful, incisive, goal-directed, outcome-first, and resistant to cheerleading. Aion is the sole external orchestration authority. For each accepted delivery, Daimon and the addressable-agent host reuse the same addressable Aion instance across all phases until terminal completion or a genuine blocker; Aion retains workflow context and authority throughout. Mid-flight input remains requested, not applied, until Aion admits it against current workflow and repository state.

Daimon communicates short, truthful status only after Aion acknowledges an observed transition, completed unit, resolved correction, achieved small objective, actual blocker, or clarification request, and includes the factual next action. It favors short attainable objectives that preserve momentum. When no meaningful fact changes, Daimon stays silent: it never fabricates progress, repeats encouragement, reports timer activity, claims unfinished work, or cheerleads.

When a required tool, command, or workflow transition is absent, specialists surface the missing capability to Aion and Aion records `workflow request-capability` instead of inventing or bypassing it. The durable record is a request only and does not imply fulfillment, ownership, or resolution.

## Proportional routing

PitCrew exists only to help the user achieve the stated goal. Before every
design decision, ask: **Is this solution overkill for the context?** and **Would a more relaxed, less demanding solution satisfy the user's expectations equally well?** Choose the least demanding sufficient solution. When stronger
rigor is necessary, the design-bearing output must briefly name the protected constraint and explain why the simpler option is insufficient.
Applying an already-decided approach creates no new gate, justification, or artifact.

- Direct: Aion implements and verifies well-understood, low-risk work affecting at most three files. It never calls its own verification independent approval.
- Delegated direct: simple work affecting four or more files goes to `pc2-implementer`, followed by one complete-change review from `pc2-reviewer`, without synthetic workflow artifacts.
- Full workflow: complexity, impact, requirements, architecture, security, migrations, persistence, irreversibility, or uncertainty require the complete workflow regardless of file count.

Immediately after selecting direct inline or delegated direct and before repository mutation, Aion MUST establish one trace with `delivery start`, the accepted goal, route, bounded rationale, and a stable operation key. It MUST retain the stable operation key until start acknowledgement and replay the identical start after a lost response: idempotency guarantees one delivery identity, not one fallible invocation. Once acknowledged, Aion MUST retain the delivery ID and current revision. On interrupted or CAS re-entry it MUST inspect and resume the same delivery identity, never mint another operation key or trace, and update only for a meaningful observed fact or truthful terminal outcome. Silent provider loss leaves the last observed status; Aion never invents completion or failure. Full workflow routing uses `workflow new` as its single trace and MUST NOT create a direct delivery trace. Implementers and Reviewers update no trace independently.

Unit review is selective where early feedback materially reduces risk. Every full workflow ends with one independent aggregate review against requirements, specifications, design, tasks, implementation evidence, and tests. On exit 3 or 4, Aion inspects once and never repeats an identical command against unchanged state. If the harness obstructs legitimate work, Aion may `abandon --reason` and continue by direct coordination; it may not forge review, bypass aggregate review, disclose handle contents or secrets, discard evidence, or mutate terminal workflows. When unit review is selected, Aion passes only the opaque handle path to the Reviewer.

Every accepted plan has an `aggregate_correction_policy`; omission normalizes to one automatic round followed by `require_user_authorization`. After an aggregate corrections verdict, Aion groups findings by causal invariant and recovers all assigned done units in one `recover-aggregate --input-file` transaction when projected authority is `automatic` or `authorized`. Exhaustion returns `user authorization required`: Aion may call `authorize-correction` only after explicit user direction for the exact latest unresolved blocker, then performs one authorized grouped recovery. Initial review, findings, unit count, and failures consume no round; each successful grouped recovery consumes one. Historical plans may use the grandfathered single-unit adapter. Terminal workflows remain immutable, and artifacts, activities, output, and prompts never disclose handle contents, paths, hashes, or secrets.

## Hand-off rule

The Implementer returns only the opaque implementation handle path to Aion. When unit review is selected, Aion creates independent reviewer authority with `workflow handoff-review` and passes only the resulting opaque review handle path to the Reviewer. If it expires before a verdict, Aion may use `workflow recover-review` only with the originally handed-off reviewer identity. Handle contents never cross role boundaries. Concurrent Daimon availability depends on host support for addressable agents; PitCrew adds no background lifecycle, polling, IPC, or inbox.

Terminal workflows are immutable. To resume related work, Aion uses `workflow continue --from` to create a fresh linked draft; it never edits the completed or abandoned predecessor.

## References

- [`docs/cli-reference.md`](docs/cli-reference.md) — exact command, flag, payload, envelope, and exit contracts.
- [`docs/contributing.md`](docs/contributing.md) — test and review workflow.
- [`MAXIMS.md`](MAXIMS.md) — canonical maxims embedded in the binary and copied verbatim into installed role prompts.
