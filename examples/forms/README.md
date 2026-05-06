# Forms example

This example shows a server-rendered form flow with Petra.

It does not include Tailwind, Alpine, controllers, or a router. The point is the
HTTP cycle:

- `GET /` renders the blank form.
- `POST /` parses submitted fields.
- invalid input re-renders the same template with errors and status `422`.
- valid input re-renders the same template with a success message.

Run it:

```sh
go run .
```

Run tests:

```sh
go test ./...
```
