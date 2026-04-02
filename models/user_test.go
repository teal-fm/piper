package models

import "testing"

func TestNormalizeServicePriority(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "default empty",
			input: "",
			want:  DefaultServicePriority,
		},
		{
			name:  "preserves supported order",
			input: "applemusic,spotify",
			want:  "applemusic,spotify,lastfm",
		},
		{
			name:  "dedupes and fills missing service",
			input: "spotify,spotify",
			want:  DefaultServicePriority,
		},
		{
			name:    "rejects unsupported service",
			input:   "tidal,spotify",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeServicePriority(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestParseServicePriorityFallsBackToDefault(t *testing.T) {
	got := ParseServicePriority("tidal")
	if len(got) != 3 || got[0] != ServiceSpotify || got[1] != ServiceAppleMusic || got[2] != ServiceLastFM {
		t.Fatalf("expected default service order, got %#v", got)
	}
}
