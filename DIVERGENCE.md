# Divergence Ledger — cluster-api-bootstrap-provider-talos

- **Repo:** knfoo/cluster-api-bootstrap-provider-talos
- **Provider upstream (`upstream` remote):** https://github.com/sidero-community/cluster-api-bootstrap-provider-talos, branch `main` (siderolabs until ADR-0004)
- **Contract upstream:** kubernetes-sigs/cluster-api (tracked by `hack/capi-watch.sh`)
- **Upstream push:** disabled (`git remote set-url --push upstream no-push`)
- **Baseline date:** 2026-10-19 (re-baselined onto sidero-community v0.9.0 the same day, ADR-0004)
- **Measured at baseline:** siderolabs: 0 ahead / 7 behind → fast-forwarded; sidero-community: tooling replayed onto `upstream/main`
- **Evidence:** ADR-0001, ADR-0004, `evidence/`

## Standing rule (enforced by CI)

Every non-merge commit in `upstream/main..HEAD` carries a trailer:

    Divergence: <tooling|local|revert>; <ADR-NNNN>

`hack/ledger-check.sh` fails CI if one is missing or uses an unknown category.
Commits that predate the rule are listed in `.divergence-allow`. The table below
is generated with `hack/ledger-check.sh --render`; git is the source of truth,
this table is a convenience view and may lag by a commit.

In Phase 0/1, upstream is merged into dry-run branches only; `main` is never
synced automatically. Rebase-vs-merge for promotion is decided in Phase 2 (own ADR).

If upstream fixes something we carry as `local`, prefer upstream's version, drop
our patch (the revert commit carries the same trailer), close the ADR, and note
the drop below.

## Ledger

| # | Patch | Title | Category | ADR |
|---|---|---|---|---|
| 1 | `27673c7` | docs: record kit commits in divergence ledger | tooling | ADR-0001 |
| 2 | `5f9bc7b` | docs(phase0): add ADR-0001 (proposed) | tooling | ADR-0001 |
| 3 | `c140baf` | chore(phase0): add fork-maintenance kit (baseline, sync dry-run, workflows) | tooling | ADR-0001 |
| 4 | `4965ea7` | chore(phase0): fix fork-maintenance kit - baseline | tooling | ADR-0001 |
| 5 | `2eef9b3` | chore(phase0): kit v3 (json guards, sync on main, capi-watch, ledger-check) | tooling | ADR-0001 |
| 6 | `8ecdfbc` | docs(phase0): update ADRs and ledger | tooling | ADR-0001 |
| 7 | `0b6ba30` | chore(phase0): fix sync-dryrun workflow to use GITHUB_TOKEN | tooling | ADR-0001 |
| 8 | `ddf0a73` | docs(phase0): update ADR-0001 to accepted with Day 4 evidence and go/no-go note | tooling | ADR-0001 |
| 9 | `037cceb` | fix(phase0): pin gh to our own repo, restore the GitHub App token for sync | tooling | ADR-0001 |

## Dropped / absorbed upstream

| Patch | Superseded by upstream commit | Date |
|---|---|---|

## Categories

- **tooling** — fork-maintenance only (`hack/`, our workflows, this file, ADRs,
  `evidence/`, `.divergence-allow`). Never touches provider code.
- **carry** — unused in this project. It requires an upstream PR/issue link,
  and upstream contributions are out of scope.
- **local** — deliberate fork-only behavior. Must state why it won't go upstream:
  it can't (repo-specific), or contributing is out of scope.
  Be most suspicious of this category.
- **revert** — an upstream change we temporarily refuse. Requires a revisit date.

## Not in scope

Upstream's own changes and anything not on `main`. Stray branches are enumerated
and deleted, not left to rot.
