# Divergence Ledger — cluster-api-bootstrap-provider-talos

- **Repo:** knfoo/cluster-api-bootstrap-provider-talos
- **Upstream (the `upstream` remote; the diff rule is relative to THIS repo):**
  https://github.com/siderolabs/cluster-api-bootstrap-provider-talos
  (branch: main — alive and ahead of its releases; v0.7.0-alpha / CAPI v1.12 + v1beta2 line)
- **Upstream push:** disabled (`git remote set-url --push upstream no-push`)
- **Baseline date:** 2026-10-19
- **Measured at baseline:** 0 commits ahead / 7 behind — caught up via fast-forward
- **Evidence:** ADR-0001, ADR-0003

## Standing rule

Every commit that lands in `upstream/main..HEAD` must appear in the table below
on the day it lands, with an ADR. The sync bot rebases this series every cycle.
If upstream absorbs a patch, drop it, close the ADR, and record the drop here.
A commit that is not in this table is a bug — either it shouldn't exist, or this
file is out of date.

## Ledger

| # | Patch | Title | ADR | Category | Absorbed upstream? |
|---|---|---|---|---|---|
| 1 | `66a777a` | docs: record kit commits in divergence ledger | ADR-0001 | tooling | n/a |
| 2 | `0719b9a` | docs(phase0): add ADR-0001 (proposed) | ADR-0001 | tooling | n/a |
| 3 | `714ca95` | chore(phase0): add fork-maintenance kit (baseline, sync dry-run, workflows) | ADR-0001 | tooling | n/a |
| 4 | `b5956c6` | chore(phase0): fix fork-maintenance kit - baseline | ADR-0001 | tooling | n/a |
| 5 | `47066e0` | chore(phase0): kit v3 (json guards, sync on main, capi-watch, ledger-check) | ADR-0001 | tooling | n/a |


## Categories

- **tooling** — fork-maintenance additions only (`hack/`, sync workflows, this
  file, ADRs). Never touches provider code; merge conflicts with upstream
  should be impossible.
- **carry** — a fix/feature we run ahead of upstream. MUST link the upstream
  PR/issue proposing it; entry deleted when upstream merges it.
- **local** — deliberate fork-only behavior. Must state why it cannot go
  upstream; be most suspicious of this category.
- **revert** — an upstream change we temporarily refuse. Requires a revisit date.

## Not in scope

Upstream's own changes, anything not on `main`, and local scratch branches —
though stray branches should be enumerated and deleted, not left to rot.
