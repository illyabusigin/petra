package petra

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/aymerick/raymond"
	"github.com/jfyne/live"
	"github.com/jfyne/live/page"
)

type Component struct {
	id   string
	name string

	session live.SessionStore
	app     *Application
	self    interface{}

	onRegister page.RegisterHandler
	Mount      page.MountHandler

	template *raymond.Template
	handler  *eventHandler
}

func (r *Component) Session(s live.SessionStore) {
	r.session = s
}

func (r *Component) getSession() (live.SessionStore, error) {
	if s := r.session; s != nil {
		return s, nil
	}

	if s := r.app.session; s != nil {
		return s, nil
	}

	return nil, fmt.Errorf("no session found")
}

func (r *Component) Validate() error {
	return nil
}

func (r *Component) Build(c *page.Component) (*page.Component, error) {
	return page.NewComponent(r.id, c.Handler, c.Socket,
		page.WithRegister(r.register(c)),
		page.WithMount(r.mount(c)),
		page.WithRender(r.Render),
	)
}

func (r *Component) Register(f func(c *page.Component) error) {
	r.onRegister = f
}

// func (r *Component) register(c *page.Component) page.RegisterHandler {
// 	fmt.Printf("Component register %#v\n", r.self)
// 	return r.onRegister
// }

func (r *Component) register(cmp *page.Component) page.RegisterHandler {
	fmt.Printf("Component register %#v\n", r.self)

	return func(c *page.Component) error {

		if r.onRegister != nil {
			if err := r.onRegister(c); err != nil {
				return err
			}
		}

		return nil
	}
}

func (r *Component) mount(c *page.Component) page.MountHandler {
	fmt.Printf("Component mount %#v\n", r.self)
	return func(ctx context.Context, cmp *page.Component, req *http.Request) error {
		if err := r.handler.registerHandlers(r.self, cmp); err != nil {
			return fmt.Errorf("Failed to register event handlers: %w", err)
		}
		data, err := r.app.router.loadComponentData(r.name)
		if err != nil {
			fmt.Println("Eror loading template", err)

			return fmt.Errorf("Failed to locate component template: %w", err)

		}

		data, err = r.handler.processTemplate(data)
		if err != nil {
			fmt.Println("Eror processing template", err)
			return fmt.Errorf("Failed to process component template: %w", err)
		}

		tmplt, err := raymond.Parse(string(data))
		if err != nil {
			fmt.Println("Eror parsing template", err)

			return fmt.Errorf("Failed to parse component template: %w", err)
		}

		r.template = tmplt

		if r.Mount != nil {
			if err := r.Mount(ctx, cmp, req); err != nil {
				return err
			}
		}
		return nil
	}
}

func (r *Component) Render(w io.Writer, cmp *page.Component) error {
	// fmt.Printf("render Component %#v\n", r.self) //not called

	result, err := r.template.Exec(r.args(cmp))
	if err != nil {
		return err
	}

	_, err = w.Write([]byte(result))
	return err
}

func (r *Component) args(c *page.Component) map[string]interface{} {
	args := map[string]interface{}{}

	if c.State != nil {
		args["state"] = c.State
	}
	return args
}

func NewComponent(name string, route *Route, self interface{}) *Component {
	r := &Component{
		name: name,
		id:   route.app.nextID(),
		app:  route.app,
		self: self,
	}
	r.handler = newEventHandler(r.id)

	return r
}
