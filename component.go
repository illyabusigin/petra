package petra

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"

	"github.com/aymerick/raymond"
	"github.com/jfyne/live"
	"github.com/jfyne/live/page"
	"github.com/rs/xid"
)

type Component struct {
	id   string
	name string

	session live.SessionStore
	app     *Application

	template  *raymond.Template
	handler   *eventHandler
	reference ComponentLifecycle
}

type ComponentInitializer func(args map[string]interface{}) *Component

type ComponentLifecycle interface {
	Register(component *page.Component) error
	Mount(ctx context.Context, c *page.Component, r *http.Request) error
	Render(w io.Writer, c *page.Component) error
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

func (r *Component) Build(route *Route, c *page.Component) (*page.Component, error) {
	r.id = xid.New().String() //route.app.nextID()
	r.app = route.app
	r.handler.id = r.id

	fmt.Println("Build", r.id)

	return page.NewComponent(r.id, c.Handler, c.Socket,
		page.WithRegister(r.register(c)),
		page.WithMount(r.mount(c)),
		page.WithRender(r.Render),
	)
}

func (r *Component) register(cmp *page.Component) page.RegisterHandler {
	fmt.Printf("Component register %#v\n", r.reference)

	return func(c *page.Component) error {

		if err := r.reference.Register(c); err != nil {
			return err
		}

		return nil
	}
}

func (r *Component) mount(c *page.Component) page.MountHandler {
	fmt.Printf("Component mount %#v\n", r.reference)
	return func(ctx context.Context, cmp *page.Component, req *http.Request) error {
		if err := r.handler.registerHandlers(r.reference, cmp); err != nil {
			return fmt.Errorf("Failed to register event handlers: %w", err)
		}
		data, err := r.app.router.loadComponentData(r.name)
		if err != nil {
			fmt.Printf("Error loading template for <%v>, %v,\n", r.name, err)

			return fmt.Errorf("Failed to locate component template: %w", err)

		}

		fmt.Println("Processing template", r.id)
		data, err = r.handler.processTemplate(data)
		if err != nil {
			fmt.Println("Error processing template", err)
			return fmt.Errorf("Failed to process component template: %w", err)
		}

		tmplt, err := raymond.Parse(string(data))
		if err != nil {
			fmt.Println("Error parsing template", err)

			return fmt.Errorf("Failed to parse component template: %w", err)
		}

		r.template = tmplt
		if err := r.reference.Mount(ctx, cmp, req); err != nil {
			return err
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

// func NewComponent(name string, route *Route, self interface{}) *CompoÎçnent {
func NewComponent(l ComponentLifecycle) *Component {
	getType := func(myvar interface{}) string {
		if t := reflect.TypeOf(myvar); t.Kind() == reflect.Ptr {
			return t.Elem().Name()
		} else {
			return t.Name()
		}
	}

	r := &Component{
		name:      getType(l),
		reference: l,
	}

	r.handler = newEventHandler(r.id)

	return r
}
