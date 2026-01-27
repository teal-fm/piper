package musicbrainz

import (
	"testing"
)

func TestGenerateCacheKey(t *testing.T) {
	tests := []struct {
		name   string
		params SearchParams
		want   string
	}{
		{
			name: "with ISRC",
			params: SearchParams{
				Track:   "Test Song",
				Artist:  "Test Artist",
				Release: "Test Album",
				ISRC:    "USUM71801197",
			},
			want: "track=Test+Song&artist=Test+Artist&release=Test+Album&isrc=USUM71801197",
		},
		{
			name: "without ISRC",
			params: SearchParams{
				Track:   "Test Song",
				Artist:  "Test Artist",
				Release: "Test Album",
				ISRC:    "",
			},
			want: "track=Test+Song&artist=Test+Artist&release=Test+Album&isrc=",
		},
		{
			name: "with special characters",
			params: SearchParams{
				Track:   "Song & Dance",
				Artist:  "Artist/Band",
				Release: "Album: Title",
				ISRC:    "US-123",
			},
			want: "track=Song+%26+Dance&artist=Artist%2FBand&release=Album%3A+Title&isrc=US-123",
		},
		{
			name: "empty params",
			params: SearchParams{
				Track:   "",
				Artist:  "",
				Release: "",
				ISRC:    "",
			},
			want: "track=&artist=&release=&isrc=",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateCacheKey(tt.params)
			if got != tt.want {
				t.Errorf("generateCacheKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildSearchQuery(t *testing.T) {
	tests := []struct {
		name   string
		params SearchParams
		want   string
	}{
		{
			name: "with ISRC",
			params: SearchParams{
				Track:   "Test Song",
				Artist:  "Test Artist",
				Release: "Test Album",
				ISRC:    "USUM71801197",
			},
			want: `isrc:"USUM71801197" AND recording:"Test Song" AND artist:"Test Artist" AND release:"Test Album"`,
		},
		{
			name: "without ISRC",
			params: SearchParams{
				Track:   "Test Song",
				Artist:  "Test Artist",
				Release: "Test Album",
				ISRC:    "",
			},
			want: `recording:"Test Song" AND artist:"Test Artist" AND release:"Test Album"`,
		},
		{
			name: "only ISRC",
			params: SearchParams{
				Track:   "",
				Artist:  "",
				Release: "",
				ISRC:    "USUM71801197",
			},
			want: `isrc:"USUM71801197"`,
		},
		{
			name: "only track and artist",
			params: SearchParams{
				Track:   "Test Song",
				Artist:  "Test Artist",
				Release: "",
				ISRC:    "",
			},
			want: `recording:"Test Song" AND artist:"Test Artist"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSearchQuery(tt.params)
			if got != tt.want {
				t.Errorf("buildSearchQuery() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildSearchEndpoint(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "basic query",
			query: `recording:"Test Song" AND artist:"Test Artist"`,
			want:  "https://musicbrainz.org/ws/2/recording?query=recording%3A%22Test+Song%22+AND+artist%3A%22Test+Artist%22&fmt=json&inc=artists+releases+isrcs",
		},
		{
			name:  "ISRC query",
			query: `isrc:"USUM71801197"`,
			want:  "https://musicbrainz.org/ws/2/recording?query=isrc%3A%22USUM71801197%22&fmt=json&inc=artists+releases+isrcs",
		},
		{
			name:  "empty query",
			query: "",
			want:  "https://musicbrainz.org/ws/2/recording?query=&fmt=json&inc=artists+releases+isrcs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSearchEndpoint(tt.query)
			if got != tt.want {
				t.Errorf("buildSearchEndpoint() = %v, want %v", got, tt.want)
			}
		})
	}
}
