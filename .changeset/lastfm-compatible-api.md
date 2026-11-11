---
"piper": minor
---

Add a Last.fm-compatible scrobbling API at `/2.0/`. It supports `track.scrobble`, `track.updateNowPlaying`, `auth.getToken`, `auth.getSession`, and `auth.getMobileSession`, using piper API keys as Last.fm API and session keys. Responses are XML by default and JSON with `format=json`; accepted scrobbles are stored under a separate `lastfm-compat` source so they do not move the Last.fm poller's import cursor, hydrated with MusicBrainz metadata, and published to the user's PDS.
