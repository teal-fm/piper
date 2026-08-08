package lastfm

import "testing"

func TestUserInfoAvatarURL(t *testing.T) {
	tests := []struct {
		name   string
		images []Image
		want   string
	}{
		{
			name: "prefers the requested size",
			images: []Image{
				{Size: "small", Text: "https://example.com/34s.png"},
				{Size: "medium", Text: "https://example.com/64s.png"},
				{Size: "large", Text: "https://example.com/174s.png"},
			},
			want: "https://example.com/174s.png",
		},
		{
			name: "falls back to another size",
			images: []Image{
				{Size: "small", Text: "https://example.com/34s.png"},
				{Size: "medium", Text: "https://example.com/64s.png"},
			},
			want: "https://example.com/34s.png",
		},
		{
			// Last.fm returns every size as an empty string for accounts with no
			// picture, rather than omitting the field.
			name: "account without a picture",
			images: []Image{
				{Size: "small", Text: ""},
				{Size: "medium", Text: ""},
				{Size: "large", Text: ""},
			},
			want: "",
		},
		{
			name: "requested size present but empty",
			images: []Image{
				{Size: "medium", Text: "https://example.com/64s.png"},
				{Size: "large", Text: ""},
			},
			want: "https://example.com/64s.png",
		},
		{
			name:   "no images at all",
			images: nil,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UserInfo{Image: tt.images}.AvatarURL("large")
			if got != tt.want {
				t.Errorf("AvatarURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
