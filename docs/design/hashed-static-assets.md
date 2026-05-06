# hashed static assets and production cache headers

Last reviewed: 2026-05-01.

Status: first pass implemented in this repository.

This plan covers production asset URLs and cache headers for Petra. It was written after reading the current Petra static server, an application using Petra, `statigz`, `andybalholm/brotli`, and `github.com/benbjohnson/hashfs@v0.2.2`.

External references checked:

- [`hashfs` README at `v0.2.2`](https://github.com/benbjohnson/hashfs/blob/v0.2.2/README.md)
- [`hashfs.go` at `v0.2.2`](https://github.com/benbjohnson/hashfs/blob/v0.2.2/hashfs.go)

## decision

Add this to Petra as an opt-in asset layer. Do not change the behavior of `StaticFS` in place.

The first implementation should:

- Keep `petra.StaticFS` working exactly as it works today.
- Add a new Petra asset API that can generate content-hashed URLs and serve those URLs with immutable cache headers.
- Use `hashfs` for SHA-256 filename formatting and validation.
- Keep Petra's current `statigz` production serving path for Brotli/gzip and startup-time compression.
- Let applications adopt the helper progressively, one asset reference at a time.

This is a progressive enhancement at the API level. Existing Petra users should not get hashed URLs or year-long browser caches unless they ask for that behavior.

It is "all-in" per asset reference. If a template keeps using `/static/app.css`, that request should still work, but it should not get immutable caching. The immutable policy belongs to URLs that include a verified content hash.

## facts from the current code

Petra production static serving currently goes through `StaticFS`:

- `StaticFS` builds a `statigz.FileServer`.
- It enables `brotli.AddEncoding`.
- It enables `statigz.EncodeOnInit`.
- It uses the configured strip prefix as the embedded filesystem prefix.
- It serves through `http.StripPrefix`.

That code lives in `static.go`.

`statigz` does useful work that we should not throw away:

- It indexes the filesystem when the server is built.
- It precompresses eligible files at startup when `EncodeOnInit` is set.
- It skips formats that are already compressed, including `.png` and `.webp`.
- It chooses Brotli or gzip based on `Accept-Encoding`.
- It sets `Vary: Accept-Encoding`.
- It sets `ETag` and handles `If-None-Match`.

The current gap is cache policy. The responses have validators, but they do not have `Cache-Control`.

The app this plan was written for uses embedded production assets:

- `services/web/cmd/web.go` embeds `templates/*`, nested template folders, and `static/*`.
- Production calls `tmpl.ParseFS(webFS, "templates")`.
- Production mounts `/static/*` with `petra.StaticFS(webFS, "/static/")`.
- Development uses `ParseDir`, `NewStaticWithOptions`, filesystem assets, and hot reload.

The current app also has asset URLs in two shapes:

- Literal template paths, such as `/static/app.css`, `/static/app.js`, `/static/brandmark.svg`, and home hero images.
- Controller data paths, such as `Product.HomeImage`, `ArchiveEntry.Image`, and OpenGraph images.

That second shape matters. Hashing only the literal CSS/JS tags would leave much of the asset traffic on raw URLs.

## facts from `hashfs@v0.2.2`

`hashfs` is a small package with no third-party dependencies. Its module target is Go 1.16.

The released `v0.2.2` API does three things we care about:

- `hashfs.NewFS(fsys)` wraps an `fs.FS`.
- `(*hashfs.FS).HashName(name)` reads a file, computes a SHA-256 hash, and formats a filename with the full 64-character hex hash before the first dot in the basename. For example, `x.tar.gz` becomes `x-<hash>.tar.gz`.
- `hashfs.ParseName(name)` splits a hashed filename back into the base path and hash when the name contains `-[0-9a-f]{64}`.

It also has a `hashfs.FileServer`, but Petra should not use that server in the first pass.

Reasons:

- `hashfs.FileServer` is intentionally simplified. Its own comment says it removes directory canonicalization, `index.html` defaulting, precondition checks, and content range headers.
- It does not do Brotli or gzip.
- `hashfs.FS` exposes `Open`; it is not a `fs.ReadDirFS`, so it is not a drop-in input to Petra's current `statigz.FileServer` call.
- Wrapping it in a Brotli middleware would usually compress on the request path.
- Petra already has a production static path that precompresses compressible assets once at startup.

Use `hashfs` as the hash naming and validation library. Keep Petra's static server responsible for bytes on the wire.

## not the plan

Do not replace `petra.StaticFS` with `hashfs.FileServer`.

Do not add dynamic Brotli middleware as the primary direct-serving path. Dynamic compression is acceptable behind a CDN that caches the compressed response, but Petra's current direct-serving path is better for this repository because it avoids per-request compression for CSS and JavaScript.

Do not make every Petra app use asset helpers. Some users may want plain embedded files, short-lived assets, or a CDN-managed build pipeline.

Do not make Petra a bundler or manifest generator. The hash comes from file contents in the Go filesystem. JavaScript and CSS build steps stay in the application.

## proposed public API

Add a new type rather than changing `StaticFS`:

```go
type AssetOptions struct {
	// Files is the embedded or virtual filesystem that contains production assets.
	Files fs.ReadDirFS

	// Root is the asset root inside Files. For many apps this is "static".
	Root string

	// Prefix is the URL prefix. For many apps this is "/static/".
	Prefix string

	// Dev switches URL generation to development behavior.
	Dev bool

	// DevDir is optional. When set, development URLs can include an mtime query
	// string for hard-refresh cache busting.
	DevDir string

	// CacheControlHashed overrides the immutable hashed response policy.
	CacheControlHashed string

	// CacheControlUnhashed overrides the raw-path production policy.
	CacheControlUnhashed string

	// CacheControlDev overrides the development policy.
	CacheControlDev string
}

type Assets struct {
	// unexported fields
}

func NewAssets(opts AssetOptions) (*Assets, error)

func (a *Assets) Handler() http.Handler
func (a *Assets) URL(name string) (string, error)
```

`URL` should return `(string, error)` so templates fail loudly when an asset path is wrong:

```go
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

Accepted input forms:

- `app.css`
- `hero/product-archive.webp`
- `/static/app.css`

Rejected input forms:

- empty path
- paths that escape the asset root after cleaning
- absolute `http://` or `https://` URLs

External URLs should stay outside this helper. That keeps the trust boundary clear.

## serving behavior

The production handler should wrap a `statigz.FileServer` rather than replacing it.

Request handling:

1. Accept the incoming request under `Prefix`.
2. Normalize the asset-relative request path with `path.Clean`.
3. Parse the normalized path with `hashfs.ParseName`.
4. If no hash is present, serve the raw file through `statigz` and set the raw-path cache policy.
5. If a hash is present, compute the expected hash name for the base file.
6. If the requested hash does not match the expected hash, return `404`.
7. If the hash matches, rewrite the request path to the base asset path and serve through `statigz`.
8. Set the hashed cache policy before calling the inner handler.

Default cache policies:

```text
hashed production URL:
  Cache-Control: public, max-age=31536000, immutable

raw production URL:
  Cache-Control: no-cache

development URL:
  Cache-Control: no-store
```

`no-cache` on raw production URLs means the browser can store the response but must revalidate before reuse. That matches the fact that raw URLs can change without the URL changing.

`immutable` only appears after Petra has verified the hash. A typo such as `/static/app-0000....css` should not fall back to `/static/app.css`.

`statigz` should continue to set `Vary`, `Content-Encoding`, `Content-Type`, `Content-Length`, and `ETag`. In the first pass, Petra should not fight `statigz` over `ETag`. The immutable URL makes the validator secondary.

## URL generation

In production:

```text
Asset("app.css") -> /static/app-<sha256>.css
```

In development:

```text
Asset("app.css") -> /static/app.css
```

If `DevDir` is set, development can keep the current hard-refresh behavior:

```text
Asset("app.css") -> /static/app.css?v=<mtime-base36>
```

The query string should not be used in production. Production cache busting should come from the filename.

## how this fits Petra's existing APIs

`StaticFS` remains the simple embedded static server:

```go
r.Handle("/static/*", petra.StaticFS(webFS, "/static/"))
```

The new API is for callers who want cache-safe asset URLs:

```go
assets, err := petra.NewAssets(petra.AssetOptions{
	Files:  webFS,
	Root:   "static",
	Prefix: "/static/",
	Dev:    w.Dev,
	DevDir: filepath.Join(w.RootDir, "cmd", "static"),
})
if err != nil {
	return nil, err
}

tmpl := petra.NewWithOptions(petra.Options{
	IncludeDir: "components",
	FuncMap: template.FuncMap{
		"Asset": assets.URL,
	},
	Plugins: plugins,
})

if w.Dev {
	r.Handle("/static/*", static)
} else {
	r.Handle("/static/*", assets.Handler())
}
```

This keeps the development watcher path intact. The first implementation can make `Assets.Handler()` production-only and return the existing dev static handler from the application.

A later pass can add a dev handler constructor if the pattern repeats across examples.

## adoption model

This should be progressive.

Existing Petra users:

- `StaticFS` keeps working.
- `NewStaticWithOptions` keeps working.
- Templates with hard-coded `/static/...` keep working.
- No one gets year-long caching by surprise.

New or migrated users:

- Add `Assets`.
- Add the `Asset` template function.
- Replace local static URLs with `{{ Asset "..." }}`.
- Use `Assets.Handler()` in production.

The benefit appears only where the asset helper is used. That is the right trade-off. A partial migration is safe, but it is easy to see from source review which URLs are still raw.

## application adoption plan

Applications should adopt this in stages.

### stage 1: Petra package

Add the Petra API and tests without changing the web app.

Files expected:

- `assets.go`
- `assets_test.go`
- `docs/design/hashed-static-assets.md`
- `README.md`
- one Petra example app update, probably `examples/tailwind`

Add `github.com/benbjohnson/hashfs v0.2.2` to `go.mod`.

### stage 2: literal template assets

Update literal asset tags:

- `layout.html`: favicon, apple touch icon, CSS, JavaScript.
- `marketing/home.html`: hero image stack.
- `marketing/archive.html`: archive hero image.
- `components/archive_card.html`: card images, if the backing data stays local.

The layout CSS line should lose `.AssetVersion`. The asset helper replaces it.

### stage 3: controller data assets

The controllers currently put local static paths into data structs:

- `Product.HomeImage`
- `Product.OGImage`
- `SelectedWorkItem.HomeImage`
- `ArchiveEntry.Image`
- `ArchiveEntry.OGImage`
- `OpenGraph.Image.URL`

There are two reasonable migration paths:

1. Store asset-relative names in data, such as `work/contract-iq-work.webp`, and call `{{ Asset .HomeImage }}` in templates.
2. Keep `/static/...` paths in data and run them through a controller helper before rendering.

Prefer option 1 for new or edited data. It makes local assets explicit and prevents accidentally passing an external URL to `Asset`.

OpenGraph needs a separate application helper because those URLs are absolute:

```go
func (e *Env) PublicAssetURL(name string) (string, error) {
	p, err := e.Assets.URL(name)
	if err != nil {
		return "", err
	}
	return publicURL(p), nil
}
```

Then `openGraphImage` can receive either an already-public URL or a local asset name. The app should keep external product links out of this helper.

### stage 4: raw URL cleanup

After migration, run:

```sh
rg '"/static/|/static/' services/web/controllers services/web/cmd/templates
```

Every remaining `/static/` should be intentional. Good candidates:

- tests that assert raw fallback behavior
- docs explaining old URLs
- external compatibility paths, if any

## tests

Petra tests should cover URL generation:

- production `URL("app.css")` returns `/static/app-<64 hex>.css`.
- production `URL("/static/app.css")` returns the same hashed URL.
- development `URL("app.css")` returns `/static/app.css`.
- development with `DevDir` returns `/static/app.css?v=<mtime>`.
- missing asset returns an error.
- path traversal returns an error.
- absolute URL returns an error.

Petra tests should cover handler behavior:

- raw production request returns `200` with `Cache-Control: no-cache`.
- valid hashed production request returns `200` with `Cache-Control: public, max-age=31536000, immutable`.
- valid hashed CSS request with `Accept-Encoding: br,gzip` returns `Content-Encoding: br`.
- valid hashed CSS response keeps `Vary: Accept-Encoding`.
- `If-None-Match` still produces `304` through `statigz`.
- mismatched hash returns `404`.
- directory requests do not list files.
- `HEAD` works for assets.
- PNG/WebP are not Brotli-compressed.

Application tests should cover:

- production HTML contains hashed CSS and JS paths.
- production HTML does not contain `.AssetVersion` query strings.
- production HTML contains hashed hero image paths.
- OpenGraph image URLs are absolute and hashed.
- the hashed OpenGraph image paths serve `200`.
- a raw `/static/app.css` still serves `200` with the raw-path cache policy during migration.

## compatibility and failure modes

Wrong hashed URL:

- Return `404`.
- Do not serve the base file.

Missing asset in template:

- Return a template execution error through the `Asset` function.
- In development, Petra debug pages should show the template error.

Missing asset requested directly:

- Let the handler return `404`.

Hash calculation failure:

- Return an error from `URL`.
- Do not silently return a raw path. `hashfs.HashName` returns the original name on read failure, but Petra should wrap it with an existence/read check so template mistakes fail visibly.

Raw URL in production:

- Serve it.
- Use the raw cache policy.
- Keep this as a migration and compatibility path.

## implementation notes

Use `fs.Sub` or an internal root adapter so `Assets` hashes names relative to the asset root. The public helper should not expose `static/` as part of the asset name. `fs.Sub` returns the static type `fs.FS`, so the implementation should assert `fs.ReadDirFS` before handing the sub-filesystem to `statigz`.

Normalize with `path`, not `filepath`, because these are URL and `fs.FS` paths.

Preserve query strings only for development URL generation. Production hashed asset URLs should be path-only.

Be careful when rewriting requests before calling `statigz`. Clone the request or restore the original path so outer middleware and logs are not confused.

The handler should set `Cache-Control` before calling the inner static server. `statigz` does not overwrite that header today.

Do not assert exact `ETag` values in new tests unless the test is about `statigz` itself. The encoded and unencoded variants can have different validators.

## docs and examples

Update Petra README with:

- When to use `StaticFS`.
- When to use `Assets`.
- Why hashed URLs get immutable caching.
- Why raw URLs do not.
- How development differs from production.

Update one example app. The Tailwind example is the best candidate because it already has CSS/JS build output and static development reload.

Add a short note to the plugin reference only if the final API exposes a template helper through `FuncMap`. This is not a plugin; do not force it into the plugin system.

## acceptance criteria

The feature is ready when:

- Existing `StaticFS` tests pass unchanged.
- `make ci` passes.
- The new Petra tests prove hashed URLs, cache headers, compression, and invalid-hash behavior.
- An application can use one `Asset` helper in templates for CSS, JS, icons, and images.
- Production static assets can be cached for a year only when the request URL contains the verified hash.
- Direct Go serving keeps Brotli/gzip without adding per-request compression for normal static asset responses.

## later work

Possible later additions:

- A helper for absolute public asset URLs, if multiple apps need OpenGraph-style URLs.
- A dev handler that combines static watching, no-store headers, and mtime URL generation.
- A manifest dump for debugging or CDN preload lists.
- Short hash display as an option. The first pass should keep `hashfs`'s full 64-character hash to avoid creating another naming contract.
