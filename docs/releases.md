# Releases

Piper uses [Changesets](https://github.com/changesets/changesets) to collect release
notes in PRs and prepare a version PR, following the pattern in
[create-t3-app's release workflow](https://github.com/t3-oss/create-t3-app/blob/main/.github/workflows/release.yml).
The release is a Go application and Docker image. Nothing is published to npm.
We use Changesets CLI 2 with `changesets/action@v1`.

## Add a change

Install Node.js 24 and run `npm ci`, then:

```sh
npm run changeset
```

Select `piper`, choose a bump, and describe the user-visible change. Include any
configuration changes or migration steps. Commit the generated `.changeset/*.md`
file alongside your code.

- `patch` fixes a bug or makes a compatible small change.
- `minor` adds functionality. While Piper is below 1.0, use a minor bump for
  breaking changes too, and explain the migration in the changeset.
- `major` is for a breaking release once Piper reaches 1.0. Moving to 1.0 is a
  deliberate maintainer decision.

For documentation, tests, or maintenance that needs no release, run:

```sh
npm run changeset -- --empty
```

The PR check requires a new changeset, including an empty one, or a release PR
that consumes changesets and increases the version. It also validates the
changesets and checks that the Go submission agent matches `package.json`.
Do not manually bump `models/constants.go` on feature PRs anymore.

## Ship a release

1. Merge feature PRs into `main`. The Release action creates or updates
   `chore(release): version Piper` with the collected changes.
2. Review that PR's version, `CHANGELOG.md`, lockfile, and Go submission agent.
   Changesets combines pending bumps and consumes their files. `package.json` is
   the version source; Nix reads it from the source being built.
3. Merge the release PR. The workflow runs release checks and Go race tests,
   builds and pushes both Docker architectures, then publishes `vX.Y.Z` and a
   GitHub release containing that version's changelog and exact install commands.

`npm run version` performs the version PR's work locally for debugging. Normally
let the action run it. Use `npm run check:version` and `npm run test:release` to
check the release scripts. Prereleases are not supported by this workflow yet.

The initial version is `0.0.14`, matching the existing submission agent. The
bootstrap changeset prepares `0.0.15` as the first automated release. Historical
releases are not reconstructed.

## Docker tags and installation

| Tag | Meaning |
| --- | --- |
| `ghcr.io/teal-fm/piper:0.0.15` | Exact stable version |
| `ghcr.io/teal-fm/piper:v0.0.15` | Alias for the same version |
| `ghcr.io/teal-fm/piper:latest` | Most recently published stable release |
| `ghcr.io/teal-fm/piper:main` | Development build from main |
| `ghcr.io/teal-fm/piper:sha-<short-sha>` | Development build for a commit |

`latest` previously tracked main. Deployments that want every main build should
switch to `main`. Prefer an exact version for production. Stable images support
`linux/amd64` and `linux/arm64`; the release only appears after both are pushed.

Each GitHub release includes copyable commands for its exact version, including
version-pinned configuration downloads and a source build. Follow the linked
README to configure credentials and callbacks. `compose.release.yml` requires
`PIPER_IMAGE` explicitly so it cannot silently select another version:

```sh
PIPER_IMAGE=ghcr.io/teal-fm/piper:0.0.15 docker compose -f compose.release.yml up -d
```

This example becomes available after the first release. The existing
`compose.yml` still builds from local source. Both files use `piper_data`; keep
the same directory or Compose project name when switching to reuse the database.
Set `SERVER_HOST=0.0.0.0`, `SERVER_PORT=8080`, and `DB_PATH=/db/piper.db` in `.env`.
Apple Music users must add a bind mount for their private key and set
`APPLE_MUSIC_PRIVATE_KEY_PATH` to its path inside the container.
Back up the database before upgrades. Downgrading the image does not reverse
schema migrations.

## Submission agent

Git checkout builds embed Go's VCS revision and report `piper/main (abcdef0)`.
The `:main` and `:sha-…` images receive the commit from CI and use the same
seven-character format. This also applies to local feature branch builds.
Versioned release images report `piper/vX.Y.Z`.

For local Docker builds, pass the revision because `.git` is excluded from the
build context:

```sh
PIPER_BUILD_REVISION=$(git rev-parse HEAD) docker compose up --build
```

Without a revision, local Docker builds report `piper/main`. Source builds without
VCS metadata fall back to the release version. To explicitly build a release from
a Git checkout, use `go build -ldflags="-X github.com/teal-fm/piper/models.buildChannel=release" -o piper ./cmd`.
The generated release instructions include this flag.

## Repository setup

In GitHub Settings, Actions, General, enable **Allow GitHub Actions to create and
approve pull requests**. The workflow uses `GITHUB_TOKEN` with contents and PR
write access for version PRs, and contents and packages write access for releases.
No npm token is needed. The existing GHCR package must grant this repository
Actions access; make the package public if anonymous Docker pulls are intended.

The version PR uses GitHub's built-in token. GitHub does not start PR workflows
for its automated pushes. If branch protection requires those checks, close and
reopen the version PR as a maintainer to trigger checks after each bot update,
or configure a GitHub App token for checkout and Changesets to trigger them
automatically. Replace any required `Check version bump` check with
`Check changeset`, and keep the Go and Docker checks required.

Docker publishing and GitHub release creation run in the same workflow. A
release created by `GITHUB_TOKEN` cannot trigger a separate release-event build.
Only the upstream repository's main branch publishes.

## Failed releases and retries

Use Actions, Release, Run workflow on `main`, or rerun the failed workflow.
Release runs are serialized and are not cancelled when another commit lands.
A draft release reserves the version and commit before the build. Failed drafts
can only be resumed by rerunning the workflow at that same commit. An already
published version is skipped. API errors fail the workflow instead
of being treated as a missing release, and an existing tag at another commit
blocks publication. Do not move or reuse release tags.

If Docker succeeded but GitHub release creation failed, rerun the failed workflow
at the same commit before merging more changes. This may rebuild and push the
same image tags. For a bad published release, add a new patch changeset and ship
a new version. The action does not overwrite published release notes.

## Pull request images

Every opened, reopened, or updated PR builds its head commit for Linux AMD64 and
ARM64, including PRs from forks and PRs targeting branches other than main.
After a successful build, a separate workflow publishes
`ghcr.io/teal-fm/piper-pr:pr-<number>` and creates or updates a comment from
`github-actions[bot]` with the commit, pull command, and Compose instructions.
The tag follows the latest successful build. Failed builds leave the previous
image and comment in place. Closed PRs and superseded commits are skipped.

The build has read-only repository permissions and uploads an OCI archive.
The publisher runs from the default branch, copies the archive without running
the image or checking out PR code, and has package and comment write permissions.
Preview images use a separate `piper-pr` package from release images. This follows
GitHub's [workflow_run artifact pattern](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_run)
and Docker's [OCI archive exporter](https://docs.docker.com/build/exporters/oci-docker/).

Both workflows must be merged into the default branch before automatic publishing
works. Fork contributions may require a maintainer to approve the build under the
repository's Actions settings. GitHub does not trigger PR workflows for PRs or
updates created using `GITHUB_TOKEN`; those require a GitHub App token or a
maintainer to close and reopen the PR. Set the `piper-pr` GHCR package to public after its
first publication so the bot's pull command works without registry login. The
package must grant this repository Actions access. Build artifacts expire after
two days; published images remain available until removed from GHCR.

## Tangled pipelines

Native Spindle checks and image publishing are documented in [Tangled workflows](tangled.md), including registry setup and differences from GitHub automation.
