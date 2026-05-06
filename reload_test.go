package petra

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestBuildGraphScopesLayoutsAndComponents(t *testing.T) {
	dir := writeReloadFixture(t)

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components"}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	graph := tmpl.graph
	if graph == nil {
		t.Fatal("graph is nil")
	}

	if got := sortedSet(graph.layoutsByFile["layout.html"]); !slices.Equal(got, []string{"about", "products/index"}) {
		t.Fatalf("root layout pages = %v", got)
	}
	if got := sortedSet(graph.layoutsByFile["products/layout.html"]); !slices.Equal(got, []string{"products/index"}) {
		t.Fatalf("products layout pages = %v", got)
	}
	if got := sortedSet(graph.includesByDir["components"]); !slices.Equal(got, []string{"about", "products/index"}) {
		t.Fatalf("root component pages = %v", got)
	}
	if got := sortedSet(graph.includesByDir["products/components"]); !slices.Equal(got, []string{"products/index"}) {
		t.Fatalf("product component pages = %v", got)
	}
	if _, ok := graph.pageIDByFile["components/header.html"]; ok {
		t.Fatal("component file was treated as a page")
	}
}

func TestBuildGraphDoesNotUseLoosePrefixMatching(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "layout.html"), `{{template "header" .}} root {{block "content" .}}{{end}}`)
	writeFile(t, filepath.Join(dir, "components", "header.html"), `{{define "header"}}header{{end}}`)
	writeFile(t, filepath.Join(dir, "product", "layout.html"), `{{define "content"}}product {{block "product" .}}{{end}}{{end}}`)
	writeFile(t, filepath.Join(dir, "product", "index.html"), `{{define "product"}}index{{end}}`)
	writeFile(t, filepath.Join(dir, "products", "index.html"), `{{define "content"}}products{{end}}`)

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components"}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	if got := sortedSet(tmpl.graph.layoutsByFile["product/layout.html"]); !slices.Equal(got, []string{"product/index"}) {
		t.Fatalf("product layout pages = %v", got)
	}
	if got := executeTemplate(t, tmpl, "products/index"); got != "header root products" {
		t.Fatalf("products/index render = %q", got)
	}
}

func TestNestedComponentFilesAreRecursiveComponentsNotPages(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "layout.html"), `{{template "shell" .}} {{block "content" .}}{{end}}`)
	writeFile(t, filepath.Join(dir, "components", "shell.html"), `{{define "shell"}}shell {{icon}}{{end}}`)
	writeFile(t, filepath.Join(dir, "components", "icons", "icon.html"), `{{define "icon"}}icon v1{{end}}`)
	writeFile(t, filepath.Join(dir, "index.html"), `{{define "content"}}home {{icon}}{{end}}`)

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components"}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	if got := executeTemplate(t, tmpl, "index"); got != "shell icon v1 home icon v1" {
		t.Fatalf("index render = %q", got)
	}
	if _, ok := tmpl.graph.pageIDByFile["components/icons/icon.html"]; ok {
		t.Fatal("nested component file was treated as a page")
	}
	if kind := tmpl.graph.allFiles["components/icons/icon.html"]; kind != fileKindComponent {
		t.Fatalf("nested component kind = %v", kind)
	}
	if componentDir := tmpl.graph.componentDirByFile["components/icons/icon.html"]; componentDir != "components" {
		t.Fatalf("nested component dir = %q", componentDir)
	}
	if got := sortedSet(tmpl.graph.includesByDir["components"]); !slices.Equal(got, []string{"index"}) {
		t.Fatalf("component pages = %v", got)
	}

	var b bytes.Buffer
	if err := tmpl.ExecuteTemplate(&b, "components/icons/icon", nil); err == nil {
		t.Fatalf("nested component executed as page, output = %q", b.String())
	}
}

