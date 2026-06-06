package petra

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestComponentSetRejectsImportCycle(t *testing.T) {
	files := fstest.MapFS{
		"templates/layout.html": {Data: []byte(`{{block "content" .}}{{end}}`)},
		"templates/index.html":  {Data: []byte(`{{define "content"}}{{A.Component}}{{end}}`)},
		"a/component.html":      {Data: []byte(`{{define "Component"}}a{{end}}`)},
		"b/component.html":      {Data: []byte(`{{define "Component"}}b{{end}}`)},
	}

	a := &ComponentSet{id: "example.com/petra/a", files: files, root: "a"}
	b := &ComponentSet{id: "example.com/petra/b", files: files, root: "b"}
	a.imports = []componentImport{{alias: "B", set: b}}
	b.imports = []componentImport{{alias: "A", set: a}}

	tmpl := NewWithOptions(Options{
		Plugins: Plugins{
			Components("A", a),
		},
	})

	err := tmpl.ParseFS(files, "templates")
	if err == nil {
		t.Fatal("ParseFS() succeeded for component set import cycle")
	}
	if !strings.Contains(err.Error(), `component set import cycle includes "example.com/petra/a"`) {
		t.Fatalf("ParseFS() error = %v", err)
	}
}
