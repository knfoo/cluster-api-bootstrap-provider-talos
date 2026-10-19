# ADR 0001: Phase 0 baseline for knfoo/cluster-api-bootstrap-provider-talos

- **Status:** accepted (2026-10-19; re-baselined the same day per ADR-0004)
- **Context:** This repo is a fork maintained by the self-improving fork-maintenance
  loop (blueprint, section 11). It has two upstreams: the provider upstream
  https://github.com/sidero-community/cluster-api-bootstrap-provider-talos (git sync,
  branch `main`; was siderolabs until ADR-0004) and kubernetes-sigs/cluster-api
  (contract sync, via go.mod). A trusted baseline is required before any automated
  proposal can be evaluated.
- **Decision:** Baseline is `upstream/main` at e624d69b5e1f9f0e99cc9830c216d4459d25e032
  (upstream tag v0.9.0), re-baselined on 2026-10-19. Divergence: all commits tooling,
  all trailered. Toolchain from go.mod: Go 1.26.5, CAPI v1.14.2, controller-runtime
  v0.24.1, client-go v0.36.3, envtest K8s 1.36.x.
  Suite green twice locally and in CI: 136 tests passed in 7 packages, must-run tests
  passed. `TestIntegration` is skipped: it needs a real cluster and a release build.
  CAPI position: v1.14.2 here, v1.14.3 latest release, v1.14.2 in CACPPT (no lag,
  no mismatch).
- **Evidence:** `evidence/baseline/baseline.json` (CI run 39277969041);
  `evidence/sync/outcome.json` (no-op, run 39290560574);
  `evidence/sync/forced-outcome.json` (forced exercise: clean, draft PR #2 opened by
  the sync App, `baseline` green on it, run 39289900981);
  `evidence/capi/capi-watch.json` (CI run 39281640277). All recorded in the repo's
  RuVector store.
- **Consequences:** Phase 1 (sensing, dry-run syncs, CAPI bump proposals) may start.
- **Go/no-go:** GO. Kit self-check passes locally and in CI; baseline green twice
  locally and in CI with must-run and ratchet guards; `ledger-check` green; no-op and
  forced PR paths exercised without manual steps in the job, and the sync PR got CI;
  CAPI position known with no cross-fork mismatch; evidence committed and in
  RuVector. Not yet exercised: the conflicts-to-issue path (both forced runs merged
  cleanly).
