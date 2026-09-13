package models

import "testing"

func TestListenBrainzDurationBounds(t *testing.T) {
	for _, seconds := range []int64{-1, 9223372036854776} {
		p := ListenBrainzPayload{TrackMetadata: ListenBrainzTrackMetadata{AdditionalInfo: &ListenBrainzAdditionalInfo{Duration: &seconds}}}
		if got := p.ConvertToTrack().DurationMs; got != 0 {
			t.Fatalf("duration %d became %d", seconds, got)
		}
	}
}
