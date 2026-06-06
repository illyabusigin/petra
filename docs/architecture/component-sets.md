# Component set architecture

Status: implemented v1.

Petra component sets let Go packages publish reusable template-backed
components without choosing the application's public namespace. A package
defines namespace-free components. The application mounts the package under a
namespace such as `UI` or `Kit`.

This document describes the shipped architecture.

## Scope

Component sets cover server-rendered Petra templates:

- template-backed components;
- namespace mounts chosen by the application;
- private imports between component sets;
- render-time Petra plugin requirements;
- hot reload through Petra's existing template rebuild path.

They do not cover browser assets, JavaScript module imports, CSS, routes,
models, database state, or application services.

## Public API

```go
type ComponentSet struct {
	// unexported
}

type ComponentSetOption func(*componentSetConfig)

func NewComponentSet(id string, files fs.FS, root string, opts ...ComponentSetOption) *ComponentSet
func Import(alias string, set *ComponentSet) ComponentSetOption
func Requires(plugins ...Plugin) ComponentSetOption
func Components(namespace string, set *ComponentSet) Plugin
```

Construction is cheap and does not touch the filesystem. Petra validates sets
while parsing templates, the same way ordinary plugins report bad template
state from `Funcs` or `Apply`.

`ComponentSet` is the reusable definition. `Components(namespace, set)` is the
plugin mount used by an application.

## Source format

Component source uses bare template definition names:

```gotemplate
{{define "TextField name label id attrs error?"}}
  <label for="{{.id}}">{{.label}}</label>
  <input id="{{.id}}" name="{{.name}}"{{range $k, $v := .attrs}} {{attrs $k $v}}{{end}}>
  {{if .error}}<span class="error">{{.error}}</span>{{end}}
{{end}}
```

Definition names inside a component set must not contain dots. Petra owns
dotted names at mount and import boundaries.

Valid:

```gotemplate
{{define "TextField name label id attrs error?"}}
{{define "fieldFrame label body"}}
```

Invalid:

```gotemplate
{{define "UI.TextField name label id attrs error?"}}
{{define "Base.Button label"}}
```

This keeps the source portable. The same set can be mounted as `UI`, `Forms`,
or `AdminUI` without editing the package.

## Visibility

Visibility follows Go's export rule:

- a name whose first rune is uppercase is public;
- a name whose first rune is not uppercase is private.

Public components are exported through app mounts and through imports. Private
components are only callable by templates in the same set.

```gotemplate
{{define "TextField name label id attrs error?"}}
  {{fieldShell .label}}
{{end}}

{{define "fieldShell label"}}
  <span>{{.label}}</span>
{{end}}
```

If the set is mounted as `UI`, app templates can call `{{UI.TextField ...}}`.
They cannot call `{{UI.fieldShell ...}}`, and another component set cannot call
`{{SomeImport.fieldShell ...}}`.

## Mounts

A mount exposes one component set under one app-facing namespace:

```go
uiSet := petra.NewComponentSet("github.com/acme/petra-ui", uiFS, "components")

tmpl := petra.NewWithOptions(petra.Options{
	Plugins: petra.Plugins{
		petra.Components("UI", uiSet),
	},
})
```

Application templates call exported components through that namespace:

```gotemplate
{{UI.TextField "email" "Email" "email" .Attrs .Errors.Email}}
```

The namespace belongs to the application. The component package does not own
`UI`.

Validation:

- the namespace is required;
- it must be a valid Go template identifier;
- two different sets cannot use the same namespace in one template build;
- mounting the same set under two different namespaces is allowed.

## Imports

Imports are dependencies between component sets. They are private to the
importing set.

Developer A publishes a base set:

```go
package base

import (
	"embed"

	"github.com/illyabusigin/petra"
)

//go:embed components/*.html
var files embed.FS

var Set = petra.NewComponentSet(
	"github.com/acme/petra-base",
	files,
	"components",
)

func Components(namespace string) petra.Plugin {
	return petra.Components(namespace, Set)
}
```

A's source:

```gotemplate
{{define "Button label"}}
<button>{{.label}}</button>
{{end}}
```

Developer B imports A privately:

```go
package kit

import (
	"embed"

	"github.com/acme/petra-base"
	"github.com/illyabusigin/petra"
)

//go:embed components/*.html
var files embed.FS

var Set = petra.NewComponentSet(
	"github.com/acme/petra-kit",
	files,
	"components",
	petra.Import("Base", base.Set),
)

func Components(namespace string) petra.Plugin {
	return petra.Components(namespace, Set)
}
```

