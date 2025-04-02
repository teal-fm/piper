package main

import (
  "context"
  "encoding/json"
  "fmt"
  "log"
  "net/http"
  "os"

  "github.com/joho/godotenv"

  "golang.org/x/oauth2"
  "golang.org/x/oauth2/spotify"
)

type OAuthService struct {
  cfg *oauth2.Config
}

func NewOAuthService() *OAuthService {
  return &OAuthService{
    cfg: &oauth2.Config{
      ClientID: os.Getenv("CLIENT_ID"),
      ClientSecret: os.Getenv("CLIENT_SECRET"),
      Endpoint:     spotify.Endpoint,
      RedirectURL:  "http://localhost:8080/callback",
      Scopes:       []string{"user-read-private", "user-read-email"},
    },
  }
}

func (o *OAuthService) HandleLogin(w http.ResponseWriter, r *http.Request) {
  url := o.cfg.AuthCodeURL("state")
  fmt.Println("Complete authorization at:", url)
  http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (o *OAuthService) HandleCallback(w http.ResponseWriter, r *http.Request) {
  state := r.URL.Query().Get("state")
  code := r.URL.Query().Get("code")
  if state != "state" {
    http.Error(w, "invalid state", http.StatusBadRequest)
    return
  }

  token, err := o.cfg.Exchange(context.Background(), code)
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
  fmt.Fprintf(w, "logged in successfully! user: %s\n", userInfo)
}

func getUserInfo(client *http.Client) (string, error) {
  resp, err := client.Get("https://api.spotify.com/v1/me")
  if err != nil {
    return "", fmt.Errorf("failed to get user info: %w", err)
  }
  defer resp.Body.Close()

  if resp.StatusCode != http.StatusOK {
    return "", fmt.Errorf("error response from Spotify: %s", resp.Status)
  }

  var user struct {
    Name string `json:"display_name"`
  }

  if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
    return "", fmt.Errorf("failed to decode user info: %w", err)
  }

  return user.Name, nil
}

func main() {
  err := godotenv.Load()
  if err != nil {
    log.Fatalf("Error loading .env file")
  }

  oauthService := NewOAuthService()

  http.HandleFunc("/login", oauthService.HandleLogin)
  http.HandleFunc("/callback", oauthService.HandleCallback)

  fmt.Println("server running at: http://localhost:8080")
  log.Fatal(http.ListenAndServe(":8080", nil))
}
