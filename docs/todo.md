# TO-DO List - Open Issues

Let's store all the TO-DO in a single place

## PitCrew en General

- [x] **Delivered — Daimon feedback relay.** Desde que aparecio Aion, la comunicacion de Daimon desaparecio. Todo funciona, pero Daimon "esta mudo". Delivered by PR #62; the named Pi supervisor-flow verifier is `scripts/tests/pi-supervisor-runtime.sh`.
- [ ] **P3 — User-channel role identity.** Cuando habla Daimon quiero que su dialogo hacia el usuario siempre este marcado porque inicia con un Emoji que lo distingue, lo mismo con Aion. Clarify how Aion attribution remains compatible with the `user ↔ Daimon ↔ Aion` boundary before implementation.
- [x] **Delivered — Durable project context and SDD initialization.** Pitcrew tiene que tener en la herramienta disponible toda la informacion del proyecto, la especifica que nos sirve para trabajar y que workflow a workflow estamos yendo a leer de nuevo.  Quiero que la cli sea capaz de guardar y retornar esa informacion para que cualquier agente que la necesite la pueda solicitar directamente al control plane.  Quiero que cuando PitCrew detecta que esta trabajando en un nuevo repo o proyecto, analiza el repositorio en busca de:
  - Stack tecnico
  - Runtime
  - Despliegue
  - Patrones de arquitectura
  - Documentacion interna del repo
  - Cualquier cosa necesaria para poder trabajar en SDD en el repo.
- Necesitamos un nuevo subagente especializado en inicializar el proceso de SDD y sea capaz de obtener toda la informacion anteriormente mencionada.  Este subagente es disparado por Aion cuando se detecta que la informacion no esta completa.
- [x] **Delivered — Universal, economical delivery traceability.** Every harness-managed delivery leaves one durable, searchable Control Plane trace whether Aion selects direct inline, delegated direct, or full workflow execution.
  - Record the delivery goal, selected route, current or terminal status, timestamps, and only a small bounded set of useful searchable details.
  - Reuse the full workflow record when one already exists. Direct routes MUST use a lightweight representation rather than creating synthetic SDD phases, review artifacts, work units, or additional gates.
  - Make the trace visible through the existing Control Plane inspection surfaces and searchable by goal, status, route, and recorded detail.
  - Preserve truthful interruption and terminal outcomes: unfinished, blocked, cancelled, failed, and completed work must not collapse into the same state or disappear.
  - Executable acceptance: equivalent small, delegated, and full-workflow deliveries each appear once with their goal and truthful status; the small and delegated cases incur no workflow lifecycle or review machinery.

## Workflow execution retrospective

1. - [ ] **P1 — Continuity cluster.** Recover active workflow continuity automatically. When a delivery is already in flight, Aion must inspect and resume its durable workflow instead of requiring the user to remind the system that work was active.
2. - [ ] **P1 — Continuity cluster.** Continue autonomously through routine lifecycle transitions until terminal completion, a genuine blocker, cancellation, or an explicit user-owned gate; do not stop between ordinary phases for confirmation.
3. - [ ] **P1 — Continuity cluster.** Report meaningful progress proactively. Surface observed transitions, completed units, corrections, and blockers without requiring the user to poll, while remaining silent when no fact changed.
4. - [x] **P0 — Bound correction cycles before implementation.** Every review-bearing delivery must declare a correction budget and terminal behavior before the first review; the system must not enter an open-ended correction/re-review loop.
5. - [x] **P0 — Consolidate aggregate findings by causal invariant.** Findings that share one authority, rollback, or safety boundary must become one bounded correction transaction instead of serial unit recovery and repeated aggregate reviews.
6. - [x] **P0 — Enforce an explicit final-review gate.** After the declared final review, approval completes the delivery; any newly discovered blocker stops automatic mutation and requires explicit user intervention rather than opening another correction cycle.
7. - [ ] **Duplicate — Continuity cluster (2–3).** Distinguish expected escalation from avoidable user polling. A genuinely new blocker beyond the correction budget requires explicit authorization; routine continuation and status reporting do not. Close with the cluster rather than adding another state or mechanism.
8. - [x] **Delivered in 0.17.1 via PR #76 — Enforce the implementation admission gate.** `workflow begin-implementation` is required before any unit claim. `list-ready-units` and `claim-unit` reject work while the workflow remains `plan_approved`, so a skipped transition cannot remain latent until final-unit completion.

## Harness correctness and token efficiency

