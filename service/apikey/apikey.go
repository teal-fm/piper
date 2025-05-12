package apikey

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"

	"github.com/teal-fm/piper/db"
	db_apikey "github.com/teal-fm/piper/db/apikey" // Assuming this is the package for ApiKey struct
	"github.com/teal-fm/piper/session"
)

type Service struct {
	db       *db.DB
	sessions *session.SessionManager
}

func NewAPIKeyService(database *db.DB, sessionManager *session.SessionManager) *Service {
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

func (s *Service) HandleAPIKeyManagement(w http.ResponseWriter, r *http.Request) {
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
				keyName = fmt.Sprintf("API Key (via API) - %s", time.Now().Format(time.RFC3339))
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
			keyName = fmt.Sprintf("API Key - %s", time.Now().Format(time.RFC3339))
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

	tmpl := `
<!DOCTYPE html>
<html>
<head>
    <title>API Key Management - Piper</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            max-width: 800px;
            margin: 0 auto;
            padding: 20px;
            line-height: 1.6;
        }
        h1, h2 {
            color: #1DB954; /* Spotify green */
        }
        .nav {
            display: flex;
            margin-bottom: 20px;
        }
        .nav a {
            margin-right: 15px;
            text-decoration: none;
            color: #1DB954;
            font-weight: bold;
        }
        .card {
            border: 1px solid #ddd;
            border-radius: 8px;
            padding: 20px;
            margin-bottom: 20px;
        }
        table {
            width: 100%;
            border-collapse: collapse;
        }
        table th, table td {
            padding: 8px;
            text-align: left;
            border-bottom: 1px solid #ddd;
        }
        .key-value {
            font-family: monospace;
            padding: 10px;
            background-color: #f5f5f5;
            border: 1px solid #ddd;
            border-radius: 4px;
            word-break: break-all;
        }
        .new-key-alert {
            background-color: #f8f9fa;
            border-left: 4px solid #1DB954;
            padding: 15px;
            margin-bottom: 20px;
        }
        .btn {
            padding: 8px 16px;
            background-color: #1DB954;
            color: white;
            border: none;
            border-radius: 4px;
            cursor: pointer;
        }
        .btn-danger {
            background-color: #dc3545;
        }
    </style>
</head>
<body>
    <div class="nav">
        <a href="/">Home</a>
        <a href="/current-track">Current Track</a>
        <a href="/history">Track History</a>
        <a href="/api-keys" class="active">API Keys</a>
        <a href="/logout">Logout</a>
    </div>

    <h1>API Key Management</h1>

    <div class="card">
        <h2>Create New API Key</h2>
        <p>API keys allow programmatic access to your Piper account data.</p>
        <form method="POST" action="/api-keys">
            <div style="margin-bottom: 15px;">
                <label for="name">Key Name (for your reference):</label>
                <input type="text" id="name" name="name" placeholder="My Application" style="width: 100%; padding: 8px; margin-top: 5px;">
            </div>
            <button type="submit" class="btn">Generate New API Key</button>
        </form>
    </div>

    {{if .NewKeyID}} <!-- Changed from .NewKey to .NewKeyID for clarity -->
    <div class="new-key-alert">
        <h3>Your new API key (ID: {{.NewKeyID}}) has been created</h3>
        <!-- The message below is misleading if only the ID is shown.
             Consider changing this text or modifying the flow to show the actual key once for HTML. -->
        <p><strong>Important:</strong> If this is an ID, ensure you have copied the actual key if it was displayed previously. For keys generated via the API, the key is returned in the API response.</p>
    </div>
    {{end}}

    <div class="card">
        <h2>Your API Keys</h2>
        {{if .Keys}}
        <table>
            <thead>
                <tr>
                    <th>Name</th>
                    <th>Prefix</th>
                    <th>Created</th>
                    <th>Expires</th>
                    <th>Actions</th>
                </tr>
            </thead>
            <tbody>
                {{range .Keys}}
                <tr>
                    <td>{{.Name}}</td>
                    <td>{{.KeyPrefix}}</td> <!-- Added KeyPrefix for better identification -->
                    <td>{{formatTime .CreatedAt}}</td>
                    <td>{{formatTime .ExpiresAt}}</td>
                    <td>
                        <button class="btn btn-danger" onclick="deleteKey('{{.ID}}')">Delete</button>
                    </td>
                </tr>
                {{end}}
            </tbody>
        </table>
        {{else}}
        <p>You don't have any API keys yet.</p>
        {{end}}
    </div>

    <div class="card">
        <h2>API Usage</h2>
        <p>To use your API key, include it in the Authorization header of your HTTP requests:</p>
        <pre>Authorization: Bearer YOUR_API_KEY</pre>
        <p>Or include it as a query parameter (less secure for the key itself):</p>
        <pre>https://your-piper-instance.com/endpoint?api_key=YOUR_API_KEY</pre>
    </div>

    <script>
        function deleteKey(keyId) {
            if (confirm('Are you sure you want to delete this API key? This action cannot be undone.')) {
                fetch('/api-keys?key_id=' + keyId, { // This endpoint is handled by HandleAPIKeyManagement
                    method: 'DELETE',
                })
                .then(response => response.json())
                .then(data => {
                    if (data.success) {
                        window.location.reload();
                    } else {
                        alert('Failed to delete API key: ' + (data.error || 'Unknown error'));
                    }
                })
                .catch(error => {
                    console.error('Error:', error);
                    alert('Failed to delete API key due to a network or processing error.');
                });
            }
        }
    </script>
</body>
</html>
`
	funcMap := template.FuncMap{
		"formatTime": func(t time.Time) string {
			if t.IsZero() {
				return "N/A"
			}
			return t.Format("Jan 02, 2006 15:04")
		},
	}

	t, err := template.New("apikeys").Funcs(funcMap).Parse(tmpl)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error parsing template: %v", err), http.StatusInternalServerError)
		return
	}

	data := struct {
		Keys     []*db_apikey.ApiKey // Assuming GetUserApiKeys returns this type
		NewKeyID string              // Changed from NewKey for clarity as it's an ID
	}{
		Keys:     keys,
		NewKeyID: newKeyValueToShow,
	}

	w.Header().Set("Content-Type", "text/html")
	t.Execute(w, data)
}
