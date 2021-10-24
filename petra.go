package petra

import (
	"embed"

	"github.com/go-chi/chi/v5"
)

//go:embed static
var static embed.FS

func New(opts ...Option) *Application {
	app := &Application{
		Router: chi.NewRouter(),
		static: &static,
	}

	app.init(opts...)

	return app
}
