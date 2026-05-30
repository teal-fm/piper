package arbiter

import (
	"strings"
	"time"

	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
)

type LiveTrackProvider interface {
	GetLiveTrack(userID int64) (*models.Track, bool)
	GetLastStampedTrack(userID int64) *models.Track
}

type Coordinator struct {
	db           *db.DB
	spotify      LiveTrackProvider
	appleMusic   LiveTrackProvider
	lastFM       LiveTrackProvider
	dedupeWindow time.Duration
}

func New(database *db.DB) *Coordinator {
	return &Coordinator{
		db:           database,
		dedupeWindow: 2 * time.Minute,
	}
}

func (c *Coordinator) SetSpotify(provider LiveTrackProvider) {
	c.spotify = provider
}

func (c *Coordinator) SetAppleMusic(provider LiveTrackProvider) {
	c.appleMusic = provider
}

func (c *Coordinator) SetLastFM(provider LiveTrackProvider) {
	c.lastFM = provider
}

func (c *Coordinator) ResolveCurrentTrack(userID int64) (*models.Track, string, error) {
	order, err := c.priorityForUser(userID)
	if err != nil {
		return nil, "", err
	}

	for _, service := range order {
		if track, ok := c.liveTrackForService(service, userID); ok && track != nil {
			return cloneTrack(track), service, nil
		}
	}

	return nil, "", nil
}

func (c *Coordinator) CanPublishNowPlaying(userID int64, service string, track *models.Track) (bool, error) {
	resolvedTrack, resolvedService, err := c.ResolveCurrentTrack(userID)
	if err != nil {
		return false, err
	}
	if resolvedTrack == nil {
		return true, nil
	}
	return resolvedService == service && TracksEquivalent(resolvedTrack, track, c.dedupeWindow), nil
}

func (c *Coordinator) CanClearNowPlaying(userID int64, service string) (bool, error) {
	resolvedTrack, resolvedService, err := c.ResolveCurrentTrack(userID)
	if err != nil {
		return false, err
	}
	return resolvedTrack == nil || resolvedService == service, nil
}

func (c *Coordinator) CanScrobble(userID int64, service string, track *models.Track) (bool, error) {
	order, err := c.priorityForUser(userID)
	if err != nil {
		return false, err
	}

	for _, higherPriorityService := range order {
		if higherPriorityService == service {
			return true, nil
		}

		if liveTrack, ok := c.liveTrackForService(higherPriorityService, userID); ok && liveTrack != nil {
			return false, nil
		}

		lastStamped := c.lastStampedForService(higherPriorityService, userID)
		if TracksEquivalent(lastStamped, track, c.dedupeWindow) {
			return false, nil
		}
	}

	return true, nil
}

func (c *Coordinator) priorityForUser(userID int64) ([]string, error) {
	user, err := c.db.GetUserByID(userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return models.ParseServicePriority(models.DefaultServicePriority), nil
	}
	return models.ParseServicePriority(user.ServicePriority), nil
}

func (c *Coordinator) liveTrackForService(service string, userID int64) (*models.Track, bool) {
	switch service {
	case models.ServiceSpotify:
		if c.spotify == nil {
			return nil, false
		}
		return c.spotify.GetLiveTrack(userID)
	case models.ServiceAppleMusic:
		if c.appleMusic == nil {
			return nil, false
		}
		return c.appleMusic.GetLiveTrack(userID)
	case models.ServiceLastFM:
		if c.lastFM == nil {
			return nil, false
		}
		return c.lastFM.GetLiveTrack(userID)
	default:
		return nil, false
	}
}

func (c *Coordinator) lastStampedForService(service string, userID int64) *models.Track {
	switch service {
	case models.ServiceSpotify:
		if c.spotify == nil {
			return nil
		}
		return c.spotify.GetLastStampedTrack(userID)
	case models.ServiceAppleMusic:
		if c.appleMusic == nil {
			return nil
		}
		return c.appleMusic.GetLastStampedTrack(userID)
	case models.ServiceLastFM:
		if c.lastFM == nil {
			return nil
		}
		return c.lastFM.GetLastStampedTrack(userID)
	default:
		return nil
	}
}

func TracksEquivalent(left *models.Track, right *models.Track, dedupeWindow time.Duration) bool {
	if left == nil || right == nil {
		return false
	}

	if left.URL != "" && left.URL == right.URL {
		return true
	}

	if left.ISRC != "" && right.ISRC != "" && strings.EqualFold(left.ISRC, right.ISRC) {
		return true
	}

	if normalized(left.Name) != normalized(right.Name) {
		return false
	}
	if normalized(left.Album) != normalized(right.Album) {
		return false
	}
	if normalized(firstArtist(left)) != normalized(firstArtist(right)) {
		return false
	}

	if left.Timestamp.IsZero() || right.Timestamp.IsZero() {
		return true
	}

	diff := left.Timestamp.Sub(right.Timestamp)
	if diff < 0 {
		diff = -diff
	}
	return diff <= dedupeWindow
}

func cloneTrack(track *models.Track) *models.Track {
	if track == nil {
		return nil
	}
	cloned := *track
	if len(track.Artist) > 0 {
		cloned.Artist = append([]models.Artist(nil), track.Artist...)
	}
	return &cloned
}

func normalized(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func firstArtist(track *models.Track) string {
	if track == nil || len(track.Artist) == 0 {
		return ""
	}
	return track.Artist[0].Name
}
