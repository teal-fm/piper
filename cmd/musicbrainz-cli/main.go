package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"

	"github.com/teal-fm/piper/models"
	"github.com/teal-fm/piper/service/musicbrainz"
)

func main() {
	mbService := musicbrainz.NewMusicBrainzService(nil)

	var (
		track   = flag.String("track", "", "Track name")
		artist  = flag.String("artist", "", "Artist name")
		release = flag.String("release", "", "Release/Album name")
		isrc    = flag.String("isrc", "", "ISRC code")
	)
	flag.Parse()

	trackModel := models.Track{
		Name:   *track,
		Album:  *release,
		ISRC:   *isrc,
		Artist: []models.Artist{{Name: *artist}},
	}

	enriched, err := musicbrainz.HydrateTrack(mbService, trackModel)
	if err != nil {
		log.Fatalf("Error enriching track: %v", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "\t")
	enc.Encode(enriched)
}
