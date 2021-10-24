package main

import (
	"embed"
	"net/http"

	"github.com/illyabusigin/petra"
)

//go:embed components/* routes/* assets/*.css index.html
var root embed.FS

func main() {
	app := petra.New(
		petra.Root(root),
		petra.HotReload(true),
	)

	http.ListenAndServe(":8080", app)
}
