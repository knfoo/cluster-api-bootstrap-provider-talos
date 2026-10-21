# ADR 0007: Track the latest Cluster API release

- **Status:** accepted (2026-10-21; owner decision Q9 in the harness review
  `docs/research/review-2026-10-21-independent.md`)
- **Context:** The fork's objective is to follow kubernetes-sigs/cluster-api at its
  latest release (v1.14.3 today). siderolabs `main` (v0.7.0-alpha.3) is on
  v1.12.11. Each CAPI minor the harness applies diverges from upstream until
  upstream catches up. The v1.13 step is a dependency bump; the v1.14 step removes
  `sigs.k8s.io/cluster-api/bootstrap/util`, which `controllers/talosconfig_controller.go`
  uses for `ConfigOwner`/`GetConfigOwner`. ADR-0006 (integration data race fix)
  exists on `phase1/w0-cabpt-e2e`; the number after it is this one.
- **Decision:**
  - CAPI-bump commits, and the changes a bump requires, carry
    `Divergence: local; ADR-0007`. One CAPI minor per commit (v1.12 → v1.13 →
    v1.14.3); the baseline is green after each.
  - For the removed `bootstrap/util`, the fork copies `ConfigOwner` and
    `GetConfigOwner` into `internal/configowner/` with their Apache-2.0 header and
    origin (owner decision Q4a, checklist 4.C1 option b) instead of importing
    `bootstrap/kubeadm/reconcilers/kubeadmconfig`, so no CABPK dependency family
    enters the module graph.
  - Behaviour-changing adoptions (in-place update extension, template metadata,
    CRD storage migration) get their own ADR.
  - When upstream merges an equivalent change, upstream's version is preferred and
    ours is dropped; the revert carries the same trailer.
- **Evidence:** per bump commit, `evidence/baseline/` and the harness run record.
- **Consequences:** `hack/ledger-check.sh` accepts the bump commits; the spike
  commit `ba87829` (option a, one step to v1.14.3) is superseded and stays
  unledgered on its spike branch until that branch is deleted. The CACPPT fork
  carries the same rule as its ADR-0004.
