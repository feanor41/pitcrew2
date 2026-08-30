# Spec: tui-inspection

## Purpose

Define the central-state, read-only terminal experience for inspecting workflow history and its durable results.

## Requirements

### Requirement: Unified delivery collection with discriminated detail

The read-only TUI SHALL label the shared collection `Deliveries` and list/search
both `dl-*` direct traces and `wf-*` workflows from the history projection. A
direct detail SHALL show goal, route, truthful status, timestamps, bounded
summary, and next action without workflow lifecycle stages or units. A workflow
detail SHALL retain its existing cockpit and expose route `full_workflow`.

#### Scenario: Direct detail stays lightweight

- GIVEN a direct trace and a full workflow match the same literal query
- WHEN the operator selects each result
- THEN each SHALL appear once
- AND the direct result SHALL show no synthetic Explore, Spec, Design, Plan,
  Build, Review, or Units branches

### Requirement: Embedded read-only session

`pitcrew tui` MUST inspect persisted state in the existing process. It MUST NOT invoke a subprocess, execute mutating workflow commands, record control-plane activity, run migrations, change schema objects, or insert, update, or delete persisted rows.

The TUI SHALL intentionally consume the full audit history projection rather
than a bounded CLI coordination, phase, unit, or aggregate view. This is the
documented inspection escape hatch required to navigate every durable record;
it SHALL NOT change the bounded default guidance for orchestration agents.

#### Scenario: Existing logical state remains invariant
- GIVEN an initialized project with captured schema, rows, workflow state, and activities
- WHEN a user browses, searches, and exits the TUI
- THEN all captured state SHALL remain unchanged

#### Scenario: TUI retains complete audit inspection

- GIVEN a workflow with normative artifacts, multiple unit results, and activities
- WHEN the operator opens its TUI detail
- THEN every durable record SHALL remain navigable
- AND no bounded CLI view SHALL replace or truncate the audit detail

### Requirement: Shared project history

The TUI MUST resolve the current checkout's canonical project identity and open its central state database read-only. It MUST NOT initialize central paths or checkout-local state. Main and linked worktrees MUST show the same history. Its workflow grid MUST order by `created_at` descending then id ascending and mark columns for start time, short name, and state. Selection MUST expose workflow metadata and every aggregate, event, artifact, plan, unit, dependency, exception, TDD evidence, review, and activity. Activities MUST be chronological with actor, timestamp, and action. Records MUST remain fully inspectable beyond the visible region.

The synopsis SHALL reuse the shared correction projection to show policy awareness, rounds used/allowed, latest unresolved blocker, authority, and exactly one contextual next action. History SHALL label aggregate correction start and authorization activities and SHALL never display handle paths, hashes, or secrets.

#### Scenario: Historical workflow inspection
- GIVEN active, completed, and abandoned workflows in one project
- WHEN history opens
- THEN every workflow and related review data SHALL be inspectable in required grid order

#### Scenario: Linked worktrees share inspection
- GIVEN an initialized main checkout and linked worktree
- WHEN the TUI starts from either checkout
- THEN each SHALL display the same central workflows without mutation

#### Scenario: Long evidence remains reachable
- GIVEN evidence longer than its visible region
- WHEN the user navigates between both boundaries
- THEN every part SHALL become visible without changing persisted state

#### Scenario: Activity opens its exact result
- GIVEN activities linked to specification Gherkin, design, plan, evidence, review, or another durable result
- WHEN an activity is selected
- THEN the exact associated record SHALL open focused and fully inspectable

#### Scenario: Legacy history is honest
- GIVEN a historical unnamed workflow with missing activity coverage
- WHEN its detail is inspected
- THEN its deterministic goal-derived name SHALL be marked as fallback
- AND available legacy records SHALL be shown without inventing actors, timestamps, or activities

### Requirement: Adaptive modern presentation

The TUI MUST render a responsive header with large `PitCrew2`, smaller `Control Plane`, and the current canonical version in a distinct color. All three MUST remain visible, including narrow layouts. The TUI MUST show hierarchy, status, context, grid markers, focus, and key hints without relying on color. Focused records and evidence MUST have non-color markers. Resize MUST preserve the exact record and a valid evidence position.

#### Scenario: Branded history grid
- GIVEN sufficient terminal dimensions
- WHEN history renders
- THEN the large name, smaller subtitle, distinctly colored version, and marked grid columns SHALL be visible

#### Scenario: Version remains visible in a narrow layout
- GIVEN a valid narrow terminal layout
- WHEN the header reflows
- THEN the current canonical version SHALL remain visible in its distinct color

#### Scenario: Terminal resize
- GIVEN a focused record with navigated evidence
- WHEN terminal dimensions change
- THEN content SHALL reflow without overlap or stale regions
- AND the same record SHALL remain focused at a valid evidence position

#### Scenario: Constrained terminal
- GIVEN dimensions too small for the inspection layout
- WHEN the TUI renders
- THEN it SHALL show minimum-size guidance and keep quit and resize handling available

#### Scenario: Focus survives color loss
- GIVEN terminal output where color distinctions are unavailable
- WHEN a record or evidence region has focus
- THEN an explicit marker SHALL identify the focused target
