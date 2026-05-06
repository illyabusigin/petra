package controllers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type homeController struct {
	*Controller
}

func Home(env *Env) chi.Router {
	return homeController{
		Controller: NewController(env, "home"),
	}.Routes()
}

func (c homeController) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", c.render)
	return r
}

func (c homeController) render(w http.ResponseWriter, r *http.Request) {
	ctx := c.MarketingContext(r)
	ctx.SetTitle("Petra MVC example")

	c.HTML(w, "marketing/home", ctx)
}