func TestReloadRebuildsNestedComponentAndExecSet(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "layout.html"), `{{template "shell" .}} {{block "content" .}}{{end}}`)
	writeFile(t, filepath.Join(dir, "components", "shell.html"), `{{define "shell"}}shell {{icon}}{{end}}`)
	writeFile(t, filepath.Join(dir, "components", "icons", "icon.html"), `{{define "icon"}}icon v1{{end}}`)
	writeFile(t, filepath.Join(dir, "index.html"), `{{define "content"}}home {{icon}}{{end}}`)

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components"}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	writeFile(t, filepath.Join(dir, "components", "icons", "icon.html"), `{{define "icon"}}icon v2{{end}}`)

	result, err := tmpl.Reload(ReloadFileEvent{Path: filepath.Join(dir, "components", "icons", "icon.html"), Op: ReloadWrite})
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if result.FullReload {
		t.Fatal("nested component edit performed full reload")
	}
	if !result.RebuiltComponents {
		t.Fatal("nested component edit did not rebuild component set")
	}
	if !slices.Equal(result.RebuiltPages, []string{"index"}) {
		t.Fatalf("rebuilt pages = %v", result.RebuiltPages)
	}
	if got := executeTemplate(t, tmpl, "index"); got != "shell icon v2 home icon v2" {
		t.Fatalf("updated index render = %q", got)
	}
	if got := executeInline(t, tmpl, `{{icon}}`); got != "icon v2" {
		t.Fatalf("updated Exec render = %q", got)
	}
}

func TestPageDiscoveryIsBroadByDefault(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "layout.html"), `{{block "content" .}}{{end}}`)
	writeFile(t, filepath.Join(dir, "robots.txt"), `{{define "content"}}User-agent: *{{end}}`)

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components"}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	if got := executeTemplate(t, tmpl, "robots"); got != "User-agent: *" {
		t.Fatalf("robots render = %q", got)
	}
}

func TestPageExtensionsFilterNonPageFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "layout.html"), `{{block "content" .}}{{end}}`)
	writeFile(t, filepath.Join(dir, "index.html"), `{{define "content"}}home{{end}}`)
	writeFile(t, filepath.Join(dir, "notes.txt"), `{{`)

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components", PageExtensions: []string{".html"}}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	if got := executeTemplate(t, tmpl, "index"); got != "home" {
		t.Fatalf("index render = %q", got)
	}
	var b bytes.Buffer
	if err := tmpl.ExecuteTemplate(&b, "notes", nil); err == nil {
		t.Fatalf("non-page file executed as page, output = %q", b.String())
	}
}

func TestReloadRebuildsOnlyChangedPage(t *testing.T) {
	dir := writeReloadFixture(t)

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components"}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	if got := executeTemplate(t, tmpl, "about"); got != "header root about v1" {
		t.Fatalf("initial about render = %q", got)
	}

	writeFile(t, filepath.Join(dir, "about.html"), `{{define "content"}}about v2{{end}}`)

	result, err := tmpl.Reload(ReloadFileEvent{Path: filepath.Join(dir, "about.html"), Op: ReloadWrite})
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if result.FullReload {
		t.Fatal("Reload() performed full reload")
	}
	if !slices.Equal(result.RebuiltPages, []string{"about"}) {
		t.Fatalf("rebuilt pages = %v", result.RebuiltPages)
	}
	if result.RebuiltComponents {
		t.Fatal("Reload() rebuilt components for page-only change")
	}
	if got := executeTemplate(t, tmpl, "about"); got != "header root about v2" {
		t.Fatalf("updated about render = %q", got)
	}
}

func TestReloadRebuildsAllPagesForRootLayout(t *testing.T) {
	dir := writeReloadFixture(t)

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components"}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	writeFile(t, filepath.Join(dir, "layout.html"), `{{template "header" .}} root v2 {{block "content" .}}{{end}}`)

	result, err := tmpl.Reload(ReloadFileEvent{Path: filepath.Join(dir, "layout.html"), Op: ReloadWrite})
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if result.FullReload {
		t.Fatal("Reload() performed full reload")
	}
	if !slices.Equal(result.RebuiltPages, []string{"about", "products/index"}) {
		t.Fatalf("rebuilt pages = %v", result.RebuiltPages)
	}
	if result.RebuiltComponents {
		t.Fatal("Reload() rebuilt components for layout-only change")
	}
	if got := executeTemplate(t, tmpl, "about"); got != "header root v2 about v1" {
		t.Fatalf("about render = %q", got)
	}
	if got := executeTemplate(t, tmpl, "products/index"); got != "header root v2 products index v1 card v1" {
		t.Fatalf("products/index render = %q", got)
	}
}

