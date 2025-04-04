package spotify

type User struct {
	Name string `json:"display_name"`
	ID   string `json:"id"`
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

