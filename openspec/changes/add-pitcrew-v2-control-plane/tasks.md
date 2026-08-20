<!--
Reconstructed from Engram observations on 2026-08-20.
Source observations:
  - pitcrew2/foundation-change (id 4441)
  - session summary (id 4440)
NOT byte-identical to the originals. Original was ~103 lines, 11 task
groups, ~40 sub-tasks. Group names follow the documented stacked-change
split; sub-tasks are reconstructed as a reasonable starting checklist and
should be reviewed before execution.
-->

# Tasks: add-pitcrew-v2-control-plane

This change is intentionally large for a foundation. It produces only
OpenSpec artifacts. When implementation begins, the 11 task groups below
will be split into stacked changes, each ≤ ~400 changed lines and ≤ ~60
review minutes:

- `add-pitcrew-store-and-domain`
- `add-pitcrew-cli-runtime`
- `add-pitcrew-subcommands`
- `add-pitcrew-installer`

---

## Group 1 — Repository bootstrap

- [ ] 1.1. Create the Go module `github.com/fmazzalomo/pitcrew` (Go 1.26+).
- [ ] 1.2. Add `MAXIMS.md` at the repo root.
- [ ] 1.3. Add `openspec/AGENTS.md`, this `proposal.md`, `design.md`, and `tasks.md`.
- [ ] 1.4. Configure `modernc.org/sqlite` (no CGO) and verify `go build ./...`.

## Group 2 — Identity and envelope

- [ ] 2.1. Implement `internal/ids` (workflow `wf-<24hex>`, work unit `wu-<24hex>`, claim_id `<32hex>`, secret `<32hex>`, RFC3339 UTC).
- [ ] 2.2. Implement `internal/envelope` (JSON output shape, exit codes 0-5).
- [ ] 2.3. Wire envelope into a `pitcrew --version` smoke test.

## Group 3 — SQLite store and migrations

- [ ] 3.1. Implement `internal/store` (PRAGMAs, single-writer connection).
- [ ] 3.2. Write schema v1 (workflows, events, plans, work_units, evidence, reviews, handles).
- [ ] 3.3. Implement migration runner with destructive-migration rejection.
- [ ] 3.4. Implement CAS-by-revision primitive; golden tests.

## Group 4 — Claim handles

- [ ] 4.1. Implement `internal/handles` (handle file format, lifecycle, expiry).
- [ ] 4.2. Implement `workflow claim-unit` and `workflow recover-unit-claim`.
- [ ] 4.3. Enforce file mode `0600`, directory mode `0700`, no symlinks.
- [ ] 4.4. Add operator-only `--print-claim-handle-secret-once` (revokes immediately, not in `--help`).

## Group 5 — Workflow aggregate and lifecycle

- [ ] 5.1. Implement `internal/workflow` aggregate with 9 states.
- [ ] 5.2. Implement `workflow new`, `workflow show`.
- [ ] 5.3. Implement `workflow explore`, `workflow spec`, `workflow design`.
- [ ] 5.4. Implement `workflow begin-implementation`, `workflow complete`, `workflow abandon`.

## Group 6 — Plan and work units

- [ ] 6.1. Implement `internal/plan` (plan validation, dependency cycles, admission, readiness, exceptions).
- [ ] 6.2. Implement `workflow plan`, `workflow approve-plan` (with `--approve-exception`).
- [ ] 6.3. Implement `workflow list-ready-units`.
- [ ] 6.4. Implement per-unit subcommands: `workflow claim-unit`, `workflow unit-tdd`, `workflow unit-review`, `workflow unit-complete`.

## Group 7 — Evidence and review

- [ ] 7.1. Define the TDD evidence shape (RED, GREEN, refactor, validation, changed paths).
- [ ] 7.2. Implement `internal/evidence` storage.
- [ ] 7.3. Enforce same-identity prohibition for Implementer vs Reviewer.

## Group 8 — CLI surface and dispatch

- [ ] 8.1. Implement `internal/cli` flag parsing and dispatch (long-form flags only).
- [ ] 8.2. Wire all 16 subcommands listed in `design.md` § 1.
- [ ] 8.3. Add `--help` epilogue: `Read the four maxims of the harness: pitcrew principles.`
- [ ] 8.4. Verify exit codes 0-5 against the documented contract.

## Group 9 — Maxims embedding

- [ ] 9.1. Implement `internal/maxims` with `//go:embed MAXIMS.md`.
- [ ] 9.2. Implement `pitcrew principles` (default verbatim, `--json` for structured array).
- [ ] 9.3. Verify the embedded text matches `MAXIMS.md` byte-for-byte at build time.

## Group 10 — Runtime installer

- [ ] 10.1. Write `scripts/install-templates.sh` in POSIX shell (NOT bash), idempotent.
- [ ] 10.2. Implement one prompt fragment per role + one agent-contract fragment.
- [ ] 10.3. Refuse to overwrite the Master fragment without `--overwrite`.
- [ ] 10.4. Roll back partial writes on failure.
- [ ] 10.5. Add POSIX shell smoke tests under `scripts/tests/`.

## Group 11 — Documentation

- [ ] 11.1. Add `AGENTS.md` at the repo root (runtime contract for agents).
- [ ] 11.2. Add `docs/cli-reference.md` with one section per subcommand.
- [ ] 11.3. Document the stacked-change split in `docs/contributing.md`.

---

## Notes for execution

- Each sub-task is intended to fit within a single work unit (≤ ~400
  changed lines, ≤ ~60 review minutes). If a sub-task grows past that,
  split it before claiming.
- The 11 groups above are NOT the execution plan. They will be split
  into stacked changes (`add-pitcrew-store-and-domain`,
  `add-pitcrew-cli-runtime`, `add-pitcrew-subcommands`,
  `add-pitcrew-installer`) when implementation begins.
- v1 (`agent-controller` in `$PATH`) stays usable for users who need
  it. There is no v1 → v2 data migration.