func TestReloadRebuildsSectionComponentAndExecSet(t *testing.T) {
	dir := writeReloadFixture(t)

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components"}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	writeFile(t, filepath.Join(dir, "products", "components", "card.html"), `{{define "card"}}card v2{{end}}`)

	result, err := tmpl.Reload(ReloadFileEvent{Path: filepath.Join(dir, "products", "components", "card.html"), Op: ReloadWrite})
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if result.FullReload {
		t.Fatal("Reload() performed full reload")
	}
	if !slices.Equal(result.RebuiltPages, []string{"products/index"}) {
		t.Fatalf("rebuilt pages = %v", result.RebuiltPages)
	}
	if !result.RebuiltComponents {
		t.Fatal("Reload() did not rebuild components")
	}
	if got := executeTemplate(t, tmpl, "products/index"); got != "header root products index v1 card v2" {
		t.Fatalf("updated product render = %q", got)
	}
	if got := executeInline(t, tmpl, `{{card}}`); got != "card v2" {
		t.Fatalf("updated Exec render = %q", got)
	}
}

func TestReloadRebuildsAllPagesForRootComponentAndExecSet(t *testing.T) {
	dir := writeReloadFixture(t)

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components"}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	writeFile(t, filepath.Join(dir, "components", "header.html"), `{{define "header"}}header v2{{end}}`)

	result, err := tmpl.Reload(ReloadFileEvent{Path: filepath.Join(dir, "components", "header.html"), Op: ReloadWrite})
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if result.FullReload {
		t.Fatal("Reload() performed full reload")
	}
	if !slices.Equal(result.RebuiltPages, []string{"about", "products/index"}) {
		t.Fatalf("rebuilt pages = %v", result.RebuiltPages)
	}
	if !result.RebuiltComponents {
		t.Fatal("Reload() did not rebuild components")
	}
	if got := executeTemplate(t, tmpl, "about"); got != "header v2 root about v1" {
		t.Fatalf("about render = %q", got)
	}
	if got := executeInline(t, tmpl, `{{header}}`); got != "header v2" {
		t.Fatalf("Exec header render = %q", got)
	}
}

func TestReloadComponentParseErrorKeepsPreviousTemplatesAndExec(t *testing.T) {
	dir := writeReloadFixture(t)

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components"}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	beforePage := executeTemplate(t, tmpl, "products/index")
	beforeExec := executeInline(t, tmpl, `{{card}}`)

	writeFile(t, filepath.Join(dir, "products", "components", "card.html"), `{{define "card"}}broken {{end`)

	_, err := tmpl.Reload(ReloadFileEvent{Path: filepath.Join(dir, "products", "components", "card.html"), Op: ReloadWrite})
	if err == nil {
		t.Fatal("Reload() error = nil")
	}
	info, ok := DebugInfo(err)
	if !ok {
		t.Fatal("DebugInfo() did not identify a Petra debug error")
	}
	if info.Operation != "Reload" {
		t.Fatalf("debug operation = %q, want Reload", info.Operation)
	}
	if info.DependencyRole != DebugDependencyRoleComponent {
		t.Fatalf("debug role = %q, want %q", info.DependencyRole, DebugDependencyRoleComponent)
	}
	if info.Path != "products/components/card.html" {
		t.Fatalf("debug path = %q, want products/components/card.html", info.Path)
	}
	if !slices.Equal(info.ChangedPaths, []string{"products/components/card.html"}) {
		t.Fatalf("debug changed paths = %v", info.ChangedPaths)
	}
	if !slices.Equal(info.AffectedPages, []string{"products/index"}) {
		t.Fatalf("debug affected pages = %v", info.AffectedPages)
	}
	if info.Source == nil || info.Source.Path != "products/components/card.html" {
		t.Fatalf("debug source = %#v, want products/components/card.html", info.Source)
	}
	if !sourceContains(info.Source, `broken {{end`) {
		t.Fatalf("debug source = %#v, want broken component line", info.Source)
	}
	if got := executeTemplate(t, tmpl, "products/index"); got != beforePage {
		t.Fatalf("template changed after failed component reload: before %q after %q", beforePage, got)
	}
	if got := executeInline(t, tmpl, `{{card}}`); got != beforeExec {
		t.Fatalf("Exec changed after failed component reload: before %q after %q", beforeExec, got)
	}
}

