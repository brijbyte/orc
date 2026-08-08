---
name: release-orc
description: Prepare, tag, and verify a new orc release. Use when the user asks to release orc, choose or check a release version, create a release tag, monitor release CI, or verify GitHub and Homebrew release artifacts.
---

# Release orc

Run commands from the repository root. Keep the release commit unchanged. Do not push; the user must push the tag.

## 1. Inspect the release process

Read these files before release work:

- `.github/workflows/release.yml`
- the `Release (maintainers)` section in `README.md`
- `Makefile` release and version rules

The Git tag is the version source. Do not edit a version file.

## 2. Check repository state

Run:

```sh
git status --short
git branch --show-current
git fetch origin --tags --prune
git rev-list --left-right --count origin/main...HEAD
git tag --merged HEAD --sort=-version:refname
git describe --tags --abbrev=0 HEAD^ 2>/dev/null || true
git log --oneline --decorate "$(git describe --tags --abbrev=0 HEAD^ 2>/dev/null || git rev-list --max-parents=0 HEAD)"..HEAD
```

Require all of these conditions:

- The branch is `main`.
- The worktree is clean.
- `HEAD` and `origin/main` are equal.
- All required changes are committed.

Stop if a condition fails. Do not stash, discard, commit, pull, or push without a separate user request.

## 3. Select the version

Use `vMAJOR.MINOR.PATCH`. If the user did not give a version, propose one from the changes and ask for approval.

List tags with Git version sorting. The version must be greater than every existing release version. Do not derive it only from the nearest tag: this repository has had tags created out of semantic order.

Use these rules:

- Patch: compatible fixes and internal changes.
- Minor: compatible user-visible features.
- Major: incompatible behavior or interface changes.

Verify that the selected tag does not exist:

```sh
git rev-parse -q --verify "refs/tags/$tag"
```

A zero exit status means the tag exists; stop.

## 4. Prepare release notes

Summarize commits since the nearest prior release tag. Group only when useful: features, fixes, and maintenance. Show the proposed version and summary before creating the tag unless the user already approved that exact version.

Do not add a changelog file. GitHub Actions creates the release from the tag.

## 5. Validate the release commit

Build the release with the proposed version before tagging:

```sh
make clean
make SYSTEM_CURL=1 VERSION="$version" release
./bin/orc --version
./bin/orc -h
git diff --check
git status --short
```

Require `./bin/orc --version` to contain the exact proposed version. Require a clean worktree after the build. Fix failures before tagging.

## 6. Create the local tag

Create an annotated tag:

```sh
git tag -a "$tag" -m "Release $tag"
git show --no-patch --decorate "$tag"
```

Do not push it. Give the user this command:

```sh
git push origin "$tag"
```

If tagging was incorrect and the tag was not pushed, delete it only after user approval.

## 7. Verify a pushed release

When the user asks to monitor or verify the release, use `gh` if available:

```sh
gh run list --workflow release.yml --limit 10
gh release view "$tag"
gh release view "$tag" --json assets --jq '.assets[].name'
```

Require these assets:

- `orc-darwin-arm64.tar.gz`
- `orc-darwin-x86_64.tar.gz`
- `orc-linux-arm64.tar.gz`
- `orc-linux-x86_64.tar.gz`
- `checksums.txt`

Confirm that the release workflow passed. If `TAP_PUSH_TOKEN` is configured, also confirm that `brijbyte/homebrew-orc` contains the new formula version. Report failures with the failed job or missing artifact. Do not retry, rerun, or change a remote release without user approval.
