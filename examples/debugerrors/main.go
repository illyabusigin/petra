package main

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/illyabusigin/petra"
)

//go:embed templates/* templates/components/* static/*
var appFS embed.FS

type pageData struct {
	Title       string
	CurrentPath string
	Nav         []navItem
}

type navItem struct {
	Label string
	Path  string
}

type app struct {
	templates *petra.Template
	dev       bool
	log       *slog.Logger
}

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	dev := flag.Bool("dev", false, "render Petra debug pages for template failures")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	a, err := newApp(*dev, logger)
	if err != nil {
		logger.Error("build app", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	server := &http.Server{
		Addr:              *addr,
		Handler:           a.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("started", "addr", *addr, "dev", *dev)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listen", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown", "error", err)
	}
}

func newApp(dev bool, logger *slog.Logger) (app, error) {
	tmpl := petra.NewWithOptions(petra.Options{
		IncludeDir: "components",
		FuncMap: template.FuncMap{
			"explode": explode,
		},
	})
	if err := tmpl.ParseFS(appFS, "templates"); err != nil {
		return app{}, err
	}
	return app{
		templates: tmpl,
		dev:       dev,
		log:       logger,
	}, nil
}

func (a app) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/static/", petra.StaticFS(appFS, "/static/"))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		a.render(w, r, "index", "Petra debug errors")
	})
	mux.HandleFunc("/broken-page", func(w http.ResponseWriter, r *http.Request) {
		a.render(w, r, "broken-page", "Broken page render")
	})
	mux.HandleFunc("/broken-component", func(w http.ResponseWriter, r *http.Request) {
		a.render(w, r, "broken-component", "Broken component render")
	})
	return mux
}

func (a app) render(w http.ResponseWriter, r *http.Request, templateName, title string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if templateName == "index" && r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	var body bytes.Buffer
	err := a.templates.ExecuteTemplate(&body, templateName, a.context(r, title))
	if err != nil {
		if a.log != nil {
			a.log.Error("render failed", "template", templateName, "path", r.URL.Path, "error", err)
		}
		if petra.RenderDebugError(w, r, err, petra.DebugOptions{
			Enabled:        a.dev,
			IncludeGoStack: a.dev,
			Title:          "Petra debug error example",
		}) {
			return
		}
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	_, _ = w.Write(body.Bytes())
}

func (a app) context(r *http.Request, title string) pageData {
	return pageData{
		Title:       title,
		CurrentPath: r.URL.Path,
		Nav: []navItem{
			{Label: "Working page", Path: "/"},
			{Label: "Broken page", Path: "/broken-page"},
			{Label: "Broken component", Path: "/broken-component"},
		},
	}
}

func explode(scope string) (string, error) {
	return "", fmt.Errorf("%s failed: intentional Petra debug example", scope)
}
