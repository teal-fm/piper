package spotify

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

type Playlist struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

type PlaylistResponse struct {
	Limit    int        `json:"limit"`
	Next     string     `json:"next"`
	Offset   int        `json:"offset"`
	Previous string     `json:"previous"`
	Total    int        `json:"total"`
	Items    []Playlist `json:"items"`
}

func (s *SpotifyService) getUserPlaylists(userID int64) (*PlaylistResponse, error) {

	s.mu.RLock()
	token, exists := s.userTokens[userID]
	s.mu.RUnlock()

	if !exists || token == "" {
		return nil, fmt.Errorf("no access token for user %d", userID)
	}

	resp, err := http.NewRequest("GET", "https://api.spotify.com/v1/me/playlists", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get user playlists: %w", err)
	}
	defer resp.Body.Close()

	if resp.Response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error response from Spotify: %s", resp.Response.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal("failed to read resp.Body")
	}

	var playlistResponse PlaylistResponse

	if err := json.Unmarshal(body, &playlistResponse); err != nil {
		return nil, fmt.Errorf("failed to decode user playlists: %w", err)
	}

	return &playlistResponse, nil
}
