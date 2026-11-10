---
"piper": minor
---

Add a Last.fm-compatible scrobbling API at `/2.0/`. It supports `track.scrobble`, `track.updateNowPlaying`, and the `auth.getToken`/`auth.getSession` handshake, using piper API keys as Last.fm API and session keys. Responses are XML by default and JSON with `format=json`; accepted scrobbles are stored under the Last.fm source, hydrated with MusicBrainz metadata, and published to the user's PDS.
