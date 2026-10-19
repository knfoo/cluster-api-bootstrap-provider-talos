# ADR 0001: Phase 0 baseline for knfoo/cluster-api-bootstrap-provider-talos

- **Status:** proposed (re-baselined onto siderolabs 2026-10-19 per ADR-0005;
  → accepted once CI baseline and a dry-run sync pass on the new base)
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
- **Evidence:** `evidence/baseline/`; `evidence/sync/` and `evidence/capi/` predate
  the re-baseline and get refreshed from CI.
- **Consequences:** Phase 1 (sensing, dry-run syncs, CAPI bump proposals) may start
  only after this ADR is accepted. The known-red stays until `TestIntegration` runs
  in CI (later phase, ADR-0005).
- **Go/no-go:** pending: CI baseline (known-red) and a no-op dry-run sync on the
  siderolabs base. The forced PR path was proven on 2026-10-19 (PR #2).
