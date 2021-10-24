package internal

import (
	"embed"
	"fmt"
	"net/http"
)

// HotReload handles serving the client side
// portion of hot-reload.
type HotReload struct {
	FS embed.FS
}

func (h HotReload) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/javascript")

	data, err := h.FS.ReadFile("static/hot_reload.js")
	if err != nil {
		fmt.Printf("Error loading hot_reload.js: %v", err)
	}
	w.Write(data)
}
