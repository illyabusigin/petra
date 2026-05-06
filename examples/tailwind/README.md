# Tailwind example

This example shows Petra with Tailwind compiled by Vite.

It does not include Alpine, forms, controllers, or a router. The point is the
asset path:

- `assets/app.css` is the Tailwind source.
- `@source "../templates/**/*.html"` tells Tailwind to scan Petra templates for
  utility classes.
- `npm run assets:build` writes `static/app.css`.
- Go embeds `templates/*` and `static/*`.
- Petra renders the page with the `Assets` helper and serves hashed production
  URLs with immutable cache headers.
- `go run . -dev` switches to disk templates/static assets and serves Petra's
  reload client from `/_reload/client.js`.

Run the production-shaped embedded app:

```sh
npm install
npm run assets:build
go run .
```

During CSS work, use two terminals:

```sh
npm run assets:watch
```

```sh
go run . -dev
```

Vite rewrites `static/app.css` when Tailwind input changes. Petra watches that
folder and sends `reload_assets`, so the browser cache-busts the matching
stylesheet link without a full page reload. Template edits still reload the
page.

For Petra reload metrics while tuning the loop:

```sh
go run . -dev -verbose
```
