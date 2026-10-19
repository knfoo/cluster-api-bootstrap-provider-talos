# ADR 0005: Back to siderolabs; baseline known-red until TestIntegration runs in CI

- **Status:** accepted (2026-10-19)
- **Context:** ADR-0004 moved this fork to sidero-community. On 2026-10-19 the
  siderolabs Slack community advised against building on sidero-community, so both
  forks move back to the siderolabs repos. siderolabs CABPT `main` is v0.7.0-alpha.3
  (CAPI v1.12.11, Talos 1.14). Its only test is `TestIntegration`, which needs a real
  cluster and a release build (`internal/integration/README.md`), so the baseline
  skips it and zero tests pass.
- **Decision:**
  - The `upstream` remote and `UPSTREAM_URL` point at
    https://github.com/siderolabs/cluster-api-bootstrap-provider-talos.git, branch
    `main` (not `release-0.6`, which siderolabs CACPPT pins). Our tooling commits were
    replayed onto `upstream/main` and `main` was force-pushed. The sidero-community
    history is kept locally as `backup/sidero-community-main`.
  - **Known-red:** the zero-test guard would fail every baseline. Instead the repo
    variable `KNOWN_RED_ALLOW=ADR-0005` makes `baseline.sh` record such a run as
    `"tests": "known-red"` (never `green`), with every other guard still enforced.
    `known_red_allow` is a new field in `baseline.json`, added by this ADR.
  - Cause: upstream ships no unit/envtest tests. Scope: all of CABPT's provider
    code is unverified by the baseline; regressions surface only through CACPPT's
    suite, which exercises CABPT's API types.
- **Revisit:** at the start of the later phase that stands up a cluster in CI
  (kind + CAPD) to run `TestIntegration` (PLAN.md, Phase 1 scope additions). Lift
  the known-red, unset `KNOWN_RED_ALLOW` and add must-run tests then. Landing the
  template patch adds `pkg/templatepatch` tests sooner, but they cover only our patch.
- **Consequences:** ADR-0004 is superseded. CAPI position is v1.12 here vs v1.10 in
  CACPPT vs v1.14 latest: real drift for Phase 1.