func TestReloadLayoutParseErrorIncludesAffectedScopeAndSource(t *testing.T) {
	dir := writeReloadFixture(t)

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components"}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	beforeAbout := executeTemplate(t, tmpl, "about")
	beforeProducts := executeTemplate(t, tmpl, "products/index")

	writeFile(t, filepath.Join(dir, "products", "layout.html"), "{{define \"content\"}}products\nbroken {{end\n{{end}}")

	_, err := tmpl.Reload(ReloadFileEvent{Path: filepath.Join(dir, "products", "layout.html"), Op: ReloadWrite})
	if err == nil {
		t.Fatal("Reload() error = nil")
	}
	if _, ok := errors.AsType[ParseError](err); !ok {
		t.Fatalf("Reload() error does not unwrap to ParseError: %T %[1]v", err)
	}

	info, ok := DebugInfo(err)
	if !ok {
		t.Fatal("DebugInfo() did not identify a Petra debug error")
	}
	if info.Kind != DebugErrorKindParse {
		t.Fatalf("debug kind = %q, want %q", info.Kind, DebugErrorKindParse)
	}
	if info.Operation != "Reload" {
		t.Fatalf("debug operation = %q, want Reload", info.Operation)
	}
	if info.DependencyRole != DebugDependencyRoleLayout {
		t.Fatalf("debug role = %q, want %q", info.DependencyRole, DebugDependencyRoleLayout)
	}
	if info.Layout != "products/layout.html" {
		t.Fatalf("debug layout = %q, want products/layout.html", info.Layout)
	}
	if info.Path != "products/layout.html" {
		t.Fatalf("debug path = %q, want products/layout.html", info.Path)
	}
	if !slices.Equal(info.ChangedPaths, []string{"products/layout.html"}) {
		t.Fatalf("debug changed paths = %v", info.ChangedPaths)
	}
	if !slices.Equal(info.AffectedPages, []string{"products/index"}) {
		t.Fatalf("debug affected pages = %v", info.AffectedPages)
	}
	if info.Source == nil || info.Source.Path != "products/layout.html" {
		t.Fatalf("debug source = %#v, want products/layout.html", info.Source)
	}
	if !sourceContains(info.Source, `broken {{end`) {
		t.Fatalf("debug source = %#v, want broken layout line", info.Source)
	}

	if got := executeTemplate(t, tmpl, "about"); got != beforeAbout {
		t.Fatalf("about changed after failed layout reload: before %q after %q", beforeAbout, got)
	}
	if got := executeTemplate(t, tmpl, "products/index"); got != beforeProducts {
		t.Fatalf("products changed after failed layout reload: before %q after %q", beforeProducts, got)
	}
}

func TestReloadKeepsPreviousTemplatesOnParseError(t *testing.T) {
	dir := writeReloadFixture(t)

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components"}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	before := executeTemplate(t, tmpl, "products/index")
	writeFile(t, filepath.Join(dir, "products", "index.html"), `{{define "product"}}broken {{end`)

	_, err := tmpl.Reload(ReloadFileEvent{Path: filepath.Join(dir, "products", "index.html"), Op: ReloadWrite})
	if err == nil {
		t.Fatal("Reload() error = nil")
	}

	after := executeTemplate(t, tmpl, "products/index")
	if after != before {
		t.Fatalf("template changed after failed reload: before %q after %q", before, after)
	}
}

func TestReloadRemoveFallsBackToFullRebuild(t *testing.T) {
	dir := writeReloadFixture(t)

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components"}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	if err := os.Remove(filepath.Join(dir, "about.html")); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	result, err := tmpl.Reload(ReloadFileEvent{Path: filepath.Join(dir, "about.html"), Op: ReloadRemove})
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if !result.FullReload {
		t.Fatal("Reload() did not perform full reload for remove")
	}
	if result.FallbackReason != "structural file event" {
		t.Fatalf("fallback reason = %q", result.FallbackReason)
	}

	var b bytes.Buffer
	if err := tmpl.ExecuteTemplate(&b, "about", nil); err == nil {
		t.Fatalf("ExecuteTemplate(\"about\") error = nil, output = %q", b.String())
	}
	if got := executeTemplate(t, tmpl, "products/index"); got != "header root products index v1 card v1" {
		t.Fatalf("products/index render = %q", got)
	}
}

