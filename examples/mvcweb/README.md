# Petra MVC example

This is a small Chi app showing how Petra fits into a conventional server-rendered Go web service:

- `cmd.Web` owns server setup, static files, template parsing, hot reload, and shutdown.
- `controllers.Env` holds shared services.
- Controllers own routes, build request-scoped page contexts, and call shared
  `HTML`/`Exec` helpers on the base controller.
- Templates use a root layout plus nested `components`.

Controller methods stay small:

```go
func (c homeController) render(w http.ResponseWriter, r *http.Request) {
	ctx := c.MarketingContext(r)
	ctx.SetTitle("Petra MVC example")

	c.HTML(w, "marketing/home", ctx)
}
```

The helpers are intentionally local to the example. Petra provides the template
runtime; the application owns controller shape, request context, logging, and
HTTP policy.

Run development mode from this directory:

```sh
go run . -dev -verbose
```

Development mode parses templates from disk, serves `/_reload/client.js`, watches templates, and reloads static assets through the shared websocket. `-verbose` enables debug logs, including Petra parse metrics, reload decisions, changed paths, and rebuilt page IDs.

Run embedded mode:

```sh
go run .
```

Embedded mode uses `embed.FS`, `ParseFS`, and `StaticFS`.
