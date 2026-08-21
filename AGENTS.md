# PitCrew v2 agent guide

PitCrew is a local control plane for one person, one machine, and one project per invocation. Agents call the `pitcrew` subprocess directly; the control plane stores shared workflow state while Daimon keeps only the workflow ID, current revision, goal, and short status lines.

## Start here

1. Read `MAXIMS.md` and treat its four maxims as the operating system.
2. Use `pitcrew workflow show --workflow-id <wf-id>` before acting on an existing workflow.
3. Call only the command assigned to your role.
4. Return a one-line completion status with the resulting revision and `next_action`. Do not relay artifact content through Daimon.

## Scope invariants

- Local subprocess and project-local `.pitcrew/state.db` only.
- No HTTP, RPC, daemon, shared cache, multi-tenancy, or cross-project registry.
- No embedded TUI, `internal/daimon`, `internal/installer`, or v1 migration. Daimon is an external agent role, never a Unix daemon or control-plane component.
- Production claims use opaque handle files only. Never use or invent `--claim-token` or `--emit-plain-token`.
- Never retry a CAS failure blindly. Inspect the workflow and decide from its current revision.

## Role contract

| Role | Allowed workflow commands | Responsibility |
|---|---|---|
| Daimon | `workflow new`, `workflow show`, `workflow approve-plan`, `workflow abandon`, optional `workflow complete` | Serve as the sole user/sub-agent bridge and approve execution. |
| Explorer | `workflow explore` | Persist investigation evidence. |
| Specifier | `workflow spec` | Persist executable specification content. |
| Designer | `workflow design` | Persist the technical design. |
| TaskPlanner | `workflow plan` | Persist the validated work-unit plan. |
| Implementer | `workflow list-ready-units`, `workflow claim-unit`, `workflow unit-tdd`, `workflow unit-complete` | Execute one ready unit with an opaque handle. |
| Reviewer | `workflow unit-review` | Review independently; never implement. |
| Archivist | `workflow complete` | Complete a ready aggregate. |

The role map is a prompt contract, not CLI authorization. `--actor` is declarative collision metadata, not authentication. The Implementer and Reviewer must use distinct actor labels for a unit revision. Daimon adapts its expression to the user while remaining truthful, incisive, goal-directed, outcome-first, and resistant to cheerleading.

## Hand-off rule

The Implementer returns only the opaque handle path to Daimon. Daimon passes only that path to the Reviewer. Handle contents never cross role boundaries.

## References

- [`docs/cli-reference.md`](docs/cli-reference.md) — exact command, flag, payload, envelope, and exit contracts.
- [`docs/contributing.md`](docs/contributing.md) — test and review workflow.
- [`MAXIMS.md`](MAXIMS.md) — canonical maxims embedded in the binary and copied verbatim into installed role prompts.
