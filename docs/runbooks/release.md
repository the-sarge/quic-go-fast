# Library releases

quic-go-fast uses an annotated Git tag and a GitHub Release without a dedicated publishing workflow. Releases follow the [stable upstream policy](../adr/0003-follow-stable-upstream-releases.md) and preserve the [application-owned module replacement](../adr/0002-adopt-through-module-replacement.md).

## Version selection

Choose the version deliberately and record it in [CHANGELOG.md](../../CHANGELOG.md) or a release issue before tagging. The first fork prerelease is `v0.62.1-fast.1`, based on upstream `v0.62.0`. The `-fast.1` suffix identifies a Go semantic prerelease; it sorts after `v0.62.0` and before `v0.62.1`. The next prerelease on this base can use `v0.62.1-fast.2`. Future upstream-base and final-release version choices remain deliberate maintainer decisions.

Never move or reuse an existing tag, or replace an existing published release in place. A correction gets a new version. Inherited upstream tags are not fork releases. Consumers must pin an explicit fork version; `@latest` can select an inherited upstream release instead of a fork prerelease.

## Prepare and validate

1. Prepare the changelog and any adoption changes in a dedicated feature worktree and merge their PR through the repository's existing checks. Keep archived evidence unchanged.
2. Select a full commit SHA on `main`. Verify it is an ancestor of the current remote default branch and that the worktree used for tagging is clean.
3. Require successful unit, integration, lint, cross-compilation, and interop-image workflows at that exact commit. Inspect individual jobs and relevant steps. Document existing limitations; do not count an unexplained failed check as green or rerun it until it passes.
4. Run a fresh `govulncheck ./...` with a recorded scanner and Go version. Inspect reachable findings before release, and record the target platform and any uncalled module advisories. A source scan can run for another `GOOS`/`GOARCH` by building the scanner for the host first, then setting the target environment when invoking that executable.
5. Verify a separate consumer can build QUIC and HTTP/3 using an application-owned replacement at the candidate revision. After tagging, repeat with the real published tag and verify the downloaded module version, commit, and archive boundary.
6. Write release notes covering the upstream base, changes, Go minimum, adoption, known limitations, and tested versus build-only platforms. Link the exact commit's workflow results. This library release has no binary assets; adding binary distribution is a separate scope decision.

## Publish and verify

Check that both the proposed remote tag and GitHub Release are absent. Create an annotated tag at the selected SHA and push only that tag. Never force-push tags. Existing push workflows also validate tag pushes; no additional release workflow is required.

Create a draft GitHub prerelease using the verified tag and prepared notes, inspect the draft, verify the public Go module download and consumer build, and wait for tag-triggered workflows to succeed before publishing it. If a published tag's validation fails, retain the failure and stop publication; repair through a new commit and a new version rather than moving the tag. Once published, verify the release URL, tag object and peeled commit, prerelease flag, notes, and absence of unexpected assets. Record the tag-run URLs and consumer verification in the release notes or release issue.

For subsequent releases, use the same flow: changelog PR, validated default-branch commit, fresh vulnerability scan, immutable annotated tag, consumer verification, and GitHub Release. Update the README's installation example only to a real released tag or a Go-generated pseudo-version. Never present a planned tag as an already available dependency.
