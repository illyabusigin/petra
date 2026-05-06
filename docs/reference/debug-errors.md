# Debug error states

Petra has a development-only error UI for broken pages and components. It is meant to feel closer to the React or Ember development overlays than to a plain `http.Error`: show the failing template, the request, changed files during hot reload, component frames when Petra can identify them, and a Go stack trace when the app opts in.

The rule is simple: Petra captures structure, the application decides whether to show it.

## Capture points

Petra captures debug metadata in four places:

- `ParseDir` and `ParseFS` wrap initial parse failures. These errors usually point at a template file, layout, component directory, or plugin function setup.
- `Reload` enriches parse failures with changed paths, fallback reason, and rebuild context. The previous working template set stays active.
- `ExecuteTemplate` wraps page render failures with the page name and execution location.
- `tmplfunc` wraps component-style calls, so errors from `{{Hero .}}` or `{{CTA "Buy"}}` can include a component frame instead of only a Go template error string.

The exported error type is `*petra.DebugError`. It unwraps to the original error, so `errors.Is`, `errors.AsType`, and callers that already check `petra.ParseError` or `petra.ExecuteError` still work.

When Petra can map the Go template location back to one file, the debug info includes a source excerpt. It does not guess across duplicate names such as two `layout.html` files. During reload, the changed path is used as a source hint, so a broken section layout can still point at `products/layout.html` even if Go reports the template name as `layout.html`.

The `DependencyRole` field says whether the failing file is a page, layout, component, func map setup, or inline template. Reload errors also include `AffectedPages` when Petra knows which pages were being rebuilt.

Use `petra.DebugInfo(err)` when code needs the structured form without rendering HTML.

## Page render boundary

Applications should render into a buffer first. If template execution fails, the app can show the debug page in development and fall back to a normal 500 in production.

```go
var buf bytes.Buffer
if err := tmpl.ExecuteTemplate(&buf, "marketing/home", data); err != nil {
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

w.WriteHeader(http.StatusOK)
_, _ = w.Write(buf.Bytes())
```

Buffering matters. If the app writes directly to `http.ResponseWriter`, a template can fail after partial HTML has already been sent, which leaves no clean way to replace the response with a debug page.

## Hot reload overlay

`HotReloadController` already broadcasts reload failures to the browser. The payload now includes the same debug metadata used by the page renderer. The browser client uses it for the development overlay.

The overlay is intentionally tied to hot reload. Do not mount the reload routes in production.

## Security

Debug pages and overlays can expose local paths, template names, changed files, and stack traces. Treat them as development UI only.

For production:

- keep `DebugOptions.Enabled` false;
- do not mount `/_reload`;
- log the original error server-side;
- return a generic 500 response to the browser.
