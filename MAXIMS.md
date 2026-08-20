<!--
Reconstructed from Engram observations on 2026-08-20.
Source observations:
  - pitcrew2/principles (id 4439)
  - pitcrew2/maxims (id 4443)
NOT byte-identical to the originals. The four maxims themselves are verbatim
from the durable memory record. The surrounding scaffolding is reconstructed
to match the documented layout (banners, separators, embed contract).
-->

# MAXIMS OF PITCREW

```
================================================================================
  THE FOUR MAXIMS OF THE HARNESS
  These four maxims are the operating system of every agent and every operator
  who touches pitcrew. They are subordinate to no other rule. They are changed
  only by an explicit edit to this file, which becomes its own OpenSpec change.
================================================================================
```

## Maxim I — Languages Have Two Surfaces

> **Technical English internally; the user's language externally.**

All code, identifiers, JSON keys, error messages, log lines, tests, comments,
documentation, and OpenSpec artifacts are written in technical English. The
control plane emits English to agents and operators. The user-facing reply
(the agent's natural-language response) is in the user's language. Do not
localize the system surface; localize the conversation around it.

## Maxim II — The Harness Serves the Result

> **The harness serves the result, never the other way around.**

The control plane is an aid to the user's expected outcome. It is never an
obstacle. Defaults are chosen to be safe, but every default has a documented,
auditable escape hatch. When a rule blocks a legitimate delivery, the rule
bends.

## Maxim III — The Usual Path, Not the Only Path

> **The harness is the usual path, not the only path.**

For most work, invoke the control plane and let it record evidence. For trivial
work (typo fixes, one-line edits, well-understood local refactors), the Master
agent is invited to skip the harness when the process cost exceeds the process
value.

## Maxim IV — Short Scope, Easy to Complete, Always

> **Short scope, easy to complete, always.**

Tasks and changes are sized so a reviewer can finish them in one sitting.
Large ambitions are decomposed into stacked changes. A change that grows past
its budget is split, not merged by exception.

```
================================================================================
  END OF MAXIMS
  Read them every morning. Quote them in design reviews. Embed them in prompts.
  Embed this file in the binary. They cannot drift; they cannot be skipped.
================================================================================
```