B's source can use the local import alias:

```gotemplate
{{define "Toolbar label"}}
<nav>{{Base.Button .label}}</nav>
{{end}}
```

Developer C only installs B:

```go
tmpl := petra.NewWithOptions(petra.Options{
	Plugins: petra.Plugins{
		kit.Components("Kit"),
	},
})
```

C's templates can call:

```gotemplate
{{Kit.Toolbar "Save"}}
```

C does not need to mount A. C also does not get a public `Base` namespace. If C
wants A directly, C mounts A explicitly:

```go
Plugins: petra.Plugins{
	base.Components("UI"),
	kit.Components("Kit"),
}
```

B still calls A as `Base.Button` internally. C's public namespace for A does not
change B's source.

## Required plugins

`Requires` declares server-side render plugins needed by a component set:

```go
var Set = petra.NewComponentSet(
	"github.com/acme/petra-ui",
	files,
	"components",
	petra.Requires(petra.HTML()),
)
```

Petra collects requirements from mounted sets and their imports before parsing
the component graph. Required plugin functions are installed before ordinary
app plugins. Explicit app plugins still win on function-name collisions.

`Requires` is not an asset system. It is only for Petra render plugins that
participate in template parsing and execution.

## Package shape

An OSS component package normally exports its set and a mount helper:

```go
package petraui

import (
	"embed"

	"github.com/illyabusigin/petra"
)

//go:embed components/*.html
var files embed.FS

var Set = petra.NewComponentSet(
	"github.com/acme/petra-ui",
	files,
	"components",
	petra.Requires(petra.HTML()),
)

func Components(namespace string) petra.Plugin {
	return petra.Components(namespace, Set)
}
```

The exported `Set` is for other component packages. The `Components` helper is
for applications.

## Set identity

Every set has a stable ID:

```go
petra.NewComponentSet("github.com/acme/petra-base", files, "components")
```

For OSS packages, use the module or package path. Petra uses the ID for error
messages and deterministic private function names.

Rules:

- IDs must be non-empty.
- The same ID must refer to the same `*ComponentSet` during one template build.
- Two different `ComponentSet` values with the same ID fail parsing.

The duplicate-ID rule matters when an app mounts A directly and also reaches A
through B's imports. Petra compiles A's private definitions once and shares
those internal functions across both paths.

## Build pipeline

For each parsed template set, Petra builds functions and components in this
order:

1. Collect all `Components(namespace, set)` mounts from `Template.Plugins`.
2. Walk mounted sets and imports to collect required render plugins.
3. Copy `Template.FuncMap`.
4. Install functions from required plugins.
5. Install functions from non-component plugins in `Template.Plugins` order.
6. Attach the function map to the Go template.
7. Apply required plugins.
8. Apply non-component plugins in `Template.Plugins` order.
9. Compile mounted component sets and private imports.
10. Parse local app component directories.
11. Parse page and layout templates.

`Components(namespace, set)` is a mount declaration. Its normal `Funcs` and
`Apply` methods are no-ops when called directly by the plugin loop. Petra
handles all component mounts together so private imports do not leak into app
templates.

## Component compilation

Component compilation is per template build. Petra discards the compiler state
after a successful parse or reload.

### 1. Validate the graph

Petra walks imports from every mounted set.

Validation covers:

- missing sets;
- missing filesystems;
- empty IDs;
- invalid namespaces;
- invalid import aliases;
- duplicate aliases inside a set;
- duplicate set IDs with different `ComponentSet` values;
- import cycles.

### 2. Discover definitions

Petra reads template files under each set root and parses them with Go
template function checks disabled. This discovery pass records definition
names, arguments, parse trees, and visibility.

Definitions are rejected when:

- the name contains a dot;
- the component name is not a valid template identifier;
- the set defines the same component name more than once.

Argument rules stay the same as `tmplfunc`: required arguments first, optional
arguments with `?`, and one final variadic argument with `...`.

### 3. Allocate private names

Every component definition gets a deterministic private function name:

```text
__petra_component_<set-hash>__TextField
__petra_component_<set-hash>__fieldFrame
```

The exact string is internal. It is a valid Go template function identifier and
includes a hash of the set ID.

### 4. Rewrite calls inside component sets

While compiling a set, Petra rewrites these component calls:

