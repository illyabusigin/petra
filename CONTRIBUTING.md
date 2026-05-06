# Contributing

Petra is a small Go package. Keep changes focused on template parsing,
component calls, plugins, static assets, reload behavior, docs, or examples.

Before opening a pull request, run:

```sh
make ci
```

That target runs tidy checks, formatting checks, `go vet`, `staticcheck`,
`govulncheck`, unit tests, race tests, package builds, example tests, example
builds, and the Tailwind/Alpine asset builds.

The example apps are separate modules. They use local `replace` directives for
Petra, so changes in this checkout are used when you run tests from an example
folder.

For plugin changes, keep the trust boundary explicit. Helpers that return
`template.HTML`, `template.JS`, or `template.HTMLAttr` must only mark output as
trusted after the helper has produced safe content itself.
