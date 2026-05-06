# Debug errors example

This example shows Petra's rendered development error page for broken page and
component templates.

It keeps the app deliberately small:

- `/` renders successfully.
- `/broken-page` fails inside the page template.
- `/broken-component` fails inside a Petra component-style call.
- `go run . -dev` renders Petra's debug UI for those failures.
- Running without `-dev` returns a generic production 500.

Run it:

```sh
go run . -dev
```

Open:

```text
http://localhost:8080/broken-page
http://localhost:8080/broken-component
```

The render helper buffers template output before writing to the response. That
keeps the app able to replace a failed render with Petra's debug page instead
of leaking partial HTML.
