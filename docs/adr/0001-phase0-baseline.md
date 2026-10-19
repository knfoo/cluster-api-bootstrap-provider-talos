# ADR 0001: Phase 0 baseline for knfoo/cluster-api-bootstrap-provider-talos

- **Status:** proposed (re-baselined 2026-10-19 per ADR-0004; → accepted once the
  forced sync exercise has passed)
- **Context:** This repo is a fork maintained by the self-improving fork-maintenance
  loop (blueprint, section 11). It has two upstreams: the provider upstream
  https://github.com/sidero-community/cluster-api-bootstrap-provider-talos (git sync,
  branch `main`; was siderolabs until ADR-0004) and kubernetes-sigs/cluster-api
  (contract sync, via go.mod). A trusted baseline is required before any automated
  proposal can be evaluated.
- **Decision:** Baseline is `upstream/main` at e624d69b5e1f9f0e99cc9830c216d4459d25e032
  (upstream tag v0.9.0), re-baselined on 2026-10-19. Divergence: 9+ commits, all
  tooling, all trailered. Toolchain from go.mod: Go 1.26.5, CAPI v1.14.2,
  controller-runtime v0.24.1, client-go v0.36.3, envtest K8s 1.36.x.
  Suite green locally: 136 tests passed in 7 packages, must-run tests passed.
  CAPI position: v1.14.2 here, v1.14.3 latest release, v1.14.2 in CACPPT (no mismatch).
- **Evidence:** `evidence/baseline/`; `evidence/sync/` and `evidence/capi/` predate
  the re-baseline and get refreshed from CI.
- **Consequences:** Phase 1 (sensing, dry-run syncs, CAPI bump proposals) may start
  only after this ADR is accepted.
- **Go/no-go:** pending: CI baseline on the re-baselined `main`, and the forced
  sync exercise (draft PR via the App token, `baseline` running on it).
