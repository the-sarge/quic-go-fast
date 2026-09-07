# Issue tracker: GitHub

Issues and PRDs live in GitHub Issues in `the-sarge/quic-go-fast`. Use the `gh` CLI and explicitly pass `--repo the-sarge/quic-go-fast` for issue and PR commands because this checkout also has an upstream remote.

## Conventions

- Create: `gh issue create --repo the-sarge/quic-go-fast --title "..." --body-file <path>`.
- Read: `gh issue view <number> --repo the-sarge/quic-go-fast --comments`; fetch structured fields, including labels, with `--json` when needed.
- List: `gh issue list --repo the-sarge/quic-go-fast --state open --json number,title,body,labels,comments`, adding appropriate filters.
- Comment: `gh issue comment <number> --repo the-sarge/quic-go-fast --body-file <path>`.
- Apply or remove labels: `gh issue edit <number> --repo the-sarge/quic-go-fast --add-label "..." --remove-label "..."`.
- Close: `gh issue close <number> --repo the-sarge/quic-go-fast`.

For multiline bodies, write the exact Markdown to a file and use `--body-file`.

## Pull requests as a triage surface

**PRs as a request surface: no.**

External PRs do not enter the issue triage queue. Explicitly requested PR review remains a separate workflow.

GitHub shares issue and PR numbers. When a reference is ambiguous, resolve its type with `gh pr view <number> --repo the-sarge/quic-go-fast`, falling back to `gh issue view` when it is an issue.

## Skill instructions

When a skill says "publish to the issue tracker", create a GitHub issue in this repository. When it says "fetch the relevant ticket", read the issue and its comments.

## Wayfinding operations

- Map: one issue labelled `wayfinder:map`, containing Notes, Decisions-so-far, and Fog.
- Children: GitHub sub-issues labelled `wayfinder:research`, `wayfinder:prototype`, `wayfinder:grilling`, or `wayfinder:task`. If sub-issues are unavailable, use a task list in the map and a `Part of #<map>` line in each child.
- Blocking: use native GitHub issue dependencies through `gh api` with explicit `repos/the-sarge/quic-go-fast/...` paths and the blocker's numeric database ID. If dependencies are unavailable, use a `Blocked by: #<number>` line.
- Frontier: select the first open, unassigned child in map order with no open blockers.
- Claim: assign the ticket to the driving developer.
- Resolve: record the answer, close the ticket, and append a brief finding and link to the map's Decisions-so-far.

Create any missing wayfinding labels when that workflow is first used.
