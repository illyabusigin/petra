package plugins

import (
	"fmt"
	"html/template"
	"io/fs"
	"path"
	"sync"

	"github.com/illyabusigin/petra/tmplfunc"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
)

func Markdown(files fs.FS, root string) *md {
	return &md{files, root}
}

type md struct {
	files fs.FS
	root  string
}

func (s md) Funcs() (template.FuncMap, error) {
	cache := map[string]template.HTML{}
	var cacheMu sync.RWMutex

	extensions := parser.CommonExtensions | parser.AutoHeadingIDs | parser.NoEmptyLineBeforeBlock
	htmlFlags := html.CommonFlags | html.HrefTargetBlank

	render := func(data []byte) template.HTML {
		p := parser.NewWithExtensions(extensions)
		renderer := html.NewRenderer(html.RendererOptions{Flags: htmlFlags})
		doc := p.Parse(data)
		return template.HTML(markdown.Render(doc, renderer))
	}

	f := func(name string) (template.HTML, error) {
		key := name
		cacheMu.RLock()
		if found, ok := cache[key]; ok {
			cacheMu.RUnlock()
			return found, nil
		}
		cacheMu.RUnlock()

		filename := name
		if path.Ext(filename) == "" {
			filename = fmt.Sprintf("%v.md", filename)
		}

		data, err := fs.ReadFile(s.files, path.Join(s.root, filename))
		if err != nil {
			return "", fmt.Errorf("load markdown %q: %w", name, err)
		}

		content := render(data)

		cacheMu.Lock()
		cache[key] = content
		cacheMu.Unlock()

		return content, nil
	}

	markdownToHTML := func(md string) template.HTML {
		return render([]byte(md))
	}

	return template.FuncMap{
		"_loadMarkdown":  f,
		"MarkdownToHTML": markdownToHTML,
	}, nil
}

func (s md) Apply(t *template.Template) error {
	tmplt := `{{define "Markdown path"}}
{{_loadMarkdown .path}}
{{end}}`
	if err := tmplfunc.Parse(t, tmplt); err != nil {
		return err
	}

	return nil
}
