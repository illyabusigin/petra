package petra_test

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing/fstest"

	"github.com/illyabusigin/petra"
)

func ExampleNewWithOptions() {
	files := fstest.MapFS{
		"templates/layout.html":            {Data: []byte(`{{Header}} {{block "content" .}}{{end}}`)},
		"templates/components/header.html": {Data: []byte(`{{define "Header"}}{{asset "/app.css"}}{{end}}`)},
		"templates/home.html":              {Data: []byte(`{{define "content"}}{{.Title}}{{end}}`)},
		"templates/robots.txt":             {Data: []byte(`ignored by PageExtensions`)},
	}

	tmpl := petra.NewWithOptions(petra.Options{
		IncludeDir:     "components",
		PageExtensions: []string{".html"},
		FuncMap: template.FuncMap{
			"asset": func(path string) string {
				return "/static" + path
			},
		},
	})
	if err := tmpl.ParseFS(files, "templates"); err != nil {
		panic(err)
	}

	var out strings.Builder
	if err := tmpl.ExecuteTemplate(&out, "home", map[string]string{
		"Title": "Home",
	}); err != nil {
		panic(err)
	}

	fmt.Println(out.String())
	// Output:
	// /static/app.css Home
}

func ExampleTemplate_ParseDir() {
	dir, err := os.MkdirTemp("", "petra-parse-dir-example-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	write := func(name, body string) {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			panic(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			panic(err)
		}
	}

	write("layout.html", `<main>{{block "content" .}}{{end}}</main>`)
	write("products/index.html", `{{define "content"}}<h1>{{.Name}}</h1>{{end}}`)

	tmpl := petra.New()
	if err := tmpl.ParseDir(dir); err != nil {
		panic(err)
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "products/index", struct{ Name string }{
		Name: "Petra",
	}); err != nil {
		panic(err)
	}

	fmt.Println(out.String())
	// Output:
	// <main><h1>Petra</h1></main>
}

func ExampleTemplate_ParseFS() {
	files := fstest.MapFS{
		"templates/layout.html":         {Data: []byte(`<main>{{block "content" .}}{{end}}</main>`)},
		"templates/marketing/home.html": {Data: []byte(`{{define "content"}}{{.}}{{end}}`)},
	}

	tmpl := petra.New()
	if err := tmpl.ParseFS(files, "templates"); err != nil {
		panic(err)
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "marketing/home", "Hello from embedded templates"); err != nil {
		panic(err)
	}

	fmt.Println(out.String())
	// Output:
	// <main>Hello from embedded templates</main>
}

func ExampleTemplate_ExecuteTemplate() {
	files := fstest.MapFS{
		"layout.html":         {Data: []byte(`<main>{{block "content" .}}{{end}}</main>`)},
		"products/index.html": {Data: []byte(`{{define "content"}}<h1>{{.Name}}</h1>{{end}}`)},
	}

	tmpl := petra.New()
	if err := tmpl.ParseFS(files, "."); err != nil {
		panic(err)
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "products/index", struct{ Name string }{
		Name: "Petra",
	}); err != nil {
		panic(err)
	}

	fmt.Println(out.String())
	// Output:
	// <main><h1>Petra</h1></main>
}

func ExampleTemplate_Exec() {
	files := fstest.MapFS{
		"layout.html":                  {Data: []byte(`{{block "content" .}}{{end}}`)},
		"index.html":                   {Data: []byte(`{{define "content"}}{{end}}`)},
		"components/status_badge.html": {Data: []byte(`{{define "StatusBadge"}}<span class="badge">{{.}}</span>{{end}}`)},
	}

	tmpl := petra.NewWithOptions(petra.Options{
		IncludeDir: "components",
	})
	if err := tmpl.ParseFS(files, "."); err != nil {
		panic(err)
	}

	var out bytes.Buffer
	if err := tmpl.Exec(&out, `{{StatusBadge .}}`, "Ready"); err != nil {
		panic(err)
	}

	fmt.Println(out.String())
	// Output:
	// <span class="badge">Ready</span>
}

func ExampleTemplate_ReloadDir() {
	dir, err := os.MkdirTemp("", "petra-reload-example-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			panic(err)
		}
	}

	write("layout.html", `<main>{{block "content" .}}{{end}}</main>`)
	write("about.html", `{{define "content"}}About v1{{end}}`)

	tmpl := petra.New()
	if err := tmpl.ParseDir(dir); err != nil {
		panic(err)
	}

	write("about.html", `{{define "content"}}About v2{{end}}`)
	result, err := tmpl.ReloadDir("about.html")
	if err != nil {
		panic(err)
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "about", nil); err != nil {
		panic(err)
	}

	fmt.Println(result.RebuiltPages)
	fmt.Println(out.String())
	// Output:
	// [about]
	// <main>About v2</main>
}
