package petra_test

import (
	"errors"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/illyabusigin/petra"
	"github.com/illyabusigin/petra/test/templates"
)

func TestNewWithOptions(t *testing.T) {
	defaults := petra.NewWithOptions(petra.Options{})
	if defaults.Layout != "layout.html" {
		t.Fatalf("default Layout = %q", defaults.Layout)
	}
	if defaults.IncludeDir != "includes" {
		t.Fatalf("default IncludeDir = %q", defaults.IncludeDir)
	}

	funcs := template.FuncMap{"hello": func() string { return "hello" }}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tmpl := petra.NewWithOptions(petra.Options{
		Layout:         "base.html",
		IncludeDir:     "components",
		PageExtensions: []string{".html"},
		FuncMap:        funcs,
		Plugins:        petra.Plugins{petra.HTML()},
		ReloadObserver: testReloadObserver{},
		Logger:         logger,
	})

	if tmpl.Layout != "base.html" {
		t.Fatalf("Layout = %q", tmpl.Layout)
	}
	if tmpl.IncludeDir != "components" {
		t.Fatalf("IncludeDir = %q", tmpl.IncludeDir)
	}
	if len(tmpl.PageExtensions) != 1 || tmpl.PageExtensions[0] != ".html" {
		t.Fatalf("PageExtensions = %v", tmpl.PageExtensions)
	}
	if tmpl.FuncMap["hello"] == nil {
		t.Fatal("FuncMap was not retained")
	}
	if len(tmpl.Plugins) != 1 {
		t.Fatalf("Plugins length = %d", len(tmpl.Plugins))
	}
	if tmpl.ReloadObserver == nil {
		t.Fatal("ReloadObserver was not retained")
	}
	if tmpl.Logger != logger {
		t.Fatal("Logger was not retained")
	}
}

func TestExec(t *testing.T) {
	tmpl := &petra.Template{
		Layout:     "layout.html",
		IncludeDir: "components",
	}
	if err := tmpl.ParseDir("test/templates"); err != nil {
		t.Fatalf("error parsing template: %v", err)
	}

	var b strings.Builder
	if err := tmpl.Exec(&b, `{{link "/docs" "Docs"}}`, nil); err != nil {
		t.Fatalf("error executing template: %v", err)
	}

	expect := `<a href="/docs">Docs</a>`
	got := b.String()
	if expect != got {
		t.Fatalf("expected:\n%v\ngot:\n%v", expect, got)
	}
}

