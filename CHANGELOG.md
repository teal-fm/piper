# piper

## 0.2.1

### Patch Changes

- Fill missing Apple Music ISRC and duration from the catalog even when a recent play already has a share URL. ([#100](https://github.com/teal-fm/piper/pull/100))

- Request a fresh Apple Music token when linking an account so users can recover after changing their Apple ID password. Fix unlinking from the Apple Music link page. ([#101](https://github.com/teal-fm/piper/pull/101))

## 0.2.0

### Minor Changes

- Add a Last.fm-compatible scrobbling API at `/2.0/`. It supports `track.scrobble`, `track.updateNowPlaying`, `auth.getToken`, `auth.getSession`, and `auth.getMobileSession`, using piper API keys as Last.fm API and session keys. Responses are XML by default and JSON with `format=json`; accepted scrobbles are stored under a separate `lastfm-compat` source so they do not move the Last.fm poller's import cursor, hydrated with MusicBrainz metadata, and published to the user's PDS. ([#99](https://github.com/teal-fm/piper/pull/99))

## 0.1.1

### Patch Changes

- 0261687: Raise the minimum Go version to 1.26.10 and update the Docker and Nix toolchains to include upstream security fixes.

## 0.1.0

### Minor Changes

- ed691d4: Expand the NixOS module with service enable flags and a separate Last.fm polling interval, improve secret validation and data-directory setup, and update the Nix package build.
- 5a75e72: add listenbrainz support

### Patch Changes

- 7773191: Add Changesets release PRs, versioned Docker images for AMD64 and ARM64, and GitHub releases with changelogs and installation instructions. The `latest` Docker tag now follows stable releases; use `main` to track development builds.
- e007420: Include the seven-character Git commit in the submission agent for source builds and main Docker images.
