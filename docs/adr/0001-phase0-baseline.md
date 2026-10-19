# ADR 0001: Phase 0 baseline for knfoo/cluster-api-bootstrap-provider-talos

- **Status:** proposed (→ accepted on day 4)
- **Context:** This repo is a true GitHub fork of siderolabs maintained by the self-improving fork-maintenance
  loop (blueprint, section 11). Upstream sync target: https://github.com/siderolabs/cluster-api-bootstrap-provider-talos (branch main).
  A trusted baseline is required before any automated proposal can be evaluated.
- **Decision:** Baseline pinned to upstream tag 411408aad84277c1f5946b99b309ee8a99819ead (fast-forwarded on 2026-10-19).
  Toolchain from go.mod: Go 1.26.5, CAPI v1.12.11, controller-runtime v0.23.3, client-go v0.35.3, envtest K8s 1.35.x.
  Test suite: vacuous green with 0 tests passed (only TestIntegration exists, which is tier-3 e2e).
  Documented in ADR-0003.
- **CAPI position:** CABPT on v1.12.11, latest CAPI release v1.14.3 (minor lag: v1.12 vs v1.14), sibling fork CACPPT on v1.14.2 (mismatch).
- **Evidence:** `_out/baseline.json`, `evidence/baseline/baseline.json` (kit v3, committed).
- **Consequences:** Phase 1 (sensing + dry-run syncs) may start only after this ADR
  is `accepted` and the ledger is written. CAPI alignment (v1.12 vs v1.14 mismatch) is first Phase 1 agenda item.
