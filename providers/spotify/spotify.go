package spotify

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

func GetUserInfo(client *http.Client, logger *slog.Logger) (*User, error) {
	resp, err := client.Get("https://api.spotify.com/v1/me")
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error response from Spotify: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("failed to read resp.Body")
	}

	var user User

	if err := json.Unmarshal(body, &user); err != nil {
		return nil, fmt.Errorf("failed to decode user info: %w", err)
	}

	return &user, nil
}

func GetCurrentlyPlaying(client *http.Client, logger *slog.Logger) (*CurrentlyPlaying, error) {
	resp, err := client.Get("https://api.spotify.com/v1/me/player/currently-playing")
	if err != nil {
		return nil, fmt.Errorf("failed to get currently playing: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
    return &CurrentlyPlaying{}, nil
  } else	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error response from Spotify: %s", resp.Status)
  }
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("failed to read resp.Body")
	}

	var playing CurrentlyPlaying

	if err := json.Unmarshal(body, &playing); err != nil {
		return nil, fmt.Errorf("failed to decode currently playing: %w", err)
	}

	return &playing, nil
}

func GetUserPlaylists(client *http.Client, logger *slog.Logger) (*PlaylistResponse, error) {
	resp, err := client.Get("https://api.spotify.com/v1/me/playlists")
	if err != nil {
		return nil, fmt.Errorf("failed to get user playlists: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error response from Spotify: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("failed to read resp.Body")
	}

	var playlistResponse PlaylistResponse

	if err := json.Unmarshal(body, &playlistResponse); err != nil {
		return nil, fmt.Errorf("failed to decode user playlists: %w", err)
	}

	return &playlistResponse, nil
}
