package controllers

import (
	"bytes"
	"net/http"

	"github.com/illyabusigin/petra"
)

type Controller struct {
	*Env
	name string
}

func NewController(env *Env, name string) *Controller {
	return &Controller{
		Env:  env,
		name: name,
	}
}

func (c *Controller) MarketingContext(r *http.Request) PageData {
	return PageData{
		CurrentPath: r.URL.Path,
		Dev:         c.Dev,
		request:     r,
		Nav: []NavItem{
			{Label: "Home", Path: "/"},
			{Label: "About", Path: "/about"},
		},
	}
}

func (c *Controller) HTML(w http.ResponseWriter, templateName string, ctx any) {
	var body bytes.Buffer
	if err := c.Templates.ExecuteTemplate(&body, templateName, ctx); err != nil {
		c.templateError(w, requestFromContext(ctx), templateName, err)
		return
	}

	_, _ = w.Write(body.Bytes())
}

func (c *Controller) Exec(w http.ResponseWriter, tmplt string, ctx any) {
	var body bytes.Buffer
	if err := c.Templates.Exec(&body, tmplt, ctx); err != nil {
		c.templateError(w, requestFromContext(ctx), "inline", err)
		return
	}

	_, _ = w.Write(body.Bytes())
}

func (c *Controller) templateError(w http.ResponseWriter, r *http.Request, templateName string, err error) {
	if c.Log != nil {
		c.Log.Error("render failed", "controller", c.name, "template", templateName, "error", err)
	}
	if petra.RenderDebugError(w, r, err, petra.DebugOptions{
		Enabled:        c.Dev,
		IncludeGoStack: c.Dev,
		Title:          "Petra MVC template error",
	}) {
		return
	}
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

type requestContext interface {
	HTTPRequest() *http.Request
}

func requestFromContext(ctx any) *http.Request {
	requestCtx, ok := ctx.(requestContext)
	if !ok {
		return nil
	}
	return requestCtx.HTTPRequest()
}