func TestHTMLPluginRendersThroughPetra(t *testing.T) {
	tmpl := petra.NewWithOptions(petra.Options{
		Plugins: petra.Plugins{petra.HTML()},
	})
	if err := tmpl.ParseFS(fstest.MapFS{
		"layout.html": {Data: []byte(`{{block "content" .}}{{end}}`)},
		"index.html":  {Data: []byte(`{{define "content"}}<a {{attrs "href" .Path}}>{{html .Label}}</a><script>{{js .Script}}</script>{{end}}`)},
	}, "."); err != nil {
		t.Fatalf("ParseFS() error = %v", err)
	}

	var b strings.Builder
	err := tmpl.ExecuteTemplate(&b, "index", map[string]string{
		"Path":   "/docs?a=1&b=2",
		"Label":  "<strong>Docs</strong>",
		"Script": "window.__petra = true",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate() error = %v", err)
	}

	want := `<a href="/docs?a=1&amp;b=2"><strong>Docs</strong></a><script>window.__petra = true</script>`
	if got := b.String(); got != want {
		t.Fatalf("rendered HTML = %q, want %q", got, want)
	}
}

func TestExecuteTemplateReturnsDebugError(t *testing.T) {
	tmpl := petra.NewWithOptions(petra.Options{
		FuncMap: template.FuncMap{
			"fail": func() (string, error) {
				return "", errors.New("page exploded")
			},
		},
	})
	if err := tmpl.ParseFS(fstest.MapFS{
		"layout.html": {Data: []byte(`{{block "content" .}}{{end}}`)},
		"index.html":  {Data: []byte(`{{define "content"}}before {{fail}}{{end}}`)},
	}, "."); err != nil {
		t.Fatalf("ParseFS() error = %v", err)
	}

	var b strings.Builder
	err := tmpl.ExecuteTemplate(&b, "index", nil)
	if err == nil {
		t.Fatal("ExecuteTemplate() error = nil")
	}

	if _, ok := errors.AsType[petra.ExecuteError](err); !ok {
		t.Fatalf("ExecuteTemplate() error does not unwrap to petra.ExecuteError: %T %[1]v", err)
	}

	info, ok := petra.DebugInfo(err)
	if !ok {
		t.Fatal("DebugInfo() did not identify a Petra debug error")
	}
	if info.Kind != petra.DebugErrorKindExecute {
		t.Fatalf("debug kind = %q, want %q", info.Kind, petra.DebugErrorKindExecute)
	}
	if info.Page != "index" {
		t.Fatalf("debug page = %q, want index", info.Page)
	}
	if info.Path != "index.html" {
		t.Fatalf("debug path = %q, want index.html", info.Path)
	}
	if info.Location == nil || info.Location.Line == 0 {
		t.Fatalf("debug location = %#v, want template line", info.Location)
	}
	if info.GoStack == "" {
		t.Fatal("debug Go stack is empty")
	}
	if info.Source == nil {
		t.Fatal("debug source is nil")
	}
	if info.Source.Path != "index.html" {
		t.Fatalf("debug source path = %q, want index.html", info.Source.Path)
	}
	if !debugSourceContains(info.Source, `before {{fail}}`) {
		t.Fatalf("debug source = %#v, want failing page line", info.Source)
	}
}

func TestParseFSDebugErrorIncludesRoleAndSourceExcerpt(t *testing.T) {
	tmpl := petra.New()
	err := tmpl.ParseFS(fstest.MapFS{
		"layout.html": {Data: []byte(`{{block "content" .}}{{end}}`)},
		"index.html":  {Data: []byte("{{define \"content\"}}\nhello {{end\n{{end}}")},
	}, ".")
	if err == nil {
		t.Fatal("ParseFS() error = nil")
	}

	if _, ok := errors.AsType[petra.ParseError](err); !ok {
		t.Fatalf("ParseFS() error does not unwrap to petra.ParseError: %T %[1]v", err)
	}

	info, ok := petra.DebugInfo(err)
	if !ok {
		t.Fatal("DebugInfo() did not identify a Petra debug error")
	}
	if info.Kind != petra.DebugErrorKindParse {
		t.Fatalf("debug kind = %q, want %q", info.Kind, petra.DebugErrorKindParse)
	}
	if info.DependencyRole != petra.DebugDependencyRolePage {
		t.Fatalf("debug role = %q, want %q", info.DependencyRole, petra.DebugDependencyRolePage)
	}
	if info.Page != "index" {
		t.Fatalf("debug page = %q, want index", info.Page)
	}
	if info.Path != "index.html" {
		t.Fatalf("debug path = %q, want index.html", info.Path)
	}
	if info.Source == nil {
		t.Fatal("debug source is nil")
	}
	if info.Source.Path != "index.html" {
		t.Fatalf("debug source path = %q, want index.html", info.Source.Path)
	}
	if !debugSourceContains(info.Source, `hello {{end`) {
		t.Fatalf("debug source = %#v, want failing source line", info.Source)
	}

	req := httptest.NewRequest(http.MethodGet, "/broken", nil)
	rec := httptest.NewRecorder()
	if !petra.RenderDebugError(rec, req, err, petra.DebugOptions{Enabled: true}) {
		t.Fatal("RenderDebugError() returned false")
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Source Excerpt",
		"index.html",
		"hello {{end",
		"Role",
		"page",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("debug page missing %q:\n%s", want, body)
		}
	}
}

func TestExecuteTemplateDebugInfoIncludesComponentFrame(t *testing.T) {
	tmpl := petra.NewWithOptions(petra.Options{
		IncludeDir: "components",
		FuncMap: template.FuncMap{
			"fail": func() (string, error) {
				return "", errors.New("component exploded")
			},
		},
	})
	if err := tmpl.ParseFS(fstest.MapFS{
		"layout.html":          {Data: []byte(`{{block "content" .}}{{end}}`)},
		"components/card.html": {Data: []byte(`{{define "Card"}}{{fail}}{{end}}`)},
		"index.html":           {Data: []byte(`{{define "content"}}{{Card}}{{end}}`)},
	}, "."); err != nil {
		t.Fatalf("ParseFS() error = %v", err)
	}

	var b strings.Builder
	err := tmpl.ExecuteTemplate(&b, "index", nil)
	if err == nil {
		t.Fatal("ExecuteTemplate() error = nil")
	}

	info, ok := petra.DebugInfo(err)
	if !ok {
		t.Fatal("DebugInfo() did not identify a Petra debug error")
	}
	if info.Kind != petra.DebugErrorKindComponent {
		t.Fatalf("debug kind = %q, want %q", info.Kind, petra.DebugErrorKindComponent)
	}
	if info.Component != "Card" {
		t.Fatalf("debug component = %q, want Card", info.Component)
	}
	if !hasDebugFrame(info.Frames, "component", "Card") {
		t.Fatalf("debug frames = %#v, want component Card", info.Frames)
	}
}

