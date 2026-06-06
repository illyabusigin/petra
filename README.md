# Petra

[![Go Reference](https://pkg.go.dev/badge/github.com/illyabusigin/petra.svg)](https://pkg.go.dev/github.com/illyabusigin/petra)
[![CI](https://github.com/illyabusigin/petra/actions/workflows/ci.yml/badge.svg)](https://github.com/illyabusigin/petra/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/illyabusigin/petra)](https://goreportcard.com/report/github.com/illyabusigin/petra)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Petra is a small layer around Go server-rendered HTML.

It keeps `html/template`, `net/http`, and normal Go handlers at the center of
the app. The point is not to hide Go. The point is to stop every web project
from rebuilding the same thin layer around templates, assets, development
reloads, and template errors.

Raw Go templates work well, but larger sites tend to grow the same private
conventions:

- every handler decides how to execute templates;
- layouts and partials become naming conventions no one wrote down;
- full pages and HTMX fragments drift into different rendering paths;
- static asset URLs get their own helper code;
- template errors are harder to read than they need to be;
- live reload is absent or bolted on per app.

Petra gives those pieces one shape.

## What Petra gives you

Petra makes the rendering boundary explicit. A controller can render a full
page, execute a fragment, or show a development template error without every
handler knowing those mechanics.

It also makes partials feel like server-side components. A template can call
`{{ PageHeader . }}` or `{{ AnswerCard . }}` instead of reaching into another
file by path. That is still Go templates. There is no client runtime behind it.

Petra separates production and development concerns. Production can use
embedded templates and static files. Development can read from disk, serve
unhashed local files, reload the browser when templates or assets change, and
show structured template errors while the previous working template set stays
active.

The package includes:

- layout discovery for a `templates/` tree;
- component-style template calls through `tmplfunc`;
- page and fragment execution helpers;
- development debug pages and hot reload;
- static file serving for local development and embedded production builds;
- asset helpers for content-hashed production URLs;
- plugins for Markdown, SVGs, and trusted HTML helpers.

## When Petra fits

Use Petra when a Go web app has enough templates that rendering deserves a real
boundary, but not so much client-side state that the server has stopped owning
the page.

It is a good fit for product sites, admin tools, documentation-style apps, HTMX
interfaces, forms, and marketing sites that still want Go to render the first
view.

It is probably not the right tool if all HTML comes from a frontend build, or if
the server only returns JSON.

## Trade-offs

Petra is a convention, not a neutral primitive. Template names become part of
the app's internal API. Layouts, page blocks, and component names need the same
care as handler methods. Moving an older app to Petra can create template churn,
especially when the app uses full-page templates or path-based partial calls
everywhere.

Petra does not replace application architecture. You still need clear
controllers, view models, cache rules, error handling, and routing. Petra gives
the template and asset layer a common set of tools.

## Install

```sh
go get github.com/illyabusigin/petra@latest
```

## Start here

For the smallest API examples, read the package examples on
[pkg.go.dev](https://pkg.go.dev/github.com/illyabusigin/petra#pkg-examples).
They cover `ParseDir`, `ParseFS`, `ExecuteTemplate`, `Exec`, `ReloadDir`, and a
custom plugin.

For working apps, start with the focused examples:

- `examples/mvcweb`: Chi, controllers, layouts/components, hot reload, embedded production mode.
- `examples/tailwind`: Tailwind compiled with Vite and served through Petra's asset helper.
- `examples/debugerrors`: development error pages for broken page and component templates.

The other example folders cover Alpine, forms, and HTMX partial swaps.

## Template layout

Petra expects a tree like this:

```text
templates/
  layout.html
  components/
    header.html
    footer.html
    icons/
      search.html
  products/
    index.html
    layout.html
```

`Layout` names the layout file. The default is `layout.html`.

`IncludeDir` names the directory used for reusable component templates. The default is `includes`; set it to `components` when a project uses that convention.

When Petra parses a page template, it includes matching layouts and component directories from the page's directory hierarchy. Component directories are recursive: files under `components/icons/search.html` are component templates, not executable pages.

By default, every non-layout file outside a component directory is treated as a page template, regardless of extension. A page such as `templates/products/index.html` is executed as `products/index`; a file such as `templates/robots.txt` is executed as `robots`.

Set `PageExtensions` when a site needs to keep non-template files in the template tree:

```go
tmpl := petra.NewWithOptions(petra.Options{
	IncludeDir:     "components",
	PageExtensions: []string{".html"},
})
```

## ParseDir and ParseFS

Construct a template set with defaults:

```go
tmpl := petra.NewWithOptions(petra.Options{
	IncludeDir:     "components",
	PageExtensions: []string{".html"},
	FuncMap:        funcs,
	Logger:         logger.With("component", "petra"),
	Plugins: petra.Plugins{
		petra.SVG(staticFS, "static/assets/svg"),
		petra.Markdown(staticFS, "static/markdown"),
		petra.HTML(),
	},
})
```

`Logger` is optional. When set, Petra writes debug-level parse and reload metrics with fields such as `duration`, `pages`, `component_dirs`, `full_reload`, `changed_path_count`, `rebuilt_page_count`, `changed_paths`, `rebuilt_pages`, and `fallback_reason`. Petra does not log by default.

Use `ParseDir` during local development:

```go
if err := tmpl.ParseDir("./cmd/site/templates"); err != nil {
	return err
}
```

Use `ParseFS` for embedded production builds:

```go
//go:embed templates/*
var templatesFS embed.FS

if err := tmpl.ParseFS(templatesFS, "templates"); err != nil {
	return err
}
```

Template parses are swapped atomically, so development hot reload can parse a new template set while requests are being served.

## Exec and ExecuteTemplate

Use `ExecuteTemplate` for normal pages:

```go
err := tmpl.ExecuteTemplate(w, "products/index", view)
```

Use `Exec` for small inline fragments that need access to component functions:

```go
err := tmpl.Exec(w, `{{ProductCard .}}`, product)
```

`Exec` clones the parsed component template pool before parsing the inline fragment.
Namespaced components work there too:

```go
err := tmpl.Exec(w, `{{UI.TextField "email" "Email" "email" .Attrs .Errors.Email}}`, view)
```

## Debug error pages

`ExecuteTemplate`, `ParseDir`, `ParseFS`, and hot reload failures carry
structured debug metadata through `*petra.DebugError`. The error unwraps to the
original parse or execution error.

In development, render pages into a buffer and opt in to Petra's debug page:

```go
var buf bytes.Buffer
if err := tmpl.ExecuteTemplate(&buf, "products/index", view); err != nil {
	if petra.RenderDebugError(w, r, err, petra.DebugOptions{
		Enabled:        dev,
		IncludeGoStack: dev,
		Title:          "Template error",
	}) {
		return
	}
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	return
}
_, _ = w.Write(buf.Bytes())
```

The hot reload browser client uses the same debug metadata for reload error
overlays. See [debug error states](docs/reference/debug-errors.md) for the
capture points and production safety rules.

## Plugins

Plugins add template functions with `Funcs()` and can install helper templates
with `Apply()`. Petra copies `FuncMap` first, then applies plugin functions in
order. If a plugin returns the same function name as `FuncMap`, the plugin wins.
If two plugins return the same function name, the later plugin wins.

`SVG` loads an SVG from an `fs.FS` root and can replace its `class` attribute:

```gotemplate
{{SVG "logo-mark" "h-8 w-8"}}
```

Missing SVG files fail template execution. SVG files are treated as trusted
repository-controlled assets; this helper is not an SVG sanitizer. The class
argument is trusted too.

`Markdown` renders Markdown from an `fs.FS` root:

```gotemplate
{{Markdown "archive/generals"}}
```

Missing Markdown files fail template execution. Markdown content is rendered as
trusted HTML after parsing, so do not use this helper for untrusted user input.

`HTML` exposes helpers for trusted HTML, JavaScript, and attributes. Use `html` and `js` only for content generated by the application. The `attrs` helper validates attribute names, escapes values, blocks event/style attributes, and rejects unsafe URL schemes.

`ComponentSet` registers reusable template-backed components. Source definitions
are namespace-free:

```gotemplate
{{define "TextField name label id attrs error?"}}
  <label for="{{.id}}">{{.label}}</label>
  <input id="{{.id}}" name="{{.name}}"{{range $k, $v := .attrs}} {{attrs $k $v}}{{end}}>
  {{if .error}}<span class="error">{{.error}}</span>{{end}}
{{end}}
```

Mount the set into the app namespace you want:

```go
uiSet := petra.NewComponentSet(
	"github.com/acme/petra-ui",
	uiFS,
	"components",
	petra.Requires(petra.HTML()),
)

tmpl := petra.NewWithOptions(petra.Options{
	FuncMap: template.FuncMap{"dict": dict},
	Plugins: petra.Plugins{
		petra.Components("UI", uiSet),
	},
})
```

```gotemplate
{{UI.TextField "email" "Email" "email" (dict "type" "email") .Errors.Email}}
```

Definitions whose first rune is uppercase are public. Lowercase and
underscore-prefixed definitions are private helpers.

A component set can privately import another set:

```go
base := petra.NewComponentSet("github.com/acme/petra-base", baseFS, "components")
kit := petra.NewComponentSet(
	"github.com/acme/petra-kit",
	kitFS,
	"components",
	petra.Import("Base", base),
)
```

`kit` templates can call `{{Base.Button "Save"}}`. The app does not get a
`Base` namespace unless it mounts `base` itself.

An OSS package can expose a set directly:

```go
package ui

//go:embed components/*
var files embed.FS

var Set = petra.NewComponentSet(
	"github.com/acme/petra-ui",
	files,
	"components",
	petra.Requires(petra.HTML()),
)

func Components(namespace string) petra.Plugin {
	return petra.Components(namespace, Set)
}
```

`Markdown` and `SVG` cache rendered output for the current parsed template set.
`ParseDir`, `ParseFS`, and successful reloads build a new template set, so those
caches reset after a reparse.

See [plugin trust and cache behavior](docs/reference/plugins.md) for the full
contract and a custom plugin example.

## Hot reload

`NewHotReloadController` watches template folders and broadcasts `reload` over `/_reload/ws`.

Template reloads are selective during development. Petra keeps a graph of page, layout, and component-directory relationships:

- Editing a page rebuilds that page.
- Editing a section layout rebuilds pages under that layout.
- Editing a component rebuilds pages that include that component directory and refreshes the component template pool used by `Exec`.
- Creating, removing, or renaming template files falls back to a full graph rebuild.
- Editing a watched component set folder outside the app template root falls back to a full template reparse.
- Failed parses keep the previous working template set active.

Manual reloads can use `Reload` or `ReloadDir`:

```go
result, err := tmpl.ReloadDir("products/index.html")
```

`Reload` accepts operation-aware file events for watcher integrations. `ReloadDir` treats paths as write events and is mostly useful in tests or custom development tools.

The controller also serves a small development client script at `/_reload/client.js`. It reloads the browser on successful template changes and shows an overlay when a template reload fails.

Mount the controller with the standard `http` package, Chi, or any router that accepts an `http.Handler`:

```go
hotReload := petra.NewHotReloadControllerWithOptions(petra.HotReloadOptions{
	Template: tmpl,
	Folders:  []string{templatesDir, uiLibraryDir},
	Logger:   logger.With("component", "petra_hot_reload"),
})
mux.Handle("/_reload/", hotReload.Handler())
```

Call `Close()` on development shutdown so watchers and websocket sessions stop cleanly:

```go
hotReload := petra.NewHotReloadControllerWithOptions(petra.HotReloadOptions{
	Template: tmpl,
	Folders:  []string{templatesDir},
})
defer hotReload.Close()
```

`Static` watches local static files. CSS writes refresh matching stylesheet
links in the browser. JavaScript, image, font, unknown, remove, and rename
events reload the page. Use `NewStaticWithOptions` when the caller needs
lifecycle control and explicit dev settings:

```go
static := petra.NewStaticWithOptions(petra.StaticOptions{
	Socket:      hotReload.Socket(),
	Folder:      staticDir,
	StripPrefix: "/static/",
	Logger:      logger.With("component", "petra_static"),
})
defer static.Close()
```

Use `StaticFS` for simple embedded static assets in production. The
`stripPrefix` argument is also used as the embedded filesystem prefix, so
`StaticFS(webFS, "/static/")` serves requests like `/static/app.css` from
`static/app.css` inside the embedded filesystem. Pass an empty prefix to serve
files from the embedded filesystem root.

Use `Assets` for templates that generate cache-safe asset URLs. Production URLs
include a SHA-256 content hash in the filename. Development URLs stay readable
and, when `DevDir` is set, include an mtime query string for local cache
busting:

```go
assets, err := petra.NewAssets(petra.AssetOptions{
	Files:  webFS,
	Root:   "static",
	Prefix: "/static/",
	Dev:    dev,
	DevDir: "./cmd/static",
})
if err != nil {
	return err
}

tmpl := petra.NewWithOptions(petra.Options{
	FuncMap: template.FuncMap{
		"Asset": assets.URL,
	},
})

mux.Handle("/static/", assets.Handler())
```

```gotemplate
<link rel="stylesheet" href="{{ Asset "app.css" }}">
```

In production, `{{ Asset "app.css" }}` returns a path like
`/static/app-<sha256>.css`. Verified hashed requests are served with
`Cache-Control: public, max-age=31536000, immutable`. Raw production requests
such as `/static/app.css` still serve for compatibility, but use
`Cache-Control: no-cache` because the URL can point at different bytes after a
deploy. Development responses use `Cache-Control: no-store`.

`Assets.Handler()` keeps Petra's startup-time Brotli/gzip compression path.
`DevDir` is used only for development URL versioning; it does not make the
handler serve from disk. If the app also wants development file watching, mount
`NewStaticWithOptions` in development and `assets.Handler()` in production, as
shown in `examples/tailwind`.

See `examples/mvcweb` for a small Chi app with controllers, nested Petra templates, hot reload, static assets, and embedded production rendering.

## More docs

- [Debug error states](docs/reference/debug-errors.md)
- [Plugin trust and cache behavior](docs/reference/plugins.md)
- [Benchmarks](docs/benchmarks.md)
- [Selective hot reload spec](docs/design/selective-hot-reload.md)
- [Hashed static assets](docs/design/hashed-static-assets.md)
- [Contributing](CONTRIBUTING.md)
