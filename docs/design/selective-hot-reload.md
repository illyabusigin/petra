# Selective hot reload spec

Implementation status: implemented in the local Petra module.

## Problem

Petra currently reparses the full template tree whenever any watched template file changes.

The current path is:

- `HotReloadController.Handler` starts a watcher per folder.
- On each write event, the watcher calls `Template.ParseDir(folder)`.
- `ParseDir` calls `parseFS`.
- `parseFS` calls `findTemplates`, then reparses every executable page template plus all matching layouts and component directories.
- After the full parse succeeds, Petra swaps the whole `templates` map and the `components` template set.

This is correct, but it does not scale. A single component edit can reparse every page in the project. The cost grows with both page count and shared component count.

The goal is to keep Petra's correctness guarantees while rebuilding only the affected part of the template graph during development.

## Goals

- Rebuild only the page templates affected by a changed template file.
- Rebuild the component template set only when a component file changes.
- Keep serving the last known-good templates if a selective rebuild fails.
- Fall back to a full graph rebuild for structural events: create, remove, rename, chmod when needed, or ambiguous editor save behavior.
- Batch and debounce file-system events so one save produces one reload decision.
- Make reload behavior observable enough to debug: event paths, classification, affected page IDs, fallback reason, parse duration, and parse errors.
- Keep `ParseDir` and `ParseFS` public behavior compatible.
- Keep production `ParseFS` simple. Selective reload is a development feature for mutable directories.

## Non-goals

- Do not implement selective rebuild for embedded `fs.FS` production templates.
- Do not build a general-purpose incremental compiler.
- Do not parse template ASTs to discover exact `{{template}}` or component function call edges. Petra's current dependency model is directory-based; this feature should stay consistent with that model.
- Do not solve watcher shutdown in this change unless it falls out naturally. Lifecycle control remains useful, but it is separate from selective rebuild.

## Current dependency model

Petra treats three kinds of template files differently.

### Page templates

A page template is any non-layout file outside an include directory.

Example:

```text
templates/products/index.html
```

This becomes executable template ID:

```text
products/index
```

### Layout files

Layout files are named by `Template.Layout`, usually `layout.html`.

For a page, Petra includes every layout whose directory is in the page's hierarchy. The current matching condition is directory-prefix based.

Example:

```text
templates/layout.html
templates/products/layout.html
templates/products/index.html
```

The page `products/index` uses both layouts.

### Component directories

Component directories are named by `Template.IncludeDir`, usually `includes` or `components`.

For a page, Petra includes every component directory whose parent directory is in the page's hierarchy. A template file directly inside a component directory is not a page.

Example:

```text
templates/components/header.html
templates/products/components/product_card.html
templates/products/index.html
```

The page `products/index` uses both component directories.

`Template.Exec` uses the global component template set. That set is built from every component directory discovered during the parse.

## Proposed architecture

Add a persistent graph to `Template`.

```go
type Template struct {
	Layout     string
	IncludeDir string
	FuncMap    template.FuncMap
	Plugins    Plugins

	mu sync.RWMutex

	templates  map[string]*template.Template
	components *template.Template
	graph      *templateGraph
}
```

The graph is built during `ParseDir` and reused by selective reload.

```go
type templateGraph struct {
	root       string
	includeDir string
	layout     string

	pagesByID   map[string]templateInfo
	pageIDByFile map[string]string

	layoutsByFile map[string]map[string]struct{} // layout path -> page IDs
	includesByDir map[string]map[string]struct{} // include dir -> page IDs
	componentDirs map[string]struct{}

	allFiles map[string]fileKind
}

type fileKind int

const (
	fileKindUnknown fileKind = iota
	fileKindPage
	fileKindLayout
	fileKindComponent
)
```

`templateInfo` should grow slightly so Petra can answer questions without recomputing from scratch.

```go
type templateInfo struct {
	id       string
	path     string
	includes []string
	files    []string
}
```

`files` remains the ordered list parsed for a page: layouts first, page last. `includes` remains the component directories parsed for that page.

## Public API

Keep:

```go
func (t *Template) ParseDir(dir string) error
func (t *Template) ParseFS(files fs.FS, dir string) error
```

Add development-only reload support:

```go
type ReloadOp uint8

const (
	ReloadWrite ReloadOp = 1 << iota
	ReloadCreate
	ReloadRemove
	ReloadRename
	ReloadChmod
)

type ReloadFileEvent struct {
	Path string
	Op   ReloadOp
}

type ReloadResult struct {
	FullReload       bool
	RebuiltPages     []string
	RebuiltComponents bool
	ChangedPaths     []string
	Duration         time.Duration
	FallbackReason   string
	Noop             bool
}

func (t *Template) Reload(events ...ReloadFileEvent) (ReloadResult, error)
func (t *Template) ReloadDir(paths ...string) (ReloadResult, error)
```

