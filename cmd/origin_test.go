package main

import (
	"net/http/httptest"
	"testing"
)

func TestRequestHasAllowedOrigin(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		want   bool
	}{
		{name: "same origin", origin: "https://piper.example", want: true},
		{name: "cross origin", origin: "https://attacker.example", want: false},
		{name: "missing origin", want: false},
		{name: "null origin", origin: "null", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "https://piper.example/unlink-lastfm", nil)
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			if got := requestHasAllowedOrigin(request, "https://piper.example"); got != test.want {
				t.Fatalf("got %v, want %v", got, test.want)
			}
		})
	}
}
