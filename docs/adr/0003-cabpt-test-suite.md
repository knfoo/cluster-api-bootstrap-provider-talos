# ADR 0003: CABPT test suite limitation for Phase 0 baseline

- **Status:** superseded by ADR-0004 (2026-10-19). Never accepted: the vacuous
  green it described came from tracking the deprecated siderolabs upstream.
- **Context:** CABPT's test suite consists solely of `TestIntegration` in `internal/integration/`, which:
  1. Requires `TalosVersion` to be non-empty (set by build process, empty in test env)
  2. Is a tier-3 e2e test that provisions real infrastructure
  3. Times out when run with envtest (takes >2 minutes, requires k8s cluster)
  
  With the default baseline `-skip '^TestIntegration$'` pattern, CABPT has **0 passing tests**, producing a vacuous green.
  
  Unlike CACPPT (which has webhook envtest tests that pass in ~10s), CABPT has no controller/envtest tests that run locally.
- **Decision:** For Phase 0:
  - Accept that CABPT baseline passes with 0 tests (vacuous green is the lesser evil vs. broken baseline)
  - Document this limitation in ADR-0003
  - Track CAPI position separately via `hack/capi-watch.sh`
  - Revisit in Phase 2: either contribute a fix upstream or create a lightweight envtest test
- **Evidence:** baseline.sh with `-skip '^TestIntegration$'` produces: `packages_passed=1, tests_passed=0, duration_s=8` (vacuous)
- **Consequences:** CABPT's test suite does not guard against regressions in Phase 0. Contract sync (CAPI bump) validation relies on `capi-watch.sh` only.
- **Revisit date:** Phase 2
