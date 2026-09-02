package atproto

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/client"
)

// The record stores the avatar as a blob reference, so the CID has to be turned
// back into a getBlob URL on the PDS that served the record.
func TestTealProfile(t *testing.T) {
	const did = "did:plc:tas6hj2xjrqben5653v5kohk"
	const cid = "bafkreig43sukcclew7t2ufoofn2bpnipcyx2ig4chsdy5zmh2kgkilsyaa"

	blobURL := func(host string) string {
		return host + "/xrpc/com.atproto.sync.getBlob?did=did%3Aplc%3Atas6hj2xjrqben5653v5kohk&cid=" + cid
	}

	tests := []struct {
		name            string
		record          string
		status          int
		wantDisplayName string
		wantAvatarURL   func(host string) string
	}{
		{
			name:            "display name and avatar",
			record:          `{"uri":"at://` + did + `/fm.teal.actor.profile/self","value":{"$type":"fm.teal.actor.profile","avatar":{"$type":"blob","ref":{"$link":"` + cid + `"},"mimeType":"image/jpeg","size":5328473},"displayName":"m@"}}`,
			status:          http.StatusOK,
			wantDisplayName: "m@",
			wantAvatarURL:   blobURL,
		},
		{
			// Plenty of people fill in a display name and never set a picture.
			name:            "display name without an avatar",
			record:          `{"uri":"at://` + did + `/fm.teal.actor.profile/self","value":{"$type":"fm.teal.actor.profile","displayName":"m@"}}`,
			status:          http.StatusOK,
			wantDisplayName: "m@",
			wantAvatarURL:   func(string) string { return "" },
		},
		{
			// And the reverse, which must not blank out the Bluesky name.
			name:            "avatar without a display name",
			record:          `{"uri":"at://` + did + `/fm.teal.actor.profile/self","value":{"$type":"fm.teal.actor.profile","avatar":{"$type":"blob","ref":{"$link":"` + cid + `"},"mimeType":"image/jpeg","size":5328473}}}`,
			status:          http.StatusOK,
			wantDisplayName: "",
			wantAvatarURL:   blobURL,
		},
		{
			// The common case: no teal.fm profile at all, so Bluesky answers.
			name:            "no profile record",
			record:          `{"error":"RecordNotFound","message":"Could not locate record"}`,
			status:          http.StatusBadRequest,
			wantDisplayName: "",
			wantAvatarURL:   func(string) string { return "" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/xrpc/com.atproto.repo.getRecord" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				query := r.URL.Query()
				if query.Get("collection") != tealProfileCollection || query.Get("rkey") != "self" || query.Get("repo") != did {
					t.Errorf("unexpected query %s", r.URL.RawQuery)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.record))
			}))
			defer server.Close()

			got := tealProfile(context.Background(), client.NewAPIClient(server.URL), did)
			if got.DisplayName != tt.wantDisplayName {
				t.Errorf("DisplayName = %q, want %q", got.DisplayName, tt.wantDisplayName)
			}
			if want := tt.wantAvatarURL(server.URL); got.AvatarURL != want {
				t.Errorf("AvatarURL = %q, want %q", got.AvatarURL, want)
			}
		})
	}
}
