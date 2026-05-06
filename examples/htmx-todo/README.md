# HTMX todo example

This example shows Petra rendering pages and components while
`github.com/donseba/go-htmx` handles HTMX request/response behavior.

It keeps the moving parts narrow:

- Petra renders the full page and the reusable `Todos` component.
- `go-htmx` detects partial requests with `RenderPartial`.
- `go-htmx` sets `HX-Retarget`, `HX-Reswap`, and trigger headers.
- The todo store is in memory so the example stays about rendering, not storage.

Run it:

```sh
go run .
```

Open:

```text
http://localhost:8080
```

The form has a plain POST fallback. HTMX requests swap only the todo list, using
the same Petra component the full page renders.
