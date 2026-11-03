# Releasing fhcli

A release is a pushed `vX.Y.Z` tag. `.github/workflows/release.yml` runs the tests, and then [goreleaser](https://goreleaser.com) builds the binaries and publishes a GitHub release with `checksums.txt`. The git tag is the only source of the version: there is no version file to bump.

Pushing a tag needs write access to `freshworks-oss/fhcli`.

## Choosing the version

The project follows [Semantic Versioning](https://semver.org/). Before 1.0:

| Change | Bump | Example |
|---|---|---|
| Bug fixes, spec refresh, docs | patch | 0.1.0 → 0.1.1 |
| New commands or flags, any behaviour change listed under **Changed** | minor | 0.1.1 → 0.2.0 |

Breaking the change-event interface (`event`, `execute`, `init`, their flags, env vars, config keys, text output or exit codes) is never routine. CI pipelines depend on it, so it needs a deprecation in an earlier release first. `pkg/cli/compat_test.go` guards that interface.

## Cutting a release

1. **Start from `main`, green.** CI must have passed on the merge commit, including the `release-dry-run` job.
2. **Refresh the embedded spec**, if the public API has changed since the last release:
   ```sh
   make spec && make test
   ```
   Commit the result through a PR.
3. **Update `CHANGELOG.md`** through a PR. Move the `Unreleased` entries under a new `## [X.Y.Z]` heading, add the compare link at the bottom, and list behaviour changes under **Changed**.
4. **Tag and push** from an up-to-date `main`:
   ```sh
   git checkout main && git pull
   git tag -a vX.Y.Z -m "fhcli vX.Y.Z"
   git push origin vX.Y.Z
   ```
   Use an annotated tag. Tags such as `v0.2.0-rc.1` are published as pre-releases.
5. **Watch the run** under Actions → Release. It takes about two minutes.
6. **Check the release:**
   ```sh
   gh release view vX.Y.Z -R freshworks-oss/fhcli   # 5 archives + checksums.txt
   ```
   Then install it with the snippet in the readme, and run `fhcli --version`, `fhcli whoami` and `fhcli event --help`.
7. **Announce it**, linking the GitHub release. If the Claude Code skill changed, remind people to re-run `fhcli install --skills`.

## If something goes wrong

- **The workflow fails before publishing.** Fix it on `main`, then move the tag: `git push --delete origin vX.Y.Z`, re-tag the fixed commit, and push again. Nobody can have downloaded it yet.
- **A bad release was published.** Don't move or reuse the tag, because people and caches may already have it. Mark the GitHub release as a pre-release or delete its assets, then ship the fix as the next patch version.

## Testing release changes locally

`make snapshot` builds every archive into `dist/` without publishing anything. CI runs the same build on every pull request.

goreleaser isn't installed separately. The Makefile runs it with `go run` at the version pinned in `GORELEASER`, so local builds, CI and releases all use the same goreleaser version. To upgrade it, change that one line.

The workflows use only GitHub's own actions (`actions/checkout`, `actions/setup-go`). The freshworks-oss Actions policy only allows actions from GitHub, the enterprise, or an allowlist, and it rejects `goreleaser/goreleaser-action@v6`. Check that policy before adding any third-party action.
