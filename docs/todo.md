# TO-DO List - Open Issues

Let's store all the TO-DO in a single place

## PitCrew en General

- [ ] Desde que aparecio Aion, la comunicacion de Daimon desaparecio.  Todo funciona, pero Daimon "esta mudo".
- [ ] Cuando habla Daimon quiero que su dialogo hacia el usuario siempre este marcado porque inicia con un Emoji que lo distingue, lo mismo con Aion.
- [ ] Pitcrew tiene que tener en la herramienta disponible toda la informacion del proyecto, la especifica que nos sirve para trabajar y que workflow a workflow estamos yendo a leer de nuevo.  Quiero que la cli sea capaz de guardar y retornar esa informacion para que cualquier agente que la necesite la pueda solicitar directamente al control plane.  Quiero que cuando PitCrew detecta que esta trabajando en un nuevo repo o proyecto, analiza el repositorio en busca de:
  - Stack tecnico
  - Runtime
  - Despliegue
  - Patrones de arquitectura
  - Documentacion interna del repo
  - Cualquier cosa necesaria para poder trabajar en SDD en el repo.
- Necesitamos un nuevo subagente especializado en inicializar el proceso de SDD y sea capaz de obtener toda la informacion anteriormente mencionada.  Este subagente es disparado por Aion cuando se detecta que la informacion no esta completa.

## TUI

- [ ] Viendo el detalle del Workflow, todos los agentes registran un trabajo recien cuando lo terminan.  Me gustaria que cada tarea que el arnes comienza, se registre en estado Pending o In Progress y que cuando termina recien ahi se marque con el estado de ahora.
- [ ]

## Installation / Upgrade

- [ ] Make `bump and install` a proportional, observable recurring operation:
  - Treat Maxim II as an enforced invariant: a maxim that lower-level heuristics can displace is decoration. File-count and routing defaults must never override the least-demanding sufficient path.
  - Require every stronger route to name the concrete protected constraint and explain why the simpler path is materially insufficient.
  - Make the selected route and rationale observable before substantial work consumes time; documentation alone is insufficient, so enforcement must occur at the routing decision point.
  - Reuse established project knowledge about canonical version sources, active release surfaces, validation commands, install paths, rollback, and detected runtimes instead of rediscovering recurring procedures and context.
  - Have Daimon pass outcome-focused intent to Aion rather than prescribing the orchestration protocol.
  - Expose useful progress and evidence checkpoints during long-running direct or delegated work instead of disappearing outside the control plane.
  - After cancellation or host-channel loss, reconcile actual repository, binary, backup, and runtime state before reporting whether work occurred.
  - Executable regression acceptance: given a stored release map, detected-runtime inventory, and no material risk, `bump and install` must select the direct route despite touching many files, emit its route/rationale before mutation, reuse the known procedure, and expose validation/install checkpoints. An interruption case must cancel after installation, reconcile repository, binary, backup, and registry state, and report observed state rather than stale channel state; fail if unnecessary delegation/workflow occurs or route evidence is late.
- [ ] Unas de las principales herramientas a utilizar es Pi.  Necesitamos que la instalacion en Pi sea capaz de instalar todo lo necesario para que Pi funcione como esperamos:
  - subagentes
  - to-do
  - ask question
  - mcp
