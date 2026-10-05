package db

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/teal-fm/piper/models"
)

// Keep three full pages of distinct resources. Repeated or truncated responses
// never evict IDs; only accepting new resources advances the window. There is no
// wall-clock expiry, which would turn unchanged overnight history into new plays.
const appleMusicHistorySize = 90

// AppleMusicHistory establishes the first nonempty response as a baseline.
// The bool reports whether this call initialized the user's history.
func (db *DB) AppleMusicHistory(ctx context.Context, userID int64, ids []string) (map[string]bool, bool, error) {
	if len(ids) == 0 {
		return nil, false, errors.New("Apple Music baseline must not be empty")
	}
	// Responses are newest first; the stored window is oldest first.
	// Retain the newest occurrence so duplicate resources do not consume space
	// or change eviction order within the distinct-resource window.
	unique := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !seen[id] {
			unique = append(unique, id)
			seen[id] = true
		}
	}
	ids = unique
	slices.Reverse(ids)
	if len(ids) > appleMusicHistorySize {
		ids = ids[len(ids)-appleMusicHistorySize:]
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, false, err
	}
	result, err := db.ExecContext(ctx, `INSERT INTO applemusic_history(user_id,resource_ids) VALUES (?,?) ON CONFLICT(user_id) DO NOTHING`, userID, string(encoded))
	if err != nil {
		return nil, false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	var raw string
	if err := db.QueryRowContext(ctx, `SELECT resource_ids FROM applemusic_history WHERE user_id = ?`, userID).Scan(&raw); err != nil {
		return nil, false, err
	}
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil, false, err
	}
	seen = make(map[string]bool, len(ids))
	for _, id := range ids {
		seen[id] = true
	}
	return seen, n > 0, nil
}

// SaveAppleMusicTrack atomically advances history and creates the track plus its
// pending submission. A failed save leaves the resource eligible for retry.
func (db *DB) SaveAppleMusicTrack(ctx context.Context, userID int64, resourceID string, track *models.Track) (bool, error) {
	if resourceID == "" {
		return false, errors.New("Apple Music resource ID is required")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT resource_ids FROM applemusic_history WHERE user_id = ?`, userID).Scan(&raw); err != nil {
		return false, err
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return false, err
	}
	if slices.Contains(ids, resourceID) {
		return false, nil
	}
	ids = append(ids, resourceID)
	if len(ids) > appleMusicHistorySize {
		ids = ids[len(ids)-appleMusicHistorySize:]
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE applemusic_history SET resource_ids = ? WHERE user_id = ?`, string(encoded), userID); err != nil {
		return false, err
	}
	trackID, err := saveTrackTx(ctx, tx, userID, SourceAppleMusic, track, nil)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	track.PlayID = trackID
	return true, nil
}
