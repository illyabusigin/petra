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
	"github.com/mitchellh/hashstructure/v2"
	"github.com/rs/xid"
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

	components map[uint64]*page.Component
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
	fmt.Println("Build route", r)
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
	return func(ctx context.Context, route *page.Component, req *http.Request) error {
		// route is a component and also has state
		fmt.Println("Mount route", len(r.components), route.State, ctx)
		if err := r.handler.registerHandlers(r.self, route); err != nil {
			return fmt.Errorf("Failed to register event handlers: %w", err)
		}
		data, err := r.app.router.loadRouteData(r.name)
		if err != nil {
			fmt.Println("Error loading template", err)

			return fmt.Errorf("Failed to locate route template: %w", err)

		}

		data, err = r.handler.processTemplate(data)
		if err != nil {
			fmt.Println("Error processing template", err)
			return fmt.Errorf("Failed to process route template: %w", err)
		}

		tmplt, err := raymond.Parse(string(data))
		if err != nil {
			fmt.Println("Error parsing template", err)

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
				return fmt.Sprintf("Error rendering component: %v", err)
			}

			return buf.String()
		})

		tmplt.RegisterHelper("avec", func(context interface{}, options *raymond.Options) string {
			fmt.Println("Conteasdf")
			return options.FnWith(context)
		})

		tmplt.RegisterHelper("render", func(identifier string, options *raymond.Options) raymond.SafeString {
			intializer, ok := r.app.components[identifier]
			if !ok {
				fmt.Println("No component found for", identifier, r.app.components)
				return ""
			}

			toHash := fmt.Sprintf("%#v %v", intializer, options.Hash())
			hashID, err := hashstructure.Hash(toHash, hashstructure.FormatV2, &hashstructure.HashOptions{UseStringer: true})
			if err != nil {
				fmt.Println("ahash error:", err)
			}

			component, ok := r.components[hashID]

			if !ok {
				fmt.Printf("rendering new %v component: %v, hash: %v\n", identifier, toHash, hashID)
				// Use the page.Init function to create a new component, register it and mount it.
				component, err = page.Init(context.Background(), func() (*page.Component, error) {
					// Each clock requires its own unique stable ID. Events for each clock can then find
					// their own component.
					child := intializer(options.Hash())
					return child.Build(r, route)
				})

				if err != nil {
					return ""
				}
			} else {
				fmt.Println("Found cached component, yipee")
			}

			r.components[hashID] = component

			buf := bytes.Buffer{}
			if err := component.Render(&buf, component); err != nil {
				fmt.Printf("Error rendering component: %v\n", err)
				return ""
			}

			fmt.Println("rendeering compoennt", hashID)
			return raymond.SafeString(buf.String())
		})

		r.template = tmplt

		if r.Mount != nil {
			if err := r.Mount(ctx, route, req); err != nil {
				return err
			}
		}
		return nil
	}
}

func (r *Route) Render(w io.Writer, cmp *page.Component) error {
	// render pass starts, generate unique ID
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
		name:       name,
		id:         xid.New().String(),
		app:        app,
		self:       self,
		components: map[uint64]*page.Component{},
	}
	r.handler = newEventHandler(r.name)

	return r
}
