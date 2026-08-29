# TO-DO List - Open Issues

Let's store all the TO-DO in a single place

## PitCrew en General

- [x] **Delivered — Daimon feedback relay.** Desde que aparecio Aion, la comunicacion de Daimon desaparecio. Todo funciona, pero Daimon "esta mudo". Delivered by PR #62; the named Pi supervisor-flow verifier is `scripts/tests/pi-supervisor-runtime.sh`.
- [ ] **P3 — User-channel role identity.** Cuando habla Daimon quiero que su dialogo hacia el usuario siempre este marcado porque inicia con un Emoji que lo distingue, lo mismo con Aion. Clarify how Aion attribution remains compatible with the `user ↔ Daimon ↔ Aion` boundary before implementation.
- [ ] **In flight — Durable project context and SDD initialization.** Pitcrew tiene que tener en la herramienta disponible toda la informacion del proyecto, la especifica que nos sirve para trabajar y que workflow a workflow estamos yendo a leer de nuevo.  Quiero que la cli sea capaz de guardar y retornar esa informacion para que cualquier agente que la necesite la pueda solicitar directamente al control plane.  Quiero que cuando PitCrew detecta que esta trabajando en un nuevo repo o proyecto, analiza el repositorio en busca de:
  - Stack tecnico
  - Runtime
  - Despliegue
  - Patrones de arquitectura
  - Documentacion interna del repo
  - Cualquier cosa necesaria para poder trabajar en SDD en el repo.
- Necesitamos un nuevo subagente especializado en inicializar el proceso de SDD y sea capaz de obtener toda la informacion anteriormente mencionada.  Este subagente es disparado por Aion cuando se detecta que la informacion no esta completa.
- [ ] **P1 — Universal, economical delivery traceability.** Every harness-managed delivery MUST leave a durable, searchable trace in the Control Plane regardless of whether Aion selects direct inline, delegated direct, or full workflow execution. Today only the highest-complexity route reliably leaves that trace.
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
8. - [ ] **P0 — Enforce the implementation admission gate.** Enforce `workflow begin-implementation` before any unit claim. `list-ready-units` and `claim-unit` must not allow work while the workflow remains `plan_approved`, so a skipped transition cannot remain latent until final-unit completion.

## TUI

- [ ] **Acceptance surface — Universal delivery traceability.** Viendo el detalle del Workflow, todos los agentes registran un trabajo recien cuando lo terminan. Me gustaria que cada tarea que el arnes comienza, se registre en estado Pending o In Progress y que cuando termina recien ahi se marque con el estado de ahora. Solve this through the universal delivery trace rather than with per-agent patches or a second representation of status.

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
