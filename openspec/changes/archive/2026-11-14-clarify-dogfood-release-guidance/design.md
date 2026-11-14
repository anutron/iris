## Context

The lease already serializes dogfood use. Its messages explain renewal and release, but do not tell agents when the user being away or a discovered problem means they must release.

## Goals / Non-goals

- Goals: State the user's three release rules briefly at grant, successful deployment, and holder check-in or release warning, and document them in tools and the shipped skill.
- Non-goals: Change lease state transitions, timers, queue ordering, or enforcement.

## Decisions

- Share one guidance string across runtime messages and dogfood tool descriptions to keep the rules consistent.
- Add a `message` field to successful set-dogfood results rather than treating normal guidance as a warning.
- Keep explicit tool names so agents can release and make a new claim without guessing.

## Risks / Trade-offs

- Longer notices: Keep the shared guidance to three short instructions.
- Documentation drift: Cover public messages and tool descriptions with assertions for each release rule.
