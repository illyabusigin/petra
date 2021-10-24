package petra

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/aymerick/raymond"
	"github.com/jfyne/live"
	"github.com/jfyne/live/page"
)

// Route
// - Should handle it's own rendering

type Route struct {
	id   string
	name string
	self interface{}

	session live.SessionStore
	app     *Application

	Register page.RegisterHandler
	Mount    page.MountHandler

	template *raymond.Template
	handler  *eventHandler
}

func (r *Route) Session(s live.SessionStore) {
	r.session = s
}

func (r *Route) getSession() (live.SessionStore, error) {
	if s := r.session; s != nil {
		return s, nil
	}

	if s := r.app.session; s != nil {
		return s, nil
	}

	return nil, fmt.Errorf("no session found")
}

func (r *Route) Validate() error {
	return nil
}

func (r *Route) build() live.HandlerConfig {
	return page.WithComponentMount(func(ctx context.Context, h *live.Handler, req *http.Request, s *live.Socket) (*page.Component, error) {
		return page.NewComponent(r.name, h, s,
			page.WithRegister(r.Register),
			page.WithMount(r.mount()),
			page.WithRender(r.Render),
		)
	})
}

func (r *Route) Handler() (*live.Handler, error) {
	s, err := r.getSession()
	if err != nil {
		return nil, err
	}

	return live.NewHandler(s, r.build(), page.WithComponentRenderer())
}

func (r *Route) mount() page.MountHandler {
	return func(ctx context.Context, cmp *page.Component, req *http.Request) error {
		fmt.Println("Mount route")
		if err := r.handler.registerHandlers(r.self, cmp); err != nil {
			return fmt.Errorf("Failed to register event handlers: %w", err)
		}
		data, err := r.app.router.loadRouteData(r.name)
		if err != nil {
			fmt.Println("Eror loading template", err)

			return fmt.Errorf("Failed to locate route template: %w", err)

		}

		data, err = r.handler.processTemplate(data)
		if err != nil {
			fmt.Println("Eror processing template", err)
			return fmt.Errorf("Failed to process route template: %w", err)
		}

		tmplt, err := raymond.Parse(string(data))
		if err != nil {
			fmt.Println("Eror parsing template", err)

			return fmt.Errorf("Failed to parse route template: %w", err)
		}

		tmplt.RegisterPartials(r.app.router.componentData)
		tmplt.RegisterHelper("yield", func(i interface{}) string {
			c, ok := i.(page.Component)
			if !ok {
				return "No component found"
			}

			buf := bytes.Buffer{}
			if err := c.Render(&buf, &c); err != nil {
				return fmt.Sprintf("Error rendering component: %v")
			}

			return buf.String()
		})

		r.template = tmplt

		if r.Mount != nil {
			if err := r.Mount(ctx, cmp, req); err != nil {
				return err
			}
		}
		return nil
	}
}

func (r *Route) Render(w io.Writer, cmp *page.Component) error {

	result, err := r.template.Exec(r.args(cmp))
	if err != nil {
		return err
	}

	_, err = w.Write([]byte(result))
	return err
}

func (r *Route) args(c *page.Component) map[string]interface{} {
	args := map[string]interface{}{}

	if c.State != nil {
		args["state"] = c.State
	}
	return args
}

func NewRoute(name string, app *Application, self interface{}) *Route {
	r := &Route{
		name: name,
		id:   app.nextID(),
		app:  app,
		self: self,
	}
	r.handler = newEventHandler(r.name)

	return r
}
