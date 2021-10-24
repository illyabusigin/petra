package petra

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"

	"github.com/illyabusigin/petra/internal"

	"github.com/go-chi/chi/v5"
	"github.com/jfyne/live"
)

type Application struct {
	chi.Router
	root   *embed.FS
	static *embed.FS

	router    *Router
	id        int
	hotReload bool

	session live.SessionStore
}

func (a *Application) init(opts ...Option) {
	a.session = live.NewCookieStore("petra", []byte("weak-secret"))
	a.id = 100
	a.hotReload = true
	a.router = newRouter(a)

	for _, opt := range opts {
		opt(a)
	}

	if err := a.router.load(); err != nil {
		panic(err)
	}

	if root := a.root; root != nil {
		assets, err := fs.Sub(root, "assets")
		if err != nil {
			log.Fatal(err)
		}

		a.Mount("/assets/", http.StripPrefix("/assets", http.FileServer(http.FS(assets))))
	}

	a.Router.Handle("/live.js", live.Javascript{})
	a.Router.Handle("/auto.js.map", live.JavascriptMap{})
	if a.hotReload {
		a.Router.Handle("/hot_reload.js", internal.HotReload{FS: *a.static})
	}
}

func (a *Application) nextID() string {
	id := a.id
	a.id += 1

	return fmt.Sprintf("petra%v", id)
}
