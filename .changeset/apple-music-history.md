---
"piper": patch
---

Prevent duplicate Apple Music plays from stale or reordered recent-history responses. Poll up to 30 tracks and persist a per-user window of 90 resource IDs, with atomic track and submission writes. The first nonempty history response after upgrade establishes a baseline without publishing old listens. Replays within the retained window are suppressed because Apple does not supply play-event identities or timestamps.