- [x] **Delivered in 0.20.0 via PRs #110–#126 — Preserve read-only project-context compatibility.** `context inspect` must treat an initialized pre-V5 store as context `missing` without querying a table that does not exist, creating state, or requiring a mutating migration. Cover the real V4-to-V5 boundary and retain strict corruption semantics for stores that do contain project-context rows.
- [x] **Delivered in 0.20.0 via PRs #110–#126 — Make the delivery trace the first mutation gate.** Direct-inline and delegated-direct work must establish one idempotent `dl-*` trace before repository mutation. Recovery may replay the same stable operation key, but the system must never fabricate a retrospective trace after work already occurred.
- [x] **Delivered in 0.20.0 via PRs #110–#126 — Add bounded workflow projections without weakening audit.** Keep the current complete projection available for forensic inspection and aggregate authority, while ordinary coordination retrieves only the role-appropriate state:
  - coordination: workflow ID, revision, lifecycle state, executable `next_action`, current/ready unit, blocker, correction authority, and latest meaningful progress;
  - phase: accepted upstream artifacts exactly once, without records/timeline duplication;
  - unit: one unit definition plus only its current TDD and review revision;
  - aggregate: normative artifacts once, latest evidence/review per current unit, and correction closure;
  - audit: immutable full artifacts, records, and timeline on explicit demand.
  - Executable acceptance: the completed project-context workflow's coordination projection is at least 90% smaller than its full audit projection, exposes the same revision and executable next action, and never exposes claim handles or secrets.
- [x] **Delivered in 0.20.0 via PRs #110–#126 — Use transcript-minimal specialist handoffs.** Aion should dispatch specialists without replaying its growing conversation when host support permits it. Pass only the workflow/revision, role or unit identity, and the applicable opaque handle; the specialist retrieves its bounded Control Plane view. Preserve Aion authority, actor separation, and handle secrecy.
- [x] **Delivered in 0.20.0 via PRs #110–#126 — Make continuations delta-aware.** A continued workflow should reference immutable predecessor exploration, specification, and design, then persist only changed assumptions, superseded requirements, design adaptations, and new scenarios. Aggregate review resolves baseline plus delta; unrelated workflows retain standalone artifacts.
- [x] **Delivered in 0.20.0 via PRs #110–#126 — Bind completion to a durable reviewed-result checkpoint.** Record project identity, checkout/worktree, base and reviewed HEAD or tree/diff digest, dirty status, and optional commit/delivery reference so a completed workflow cannot strand an unidentifiable implementation. Do not require GitHub publication or a clean tree.
- [x] **Delivered in 0.20.0 via PRs #110–#126 — Shift predictable defects left while preserving independent review.** Give units machine-readable requirement/scenario IDs and require focused acceptance evidence against them before handoff. Keep mandatory aggregate review and select unit review by material risk; do not blanket-skip review where migrations, persistence, confinement, production composition, or public CLI contracts are involved.
- [x] **Delivered in 0.20.0 via PRs #110–#126 — Tier verification and evidence reuse.** Run focused and affected-package gates for each implementation revision, then run complete Go, vet, build, POSIX, and contract gates at aggregate review and clean publication boundaries. Permit early full gates for cross-cutting risk, and retain truthful commands/outcomes with repository-state fingerprints so unchanged evidence is not restated or rerun without reason.
- [x] **Delivered in 0.20.0 via PRs #110–#126 — Reduce recovery and prompt overhead proportionately.** Evaluate a bounded 15-minute or task-aware opaque-handle lease without heartbeat polling, background renewal, or weaker expiry. Remove duplicated orchestration prose and unnecessary full-history forks while keeping the four MAXIMS, CAS inspection, terminal immutability, and every safety boundary authoritative.

## TUI

- [x] **Delivered acceptance surface — Universal delivery traceability.** The TUI lists unified Deliveries at start, renders direct work as a thin truthful status/detail, and retains the existing full-workflow cockpit without a second status representation.

## Installation / Upgrade

- [ ] **P2 — Recurring release operation.** Make `bump and install` a proportional, observable recurring operation. Reuse the in-flight durable project context, universal delivery trace, and P1 continuity/progress semantics; the remaining root is a reusable release map plus post-interruption reconciliation:
  - Treat Maxim II as an enforced invariant: a maxim that lower-level heuristics can displace is decoration. File-count and routing defaults must never override the least-demanding sufficient path.
  - Require every stronger route to name the concrete protected constraint and explain why the simpler path is materially insufficient.
  - Make the selected route and rationale observable before substantial work consumes time; documentation alone is insufficient, so enforcement must occur at the routing decision point.
  - Reuse established project knowledge about canonical version sources, active release surfaces, validation commands, install paths, rollback, and detected runtimes instead of rediscovering recurring procedures and context.
  - Have Daimon pass outcome-focused intent to Aion rather than prescribing the orchestration protocol.
  - Expose useful progress and evidence checkpoints during long-running direct or delegated work instead of disappearing outside the control plane.
  - After cancellation or host-channel loss, reconcile actual repository, binary, backup, and runtime state before reporting whether work occurred.
  - Executable regression acceptance: given a stored release map, detected-runtime inventory, and no material risk, `bump and install` must select the direct route despite touching many files, emit its route/rationale before mutation, reuse the known procedure, and expose validation/install checkpoints. An interruption case must cancel after installation, reconcile repository, binary, backup, and registry state, and report observed state rather than stale channel state; fail if unnecessary delegation/workflow occurs or route evidence is late.
- [ ] **Needs clarification — Pi capability provisioning.** Unas de las principales herramientas a utilizar es Pi.  Necesitamos que la instalacion en Pi sea capaz de instalar todo lo necesario para que Pi funcione como esperamos. Decide which packages/configuration are authoritative and whether PitCrew may access the network or mutate Pi configuration; the current installer intentionally does neither:
  - subagentes
  - to-do
  - ask question
  - mcp