func TestReloadCreateFallsBackToFullRebuild(t *testing.T) {
	dir := writeReloadFixture(t)

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components"}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	writeFile(t, filepath.Join(dir, "new.html"), `{{define "content"}}new page{{end}}`)

	result, err := tmpl.Reload(ReloadFileEvent{Path: filepath.Join(dir, "new.html"), Op: ReloadCreate})
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if !result.FullReload {
		t.Fatal("Reload() did not perform full reload for create")
	}
	if got := executeTemplate(t, tmpl, "new"); got != "header root new page" {
		t.Fatalf("new page render = %q", got)
	}
}

func TestReloadIgnoresNoiseAndKnownChmod(t *testing.T) {
	dir := writeReloadFixture(t)

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components"}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	result, err := tmpl.Reload(
		ReloadFileEvent{Path: filepath.Join(dir, ".DS_Store"), Op: ReloadWrite},
		ReloadFileEvent{Path: filepath.Join(dir, "about.html"), Op: ReloadChmod},
	)
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if !result.Noop {
		t.Fatalf("Reload() Noop = false, result = %+v", result)
	}
	if len(result.ChangedPaths) != 0 {
		t.Fatalf("changed paths = %v", result.ChangedPaths)
	}
}

func TestReloadDirUsesRootRelativePaths(t *testing.T) {
	dir := writeReloadFixture(t)

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components"}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	writeFile(t, filepath.Join(dir, "about.html"), `{{define "content"}}about relative{{end}}`)

	result, err := tmpl.ReloadDir("about.html")
	if err != nil {
		t.Fatalf("ReloadDir() error = %v", err)
	}
	if !slices.Equal(result.RebuiltPages, []string{"about"}) {
		t.Fatalf("rebuilt pages = %v", result.RebuiltPages)
	}
}

func TestReloadObserverReportsSuccessAndFailure(t *testing.T) {
	dir := writeReloadFixture(t)
	observer := &recordingReloadObserver{}

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components", ReloadObserver: observer}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	writeFile(t, filepath.Join(dir, "about.html"), `{{define "content"}}about observed{{end}}`)
	if _, err := tmpl.ReloadDir("about.html"); err != nil {
		t.Fatalf("ReloadDir() error = %v", err)
	}
	if len(observer.events) != 1 {
		t.Fatalf("observer events = %d", len(observer.events))
	}
	if event := observer.events[0]; event.Err != nil ||
		event.FullReload ||
		event.RebuiltComponents ||
		event.Noop ||
		!slices.Equal(event.ChangedPaths, []string{"about.html"}) ||
		!slices.Equal(event.RebuiltPages, []string{"about"}) {
		t.Fatalf("success event = %+v", event)
	}

	writeFile(t, filepath.Join(dir, "about.html"), `{{define "content"}}broken {{end`)
	if _, err := tmpl.ReloadDir("about.html"); err == nil {
		t.Fatal("ReloadDir() error = nil")
	}
	if len(observer.events) != 2 {
		t.Fatalf("observer events = %d", len(observer.events))
	}
	if event := observer.events[1]; event.Err == nil ||
		event.Noop ||
		!slices.Equal(event.ChangedPaths, []string{"about.html"}) {
		t.Fatalf("failure event = %+v", event)
	}
}

func TestReloadObserverReportsNoop(t *testing.T) {
	dir := writeReloadFixture(t)
	observer := &recordingReloadObserver{}

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components", ReloadObserver: observer}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	if _, err := tmpl.Reload(ReloadFileEvent{Path: filepath.Join(dir, "about.html"), Op: ReloadChmod}); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if len(observer.events) != 1 {
		t.Fatalf("observer events = %d", len(observer.events))
	}
	if event := observer.events[0]; !event.Noop ||
		event.Err != nil ||
		event.FullReload ||
		event.RebuiltComponents ||
		len(event.ChangedPaths) != 0 ||
		len(event.RebuiltPages) != 0 {
		t.Fatalf("noop event = %+v", event)
	}
}

