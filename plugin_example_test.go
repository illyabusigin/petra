package petra_test

import (
	"bytes"
	"fmt"
	"html/template"
	"testing"
	"testing/fstest"

	"github.com/illyabusigin/petra"
)

type badgePlugin struct{}

func (badgePlugin) Funcs() (template.FuncMap, error) {
	return template.FuncMap{
		"badge": func(label string) template.HTML {
			return template.HTML(`<span class="badge">` + template.HTMLEscapeString(label) + `</span>`)
		},
	}, nil
}

func (badgePlugin) Apply(*template.Template) error {
	return nil
}

func ExamplePlugin() {
	files := fstest.MapFS{
		"templates/layout.html": {Data: []byte(`{{block "content" .}}{{end}}`)},
		"templates/index.html":  {Data: []byte(`{{define "content"}}{{badge .Role}}{{end}}`)},
	}

	tmpl := petra.NewWithOptions(petra.Options{
		Plugins: petra.Plugins{badgePlugin{}},
	})
	if err := tmpl.ParseFS(files, "templates"); err != nil {
		panic(err)
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "index", map[string]string{
		"Role": "Admin <Owner>",
	}); err != nil {
		panic(err)
	}

	fmt.Println(out.String())
	// Output:
	// <span class="badge">Admin &lt;Owner&gt;</span>
}

type labelPlugin string

func (p labelPlugin) Funcs() (template.FuncMap, error) {
	return template.FuncMap{
		"label": func() string {
			return string(p)
		},
	}, nil
}

func (labelPlugin) Apply(*template.Template) error {
	return nil
}

func TestPluginFunctionsOverrideFuncMap(t *testing.T) {
	got := renderPluginFixture(t, template.FuncMap{
		"label": func() string {
			return "funcmap"
		},
	}, petra.Plugins{labelPlugin("plugin")})

	if got != "plugin" {
		t.Fatalf("rendered label = %q, want plugin", got)
	}
}

func TestLaterPluginFunctionsOverrideEarlierPlugins(t *testing.T) {
	got := renderPluginFixture(t, nil, petra.Plugins{
		labelPlugin("first"),
		labelPlugin("second"),
	})

	if got != "second" {
		t.Fatalf("rendered label = %q, want second", got)
	}
}

type cachedPlugin struct{}

func (cachedPlugin) Funcs() (template.FuncMap, error) {
	calls := 0
	return template.FuncMap{
		"cached": func() string {
			calls++
			return fmt.Sprintf("hit-%d", calls)
		},
	}, nil
}

func (cachedPlugin) Apply(*template.Template) error {
	return nil
}

func TestPluginFunctionClosuresResetAfterReparse(t *testing.T) {
	files := fstest.MapFS{
		"templates/layout.html": {Data: []byte(`{{block "content" .}}{{end}}`)},
		"templates/index.html":  {Data: []byte(`{{define "content"}}{{cached}}{{end}}`)},
	}
	tmpl := petra.NewWithOptions(petra.Options{
		Plugins: petra.Plugins{cachedPlugin{}},
	})

	if err := tmpl.ParseFS(files, "templates"); err != nil {
		t.Fatalf("ParseFS() error = %v", err)
	}
	if got := executePluginFixture(t, tmpl); got != "hit-1" {
		t.Fatalf("first render = %q, want hit-1", got)
	}
	if got := executePluginFixture(t, tmpl); got != "hit-2" {
		t.Fatalf("second render = %q, want hit-2", got)
	}

	if err := tmpl.ParseFS(files, "templates"); err != nil {
		t.Fatalf("second ParseFS() error = %v", err)
	}
	if got := executePluginFixture(t, tmpl); got != "hit-1" {
		t.Fatalf("render after reparse = %q, want hit-1", got)
	}
}

func renderPluginFixture(t *testing.T, funcs template.FuncMap, plugins petra.Plugins) string {
	t.Helper()

	files := fstest.MapFS{
		"templates/layout.html": {Data: []byte(`{{block "content" .}}{{end}}`)},
		"templates/index.html":  {Data: []byte(`{{define "content"}}{{label}}{{end}}`)},
	}
	tmpl := petra.NewWithOptions(petra.Options{
		FuncMap: funcs,
		Plugins: plugins,
	})
	if err := tmpl.ParseFS(files, "templates"); err != nil {
		t.Fatalf("ParseFS() error = %v", err)
	}

	return executePluginFixture(t, tmpl)
}

func executePluginFixture(t *testing.T, tmpl *petra.Template) string {
	t.Helper()

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "index", nil); err != nil {
		t.Fatalf("ExecuteTemplate() error = %v", err)
	}
	return out.String()
}
