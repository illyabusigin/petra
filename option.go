package petra

import (
	"embed"

	"github.com/jfyne/live"
)

type Option func(a *Application)

func SessionStore(sessionName string, keyPairs ...[]byte) Option {
	return func(a *Application) {
		a.session = live.NewCookieStore(sessionName, keyPairs...)
	}
}

func Root(root embed.FS) Option {
	return func(a *Application) {
		a.root = &root
	}
}

func HotReload(v bool) Option {
	return func(a *Application) {
		a.hotReload = v
	}
}
