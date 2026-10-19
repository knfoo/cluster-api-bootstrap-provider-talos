# ADR 0001: Phase 0 baseline for knfoo/cluster-api-bootstrap-provider-talos

- **Status:** accepted (2026-10-19)
- **Context:** This repo is a true GitHub fork of siderolabs maintained by the self-improving fork-maintenance
  loop (blueprint, section 11). Upstream sync target: https://github.com/siderolabs/cluster-api-bootstrap-provider-talos (branch main).
  A trusted baseline is required before any automated proposal can be evaluated.
- **Decision:** Baseline pinned to upstream tag 411408aad84277c1f5946b99b309ee8a99819ead (fast-forwarded on 2026-10-19).
  Toolchain from go.mod: Go 1.26.5, CAPI v1.12.11, controller-runtime v0.23.3, client-go v0.35.3, envtest K8s 1.35.x.
  Test suite: vacuous green with 0 tests passed (only TestIntegration exists, which is tier-3 e2e).
  Documented in ADR-0003.
- **CAPI position:** CABPT on v1.12.11, latest CAPI release v1.14.3 (minor lag: v1.12 vs v1.14), sibling fork CACPPT on v1.14.2 (mismatch).
- **Evidence:** `_out/baseline.json`, `evidence/baseline/baseline.json` (kit v3, committed), `evidence/sync/outcome.json` (sync-dryrun noop), `evidence/capi/capi-watch.json` (CAPI position with minor lag).
- **Consequences:** Phase 1 (sensing + dry-run syncs) may start. CAPI alignment (v1.12 vs v1.14 mismatch + minor lag) is first Phase 1 agenda item.
- **Go/no-go:** GO. Kit intact, baseline reproducible (with documented vacuous green limitation per ADR-0003), divergence ledger enforced, dry-run sync logic verified (PR creation requires PAT for GraphQL - Phase 1 task), CAPI position known with minor lag. All Phase 0 criteria met.
