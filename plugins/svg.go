package plugins

import (
	"fmt"
	"html/template"
	"io/fs"
	"path"
	"regexp"
	"sync"

	"github.com/illyabusigin/petra/tmplfunc"
)

func SVG(files fs.FS, root string) *svg {
	return &svg{files, root}
}

type svg struct {
	files fs.FS
	root  string
}

func (s svg) Funcs() (template.FuncMap, error) {
	pattern := `class\s*=\s*["'][^"']*["']`
	re := regexp.MustCompile(pattern)
	cache := map[string]template.HTML{}
	var cacheMu sync.RWMutex

	f := func(name, class string) (template.HTML, error) {
		key := name + "-" + pattern + class
		cacheMu.RLock()
		if found, ok := cache[key]; ok {
			cacheMu.RUnlock()
			return found, nil
		}
		cacheMu.RUnlock()

		filename := name
		if path.Ext(filename) == "" {
			filename = fmt.Sprintf("%v.svg", filename)
		}

		data, err := fs.ReadFile(s.files, path.Join(s.root, filename))
		if err != nil {
			return "", fmt.Errorf("load svg %q: %w", name, err)
		}

		var content template.HTML
		if class == "" {
			content = template.HTML(data)
		} else {
			content = template.HTML(re.ReplaceAllString(string(data), fmt.Sprintf(`class="%v"`, class)))
		}

		cacheMu.Lock()
		cache[key] = content
		cacheMu.Unlock()

		return content, nil
	}

	return template.FuncMap{
		"_loadSVG": f,
	}, nil
}

func (s svg) Apply(t *template.Template) error {
	tmplt := `{{define "SVG path class?"}}
{{if .class}}
{{_loadSVG .path .class}}
{{else}}
{{_loadSVG .path ""}}
{{end}}
{{end}}`
	if err := tmplfunc.Parse(t, tmplt); err != nil {
		return err
	}

	return nil
}
