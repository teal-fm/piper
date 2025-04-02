package main

import (
  "context"
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

  fmt.Printf("token: %v+\n", token)
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