func TestTemplateLogsReloadMetrics(t *testing.T) {
	dir := writeReloadFixture(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components", Logger: logger}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	writeFile(t, filepath.Join(dir, "about.html"), `{{define "content"}}about logged{{end}}`)
	if _, err := tmpl.ReloadDir("about.html"); err != nil {
		t.Fatalf("ReloadDir() error = %v", err)
	}

	got := logs.String()
	for _, want := range []string{
		"parse_dir_complete",
		"reload_complete",
		"duration=",
		"full_reload=false",
		"rebuilt_components=false",
		"noop=false",
		"rebuilt_page_count=1",
		"changed_path_count=1",
		"rebuilt_pages=[about]",
		"changed_paths=[about.html]",
		"fallback_reason=\"\"",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("logs missing %q:\n%s", want, got)
		}
	}
}

func TestTemplateLogsReloadFallbackDecision(t *testing.T) {
	dir := writeReloadFixture(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components", Logger: logger}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	writeFile(t, filepath.Join(dir, "new.html"), `{{define "content"}}new logged page{{end}}`)
	if _, err := tmpl.Reload(ReloadFileEvent{Path: filepath.Join(dir, "new.html"), Op: ReloadCreate}); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	got := logs.String()
	for _, want := range []string{
		"reload_complete",
		"full_reload=true",
		"rebuilt_components=true",
		"changed_path_count=1",
		"rebuilt_page_count=3",
		"changed_paths=[new.html]",
		"fallback_reason=\"new template path\"",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("logs missing %q:\n%s", want, got)
		}
	}
}

func TestConcurrentSelectiveReloadAndExecute(t *testing.T) {
	dir := writeReloadFixture(t)

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components"}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	errs := make(chan error, 64)
	done := make(chan struct{})
	var wg sync.WaitGroup

	for range 8 {
		wg.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
					var b bytes.Buffer
					if err := tmpl.ExecuteTemplate(&b, "about", nil); err != nil {
						errs <- err
						return
					}
				}
			}
		})
	}

	for range 25 {
		writeFile(t, filepath.Join(dir, "about.html"), `{{define "content"}}about race{{end}}`)
		if _, err := tmpl.ReloadDir("about.html"); err != nil {
			t.Fatalf("ReloadDir() error = %v", err)
		}
	}

	close(done)
	wg.Wait()
	select {
	case err := <-errs:
		t.Fatal(err)
	default:
	}
}

func TestReloadDeepNestedTemplateGraph(t *testing.T) {
	dir := writeDeepReloadFixture(t)

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components"}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	if got := executeTemplate(t, tmpl, "products/enterprise/cases/acme"); got != "header root products nav enterprise badge v1 acme badge v1" {
		t.Fatalf("initial deep render = %q", got)
	}
	if got := sortedSet(tmpl.graph.layoutsByFile["products/layout.html"]); !slices.Equal(got, []string{"products/enterprise/cases/acme", "products/index"}) {
		t.Fatalf("products layout pages = %v", got)
	}
	if got := sortedSet(tmpl.graph.layoutsByFile["products/enterprise/layout.html"]); !slices.Equal(got, []string{"products/enterprise/cases/acme"}) {
		t.Fatalf("enterprise layout pages = %v", got)
	}
	if got := sortedSet(tmpl.graph.includesByDir["products/enterprise/components"]); !slices.Equal(got, []string{"products/enterprise/cases/acme"}) {
		t.Fatalf("enterprise component pages = %v", got)
	}

	writeFile(t, filepath.Join(dir, "products", "enterprise", "components", "badge.html"), `{{define "badge"}}badge v2{{end}}`)
	result, err := tmpl.Reload(ReloadFileEvent{Path: filepath.Join(dir, "products", "enterprise", "components", "badge.html"), Op: ReloadWrite})
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if result.FullReload {
		t.Fatal("enterprise component edit performed full reload")
	}
	if !result.RebuiltComponents {
		t.Fatal("enterprise component edit did not rebuild component set")
	}
	if !slices.Equal(result.RebuiltPages, []string{"products/enterprise/cases/acme"}) {
		t.Fatalf("enterprise component rebuilt pages = %v", result.RebuiltPages)
	}
	if got := executeTemplate(t, tmpl, "products/enterprise/cases/acme"); got != "header root products nav enterprise badge v2 acme badge v2" {
		t.Fatalf("updated deep render = %q", got)
	}

	writeFile(t, filepath.Join(dir, "products", "layout.html"), `{{define "content"}}products v2 {{nav}} {{block "product" .}}{{end}}{{end}}`)
	result, err = tmpl.Reload(ReloadFileEvent{Path: filepath.Join(dir, "products", "layout.html"), Op: ReloadWrite})
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if result.FullReload {
		t.Fatal("products layout edit performed full reload")
	}
	if !slices.Equal(result.RebuiltPages, []string{"products/enterprise/cases/acme", "products/index"}) {
		t.Fatalf("products layout rebuilt pages = %v", result.RebuiltPages)
	}
	if got := executeTemplate(t, tmpl, "products/index"); got != "header root products v2 nav index nav" {
		t.Fatalf("updated products/index render = %q", got)
	}
}

