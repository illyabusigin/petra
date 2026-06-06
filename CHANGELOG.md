# Changelog

## v0.0.4 - 2026-06-06

- Adds `ComponentSet` support for namespace-free component libraries mounted with `petra.Components("UI", set)`.
- Adds private component-set imports, Go-style public/private visibility by casing, required render plugins, and `Exec` support for mounted component namespaces.
- Refactors `tmplfunc` to use Go's standard `text/template/parse` package, adds checked dotted-call rewriting, and removes Petra's unused parser fork.
- Documents the component-set architecture and plugin parse order.
- Adds `examples/component-set`, a runnable app with a small UI component set and dev hot reload for both app templates and component files.

## v0.0.3 - 2026-05-07

- Updates CI and all Go modules to Go 1.26.3 to pick up `html/template` XSS fixes for GO-2026-4982 and GO-2026-4980.

## v0.0.2 - 2026-05-06

Initial public release candidate for `github.com/illyabusigin/petra`.

- Adds template tree parsing with nested layouts and recursive component directories.
- Adds component-style template calls through `tmplfunc`.
- Adds structured debug errors and opt-in development debug pages.
- Adds template and static-asset hot reload helpers for development.
- Adds production static serving, hashed asset URLs, and built-in HTML, Markdown, and SVG plugins.
- Adds focused examples for MVC-style apps, forms, Tailwind, Alpine, debug errors, and HTMX.
