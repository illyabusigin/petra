# Component set example

This example shows a small Petra component library mounted into an app.

The `ui` package exports:

- `ui.Set` for other component packages;
- `ui.Components(namespace)` for production app mounting;
- `ui.DevComponents(namespace, root)` for live component development.

The app mounts the library as `UI`, so templates call components such as:

```gotemplate
{{UI.Stat "Published components" "4" "from a component set"}}
```

The source in `ui/components/core.html` stays namespace-free:

```gotemplate
{{define "Stat label value detail?"}}...{{end}}
```

Run the embedded production-style app:

```sh
go run .
```

Run from disk with hot reload:

```sh
go run . -dev
```

Open:

```text
http://localhost:8080
```

In dev mode, Petra watches both `templates/` and `ui/components/`. Editing the
component set triggers a full template reparse; if parsing fails, Petra keeps
serving the previous working template set.
