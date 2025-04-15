package apikey

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/db/apikey"
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

func (s *Service) HandleAPIKeyManagement(w http.ResponseWriter, r *http.Request) {
	userID, ok := session.GetUserID(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// if we have an api request return json
	if session.IsAPIRequest(r.Context()) {
		keys, err := s.sessions.GetAPIKeyManager().GetUserApiKeys(userID)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error fetching API keys: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"api_keys": keys,
		})
		return
	}

	// if not return html
	if r.Method == "POST" {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form data", http.StatusBadRequest)
			return
		}

		keyName := r.FormValue("name")
		if keyName == "" {
			keyName = fmt.Sprintf("API Key - %s", time.Now().Format(time.RFC3339))
		}

		validityDays := 30

		apiKey, err := s.sessions.CreateAPIKey(userID, keyName, validityDays)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error creating API key: %v", err), http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/api-keys?created="+apiKey.ID, http.StatusSeeOther)
		return
	}

	// if we want to delete a key
	if r.Method == "DELETE" {
		keyID := r.URL.Query().Get("key_id")
		if keyID == "" {
			http.Error(w, "Key ID is required", http.StatusBadRequest)
			return
		}

		key, exists := s.sessions.GetAPIKeyManager().GetApiKey(keyID)
		if !exists || key.UserID != userID {
			http.Error(w, "Invalid API key", http.StatusBadRequest)
			return
		}

		if err := s.sessions.GetAPIKeyManager().DeleteApiKey(keyID); err != nil {
			http.Error(w, fmt.Sprintf("Error deleting API key: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success": true}`))
		return
	}

	// show keys
	keys, err := s.sessions.GetAPIKeyManager().GetUserApiKeys(userID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error fetching API keys: %v", err), http.StatusInternalServerError)
		return
	}

	newlyCreatedKey := r.URL.Query().Get("created")

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

    {{if .NewKey}}
    <div class="new-key-alert">
        <h3>Your new API key has been created</h3>
        <p><strong>Important:</strong> This is the only time you'll see this key. Please copy it now and store it securely.</p>
        <div class="key-value">{{.NewKey}}</div>
    </div>
    {{end}}

    <div class="card">
        <h2>Your API Keys</h2>
        {{if .Keys}}
        <table>
            <thead>
                <tr>
                    <th>Name</th>
                    <th>Created</th>
                    <th>Expires</th>
                    <th>Actions</th>
                </tr>
            </thead>
            <tbody>
                {{range .Keys}}
                <tr>
                    <td>{{.Name}}</td>
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
        <p>Or include it as a query parameter:</p>
        <pre>https://your-piper-instance.com/endpoint?api_key=YOUR_API_KEY</pre>
    </div>

    <script>
        function deleteKey(keyId) {
            if (confirm('Are you sure you want to delete this API key? This action cannot be undone.')) {
                fetch('/api-keys?key_id=' + keyId, {
                    method: 'DELETE',
                })
                .then(response => response.json())
                .then(data => {
                    if (data.success) {
                        window.location.reload();
                    } else {
                        alert('Failed to delete API key');
                    }
                })
                .catch(error => {
                    console.error('Error:', error);
                    alert('Failed to delete API key');
                });
            }
        }
    </script>
</body>
</html>
`

	// Format time function for the template
	funcMap := template.FuncMap{
		"formatTime": func(t time.Time) string {
			return t.Format("Jan 02, 2006 15:04")
		},
	}

	// Parse the template with the function map
	t, err := template.New("apikeys").Funcs(funcMap).Parse(tmpl)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error parsing template: %v", err), http.StatusInternalServerError)
		return
	}

	data := struct {
		Keys   []*apikey.ApiKey
		NewKey string
	}{
		Keys:   keys,
		NewKey: newlyCreatedKey,
	}

	w.Header().Set("Content-Type", "text/html")
	t.Execute(w, data)
}
