# ADR 0004: Track sidero-community as CABPT's provider upstream

- **Status:** superseded by ADR-0005 (2026-10-19): back to siderolabs.
- **Context:** Phase 0 started with `siderolabs/cluster-api-bootstrap-provider-talos`
  as the provider upstream. The day-4 review found that:
  - siderolabs CABPT is deprecated (`855be5e`, "no longer actively developing").
  - CACPPT's `go.mod` already `replace`s the CABPT module with
    `github.com/sidero-community/cluster-api-bootstrap-provider-talos v0.9.0`, so
    this fork was not what CACPPT builds against.
  - The recorded "CAPI v1.14 vs v1.12 mismatch" was an artifact of that choice:
    sidero-community v0.9.0 is on CAPI v1.14.2, the same as CACPPT.
  - The siderolabs tree had no runnable tests besides `TestIntegration`, so the
    baseline was a vacuous green (ADR-0003).
- **Decision:** The `upstream` remote and `UPSTREAM_URL` point at
  https://github.com/sidero-community/cluster-api-bootstrap-provider-talos.git,
  branch `main` (= tag v0.9.0, `e624d69`). sidero-community branched from
  siderolabs at `855be5e`, so the two siderolabs-only commits (`bdc9245` Talos 1.14
  contract, `411408a` v0.7.0-alpha.3) are dropped; sidero-community carries its own
  Talos 1.14 work. Our tooling commits were replayed onto `upstream/main` and
  `main` was force-pushed; commits that predated the trailer rule gained a
  `Divergence: tooling; ADR-0001` trailer during the replay, so `.divergence-allow`
  is empty. The pre-switch history is kept locally as `backup/siderolabs-main`.
- **Evidence:** `evidence/baseline/baseline.json`: 136 tests passed in 7 packages,
  CAPI v1.14.2, must-run tests passed. `TestIntegration` stays skipped: it needs a
  real cluster and a release build (`internal/integration/README.md`).
- **Consequences:** ADR-0003 is superseded. Both forks are on the same CAPI minor;
  the Phase 1 question becomes whether CACPPT's `replace` should point at this fork.
  Upstream issues are disabled on sidero-community; irrelevant, since upstream
  contributions are out of scope.