`Reload` is the primary API. It carries operation data without exposing `fsnotify` from the `Template` type. `HotReloadController` maps `fsnotify.Event` values into `ReloadFileEvent`.

`ReloadDir` is a convenience for tests and manual reloads. It treats every path as `ReloadWrite`.

Both methods require a prior successful `ParseDir`. If no graph exists, Petra performs a full `ParseDir` using the remembered root.

The result should be useful to logs and tests. The caller should broadcast a browser reload only when `Reload` succeeds.

## Internal API

Split the current parser into reusable pieces.

```go
func buildGraph(files fs.FS, dir, includeDir, layout string) (*templateGraph, error)

func parsePage(files fs.FS, info templateInfo, includeDir, layout string, funcMap template.FuncMap, plugins Plugins) (*template.Template, error)

func parseComponents(files fs.FS, componentDirs []string, funcMap template.FuncMap, plugins Plugins) (*template.Template, error)
```

`parseFS` becomes orchestration:

```go
func parseFS(...) (map[string]*template.Template, *template.Template, *templateGraph, error)
```

`ParseDir` stores `graph`; `ParseFS` may store a graph too, but selective reload must only be enabled for a mutable OS directory.

## File event batching

fsnotify can emit several events for one save. Some editors write a temp file, rename it, then chmod it. Treating every event independently causes duplicate work and wrong classifications.

Add a debouncer owned by `HotReloadController`.

Recommended defaults:

- Debounce window: 75ms.
- Max wait: 250ms if events keep arriving.
- Drop `.DS_Store`, swap files, temporary editor files, and files outside the watched template root before classification.

Template file filter:

- Do not use extension filtering as the main correctness boundary.
- Petra currently treats any non-layout file outside an include directory as a page template, regardless of extension.
- Drop only known non-template noise: `.DS_Store`, temporary editor files, swap files, and files outside the watched template root.
- Unknown non-noise files inside the template root should force a full rebuild, because they may be new page templates.

## Event classification

For a debounced batch, classify changed paths against the graph.

### Page edit

If a changed path is `fileKindPage`, rebuild only that page.

Example:

```text
templates/products/index.html
```

Affected:

```text
products/index
```

### Layout edit

If a changed path is `fileKindLayout`, rebuild every page listed in `layoutsByFile[path]`.

Root layout changes will naturally affect all pages because every page includes the root layout.

### Component edit

If a changed path is inside a known component directory, rebuild:

- every page listed in `includesByDir[componentDir]`
- the global `components` set used by `Exec`

The global `components` set should be rebuilt from all component directories, not only the edited one, because `tmplfunc` lets components call each other and the component set is a single template namespace.

This is still much cheaper than rebuilding every page if the edited component directory is local to one site section. If the edited component is global, it will correctly rebuild most or all pages.

### New file

If a changed path did not exist in `graph.allFiles`, do a full graph rebuild.

Reasons:

- A new page adds a new executable template ID.
- A new layout can affect many pages depending on directory hierarchy.
- A new component file changes component directory contents and possibly component functions.
- A new component directory changes dependency matching.

### Remove or rename

If any reload event includes `ReloadRemove` or `ReloadRename`, do a full graph rebuild.

Reasons:

- A page may need to be removed from `templates`.
- A deleted layout or component can invalidate many pages.
- Rename often appears as remove plus create, and editor behavior differs by platform.

### Chmod

Ignore chmod-only events unless the path is unknown and another event appears in the same batch. Chmod often follows saves on macOS and should not force work by itself.

### Unknown known-extension file

If a path is inside the template root but cannot be classified and is not known noise, do a full rebuild. This catches new pages, new component files, new layouts, directory changes, and graph drift.

## Rebuild transaction

Selective reload must be transactional.

Current good state:

```go
oldTemplates
oldComponents
oldGraph
```

Reload builds into locals first:

```go
nextTemplates := maps.Clone(oldTemplates)
nextComponents := oldComponents
nextGraph := oldGraph
```

For selective page rebuild:

- Parse each affected page into a fresh `*template.Template`.
- If any parse fails, return the error and do not swap anything.
- If all succeed, replace only those page IDs in `nextTemplates`.

For component rebuild:

- Parse the full component set into a fresh `*template.Template`.
- If it fails, return the error and keep old state.
- If it succeeds, set `nextComponents`.

For full rebuild:

- Build a fresh graph.
- Parse all pages and all components.
- Swap everything only after the full parse succeeds.

Only after all required parses succeed:

```go
t.mu.Lock()
t.templates = nextTemplates
t.components = nextComponents
t.graph = nextGraph
t.mu.Unlock()
```