func TestEventDebouncerBatchesEvents(t *testing.T) {
	eventsCh := make(chan []fsnotify.Event, 1)
	debouncer := newEventDebouncer(10*time.Millisecond, 100*time.Millisecond, func(events []fsnotify.Event) {
		eventsCh <- events
	})

	debouncer.Add(fsnotify.Event{Name: "a.html", Op: fsnotify.Write})
	debouncer.Add(fsnotify.Event{Name: "b.html", Op: fsnotify.Write})

	select {
	case events := <-eventsCh:
		if len(events) != 2 {
			t.Fatalf("batched event count = %d", len(events))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for debounced events")
	}
}

func TestReloadOpFromFSNotify(t *testing.T) {
	op := reloadOpFromFSNotify(fsnotify.Write | fsnotify.Create | fsnotify.Chmod)
	if op&ReloadWrite == 0 || op&ReloadCreate == 0 || op&ReloadChmod == 0 {
		t.Fatalf("mapped op = %b", op)
	}
	if op&ReloadRemove != 0 || op&ReloadRename != 0 {
		t.Fatalf("mapped unexpected op = %b", op)
	}
}

func TestReloadLargeTreeRebuildCounts(t *testing.T) {
	dir := writeLargeReloadFixture(t, 5, 10)

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components"}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	writeFile(t, filepath.Join(dir, "section-2", "components", "card.html"), `{{define "card"}}section 2 card v2{{end}}`)
	result, err := tmpl.Reload(ReloadFileEvent{Path: filepath.Join(dir, "section-2", "components", "card.html"), Op: ReloadWrite})
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if result.FullReload {
		t.Fatal("section component edit performed full reload")
	}
	if len(result.RebuiltPages) != 10 {
		t.Fatalf("section component rebuilt %d pages, want 10: %v", len(result.RebuiltPages), result.RebuiltPages)
	}

	writeFile(t, filepath.Join(dir, "section-3", "layout.html"), `{{define "content"}}section 3 layout v2 {{block "body" .}}{{end}}{{end}}`)
	result, err = tmpl.Reload(ReloadFileEvent{Path: filepath.Join(dir, "section-3", "layout.html"), Op: ReloadWrite})
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if result.FullReload {
		t.Fatal("section layout edit performed full reload")
	}
	if len(result.RebuiltPages) != 10 {
		t.Fatalf("section layout rebuilt %d pages, want 10: %v", len(result.RebuiltPages), result.RebuiltPages)
	}

	writeFile(t, filepath.Join(dir, "components", "header.html"), `{{define "header"}}header v2{{end}}`)
	result, err = tmpl.Reload(ReloadFileEvent{Path: filepath.Join(dir, "components", "header.html"), Op: ReloadWrite})
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if result.FullReload {
		t.Fatal("global component edit performed full reload")
	}
	if len(result.RebuiltPages) != 50 {
		t.Fatalf("global component rebuilt %d pages, want 50: %v", len(result.RebuiltPages), result.RebuiltPages)
	}

	writeFile(t, filepath.Join(dir, "layout.html"), `{{template "header" .}} root v2 {{block "content" .}}{{end}}`)
	result, err = tmpl.Reload(ReloadFileEvent{Path: filepath.Join(dir, "layout.html"), Op: ReloadWrite})
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if result.FullReload {
		t.Fatal("root layout edit performed full reload")
	}
	if len(result.RebuiltPages) != 50 {
		t.Fatalf("root layout rebuilt %d pages, want 50: %v", len(result.RebuiltPages), result.RebuiltPages)
	}
}

func writeReloadFixture(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "layout.html"), `{{template "header" .}} root {{block "content" .}}{{end}}`)
	writeFile(t, filepath.Join(dir, "components", "header.html"), `{{define "header"}}header{{end}}`)
	writeFile(t, filepath.Join(dir, "about.html"), `{{define "content"}}about v1{{end}}`)
	writeFile(t, filepath.Join(dir, "products", "layout.html"), `{{define "content"}}products {{block "product" .}}{{end}}{{end}}`)
	writeFile(t, filepath.Join(dir, "products", "components", "card.html"), `{{define "card"}}card v1{{end}}`)
	writeFile(t, filepath.Join(dir, "products", "index.html"), `{{define "product"}}index v1 {{card}}{{end}}`)

	return dir
}

