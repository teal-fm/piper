# Apple Music history tracking

Piper polls the first 30 resources from Apple's
[recently played tracks endpoint](https://developer.apple.com/documentation/applemusicapi/get-v1-me-recent-played-tracks)
and processes unseen tracks in reverse response order, oldest first. It does not
follow older pages or reconstruct listening activity beyond that page.

Each user has a persistent window of 90 Apple Music resource IDs. Repeated,
reordered, empty, or truncated responses do not replace that window. Only new
accepted resources evict its oldest entries. The window has no time-based expiry,
so leaving playback idle overnight cannot by itself create another listen.
Spotify and other providers do not affect this state.

Identity uses the resource's `id`, then `attributes.playParams.id` if needed.
For responses without either ID, Piper falls back to the URL or an uploaded
track's metadata hash. Changes to metadata or resolved catalog URLs do not change
identity when Apple supplies an ID.

The first valid, nonempty response establishes a baseline without creating plays.
This also applies to existing users on the first poll after upgrading. Subsequent
restarts retain the baseline and accepted IDs in the database. Empty responses
and API errors do not reset it. The new `applemusic_history` table is created
automatically during database initialization.

History updates, track rows, and pending ATProto submissions commit together. A
failed database write leaves the resource available for retry. Each accepted
track gets its own pending submission; only the newest returned track can update
playing-now status.

## Limits

Apple returns track resources without play-event IDs or actual play timestamps.
A stale response and a genuine replay can therefore look identical. Piper favors
suppressing duplicates: a track replayed while its ID remains among the 90
retained resources is skipped. After eviction, the same ID can create another
listen, including if Apple returns history older than this window.

Saved timestamps represent when Piper observed a new resource, not when the user
actually played it. The response order determines ingestion order; reordered
responses cannot supply a reliable chronology. This change prevents the reported
short stale-history cycles but does not provide an exact listening ledger.
