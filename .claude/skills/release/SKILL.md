---
name: release
description: Cut a mock release. Finalize CHANGELOG.md, verify, tag vX.Y.Z on master, push, and watch the GoReleaser workflow.
argument-hint: vX.Y.Z
disable-model-invocation: true
---

# Release $ARGUMENTS

Pushing a `v*` tag triggers `.github/workflows/release.yml`. That workflow re-runs `make verify`, `go test -race ./...` and `make vulncheck`, then publishes with GoReleaser (`.goreleaser.yaml`). GitHub release notes are disabled. The release body links to `CHANGELOG.md` at the tag, so the changelog *is* the release notes.

Stop and report at any failed step. Don't work around it.

## 1. Preflight

- The version must look like `vX.Y.Z`. If no argument was given, propose one from `git tag --sort=-v:refname | head -1` and the Unreleased notes:
  - Any `**Breaking:**` entry means a minor bump while we are on 0.x.
  - Otherwise it's a patch bump.
  - Ask the user to confirm the version.
- The tag must not already exist, locally or remotely: `git tag -l`, `git ls-remote --tags origin`.
- You must be on `master` with a clean tree. Run `git fetch origin`. `master` must equal `origin/master`, or be ahead of it with only the release commit to come.
  - If you're on another branch, stop and tell the user to merge first.
- `CHANGELOG.md` must have a non-empty `## Unreleased` section.

## 2. Verify locally

```sh
make verify
go test -race ./...
make vulncheck
.claude/skills/smoke/smoke.sh
```

## 3. Finalize the changelog

- Rename `## Unreleased` to `## vX.Y.Z - YYYY-MM-DD`, using today's date.
- Keep the one-line summary under the heading, in the same style as earlier entries.
- Don't add a new empty Unreleased section. The next change adds it.
- Check that README.md and the pages in `docs/` still match the behavior described in the changelog entries. The `docs/` pages are the spec.

## 4. Commit, then confirm before anything leaves the machine

Commit with message `Release vX.Y.Z` and the Co-Authored-By trailer.

Then show the user:
- the version
- the commit
- the changelog section

Ask for an explicit yes to push. Pushing a tag publishes a public release and can't be cleanly undone.

## 5. Push and tag

Push master first, so the tagged commit is already on the default branch:

```sh
git push origin master
git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin vX.Y.Z
```

## 6. Watch the release

```sh
gh run list --workflow release.yml --limit 1
gh run watch <id> --exit-status
gh release view vX.Y.Z
```

Confirm the release has archives for linux, darwin and windows on amd64 and arm64, plus `checksums.txt`, that `ghcr.io/sspencer/mock:X.Y.Z` (and `latest`) was pushed for linux/amd64 and linux/arm64 (`docker buildx imagetools inspect ghcr.io/sspencer/mock:X.Y.Z`), and that `sspencer/homebrew-tap` got a `Casks/mock.rb` commit. The cask upload is skipped, with a log line, when the `HOMEBREW_TAP_GITHUB_TOKEN` secret is not set.

If the workflow fails, report the failing step's log (`gh run view <id> --log-failed`). Don't delete or move the tag without the user's say-so.
