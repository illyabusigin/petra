package petra_test

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/illyabusigin/petra"
)

func TestComponentSetRendersMountedNamespaceFreeComponent(t *testing.T) {
	files := fstest.MapFS{
		"templates/layout.html": {Data: []byte(`{{block "content" .}}{{end}}`)},
		"templates/index.html":  {Data: []byte(`{{define "content"}}{{UI.TextField "email" "Email" "email" (dict "type" "email") .Errors.Email}}{{end}}`)},
		"ui/fields.html":        {Data: []byte(`{{define "TextField name label id attrs error?"}}{{fieldLabel .id .label}}<input id="{{.id}}" name="{{.name}}"{{range $k, $v := .attrs}} {{attrs $k $v}}{{end}}>{{if .error}}<span class="error">{{.error}}</span>{{end}}{{end}}{{define "fieldLabel id label"}}<label for="{{.id}}">{{.label}}</label>{{end}}`)},
	}

	ui := petra.NewComponentSet("example.com/petra/ui", files, "ui")
	tmpl := petra.NewWithOptions(petra.Options{
		FuncMap: template.FuncMap{
			"dict": testDict,
		},
		Plugins: petra.Plugins{
			petra.HTML(),
			petra.Components("UI", ui),
		},
	})
	if err := tmpl.ParseFS(files, "templates"); err != nil {
		t.Fatalf("ParseFS() error = %v", err)
	}

	var out bytes.Buffer
	err := tmpl.ExecuteTemplate(&out, "index", map[string]any{
		"Errors": map[string]string{
			"Email": "Use a valid email",
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate() error = %v", err)
	}

	want := `<label for="email">Email</label><input id="email" name="email" type="email"><span class="error">Use a valid email</span>`
	if got := out.String(); got != want {
		t.Fatalf("rendered HTML = %q, want %q", got, want)
	}
}

func TestComponentSetIsAvailableToExec(t *testing.T) {
	files := fstest.MapFS{
		"templates/layout.html": {Data: []byte(`{{block "content" .}}{{end}}`)},
		"templates/index.html":  {Data: []byte(`{{define "content"}}index{{end}}`)},
		"ui/text.html":          {Data: []byte(`{{define "Text value"}}<span>{{.value}}</span>{{end}}`)},
	}

	ui := petra.NewComponentSet("example.com/petra/ui", files, "ui")
	tmpl := petra.NewWithOptions(petra.Options{
		Plugins: petra.Plugins{
			petra.Components("UI", ui),
		},
	})
	if err := tmpl.ParseFS(files, "templates"); err != nil {
		t.Fatalf("ParseFS() error = %v", err)
	}

	var out bytes.Buffer
	if err := tmpl.Exec(&out, `{{UI.Text "inline"}}`, nil); err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if got, want := out.String(), `<span>inline</span>`; got != want {
		t.Fatalf("Exec() output = %q, want %q", got, want)
	}
}

func TestComponentSetReloadsFromWatchedExternalFolder(t *testing.T) {
	appDir := t.TempDir()
	libDir := t.TempDir()

	writeFile(t, filepath.Join(appDir, "layout.html"), `{{block "content" .}}{{end}}`)
	writeFile(t, filepath.Join(appDir, "index.html"), `{{define "content"}}{{UI.Badge .Label}}{{end}}`)
	componentPath := filepath.Join(libDir, "badge.html")
	writeFile(t, componentPath, `{{define "Badge label"}}<strong>{{.label}}</strong>{{end}}`)

	ui := petra.NewComponentSet("example.com/petra/ui", os.DirFS(libDir), ".")
	tmpl := petra.NewWithOptions(petra.Options{
		Plugins: petra.Plugins{
			petra.Components("UI", ui),
		},
	})
	if err := tmpl.ParseDir(appDir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}
	if got := executeComponentSetReloadFixture(t, tmpl, "First"); got != `<strong>First</strong>` {
		t.Fatalf("before reload = %q", got)
	}

	writeFile(t, componentPath, `{{define "Badge label"}}<em>{{.label}}</em>{{end}}`)
	result, err := tmpl.Reload(petra.ReloadFileEvent{Path: componentPath, Op: petra.ReloadWrite})
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if !result.FullReload {
		t.Fatalf("Reload() FullReload = false, result = %#v", result)
	}
	if !strings.Contains(result.FallbackReason, "outside template root") {
		t.Fatalf("Reload() fallback = %q, want outside template root", result.FallbackReason)
	}
	if got := executeComponentSetReloadFixture(t, tmpl, "Second"); got != `<em>Second</em>` {
		t.Fatalf("after reload = %q", got)
	}
}

func TestComponentSetSupportsTransitivePrivateImports(t *testing.T) {
	files := fstest.MapFS{
		"templates/layout.html": {Data: []byte(`{{block "content" .}}{{end}}`)},
		"templates/index.html":  {Data: []byte(`{{define "content"}}{{Kit.Toolbar "Save"}}{{end}}`)},
		"base/button.html":      {Data: []byte(`{{define "Button label"}}<button>{{.label}}</button>{{end}}`)},
		"kit/toolbar.html":      {Data: []byte(`{{define "Toolbar label"}}<nav>{{Base.Button .label}}</nav>{{end}}`)},
	}

	base := petra.NewComponentSet("example.com/petra/base", files, "base")
	kit := petra.NewComponentSet("example.com/petra/kit", files, "kit", petra.Import("Base", base))
	tmpl := petra.NewWithOptions(petra.Options{
		Plugins: petra.Plugins{
			petra.Components("Kit", kit),
		},
	})
	if err := tmpl.ParseFS(files, "templates"); err != nil {
		t.Fatalf("ParseFS() error = %v", err)
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "index", nil); err != nil {
		t.Fatalf("ExecuteTemplate() error = %v", err)
	}
	if got, want := out.String(), `<nav><button>Save</button></nav>`; got != want {
		t.Fatalf("rendered HTML = %q, want %q", got, want)
	}

	out.Reset()
	if err := tmpl.Exec(&out, `{{Base.Button "Save"}}`, nil); err == nil {
		t.Fatalf("Exec() succeeded for private transitive import, output = %q", out.String())
	}
}

func TestComponentSetCanMountTransitiveDependencyDirectly(t *testing.T) {
	files := fstest.MapFS{
		"templates/layout.html": {Data: []byte(`{{block "content" .}}{{end}}`)},
		"templates/index.html":  {Data: []byte(`{{define "content"}}{{Kit.Toolbar "Save"}} {{UI.Button "Cancel"}}{{end}}`)},
		"base/button.html":      {Data: []byte(`{{define "Button label"}}<button>{{.label}}</button>{{end}}`)},
		"kit/toolbar.html":      {Data: []byte(`{{define "Toolbar label"}}<nav>{{Base.Button .label}}</nav>{{end}}`)},
	}

	base := petra.NewComponentSet("example.com/petra/base", files, "base")
	kit := petra.NewComponentSet("example.com/petra/kit", files, "kit", petra.Import("Base", base))
	tmpl := petra.NewWithOptions(petra.Options{
		Plugins: petra.Plugins{
			petra.Components("Kit", kit),
			petra.Components("UI", base),
		},
	})
	if err := tmpl.ParseFS(files, "templates"); err != nil {
		t.Fatalf("ParseFS() error = %v", err)
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "index", nil); err != nil {
		t.Fatalf("ExecuteTemplate() error = %v", err)
	}
	if got, want := out.String(), `<nav><button>Save</button></nav> <button>Cancel</button>`; got != want {
		t.Fatalf("rendered HTML = %q, want %q", got, want)
	}
}

func TestComponentSetDoesNotExposeBareAppAliases(t *testing.T) {
	files := fstest.MapFS{
		"templates/layout.html": {Data: []byte(`{{block "content" .}}{{end}}`)},
		"templates/index.html":  {Data: []byte(`{{define "content"}}{{Text "inline"}}{{end}}`)},
		"ui/text.html":          {Data: []byte(`{{define "Text value"}}<span>{{.value}}</span>{{end}}`)},
	}

	ui := petra.NewComponentSet("example.com/petra/ui", files, "ui")
	tmpl := petra.NewWithOptions(petra.Options{
		Plugins: petra.Plugins{
			petra.Components("UI", ui),
		},
	})

	err := tmpl.ParseFS(files, "templates")
	if err == nil {
		t.Fatal("ParseFS() succeeded for bare component call")
	}
	if !strings.Contains(err.Error(), `function "Text" not defined`) {
		t.Fatalf("ParseFS() error = %v", err)
	}
}

func TestComponentSetRejectsMissingMountedComponent(t *testing.T) {
	files := fstest.MapFS{
		"templates/layout.html": {Data: []byte(`{{block "content" .}}{{end}}`)},
		"templates/index.html":  {Data: []byte(`{{define "content"}}{{UI.Missing "inline"}}{{end}}`)},
		"ui/text.html":          {Data: []byte(`{{define "Text value"}}<span>{{.value}}</span>{{end}}`)},
	}

	ui := petra.NewComponentSet("example.com/petra/ui", files, "ui")
	tmpl := petra.NewWithOptions(petra.Options{
		Plugins: petra.Plugins{
			petra.Components("UI", ui),
		},
	})

	err := tmpl.ParseFS(files, "templates")
	if err == nil {
		t.Fatal("ParseFS() succeeded for missing mounted component")
	}
	if !strings.Contains(err.Error(), `function "UI.Missing" not defined`) {
		t.Fatalf("ParseFS() error = %v", err)
	}
}

func TestComponentSetExportsOnlyUppercaseDefinitions(t *testing.T) {
	files := fstest.MapFS{
		"templates/layout.html": {Data: []byte(`{{block "content" .}}{{end}}`)},
		"templates/index.html":  {Data: []byte(`{{define "content"}}{{UI.Text "inline"}}{{end}}`)},
		"ui/text.html":          {Data: []byte(`{{define "Text value"}}{{privateText .value}}{{end}}{{define "privateText value"}}<span>{{.value}}</span>{{end}}`)},
	}

	ui := petra.NewComponentSet("example.com/petra/ui", files, "ui")
	tmpl := petra.NewWithOptions(petra.Options{
		Plugins: petra.Plugins{
			petra.Components("UI", ui),
		},
	})
	if err := tmpl.ParseFS(files, "templates"); err != nil {
		t.Fatalf("ParseFS() error = %v", err)
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "index", nil); err != nil {
		t.Fatalf("ExecuteTemplate() error = %v", err)
	}
	if got, want := out.String(), `<span>inline</span>`; got != want {
		t.Fatalf("rendered HTML = %q, want %q", got, want)
	}

	out.Reset()
	if err := tmpl.Exec(&out, `{{UI.privateText "inline"}}`, nil); err == nil {
		t.Fatalf("Exec() succeeded for private component, output = %q", out.String())
	}
}

func TestComponentSetRequiresPlugins(t *testing.T) {
	files := fstest.MapFS{
		"templates/layout.html": {Data: []byte(`{{block "content" .}}{{end}}`)},
		"templates/index.html":  {Data: []byte(`{{define "content"}}{{UI.Link "/account" "Account"}}{{end}}`)},
		"ui/link.html":          {Data: []byte(`{{define "Link href label"}}<a {{attrs "href" .href}}>{{.label}}</a>{{end}}`)},
	}

	ui := petra.NewComponentSet("example.com/petra/ui", files, "ui", petra.Requires(petra.HTML()))
	tmpl := petra.NewWithOptions(petra.Options{
		Plugins: petra.Plugins{
			petra.Components("UI", ui),
		},
	})
	if err := tmpl.ParseFS(files, "templates"); err != nil {
		t.Fatalf("ParseFS() error = %v", err)
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "index", nil); err != nil {
		t.Fatalf("ExecuteTemplate() error = %v", err)
	}
	if got, want := out.String(), `<a href="/account">Account</a>`; got != want {
		t.Fatalf("rendered HTML = %q, want %q", got, want)
	}
}

func TestComponentSetRejectsDuplicateNamespace(t *testing.T) {
	files := fstest.MapFS{
		"templates/layout.html": {Data: []byte(`{{block "content" .}}{{end}}`)},
		"templates/index.html":  {Data: []byte(`{{define "content"}}{{UI.One}}{{end}}`)},
		"one/one.html":          {Data: []byte(`{{define "One"}}one{{end}}`)},
		"two/two.html":          {Data: []byte(`{{define "Two"}}two{{end}}`)},
	}

	one := petra.NewComponentSet("example.com/petra/one", files, "one")
	two := petra.NewComponentSet("example.com/petra/two", files, "two")
	tmpl := petra.NewWithOptions(petra.Options{
		Plugins: petra.Plugins{
			petra.Components("UI", one),
			petra.Components("UI", two),
		},
	})

	err := tmpl.ParseFS(files, "templates")
	if err == nil {
		t.Fatal("ParseFS() succeeded for duplicate namespace")
	}
	if !strings.Contains(err.Error(), `component namespace "UI" is already mounted`) {
		t.Fatalf("ParseFS() error = %v", err)
	}
}

func TestComponentSetRejectsDuplicateSetID(t *testing.T) {
	files := fstest.MapFS{
		"templates/layout.html": {Data: []byte(`{{block "content" .}}{{end}}`)},
		"templates/index.html":  {Data: []byte(`{{define "content"}}{{One.Component}} {{Two.Component}}{{end}}`)},
		"one/component.html":    {Data: []byte(`{{define "Component"}}one{{end}}`)},
		"two/component.html":    {Data: []byte(`{{define "Component"}}two{{end}}`)},
	}

	one := petra.NewComponentSet("example.com/petra/shared", files, "one")
	two := petra.NewComponentSet("example.com/petra/shared", files, "two")
	tmpl := petra.NewWithOptions(petra.Options{
		Plugins: petra.Plugins{
			petra.Components("One", one),
			petra.Components("Two", two),
		},
	})

	err := tmpl.ParseFS(files, "templates")
	if err == nil {
		t.Fatal("ParseFS() succeeded for duplicate set ID")
	}
	if !strings.Contains(err.Error(), `component set "example.com/petra/shared" registered with multiple definitions`) {
		t.Fatalf("ParseFS() error = %v", err)
	}
}

func TestComponentSetImportMustReferenceExportedComponents(t *testing.T) {
	files := fstest.MapFS{
		"templates/layout.html": {Data: []byte(`{{block "content" .}}{{end}}`)},
		"templates/index.html":  {Data: []byte(`{{define "content"}}{{Kit.Toolbar "Save"}}{{end}}`)},
		"base/button.html":      {Data: []byte(`{{define "button label"}}<button>{{.label}}</button>{{end}}`)},
		"kit/toolbar.html":      {Data: []byte(`{{define "Toolbar label"}}<nav>{{Base.button .label}}</nav>{{end}}`)},
	}

	base := petra.NewComponentSet("example.com/petra/base", files, "base")
	kit := petra.NewComponentSet("example.com/petra/kit", files, "kit", petra.Import("Base", base))
	tmpl := petra.NewWithOptions(petra.Options{
		Plugins: petra.Plugins{
			petra.Components("Kit", kit),
		},
	})

	err := tmpl.ParseFS(files, "templates")
	if err == nil {
		t.Fatal("ParseFS() succeeded for import of private component")
	}
	if !strings.Contains(err.Error(), `component "Base.button" is not exported by import "Base"`) {
		t.Fatalf("ParseFS() error = %v", err)
	}
}

func TestComponentSetReloadFailureKeepsPreviousTemplateSet(t *testing.T) {
	appDir := t.TempDir()
	libDir := t.TempDir()

	writeFile(t, filepath.Join(appDir, "layout.html"), `{{block "content" .}}{{end}}`)
	writeFile(t, filepath.Join(appDir, "index.html"), `{{define "content"}}{{UI.Badge .Label}}{{end}}`)
	componentPath := filepath.Join(libDir, "badge.html")
	writeFile(t, componentPath, `{{define "Badge label"}}<strong>{{.label}}</strong>{{end}}`)

	ui := petra.NewComponentSet("example.com/petra/ui", os.DirFS(libDir), ".")
	tmpl := petra.NewWithOptions(petra.Options{
		Plugins: petra.Plugins{
			petra.Components("UI", ui),
		},
	})
	if err := tmpl.ParseDir(appDir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	writeFile(t, componentPath, `{{define "Badge label"}}<em>{{.label}}</em>`)
	if _, err := tmpl.Reload(petra.ReloadFileEvent{Path: componentPath, Op: petra.ReloadWrite}); err == nil {
		t.Fatal("Reload() succeeded for invalid component set")
	}

	if got := executeComponentSetReloadFixture(t, tmpl, "Stable"); got != `<strong>Stable</strong>` {
		t.Fatalf("after failed reload = %q", got)
	}
}

func testDict(values ...any) (map[string]any, error) {
	if len(values)%2 != 0 {
		return nil, fmt.Errorf("dict requires key/value pairs")
	}
	out := make(map[string]any, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		key, ok := values[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict key %d is %T, want string", i/2, values[i])
		}
		out[key] = values[i+1]
	}
	return out, nil
}

func executeComponentSetReloadFixture(t *testing.T, tmpl *petra.Template, label string) string {
	t.Helper()

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "index", map[string]string{"Label": label}); err != nil {
		t.Fatalf("ExecuteTemplate() error = %v", err)
	}
	return out.String()
}

func writeFile(t *testing.T, name, content string) {
	t.Helper()

	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", name, err)
	}
}