```gotemplate
{{TextField ...}}      -> this set's private TextField function
{{fieldShell ...}}     -> this set's private fieldShell function
{{Base.Button ...}}    -> imported set's private Button function
```

Imported private calls fail:

```gotemplate
{{Base.fieldShell ...}}
```

Ordinary helper calls from `FuncMap` and plugins are left alone.

### 5. Install public exports

Private definitions are compiled first. Public exports are then installed under
mounted dotted names:

```text
UI.TextField
Kit.Toolbar
```

The public template body calls private internal definitions. That keeps import
aliases private while preserving the app-facing dotted syntax.

## Namespaced template parsing

Petra's `tmplfunc` layer supports callable template definitions with dotted
names. Go's parser only checks root identifiers, so `{{UI.Missing}}` can pass
root checking when `UI.TextField` exists. Petra fixes that with an AST pass:

- known namespace roots are allowed during Go's checked parse;
- full dotted calls must resolve to a known generated function;
- namespace roots alone are not callable;
- missing dotted members fail during parse.

After validation, `tmplfunc` rewrites dotted calls to generated function names
and installs parse trees with `AddParseTree`. `html/template` still owns
escaping and contextual safety.

## Runtime behavior

After parsing, component calls are normal template function calls. A generated
function bundles positional arguments into the map shape expected by the
component body, executes the named template, and returns trusted HTML for
`html/template`.

`Exec` clones the parsed component template pool before parsing the inline
fragment, so mounted component namespaces are available there too:

```go
err := tmpl.Exec(w, `{{UI.TextField "email" "Email" "email" .Attrs .Errors.Email}}`, view)
```

## Hot reload

Component sets use Petra's existing reload machinery.

For files inside the app template root, Petra uses the existing selective
reload graph. For watched component-set folders outside the app template root,
Petra falls back to a full template reparse. If the reparse fails, Petra keeps
serving the previous parsed template set.

Development setup:

```go
devSet := petra.NewComponentSet(
	"github.com/acme/petra-ui",
	os.DirFS("../petra-ui"),
	"components",
)

tmpl := petra.NewWithOptions(petra.Options{
	Plugins: petra.Plugins{
		petra.Components("UI", devSet),
	},
})

hotReload := petra.NewHotReloadControllerWithOptions(petra.HotReloadOptions{
	Template: tmpl,
	Folders: []string{
		"./templates",
		"../petra-ui/components",
	},
})
```

The v1 behavior is intentionally conservative. Petra does not track exact
page-to-component-set call edges.

## Errors

Component-set errors include component package boundaries where Petra has that
context:

```text
petra: component set "github.com/acme/petra-kit" component "Toolbar":
component "Base.fieldShell" is not exported by import "Base"
```

```text
petra: component set "github.com/acme/petra-ui":
template: :1: function "attrs" not defined
```

The current cycle error identifies the repeated set:

```text
petra: component set import cycle includes "github.com/acme/petra-kit"
```

Generated internal function names are implementation details. They can appear
in low-level parse or execution errors, but the public architecture does not
require users to write them.

## Limits

The v1 architecture intentionally leaves these out:

- client assets;
- browser modules;
- CSS bundling;
- import maps;
- web-component bootstrapping;
- CSP nonce propagation;
- component-specific routes;
- model or form schema registration;
- exact selective reload by component call edge;
- multiple versions of the same component set in one template build.

Packages that wrap browser component libraries can document their client setup
and expose ordinary Go helpers when useful:

```go
func Head() template.HTML {
	return `<link rel="stylesheet" href="/static/shoelace/light.css">
<script type="module" src="/static/shoelace/shoelace.js"></script>`
}
```

Application layouts opt into those resources explicitly:

```gotemplate
<head>
  {{ShoelaceHead}}
</head>
```

Assets and models need a separate architecture because their failure modes are
not template parsing failures.

## Migration from the prototype

The pre-v1 prototype made the component source carry the app namespace:

```go
petra.ComponentLibrary(files, "ui")
```

```gotemplate
{{define "UI.TextField name label id attrs error?"}}
...
{{end}}
```

The component-set API moves the namespace to the mount:

```go
var UISet = petra.NewComponentSet("github.com/acme/ui", files, "ui")

petra.Components("UI", UISet)
```

```gotemplate
{{define "TextField name label id attrs error?"}}
...
{{end}}
```

The app call stays the same:

```gotemplate
{{UI.TextField "email" "Email" "email" .Attrs .Errors.Email}}
```
