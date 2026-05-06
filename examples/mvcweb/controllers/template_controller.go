package controllers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type templateController struct {
	*Controller
	templateName string
	title        string
}

func Template(env *Env, templateName, title string) chi.Router {
	return templateController{
		Controller:   NewController(env, templateName),
		templateName: templateName,
		title:        title,
	}.Routes()
}

func (c templateController) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", c.render)
	return r
}

func (c templateController) render(w http.ResponseWriter, r *http.Request) {
	ctx := c.MarketingContext(r)
	ctx.SetTitle(c.title)

	c.HTML(w, c.templateName, ctx)
}
