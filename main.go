package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/spotify"
)

type OAuthService struct {
	cfg      *oauth2.Config
	verifier string
}

type User struct {
	Name string `json:"display_name"`
	ID   string `json:"id"`
}

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

func NewOAuthService() *OAuthService {
	return &OAuthService{
		cfg: &oauth2.Config{
			ClientID:     os.Getenv("CLIENT_ID"),
			ClientSecret: os.Getenv("CLIENT_SECRET"),
			Endpoint:     spotify.Endpoint,
			RedirectURL:  os.Getenv("REDIRECT_URL"),
			Scopes:       []string{"user-read-private", "user-read-email", "user-library-read"},
		},
		verifier: oauth2.GenerateVerifier(),
	}
}

func home(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte("visit <a href='/login'>/login</a> to get started"))
}

func (o *OAuthService) HandleLogin(w http.ResponseWriter, r *http.Request) {
	url := o.cfg.AuthCodeURL("state", oauth2.AccessTypeOffline, oauth2.S256ChallengeOption(o.verifier))
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (o *OAuthService) HandleCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if state != "state" {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}

	token, err := o.cfg.Exchange(context.Background(), code, oauth2.VerifierOption(o.verifier))
	if err != nil {
		http.Error(w, "failed to exchange token", http.StatusInternalServerError)
		log.Println("token exchange error:", err)
		return
	}

	client := o.cfg.Client(context.Background(), token)
	userInfo, err := getUserInfo(client)
	if err != nil {
		http.Error(w, "failed to get user info", http.StatusInternalServerError)
		log.Println("user info error: ", err)
		return
	}

	playlistsInfo, err := getUserPlaylists(client)
	if err != nil {
		http.Error(w, "failed to get user playlists", http.StatusInternalServerError)
		log.Println("user playlist info error: ", err)
		return
	}
	fmt.Fprintf(w, "logged in successfully!\nuser: %s; id: %s\n", userInfo.Name, userInfo.ID)
	fmt.Fprintf(w, "playlistResponse: %v\n", playlistsInfo)
}

func getUserInfo(client *http.Client) (*User, error) {
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
		log.Fatal("failed to read resp.Body")
	}

	var user User

	if err := json.Unmarshal(body, &user); err != nil {
		return nil, fmt.Errorf("failed to decode user info: %w", err)
	}

	return &user, nil
}

func getUserPlaylists(client *http.Client) (*PlaylistResponse, error) {
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
		log.Fatal("failed to read resp.Body")
	}

	var playlistResponse PlaylistResponse

	if err := json.Unmarshal(body, &playlistResponse); err != nil {
		return nil, fmt.Errorf("failed to decode user playlists: %w", err)
	}

	return &playlistResponse, nil
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file")
	}

	oauthService := NewOAuthService()

	http.HandleFunc("/", home)
	http.HandleFunc("/login", oauthService.HandleLogin)
	http.HandleFunc("/callback", oauthService.HandleCallback)

	fmt.Println("server running at: http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
