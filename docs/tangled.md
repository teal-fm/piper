# Tangled checks

The `.tangled/workflows` directory contains three native
[Spindle workflows](https://docs.tangled.org/spindles#spindles):

| Workflow | Runs on | Check |
| --- | --- | --- |
| `build.yaml` | Main pushes and PRs targeting any branch | `go build ./...` |
| `test.yaml` | Main pushes and PRs targeting any branch | `go test -race ./...` |
| `changesets.yaml` | PRs targeting any branch | Require a changeset using `scripts/check-changeset.mjs` |

The changeset check fetches the PR's target branch and compares against its merge
base, including for stacked PRs. It accepts empty changesets for maintenance and
the existing exception for release PRs that consume changesets.

Enable a spindle with the Nixery engine for `teal.fm/piper` and push these files
to Tangled. The workflows request Go, GCC, Git, and Node.js as needed. They require
no registry credentials or publishing secrets.
