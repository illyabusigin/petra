# hashed static assets and production cache headers

Last reviewed: 2026-05-08.

Status: implemented in `assets.go` and covered by `assets_test.go`.

This document describes Petra's implemented hashed asset layer. Earlier design
notes in this file have been folded into the current API contract, serving
behavior, and rationale.

## what is implemented

Petra has an opt-in `Assets` type for applications that want templates to emit
cache-safe asset URLs:

- `StaticFS` remains the simple embedded static file server.
- `NewStaticWithOptions` remains the development static server with hot reload.
- `NewAssets` creates a template URL helper and an HTTP handler.
- Production URLs include a full SHA-256 content hash in the filename.
- Verified hashed production requests get immutable cache headers.
- Raw production requests still serve, but use revalidation.
- Development URLs stay readable and can include an mtime query string.
- The handler keeps Petra's `statigz` path for startup-time Brotli/gzip
  compression.

Existing Petra users do not get hashed URLs or year-long browser caches unless
they choose `Assets` and call the helper from templates.

## public API

```go
type AssetOptions struct {
	// Files is the embedded or virtual filesystem containing the asset tree.
	Files fs.ReadDirFS

	// Root is the asset root inside Files. When empty, Files is used as-is.
	Root string

	// Prefix is the request path prefix used for generated URLs. When empty,
	// "/" is used.
	Prefix string

	// Dev switches URL generation and cache headers to development behavior.
	Dev bool

	// DevDir is the local filesystem directory for development assets. When
	// set, generated dev URLs include an mtime query string.
	DevDir string

	// CacheControlHashed overrides the cache policy for verified hashed asset
	// URLs.
	CacheControlHashed string

	// CacheControlUnhashed overrides the cache policy for raw asset URLs.
	CacheControlUnhashed string

	// CacheControlDev overrides the cache policy when Dev is true.
	CacheControlDev string
}

type Assets struct {
	// unexported fields
}

func NewAssets(opts AssetOptions) (*Assets, error)
func (a *Assets) Handler() http.Handler
func (a *Assets) URL(name string) (string, error)
```

`URL` returns `(string, error)` so template mistakes fail during execution:

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
```

Template usage:

```gotemplate
<link rel="stylesheet" href="{{ Asset "app.css" }}">
<script type="module" src="{{ Asset "app.js" }}"></script>
<img src="{{ Asset "hero/product-archive.webp" }}" alt="">
```

The helper is just a `FuncMap` entry. It is not a Petra plugin.

## URL generation

In production:

```text
Asset("app.css") -> /static/app-<sha256>.css
```

In development:

```text
Asset("app.css") -> /static/app.css
```

When `DevDir` is set, Petra checks the local development file and includes its
mtime in base 36:

```text
Asset("app.css") -> /static/app.css?v=<mtime>
```

Production URLs are path-only. Query-string cache busting is only for
development.

Accepted input forms:

- `app.css`
- `hero/product-archive.webp`
- `/static/app.css`, when `Prefix` is `/static/`

Rejected input forms:

- empty paths
- paths with query strings or fragments
- paths that escape the asset root after cleaning
- absolute URLs, including protocol-relative URLs
- absolute local paths outside the configured `Prefix`
- missing files
- directories

External URLs stay outside this helper. That keeps the trust boundary clear:
`Asset` is for local files in the configured asset filesystem.

## handler behavior

`Assets.Handler()` serves requests under `Prefix`.

Production behavior:

- Raw requests such as `/static/app.css` serve the file with
  `Cache-Control: no-cache`.
- Hashed requests are parsed with `hashfs.ParseName`.
- For a hashed request, Petra computes the expected hash name for the base file.
- If the requested hash matches, Petra rewrites the request to the base file and
  serves it with `Cache-Control: public, max-age=31536000, immutable`.
- If the hash does not match, Petra returns `404`.

Development behavior:

- Requests are served with `Cache-Control: no-store`.
- The handler serves the configured `Files` filesystem. `DevDir` is used for
  mtime URL generation, not for serving bytes.
- If the app needs disk serving, file watching, and browser reload messages,
  mount `NewStaticWithOptions` in development instead.

Custom cache policies can override the three defaults:

```text
hashed production URL: Cache-Control: public, max-age=31536000, immutable
raw production URL:    Cache-Control: no-cache
development URL:       Cache-Control: no-store
```

The immutable policy only appears after Petra verifies the hash. A typo such as
`/static/app-0000....css` does not fall back to `/static/app.css`.

## how it fits Petra's other static APIs

Use `StaticFS` when the application wants a small embedded static server without
template URL rewriting:

```go
r.Handle("/static/", petra.StaticFS(webFS, "/static/"))
```

Use `Assets` for templates that generate content-hashed URLs:

```go
assets, err := petra.NewAssets(petra.AssetOptions{
	Files:  webFS,
	Root:   "static",
	Prefix: "/static/",
	Dev:    dev,
	DevDir: filepath.Join(rootDir, "static"),
})
if err != nil {
	return err
}

