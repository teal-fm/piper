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
