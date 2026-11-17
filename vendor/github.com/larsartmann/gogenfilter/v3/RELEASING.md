# Releasing gogenfilter

This runbook covers cutting a new release end-to-end. Every step is verifiable.

## Prerequisites

- You are on `master` with a clean working tree (`git status` clean).
- All CI checks are green on the commit you plan to release.
- Nix is available (this project uses `flake.nix` for all build automation).

## 0. Pre-release checklist

Before starting the release process, verify ALL of the following:

- [ ] **CI is green** — All 4 Go CI jobs pass (test, vulncheck, lint, docs) on the latest master commit.
- [ ] **`nix flake check` passes** — No vendorHash mismatches, no test failures.
- [ ] **`nix run .#lint` passes** — Zero golangci-lint issues.
- [ ] **`nix run .#test` passes** — All tests pass with race detector.
- [ ] **Docs are fresh** — `go generate ./... && git diff --exit-code` shows no changes.
- [ ] **CHANGELOG is complete** — All changes since the last release are documented under `[Unreleased]`.
- [ ] **No uncommitted changes** — `git status` is clean (BuildFlow auto-commits may create unexpected commits; verify the log).
- [ ] **Commit message convention** — The release commit will use `release: vX.Y.Z` format.
- [ ] **Website builds** — `cd website && pnpm run build` succeeds (if website files changed).

> **BuildFlow note**: BuildFlow runs continuously and may auto-commit changes mid-session.
> Check `git log --oneline -5` and `git status` before tagging to ensure the working tree
> reflects your intended release state.

## 1. Run the full quality gate

```sh
nix flake check          # build + tests in sandbox + treefmt
nix run .#lint           # golangci-lint (0 issues required)
nix run .#test           # go test ./...
go generate ./... && git diff --exit-code   # docs freshness
```

Every command must pass. If `nix flake check` reports a `vendorHash` mismatch, update
`vendorHash` in `flake.nix` (the `pkg` derivation) to the "got" hash and re-run.

## 2. Update CHANGELOG

1. Move entries from `## [Unreleased]` to a new `## [vX.Y.Z] — YYYY-MM-DD` section in
   **both** `CHANGELOG.md` and `website/src/content/docs/changelog.mdx`.
2. Ensure both files have identical version headers (CI checks this).
3. Verify compare-reference links at the bottom of both files cover the new version.
4. Leave an empty `## [Unreleased]` section at the top.

## 3. Bump the version

Update the version in:

- `TODO_LIST.md` header (`**Current version:**`)
- `ROADMAP.md` (if it tracks current version)
- `doc.go` (if it references a version)

The Go module version (`/v3`) does **not** change for minor/patch releases — only a
major bump (v4) would change the import path.

## 4. Commit and tag

```sh
git add -A
git commit -m "release: vX.Y.Z"
git tag vX.Y.Z
```

Use the convention `release: vX.Y.Z` for the commit message. The tag MUST be `vX.Y.Z`
(not `X.Y.Z`) — this is what Go module proxy and GitHub Releases expect.

## 5. Push

```sh
git push origin master
git push origin vX.Y.Z
```

Pushing the tag triggers:

- GitHub Actions CI (runs on tag push)
- Go module proxy picks up the tag (may take a few minutes)

## 6. Create GitHub Release

```sh
gh release create vX.Y.Z --generate-notes --title "vX.Y.Z"
```

Review the auto-generated notes, add a link to the CHANGELOG section, and publish.

## 7. Verify

- [ ] `pkg.go.dev/github.com/LarsArtmann/gogenfilter/v3` shows the new version
- [ ] GitHub Release page shows the new tag with notes
- [ ] Website deploys (Firebase CI runs on master push)
- [ ] `go install github.com/LarsArtmann/gogenfilter/v3@latest` works

## Versioning

This project follows [Semantic Versioning](https://semver.org/):

- **Major (v4)**: Breaking API changes, new import path (`/v4`)
- **Minor (v3.X)**: New detectors, new features (backward-compatible)
- **Patch (v3.X.Y)**: Bug fixes, docs, CI improvements

## Rollback

If a release has a critical bug:

1. Do NOT delete the tag (consumers may have already pinned it).
2. Cut a patch release (`vX.Y.Z+1`) with the fix.
3. If the tag itself is wrong (e.g., tagged the wrong commit), use `git tag -d` locally
   and `git push origin :refs/tags/vX.Y.Z` to remove the remote tag — but only if no
   consumer could have fetched it yet.