This preserves the current "last good render keeps working" behavior.

## Error behavior

If a template edit introduces a syntax error:

- `Reload` returns an error.
- `HotReloadController` does not broadcast `reload`.
- Petra keeps serving the previous good template set.
- The error is logged with changed paths and affected page IDs.

Browser behavior should stay stable. The page should not reload into a broken server response because the new template set was not accepted.

Future enhancement: broadcast a separate `reload_error` message so the browser can display an overlay. Do not include that in the first implementation unless the basic selective reload is already done.

## Logging and diagnostics

Petra should not write directly to stdout for normal operation.

Add an optional observer hook.

```go
type ReloadEvent struct {
	ChangedPaths      []string
	RebuiltPages      []string
	RebuiltComponents bool
	FullReload        bool
	FallbackReason    string
	Duration          time.Duration
	Err               error
}

type ReloadObserver interface {
	ObserveReload(ReloadEvent)
}
```

This can be wired to `slog` by the host app without Petra depending on `log/slog`.

## HotReloadController changes

Current controller only knows "something changed, call ParseDir".

Change it to:

- Watch folders.
- Collect fsnotify events.
- Debounce events.
- Call `Template.Reload(events...)`.
- Broadcast `reload` only on successful template reload.
- Broadcast `reload_assets` only from `Static` when static assets change.

Sketch:

```go
func (c *HotReloadController) handleBatch(events []fsnotify.Event) {
	result, err := c.t.Reload(mapFSNotifyEvents(events)...)
	c.observe(result, err)
	if err != nil {
		return
	}
	c.m.Broadcast([]byte("reload"))
}
```

The controller should avoid starting duplicate watcher goroutines if `Handler()` is called more than once. Add `sync.Once`.

```go
type HotReloadController struct {
	startWatchers sync.Once
}
```

## Static watcher changes

`Static` currently broadcasts `reload_assets` for every write after a fixed 50ms delay.

For production-polished behavior, give static watching the same debounce primitive as template watching. This avoids multiple asset reloads for a single CSS build.

This can be a shared unexported helper:

```go
type eventDebouncer struct {
	delay   time.Duration
	maxWait time.Duration
	emit    func([]fsnotify.Event)
}
```

The helper should preserve enough event data to distinguish write/create/remove/rename.

## Path normalization

All graph paths should be slash-separated and relative to the parsed root.

Problems to avoid:

- `filepath.Walk` returns OS-specific separators.
- fsnotify returns OS paths.
- `fs.FS` expects slash-separated paths.
- `ParseDir` currently uses `os.DirFS(".")` with the full `dir` path, so stored paths may include the root prefix.

Define one normalization path:

```go
func normalizeTemplatePath(root, path string) (string, error)
```

Rules:

- Clean both root and path.
- Convert to slash form.
- If the path is absolute, make it relative to root.
- Reject paths outside root.
- Keep graph paths in the same form used by `fs.ReadFile` and `ParseFS`.

This is important for macOS and for editor save behavior.

Implementation note: prefer changing `ParseDir` internals to use `os.DirFS(dir)` plus root `"."` for graph construction and parsing. Keep template IDs identical to the current behavior. This gives the graph stable root-relative slash paths and removes a class of absolute/relative path mismatches.

## Correctness rules

- Never mutate an executed `*template.Template`.
- Never publish partial parse state.
- Never remove an old template from the live map unless a full graph rebuild succeeds.
- Never broadcast browser reload after a failed parse.
- Component change always rebuilds the global component set.
- Structural ambiguity falls back to full rebuild.
- Full rebuild must produce the same result as `ParseDir`.

## Performance target

Use measured targets once the implementation exists.

Suggested acceptance target for a test tree:

- 250 pages
- 1 root layout
- 10 section layouts
- 1 global component directory with 20 components
- 10 section component directories with 10 components each

Expected behavior:

- Editing one page reparses 1 page.
- Editing one section layout reparses pages in that section.
- Editing one section component reparses pages in that section plus the component set.
- Editing one global component reparses all pages plus the component set.
- Adding/removing/renaming a template performs full rebuild.

The first implementation should report page rebuild counts, not just duration, so performance regressions are obvious.

## Test plan

### Unit tests

Add tests for graph construction:

- Page IDs match current behavior.
- Root layout maps to all pages.
- Section layout maps only to section pages.
- Root component directory maps to all pages.
- Section component directory maps only to section pages.
- Component files are not treated as pages.

Add tests for classification:

- Page write -> one page.
- Layout write -> pages using layout.
- Component write -> pages using component dir and component rebuild.
- Unknown template path -> full rebuild.
- `ReloadRemove` event -> full rebuild.
- `ReloadRename` event -> full rebuild.
- Chmod-only known path -> no-op.

