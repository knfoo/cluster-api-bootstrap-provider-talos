# ADR 0001: Phase 0 baseline for knfoo/cluster-api-bootstrap-provider-talos

- **Status:** accepted (2026-10-19; re-baselined onto siderolabs the same day, ADR-0005)
- **Context:** This repo is a fork maintained by the self-improving fork-maintenance
  loop (blueprint, section 11). It has two upstreams: the provider upstream
  https://github.com/siderolabs/cluster-api-bootstrap-provider-talos (git sync, branch
  `main`; briefly sidero-community, ADR-0004/0005) and kubernetes-sigs/cluster-api
  (contract sync, via go.mod). A trusted baseline is required before any automated
  proposal can be evaluated.
- **Decision:** Baseline is `upstream/main` at 411408a (upstream tag v0.7.0-alpha.3),
  re-baselined on 2026-10-19. Divergence: all commits tooling, all trailered.
  Toolchain from go.mod: CAPI v1.12.11, controller-runtime v0.23.3, envtest K8s 1.35.x.
  Suite **known-red** (ADR-0005): upstream has no runnable test besides
  `TestIntegration`, so zero tests pass; every other guard passes.
  CAPI position: v1.12 here, v1.10 in CACPPT, v1.14 latest release.
- **Evidence:** `evidence/baseline/baseline.json` (CI run 39298759287, known-red);
  `evidence/sync/outcome.json` (no-op on siderolabs, run 39298881495);
  `evidence/sync/forced-outcome.json` (forced exercise on the earlier base: clean,
  draft PR #2 opened by the sync App, `baseline` green on it; the mechanism does not
  depend on the upstream); `evidence/capi/capi-watch.json` (run 39298956637, opened
  `capi-lag` #3). All recorded in the repo's RuVector store.
- **Consequences:** Phase 1 (sensing, dry-run syncs, CAPI bump proposals) may start.
  The known-red stays until `TestIntegration` runs in CI (later phase, ADR-0005).
- **Go/no-go:** GO. Kit self-check passes locally and in CI; baseline known-red twice
  locally and in CI, tied to ADR-0005 with cause, scope and revisit point, every
  other guard passing (no silent skip, no vacuous green); `ledger-check` green; no-op
  sync on siderolabs and the forced PR path exercised, and the sync PR got CI; CAPI
  position known and the cross-fork mismatch explained (v1.12 here, v1.10 in CACPPT,
  v1.14 latest), tracked in `capi-lag` #3; evidence committed and in RuVector. Not
  yet exercised: the conflicts-to-issue path.
