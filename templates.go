package petra

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/aymerick/raymond"
)

var (
	ErrTemplateNotFound = errors.New("Not found")
)

// Router does the following
// - Loads and parses templates
type Router struct {
	app *Application

	routes    map[string]*raymond.Template
	routeData map[string]string

	components    map[string]*raymond.Template
	componentData map[string]string

	index string
}

func newRouter(app *Application) *Router {
	return &Router{
		app:       app,
		routes:    map[string]*raymond.Template{},
		routeData: map[string]string{},

		components:    map[string]*raymond.Template{},
		componentData: map[string]string{},
	}
}

func (r *Router) load() error {
	if err := r.parseIndex(); err != nil {
		return err
	}

	if err := r.parseTemplates(*r.app.root); err != nil {
		return err
	}

	return nil
}

func (r *Router) parseIndex() error {
	if r.app.root == nil {
		return fmt.Errorf("no embed.FS specified")
	}

	index, err := r.app.root.ReadFile("index.html")
	if err != nil {
		return fmt.Errorf("no index.html found: %w", err)
	}

	r.index = string(index)

	return nil
}

func (r *Router) parseTemplates(f embed.FS) error {
	err := fs.WalkDir(f, ".", func(path string, d fs.DirEntry, err error) error {
		isComponent := strings.HasSuffix(path, ".hbs") && strings.HasPrefix(path, "components/")
		isRoute := strings.HasSuffix(path, ".hbs") && strings.HasPrefix(path, "routes/")
		if isComponent {
			if err := r.processComponentTemplate(f, path); err != nil {
				fmt.Println("error procesing", err)
				return err
			}
		}

		if isRoute {
			if err := r.processRouteTemplate(f, path); err != nil {
				fmt.Println("error procesing", err)
				return err
			}
		}

		return nil
	})

	if err != nil {
		return err
	}

	r.registerRouteComponents()

	return nil
}

func (r *Router) registerRouteComponents() {
	for _, tmplt := range r.routes {
		tmplt.RegisterPartials(r.componentData)
	}
}

func (r *Router) processComponentTemplate(fs embed.FS, path string) error {
	data, err := fs.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read component template: %w", err)
	}

	tmplt, err := raymond.Parse(string(data))
	if err != nil {
		return fmt.Errorf("failed to parse component template: %w", err)
	}

	name := filepath.Base(path)
	name = strings.TrimSuffix(name, ".hbs")
	r.components[name] = tmplt
	r.componentData[name] = string(data)

	return nil
}

func (r *Router) processRouteTemplate(fs embed.FS, path string) error {
	fmt.Println("process template", path)
	data, err := fs.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read route template: %w", err)
	}

	template := string(data)

	// Use index.html as content container for all routes
	template = strings.Replace(r.index, `{{content-for "body"}}`, template, 1)

	if r.app.hotReload {
		template = strings.Replace(template, `{{content-for "hot-reload-tag"}}`, `live-hook="petra"`, 1)
		template = strings.Replace(template, `{{content-for "hot-reload"}}`, `<script type="text/javascript" src="/hot_reload.js"></script>`, 1)
	} else {
		template = strings.Replace(template, `{{content-for "hot-reload-tag"}}`, ``, 1)
	}

	tmplt, err := raymond.Parse(template)
	if err != nil {
		return err
	}

	name := filepath.Base(path)
	name = strings.TrimSuffix(name, ".hbs")
	r.routes[name] = tmplt
	r.routeData[name] = string(template)

	return nil
}

func (r *Router) loadRouteData(route string) (string, error) {
	t, ok := r.routeData[route]
	if !ok {
		return "", ErrTemplateNotFound
	}

	return t, nil
}

func (r *Router) loadComponentData(component string) (string, error) {
	t, ok := r.componentData[component]
	if !ok {
		return "", ErrTemplateNotFound
	}

	return t, nil
}
