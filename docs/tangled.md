# Tangled Spindle workflows

The `.tangled/workflows` directory adds native Spindle pipelines alongside GitHub
Actions.

## Workflow mapping

| GitHub Actions | Tangled Spindles |
| --- | --- |
| Go Tests | `test.yaml`: generate lexicons/CSS, run Go race tests, check formatting, and run release-tooling tests on main pushes and PRs |
| Changesets | `changesets.yaml`: validate versions and changesets against the PR's target branch, including stacked PRs |
| Build | `build.yaml`: build and publish both architectures on main pushes |
| Build PR image | `pr-image.yaml`: build both architectures for PR creation and updates |
| Publish PR image | Same PR workflow publishes when Spindle supplies registry credentials and prints pull commands in its logs |
| Release | `release.yaml`: validate a pushed `vX.Y.Z` tag and changelog, run release checks, and publish versioned images |

Spindle supports push, PR, and manual triggers, with serial shell steps inside
each workflow. These files use its NixOS microVM engine and Docker-in-VM support.
See the [Spindle documentation](https://docs.tangled.org/spindles#spindles).

## Enable the pipelines

1. Enable a spindle for `teal.fm/piper` in Tangled's repository settings. Its
   operator must provide the microVM engine with a `nixos` image, Docker support,
   and enough time and resources for emulated AMD64/ARM64 builds and Go race tests.
   The documented default workflow timeout is only five minutes; allow at least
   an hour for these image builds and adjust based on actual runtime.
2. Add `TANGLED_REGISTRY_USERNAME` and `TANGLED_REGISTRY_TOKEN` in repository
   secrets. Use a registry account/token able to publish to
   `ghcr.io/teal-fm/piper-tangled`. Spindle does not supply a GitHub token.
3. Make the new GHCR package public after its first upload if anonymous pulls are
   wanted. Change `PIPER_TANGLED_IMAGE` in the three image workflows to use another
   registry/repository. Keep it separate from GitHub's `piper` and `piper-pr` images.
4. Push the workflows to Tangled. Opening a GitHub PR alone does not send the
   branch to Tangled or create a Tangled PR. The repository has two push URLs;
   check `git remote -v` before choosing where to push.

Images support `linux/amd64` and `linux/arm64`. The shared script exports an OCI
archive, then copies all architectures to the registry with Skopeo. Registry
credentials are not passed into the Docker build.

## Image tags

| Tag under `ghcr.io/teal-fm/piper-tangled` | Meaning |
| --- | --- |
| `main` | Most recently published main build |
| `sha-<seven-character-commit>` | Main build for that commit |
| `pr-<branch-key>` | Latest published build for a trusted PR branch pair |
| `pr-<branch-key>-<seven-character-commit>` | Trusted PR build for that commit |
| `X.Y.Z` and `vX.Y.Z` | Release built from the matching version tag |
| `latest` | Release whose commit matched main when it was published |

The branch key is the first 12 hex characters of the SHA-256 hash of the source
repository URL, source branch, and target branch, each followed by a newline.
Spindle does not document a PR-number environment variable. Successful publishing
prints the full `docker pull` commands in the workflow logs. Pull the moving tag
again to download a later build.

Main and PR images use `piper/main (abcdef0)` as their submission agent. Release
images use `piper/vX.Y.Z`. A build that finds its source branch has advanced skips
publishing. Spindle has no documented concurrency groups, so a branch update
during the final upload can still race; use the commit-specific tag when pinning
a deployment.

## Releases and platform differences

Keep Changesets version-PR creation in GitHub. Spindle has no built-in equivalent
to `changesets/action`, GitHub release creation, or the `github-actions[bot]`
identity. This change does not add a custom AT Protocol bot or external service.
Version preparation can also be done with the existing `npm run version` command
and a manually submitted Tangled PR.

After the version PR is merged, push its release tag to Tangled. For a release
already created on GitHub:

```sh
git fetch https://github.com/teal-fm/piper refs/tags/vX.Y.Z:refs/tags/vX.Y.Z
git push git@tangled.sh:teal.fm/piper refs/tags/vX.Y.Z
```

Replace `vX.Y.Z` with the actual release. Both the tag and `package.json` must
agree. The pipeline validates that the changelog contains that version before
publishing. Existing tag pushes to the mirror must be arranged separately from
branch mirroring. Rerunning an older tag does not update `latest` if main has
advanced. Version tags must not be repointed or reused.

There is no documented Spindle completion trigger equivalent to GitHub's
`workflow_run` publisher. Fork pipelines receive no repository secrets, so they
build as checks and skip registry publishing. Their temporary OCI archive is
removed at the end of the run, not presented as a downloadable artifact. Trusted
PRs can publish with secrets, but no automatic Tangled PR comment is created.
Main and release publication fail with a setup error if credentials are missing.

The secret behavior is enforced by Spindle itself, not by a check in PR code.
The implementation was checked against Tangled core at
[`c76c046`](https://tangled.org/tangled.org/core/tree/c76c0468736c1bf1a397436b7bd7776a33a035f0),
including the workflow schema and the engine's exclusion of secrets from
untrusted pipeline sources. An older spindle may need an upgrade for microVM
support. These workflows have local validation and mocked publishing tests;
actual VM execution and registry publication require the configured spindle.