if dev {
	static := petra.NewStaticWithOptions(petra.StaticOptions{
		Socket:      hotReload.Socket(),
		Folder:      filepath.Join(rootDir, "static"),
		StripPrefix: "/static/",
	})
	defer static.Close()
	r.Handle("/static/", static)
} else {
	r.Handle("/static/", assets.Handler())
}
```

That split keeps the development watcher path intact while production uses the
hashed asset handler. If an app does not need static hot reload, it can mount
`assets.Handler()` in development too; dev responses use `no-store`.

`examples/tailwind` shows the production and development wiring.

## implementation details

`NewAssets` requires an `fs.ReadDirFS`. If `Root` is set, Petra creates an
asset-relative filesystem with `fs.Sub`. Public asset names are relative to that
root, so templates use `app.css`, not `static/app.css`.

`Prefix` is normalized to a leading and trailing slash. An empty prefix becomes
`/`.

Path normalization uses `path`, not `filepath`, because these are URL and
`fs.FS` paths. Names are cleaned before hash calculation and before serving.

Production hash names come from `github.com/benbjohnson/hashfs@v0.2.2`.
`hashfs.HashName` reads the file and formats the full 64-character SHA-256 hash
before the first dot in the basename. For example, `x.tar.gz` becomes
`x-<hash>.tar.gz`.

The handler does not use `hashfs.FileServer`. Petra wraps a
`statigz.FileServer` instead, with `brotli.AddEncoding` and
`statigz.EncodeOnInit`. That preserves startup-time precompression, Brotli/gzip
content negotiation, `Vary: Accept-Encoding`, `ETag`, `If-None-Match`, and
`HEAD` handling.

When serving a verified hashed request, Petra clones the request and rewrites
the cloned URL path to the unhashed target before calling `statigz`. The
original request is left alone for outer middleware.

Petra sets `Cache-Control` before calling the inner static server. The current
`statigz` handler does not overwrite that header.

## migration notes

Adoption is progressive. Existing hard-coded `/static/...` URLs keep working,
but they do not get immutable caching. The benefit appears only where templates
or view data call `Asset`.

For literal template assets, replace local static paths with helper calls:

```gotemplate
<link rel="stylesheet" href="{{ Asset "app.css" }}">
```

For controller-provided asset names, prefer storing asset-relative names in the
view model:

```go
Product{HomeImage: "work/contract-iq-work.webp"}
```

Then call the helper in the template:

```gotemplate
<img src="{{ Asset .HomeImage }}" alt="">
```

If a field can contain either an external URL or a local asset, keep that choice
in application code. Do not pass arbitrary public URLs to `Asset`.

OpenGraph images usually need absolute URLs. Build that in the application by
calling `assets.URL(name)` first and then adding the public origin:

```go
func publicAssetURL(assets *petra.Assets, origin, name string) (string, error) {
	p, err := assets.URL(name)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(origin, "/") + p, nil
}
```

After a migration, search for remaining raw static paths and decide whether each
one is intentional:

```sh
rg '"/static/|/static/' .
```

Good remaining cases include compatibility paths, tests for raw fallback
behavior, and docs that explain the old URL shape.

## test coverage

`assets_test.go` covers the implemented contract:

- production URL generation, including `/static/...` input
- development URL generation
- development mtime query strings from `DevDir`
- rejection of empty, escaping, absolute, queried, fragmented, wrong-prefix,
  missing, and directory asset names
- raw production serving with `Cache-Control: no-cache`
- verified hashed production serving with immutable cache headers
- Brotli serving for hashed CSS when the client accepts it
- `Vary: Accept-Encoding`
- mismatched hashes returning `404`
- conditional requests through `ETag` and `If-None-Match`
- `HEAD` requests
- already compressed images, such as WebP, not being Brotli-compressed
- directory requests not exposing file listings
- development serving with `Cache-Control: no-store`

The Tailwind example has tests for production hashed CSS output, hashed asset
serving, embedded static content, and development `reload_assets` behavior.

## reference dependency notes

External references checked for the original design:

- [`hashfs` README at `v0.2.2`](https://github.com/benbjohnson/hashfs/blob/v0.2.2/README.md)
- [`hashfs.go` at `v0.2.2`](https://github.com/benbjohnson/hashfs/blob/v0.2.2/hashfs.go)

`hashfs.FileServer` is still not used here. Its own implementation removes
several `http.FileServer` behaviors, and it does not provide Brotli/gzip
serving. Petra keeps `statigz` so the asset layer keeps startup-time
compression, encoding negotiation, directory and `index.html` redirects, ETag
conditional requests, and range support.

Petra uses `hashfs` for naming and validation. `statigz` remains responsible
for bytes on the wire.

## possible later work

- A helper for absolute public asset URLs, if multiple apps need
  OpenGraph-style URLs.
- A development handler that combines static watching, `no-store` headers, and
  mtime URL generation.
- A manifest dump for debugging or CDN preload lists.
- Short hash display as an option. The current implementation uses the full
  `hashfs` hash to avoid creating another naming contract.
