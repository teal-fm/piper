# piper

## 0.1.0

### Minor Changes

- ed691d4: Expand the NixOS module with service enable flags and a separate Last.fm polling interval, improve secret validation and data-directory setup, and update the Nix package build.
- 5a75e72: add listenbrainz support

### Patch Changes

- 7773191: Add Changesets release PRs, versioned Docker images for AMD64 and ARM64, and GitHub releases with changelogs and installation instructions. The `latest` Docker tag now follows stable releases; use `main` to track development builds.
- e007420: Include the seven-character Git commit in the submission agent for source builds and main Docker images.
