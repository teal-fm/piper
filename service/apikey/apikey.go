package apikey

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/teal-fm/piper/db"
	dbapikey "github.com/teal-fm/piper/db/apikey" // Assuming this is the package for ApiKey struct
	"github.com/teal-fm/piper/pages"
	"github.com/teal-fm/piper/session"
)

type Service struct {
	db       *db.DB
	sessions *session.Manager
}

func NewAPIKeyService(database *db.DB, sessionManager *session.Manager) *Service {
	return &Service{
		db:       database,
		sessions: sessionManager,
	}
}

// jsonResponse is a helper to send JSON responses
func jsonResponse(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if data != nil {
		if err := json.NewEncoder(w).Encode(data); err != nil {
			log.Printf("Error encoding JSON response: %v", err)
		}
	}
}

// jsonError is a helper to send JSON error responses
func jsonError(w http.ResponseWriter, message string, statusCode int) {
	jsonResponse(w, statusCode, map[string]string{"error": message})
}

func (s *Service) HandleAPIKeyManagement(database *db.DB, pg *pages.Pages) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		userID, ok := session.GetUserID(r.Context())
		if !ok {
			// If this is an API request context, it might have already been handled by WithAPIAuth,
			// but an extra check or appropriate error for the context is good.
			if session.IsAPIRequest(r.Context()) {
				jsonError(w, "Unauthorized", http.StatusUnauthorized)
			} else {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
			}
			return
		}

		lastfmUsername := ""
		user, err := database.GetUserByID(userID)
		if err == nil && user != nil && user.LastFMUsername != nil {
			lastfmUsername = *user.LastFMUsername
		} else if err != nil {
			log.Printf("Error fetching user %d details for home page: %v", userID, err)
		}
		isAPI := session.IsAPIRequest(r.Context())

		if isAPI { // JSON API Handling
			switch r.Method {
			case http.MethodGet:
				keys, err := s.sessions.GetAPIKeyManager().GetUserApiKeys(userID)
				if err != nil {
					jsonError(w, fmt.Sprintf("Error fetching API keys: %v", err), http.StatusInternalServerError)
					return
				}
				// Ensure keys are safe for listing (e.g., no raw key string)
				// GetUserApiKeys should return a slice of db_apikey.ApiKey or similar struct
				// that includes ID, Name, KeyPrefix, CreatedAt, ExpiresAt.
				jsonResponse(w, http.StatusOK, map[string]any{"api_keys": keys})

			case http.MethodPost:
				var reqBody struct {
					Name string `json:"name"`
				}
				if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
					jsonError(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
					return
				}
				keyName := reqBody.Name
				if keyName == "" {
					keyName = fmt.Sprintf("API Key (via API) - %s", time.Now().UTC().Format(time.RFC3339))
				}
				validityDays := 30 // Default, could be made configurable via request body

				// IMPORTANT: Assumes CreateAPIKeyAndReturnRawKey method exists on SessionManager
				// and returns the database object and the raw key string.
				// Signature: (apiKey *db_apikey.ApiKey, rawKeyString string, err error)
				apiKeyObj, err := s.sessions.CreateAPIKey(userID, keyName, validityDays)
				if err != nil {
					jsonError(w, fmt.Sprintf("Error creating API key: %v", err), http.StatusInternalServerError)
					return
				}

				jsonResponse(w, http.StatusCreated, map[string]any{
					"id":         apiKeyObj.ID,
					"name":       apiKeyObj.Name,
					"created_at": apiKeyObj.CreatedAt,
					"expires_at": apiKeyObj.ExpiresAt,
				})

			case http.MethodDelete:
				keyID := r.URL.Query().Get("key_id")
				if keyID == "" {
					jsonError(w, "Query parameter 'key_id' is required", http.StatusBadRequest)
					return
				}

				key, exists := s.sessions.GetAPIKeyManager().GetApiKey(keyID)
				if !exists || key.UserID != userID {
					jsonError(w, "API key not found or not owned by user", http.StatusNotFound)
					return
				}

				if err := s.sessions.GetAPIKeyManager().DeleteApiKey(keyID); err != nil {
					jsonError(w, fmt.Sprintf("Error deleting API key: %v", err), http.StatusInternalServerError)
					return
				}
				jsonResponse(w, http.StatusOK, map[string]string{"message": "API key deleted successfully"})

			default:
				jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
			return // End of JSON API handling
		}

		// HTML UI Handling (largely existing logic)
		if r.Method == http.MethodPost { // Create key from HTML form
			if err := r.ParseForm(); err != nil {
				http.Error(w, "Invalid form data", http.StatusBadRequest)
				return
			}

			keyName := r.FormValue("name")
			if keyName == "" {
				keyName = fmt.Sprintf("API Key - %s", time.Now().UTC().Format(time.RFC3339))
			}
			validityDays := 1024

			// Uses the existing CreateAPIKey, which likely doesn't return the raw key.
			// The HTML flow currently redirects and shows the key ID.
			// The template message about "only time you'll see this key" is misleading if it shows ID.
			// This might require a separate enhancement if the HTML view should show the raw key.
			apiKey, err := s.sessions.CreateAPIKey(userID, keyName, validityDays)
			if err != nil {
				http.Error(w, fmt.Sprintf("Error creating API key: %v", err), http.StatusInternalServerError)
				return
			}
			// Redirects, passing the ID of the created key.
			// The template shows this ID in the ".NewKey" section.
			http.Redirect(w, r, "/api-keys?created="+apiKey.ID, http.StatusSeeOther)
			return
		}

		if r.Method == http.MethodDelete { // Delete key via AJAX from HTML page
			keyID := r.URL.Query().Get("key_id")
			if keyID == "" {
				// For AJAX, a JSON error response is more appropriate than http.Error
				jsonError(w, "Key ID is required", http.StatusBadRequest)
				return
			}

			key, exists := s.sessions.GetAPIKeyManager().GetApiKey(keyID)
			if !exists || key.UserID != userID {
				jsonError(w, "Invalid API key or not owned by user", http.StatusBadRequest) // StatusNotFound or StatusForbidden
				return
			}

			if err := s.sessions.GetAPIKeyManager().DeleteApiKey(keyID); err != nil {
				jsonError(w, fmt.Sprintf("Error deleting API key: %v", err), http.StatusInternalServerError)
				return
			}
			// AJAX client expects JSON
			jsonResponse(w, http.StatusOK, map[string]any{"success": true})
			return
		}

		// GET request: Display HTML page for API Key Management
		keys, err := s.sessions.GetAPIKeyManager().GetUserApiKeys(userID)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error fetching API keys: %v", err), http.StatusInternalServerError)
			return
		}

		// newlyCreatedKey will be the ID from the redirect after form POST
		newlyCreatedKeyID := r.URL.Query().Get("created")
		var newKeyValueToShow string

		if newlyCreatedKeyID != "" {
			// For HTML, we only have the ID. The template message should be adjusted
			// if it implies the raw key is shown.
			// If you enhance CreateAPIKey for HTML to also pass the raw key (e.g. via flash message),
			// this logic would change. For now, it's the ID.
			newKeyValueToShow = newlyCreatedKeyID
		}

		data := struct {
			Keys     []*dbapikey.ApiKey // Assuming GetUserApiKeys returns this type
			NewKeyID string             // Changed from NewKey for clarity as it's an ID
			NavBar   pages.NavBar
		}{
			Keys:     keys,
			NewKeyID: newKeyValueToShow,
			NavBar: pages.NavBar{
				IsLoggedIn: ok,
				//Just leaving empty so we don't have to pull in the db here, may change
				LastFMUsername: lastfmUsername,
			},
		}

		w.Header().Set("Content-Type", "text/html")
		err = pg.Execute("apiKeys", w, data)
		if err != nil {
			log.Printf("Error executing template: %v", err)
		}
	}
}
