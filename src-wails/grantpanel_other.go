//go:build !darwin && !headless

package main

// GrantTheme is the app's current look; see grantpanel_darwin.go.
type GrantTheme struct {
	Bg     string `json:"bg"`
	Fg     string `json:"fg"`
	Muted  string `json:"muted"`
	Border string `json:"border"`
	Dark   bool   `json:"dark"`
}

func showGrantHelper(path, pane, title, body string, t GrantTheme) {}
func closeGrantHelper()                                            {}
