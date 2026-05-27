# Spec audit – 2026-05-26 (v2, scoped to iris-reload)

## Scope

Scoped audit on the `iris-reload` capability, triggered by archive of the `refuse-cli-self-reload` change. Other 18 capabilities covered by prior manual audit at `.workflow/audits/2026-05-26/index.md` (concluded healthy).

## Summary

- Capability: iris-reload
- Requirements: 13
- Coverage: 13 implemented, 13 tested (all 13 requirements have at least one direct test; a handful of individual spec scenarios are covered indirectly via shared code paths — see "Test-coverage gaps" in the module report)
- Gaps: 0 behavioral, 0 contradictions, 0 unimplemented promises
- Verdict: HEALTHY

## Findings

See `modules/iris-reload.md` for details.

## Delta from prior audit

Compared to `.workflow/audits/2026-05-26/index.md`:

- The `iris-reload` capability did not exist as a base spec in the prior audit (all v1 reload behavior lived under the in-flight `add-daemon-self-management` change; only post-archive does it become `openspec/specs/iris-reload/spec.md`).
- The `refuse-cli-self-reload` change added 1 requirement ("Refuses CLI self-reload at pre-flight") and modified 1 ("Direct CLI invocation mirrors MCP behavior") — both now in the audited base spec. Both verified COVERED with dedicated tests (`TestReload_CLISelf{NoArg,ExplicitPath,TaskID}_Refused`, `TestReload_MCPSelfUnaffected`, `TestReload_CLICrossUnaffected`) and the stable `cli-self-reload-not-supported` token sentinel.

## Validation gates

- `openspec validate --all --strict`: 19/19 pass (verified earlier this session)
- `go test ./...`: green (verified earlier this session)
