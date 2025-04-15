package spotify

type User struct {
	Country         string `json:"country"`
	DisplayName     string `json:"display_name"`
	Email           string `json:"email"`
	ExplicitContent struct {
		FilterEnabled bool `json:"filter_enabled"`
		FilterLocked  bool `json:"filter_locked"`
	} `json:"explicit_content"`
	ExternalURLS struct {
		Spotify string `json:"spotify"`
	} `json:"external_urls"`
	Followers struct {
		Href  string `json:"href"`
		Total int    `json:"total"`
	}
	Href    string   `json:"hef"`
	ID      string   `json:"id"`
	Images  []string `json:"images"`
	Product string   `json:"product"`
	Type    string   `json:"type"`
	Uri     string   `json:"uri"`
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