func writeLargeReloadFixture(t *testing.T, sections, pagesPerSection int) string {
	t.Helper()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "layout.html"), `{{template "header" .}} {{block "content" .}}{{end}}`)
	writeFile(t, filepath.Join(dir, "components", "header.html"), `{{define "header"}}header{{end}}`)

	for section := range sections {
		sectionName := "section-" + strconv.Itoa(section)
		writeFile(t, filepath.Join(dir, sectionName, "layout.html"), `{{define "content"}}`+sectionName+` {{block "body" .}}{{end}}{{end}}`)
		writeFile(t, filepath.Join(dir, sectionName, "components", "card.html"), `{{define "card"}}`+sectionName+` card{{end}}`)

		for page := range pagesPerSection {
			writeFile(t, filepath.Join(dir, sectionName, "page-"+strconv.Itoa(page)+".html"), `{{define "body"}}page `+strconv.Itoa(page)+` {{card}}{{end}}`)
		}
	}

	return dir
}

func writeDeepReloadFixture(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "layout.html"), `{{template "header" .}} root {{block "content" .}}{{end}}`)
	writeFile(t, filepath.Join(dir, "components", "header.html"), `{{define "header"}}header{{end}}`)
	writeFile(t, filepath.Join(dir, "products", "layout.html"), `{{define "content"}}products {{nav}} {{block "product" .}}{{end}}{{end}}`)
	writeFile(t, filepath.Join(dir, "products", "components", "nav.html"), `{{define "nav"}}nav{{end}}`)
	writeFile(t, filepath.Join(dir, "products", "index.html"), `{{define "product"}}index {{nav}}{{end}}`)
	writeFile(t, filepath.Join(dir, "products", "enterprise", "layout.html"), `{{define "product"}}enterprise {{badge}} {{block "case" .}}{{end}}{{end}}`)
	writeFile(t, filepath.Join(dir, "products", "enterprise", "components", "badge.html"), `{{define "badge"}}badge v1{{end}}`)
	writeFile(t, filepath.Join(dir, "products", "enterprise", "cases", "acme.html"), `{{define "case"}}acme {{badge}}{{end}}`)

	return dir
}

type recordingReloadObserver struct {
	events []ReloadEvent
}

func (o *recordingReloadObserver) ObserveReload(event ReloadEvent) {
	o.events = append(o.events, event)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func executeTemplate(t *testing.T, tmpl *Template, name string) string {
	t.Helper()

	var b bytes.Buffer
	if err := tmpl.ExecuteTemplate(&b, name, nil); err != nil {
		t.Fatalf("ExecuteTemplate(%q) error = %v", name, err)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func executeInline(t *testing.T, tmpl *Template, value string) string {
	t.Helper()

	var b bytes.Buffer
	if err := tmpl.Exec(&b, value, nil); err != nil {
		t.Fatalf("Exec(%q) error = %v", value, err)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func sourceContains(source *DebugSourceExcerpt, text string) bool {
	if source == nil {
		return false
	}
	for _, line := range source.Lines {
		if strings.Contains(line.Text, text) {
			return true
		}
	}
	return false
}
