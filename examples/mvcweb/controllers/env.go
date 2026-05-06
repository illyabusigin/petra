package controllers

import (
	"log/slog"
	"net/http"

	"github.com/illyabusigin/petra"
)

type Env struct {
	Templates *petra.Template
	Log       *slog.Logger
	Dev       bool
}

type PageData struct {
	Title       string
	CurrentPath string
	Dev         bool
	Nav         []NavItem

	request *http.Request
}

type NavItem struct {
	Label string
	Path  string
}

func (p *PageData) SetTitle(title string) {
	p.Title = title
}

func (p PageData) HTTPRequest() *http.Request {
	return p.request
}
