# Alpine example

This example shows Petra with Alpine bundled by Vite.

It does not include Tailwind, forms, controllers, or a router. The point is the
progressive enhancement path:

- `assets/app.js` imports Alpine and registers one `disclosure` component.
- `npm run assets:build` writes `static/app.js`.
- Go embeds `templates/*` and `static/*`.
- Petra renders HTML with Alpine attributes already in place.
- `go run . -dev` switches to disk templates/static assets and serves Petra's
  reload client from `/_reload/client.js`.

Run the production-shaped embedded app:

```sh
npm install
npm run assets:build
go run .
```

During JavaScript work, use two terminals:

```sh
npm run assets:watch
```

```sh
go run . -dev
```

Vite rewrites `static/app.js` when Alpine code changes. Petra watches that
folder and reloads the page for JavaScript changes so the browser gets a fresh
module graph and Alpine starts from clean state. Template edits also reload the
page.

For Petra reload metrics while tuning the loop:

```sh
go run . -dev -verbose
```