Add tests for transactional reload:

- Successful page edit replaces only that page ID.
- Failed page edit keeps previous rendered output.
- Successful component edit updates `Exec`.
- Failed component edit keeps previous `Exec` output.
- New page after full rebuild appears in `templates`.
- Deleted page after full rebuild disappears from `templates`.

### Race tests

Run with `go test -race`.

Cases:

- Many goroutines executing pages while one goroutine selectively reloads pages.
- Many goroutines calling `Exec` while component reload runs.
- Repeated full rebuilds mixed with selective rebuilds.

### Integration tests

Use temporary directories and fsnotify when possible:

- Start `HotReloadController`.
- Modify a page file.
- Assert one reload broadcast and affected page count of 1.
- Modify a component.
- Assert one reload broadcast after debounce.
- Save a file with write plus chmod.
- Assert one rebuild.
- Rename a file.
- Assert full rebuild.

If websocket-level assertions are too brittle, test the debouncer and controller reload callback directly.

### Regression tests

Build a synthetic large template tree and count parse calls.

Instrument the parser in tests with a callback:

```go
type parseObserver interface {
	ParsedPage(id string)
	ParsedComponents()
}
```

Assert:

- Page edit parses 1 page.
- Section component edit parses N section pages, not all pages.
- Full rebuild parses all pages.

## Implementation phases

### Phase 1: Parser extraction

- Split `parseFS` into `buildGraph`, `parsePage`, and `parseComponents`.
- Add `templateInfo.path`.
- Add `templateGraph`.
- Keep `ParseDir` and `ParseFS` behavior unchanged.
- Add graph construction tests.

Acceptance:

- Existing tests pass.
- New graph tests pass.
- Full parse output matches current behavior.

### Phase 2: Selective reload core

- Add `Template.Reload(events ...ReloadFileEvent)` and `Template.ReloadDir(paths ...string)`.
- Implement classification from reload events to affected pages.
- Implement transactional selective page/component rebuild.
- Implement conservative full rebuild fallback.
- Add transactional tests and race tests.

Acceptance:

- `GOWORK=off go test ./...` passes.
- `GOWORK=off go test -race ./...` passes.
- Failed selective reload keeps previous output.

### Phase 3: Debounced watcher integration

- Add shared debouncer.
- Update `HotReloadController` to call `Reload`.
- Prevent duplicate watcher start with `sync.Once`.
- Update `Static` to use debounce for `reload_assets`.
- Add integration or controller-level tests.

Acceptance:

- A single editor save triggers one reload.
- Template parse errors do not broadcast `reload`.
- Static writes broadcast `reload_assets`, not `reload`.

### Phase 4: Diagnostics

- Add `ReloadResult`.
- Add optional reload observer.
- Log or observe affected pages, full reload fallback reason, duration, and error.
- Document the behavior in `README`.

Acceptance:

- A development server can print reload diagnostics without Petra writing directly to stdout.

## Rollout plan

1. Keep current `ParseDir` as the full rebuild path.
2. Add selective code behind `Reload`; do not wire it to the watcher until tests pass.
3. Switch `HotReloadController` from `ParseDir` to `Reload`.
4. Keep full rebuild fallback for unknown or structural changes.
5. Build application templates on top of this behavior.

## Risks

### Directory-prefix matching can be too broad

The current code uses string prefix checks to decide whether a page is under a layout/component hierarchy. This can misclassify paths like `product` and `products` if not normalized carefully.

Mitigation: replace raw string prefix checks with a helper that checks directory ancestry by path segments.

### Component function dependencies are implicit

`tmplfunc` turns templates into functions, so one component can call another. Petra's current model handles this by parsing all relevant component directories into each page and all component directories into the global component set.

Mitigation: component edits rebuild all pages that include the component directory, and rebuild the full component set for `Exec`.

### Editor save behavior differs

Some editors write temp files, rename, then chmod. fsnotify behavior differs across macOS and Linux.

Mitigation: debounce, ignore common temp files, and use full rebuild for create/remove/rename ambiguity.

### Partial graph drift

If the graph gets out of sync, selective reload could miss affected pages.

Mitigation: structural events trigger full rebuild. Unknown template paths trigger full rebuild. Expose diagnostics so unexpected full rebuilds are visible.

## Static asset debounce decision

Static asset debouncing belongs in Phase 3 with watcher integration. It uses the same event batching primitive as template reload, and it fixes the same class of development-loop problem: one save or CSS build producing several browser reload messages.

## Recommendation

Implement this before a large template tree makes full reparses too slow.

The first production-quality version should be conservative: selective for known page/layout/component edits, full rebuild for structural changes. That preserves correctness while removing the slowest path from normal development: reparsing the whole site after editing one component.
