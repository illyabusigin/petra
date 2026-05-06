package plugins_test

import (
	"bytes"
	"html/template"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/illyabusigin/petra/plugins"
)

func TestMarkdownConcurrentCache(t *testing.T) {
	plugin := plugins.Markdown(fstest.MapFS{
		"content/page.md": {Data: []byte("# Hello\n\nA short page.")},
	}, "content")

	funcs, err := plugin.Funcs()
	if err != nil {
		t.Fatalf("Funcs() error = %v", err)
	}

	load := funcs["_loadMarkdown"].(func(string) (template.HTML, error))
	errs := make(chan string, 64)
	var wg sync.WaitGroup

	for range 16 {
		wg.Go(func() {
			for range 20 {
				got, err := load("page")
				if err != nil {
					errs <- err.Error()
					return
				}
				if !strings.Contains(string(got), "Hello") {
					errs <- string(got)
					return
				}
			}
		})
	}

	wg.Wait()
	close(errs)

	for got := range errs {
		t.Fatalf("rendered markdown missing expected content: %q", got)
	}
}

func TestMarkdownMissingFileFailsClosed(t *testing.T) {
	plugin := plugins.Markdown(fstest.MapFS{}, "content")

	funcs, err := plugin.Funcs()
	if err != nil {
		t.Fatalf("Funcs() error = %v", err)
	}

	load := funcs["_loadMarkdown"].(func(string) (template.HTML, error))
	got, err := load("missing")
	if err == nil {
		t.Fatalf("_loadMarkdown() error = nil, output = %q", got)
	}
	if got != "" {
		t.Fatalf("_loadMarkdown() output = %q, want empty trusted HTML on error", got)
	}
	if !strings.Contains(err.Error(), `load markdown "missing"`) {
		t.Fatalf("_loadMarkdown() error = %v", err)
	}

	tmpl := template.New("test").Funcs(funcs)
	if err := plugin.Apply(tmpl); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	tmpl, err = tmpl.Parse(`{{Markdown "missing"}}`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	var b bytes.Buffer
	err = tmpl.Execute(&b, nil)
	if err == nil {
		t.Fatalf("Execute() error = nil, output = %q", b.String())
	}
	if !strings.Contains(err.Error(), `load markdown "missing"`) {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestMarkdownCacheResetsWhenFuncsRebuilt(t *testing.T) {
	files := fstest.MapFS{
		"content/page.md": {Data: []byte("# First")},
	}
	plugin := plugins.Markdown(files, "content")

	funcs, err := plugin.Funcs()
	if err != nil {
		t.Fatalf("Funcs() error = %v", err)
	}
	load := funcs["_loadMarkdown"].(func(string) (template.HTML, error))

	first, err := load("page")
	if err != nil {
		t.Fatalf("_loadMarkdown() error = %v", err)
	}
	if !strings.Contains(string(first), "First") {
		t.Fatalf("first markdown = %q", first)
	}

	files["content/page.md"].Data = []byte("# Second")

	cached, err := load("page")
	if err != nil {
		t.Fatalf("_loadMarkdown() cached error = %v", err)
	}
	if !strings.Contains(string(cached), "First") {
		t.Fatalf("cached markdown = %q, want first render", cached)
	}

	funcs, err = plugin.Funcs()
	if err != nil {
		t.Fatalf("second Funcs() error = %v", err)
	}
	load = funcs["_loadMarkdown"].(func(string) (template.HTML, error))

	reloaded, err := load("page")
	if err != nil {
		t.Fatalf("_loadMarkdown() reloaded error = %v", err)
	}
	if !strings.Contains(string(reloaded), "Second") {
		t.Fatalf("reloaded markdown = %q, want second render", reloaded)
	}
}

func TestSVGConcurrentCache(t *testing.T) {
	plugin := plugins.SVG(fstest.MapFS{
		"icons/logo.svg": {Data: []byte(`<svg class="old" viewBox="0 0 1 1"></svg>`)},
	}, "icons")

	funcs, err := plugin.Funcs()
	if err != nil {
		t.Fatalf("Funcs() error = %v", err)
	}

	load := funcs["_loadSVG"].(func(string, string) (template.HTML, error))
	errs := make(chan string, 64)
	var wg sync.WaitGroup

	for range 16 {
		wg.Go(func() {
			for range 20 {
				got, err := load("logo", "new")
				if err != nil {
					errs <- err.Error()
					return
				}
				if !strings.Contains(string(got), `class="new"`) {
					errs <- string(got)
					return
				}
			}
		})
	}

	wg.Wait()
	close(errs)

	for got := range errs {
		t.Fatalf("rendered svg missing expected class: %q", got)
	}
}

func TestSVGCacheResetsWhenFuncsRebuilt(t *testing.T) {
	files := fstest.MapFS{
		"icons/logo.svg": {Data: []byte(`<svg class="old"><title>First</title></svg>`)},
	}
	plugin := plugins.SVG(files, "icons")

	funcs, err := plugin.Funcs()
	if err != nil {
		t.Fatalf("Funcs() error = %v", err)
	}
	load := funcs["_loadSVG"].(func(string, string) (template.HTML, error))

	first, err := load("logo", "icon")
	if err != nil {
		t.Fatalf("_loadSVG() error = %v", err)
	}
	if !strings.Contains(string(first), "First") || !strings.Contains(string(first), `class="icon"`) {
		t.Fatalf("first svg = %q", first)
	}

	files["icons/logo.svg"].Data = []byte(`<svg class="old"><title>Second</title></svg>`)

	cached, err := load("logo", "icon")
	if err != nil {
		t.Fatalf("_loadSVG() cached error = %v", err)
	}
	if !strings.Contains(string(cached), "First") {
		t.Fatalf("cached svg = %q, want first render", cached)
	}

	funcs, err = plugin.Funcs()
	if err != nil {
		t.Fatalf("second Funcs() error = %v", err)
	}
	load = funcs["_loadSVG"].(func(string, string) (template.HTML, error))

	reloaded, err := load("logo", "icon")
	if err != nil {
		t.Fatalf("_loadSVG() reloaded error = %v", err)
	}
	if !strings.Contains(string(reloaded), "Second") || !strings.Contains(string(reloaded), `class="icon"`) {
		t.Fatalf("reloaded svg = %q, want second render", reloaded)
	}
}

func TestSVGMissingFileFailsClosed(t *testing.T) {
	plugin := plugins.SVG(fstest.MapFS{}, "icons")

	funcs, err := plugin.Funcs()
	if err != nil {
		t.Fatalf("Funcs() error = %v", err)
	}

	load := funcs["_loadSVG"].(func(string, string) (template.HTML, error))
	got, err := load("missing", "icon")
	if err == nil {
		t.Fatalf("_loadSVG() error = nil, output = %q", got)
	}
	if got != "" {
		t.Fatalf("_loadSVG() output = %q, want empty trusted HTML on error", got)
	}
	if !strings.Contains(err.Error(), `load svg "missing"`) {
		t.Fatalf("_loadSVG() error = %v", err)
	}

	tmpl := template.New("test").Funcs(funcs)
	if err := plugin.Apply(tmpl); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	tmpl, err = tmpl.Parse(`{{SVG "missing" "icon"}}`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	var b bytes.Buffer
	err = tmpl.Execute(&b, nil)
	if err == nil {
		t.Fatalf("Execute() error = nil, output = %q", b.String())
	}
	if !strings.Contains(err.Error(), `load svg "missing"`) {
		t.Fatalf("Execute() error = %v", err)
	}
}