func TestRenderDebugErrorRequiresOptIn(t *testing.T) {
	err := errors.New("render failed")
	req := httptest.NewRequest(http.MethodGet, "/broken", nil)

	disabled := httptest.NewRecorder()
	if petra.RenderDebugError(disabled, req, err, petra.DebugOptions{}) {
		t.Fatal("RenderDebugError() returned true when disabled")
	}
	if disabled.Body.Len() != 0 {
		t.Fatalf("disabled body = %q, want empty", disabled.Body.String())
	}

	enabled := httptest.NewRecorder()
	if !petra.RenderDebugError(enabled, req, err, petra.DebugOptions{
		Enabled:        true,
		IncludeGoStack: true,
		Title:          "Debug failure",
	}) {
		t.Fatal("RenderDebugError() returned false when enabled")
	}
	if enabled.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", enabled.Code)
	}
	got := enabled.Body.String()
	for _, want := range []string{
		"Petra debug",
		"Debug failure",
		"render failed",
		"GET /broken",
		"Go Stack",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("debug page missing %q:\n%s", want, got)
		}
	}
}

func TestExecuteTemplate(t *testing.T) {
	tmpl := petra.New()
	if err := tmpl.ParseDir("test/templates"); err != nil {
		t.Errorf("error parsing template: %v", err)
	}

	{
		var b strings.Builder
		if err := tmpl.ExecuteTemplate(&b, "profile/payments/methods", "testdata"); err != nil {
			t.Errorf("error executing template: %v\n", err)
		}
		expect := "header layout profile payments_layout methods method footer \n"
		got := b.String()
		if expect != got {
			t.Errorf("expected:\n%v\ngot: %v\n", expect, got)
		}
	}

	{
		var b strings.Builder
		if err := tmpl.ExecuteTemplate(&b, "profile/edit", "testdata"); err != nil {

			t.Errorf("error executing template: %v\n", err)
		}

		expect := "header layout profile edit testdatafooter \n"
		got := b.String()
		if expect != got {
			t.Errorf("expected:\n%v\ngot: %v\n", expect, got)
		}
	}

	{
		var b strings.Builder
		if err := tmpl.ExecuteTemplate(&b, "profile/view", "testdata"); err != nil {
			t.Errorf("error executing template: %v\n", err)
		}
		expect := "header layout profile view testdatafooter \n"
		got := b.String()
		if expect != got {
			t.Errorf("expected:\n%v\ngot: %v\n", expect, got)
		}
	}
}

func TestExecuteTemplateFS(t *testing.T) {
	tmpl := petra.New()

	if err := tmpl.ParseFS(templates.FS, "."); err != nil {
		t.Errorf("error executing template: %v", err)
	}

	{
		var b strings.Builder
		if err := tmpl.ExecuteTemplate(&b, "profile/payments/methods", "testdata"); err != nil {
			t.Errorf("error executing template: %v\n", err)
		}
		expect := "header layout profile payments_layout methods method footer \n"
		got := b.String()
		if expect != got {
			t.Errorf("expected:\n%v\ngot: %v\n", expect, got)
		}
	}

	{
		var b strings.Builder
		if err := tmpl.ExecuteTemplate(&b, "profile/edit", "testdata"); err != nil {

			t.Errorf("error executing template: %v\n", err)
		}

		expect := "header layout profile edit testdatafooter \n"
		got := b.String()
		if expect != got {
			t.Errorf("expected:\n%v\ngot: %v\n", expect, got)
		}
	}

	{
		var b strings.Builder
		if err := tmpl.ExecuteTemplate(&b, "profile/view", "testdata"); err != nil {
			t.Errorf("error executing template: %v\n", err)
		}
		expect := "header layout profile view testdatafooter \n"
		got := b.String()
		if expect != got {
			t.Errorf("expected:\n%v\ngot: %v\n", expect, got)
		}
	}
}

func TestConcurrentParseAndExecute(t *testing.T) {
	tmpl := petra.New()
	if err := tmpl.ParseDir("test/templates"); err != nil {
		t.Fatalf("error parsing template: %v", err)
	}

	errs := make(chan error, 128)
	var wg sync.WaitGroup

	for range 8 {
		wg.Go(func() {
			for range 100 {
				var b strings.Builder
				if err := tmpl.ExecuteTemplate(&b, "profile/view", "testdata"); err != nil {
					errs <- err
					return
				}
			}
		})
	}

	wg.Go(func() {
		for range 25 {
			if err := tmpl.ParseDir("test/templates"); err != nil {
				errs <- err
				return
			}
		}
	})

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatal(err)
	}
}

type testReloadObserver struct{}

func (testReloadObserver) ObserveReload(petra.ReloadEvent) {}

func hasDebugFrame(frames []petra.DebugFrame, kind, name string) bool {
	for _, frame := range frames {
		if frame.Kind == kind && frame.Name == name {
			return true
		}
	}
	return false
}

func debugSourceContains(source *petra.DebugSourceExcerpt, text string) bool {
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
