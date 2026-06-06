package main

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/illyabusigin/petra"
	"github.com/illyabusigin/petra/examples/component-set/ui"
)

//go:embed templates/* static/*
var appFS embed.FS

type appOptions struct {
	Dev     bool
	RootDir string
	Logger  *slog.Logger
}

type app struct {
	templates      *petra.Template
	static         http.Handler
	hotReload      *petra.HotReloadController
	staticFiles    *petra.StaticFileServer
	dev            bool
	watchedFolders []string
	logger         *slog.Logger
}

type metric struct {
	Label  string
	Value  string
	Detail string
}

type update struct {
	Title string
	Body  string
	Tone  string
}

type pageData struct {
	Title   string
	Dev     bool
	Metrics []metric
	Updates []update
}

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	dev := flag.Bool("dev", false, "read templates/components/static files from disk and enable hot reload")
	root := flag.String("root", ".", "example root used with -dev")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	a, err := newApp(appOptions{
		Dev:     *dev,
		RootDir: *root,
		Logger:  logger,
	})
	if err != nil {
		logger.Error("build app", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := a.Close(); err != nil {
			logger.Error("close app", "error", err)
		}
	}()

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

func newApp(opts appOptions) (app, error) {
	if opts.RootDir == "" {
		opts.RootDir = "."
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}

	tmpl := petra.NewWithOptions(petra.Options{
		PageExtensions: []string{".html"},
		Plugins:        componentPlugins(opts),
		Logger:         opts.Logger.With("component", "petra"),
	})

	a := app{
		templates: tmpl,
		dev:       opts.Dev,
		logger:    opts.Logger,
	}

	if opts.Dev {
		templatesDir := filepath.Join(opts.RootDir, "templates")
		staticDir := filepath.Join(opts.RootDir, "static")
		uiComponentsDir := filepath.Join(opts.RootDir, "ui", "components")

		if err := tmpl.ParseDir(templatesDir); err != nil {
			return app{}, err
		}

		a.watchedFolders = []string{templatesDir, uiComponentsDir}
		a.hotReload = petra.NewHotReloadControllerWithOptions(petra.HotReloadOptions{
			Template: tmpl,
			Folders:  a.watchedFolders,
			Logger:   opts.Logger.With("component", "petra_hot_reload"),
		})
		a.staticFiles = petra.NewStaticWithOptions(petra.StaticOptions{
			Socket:       a.hotReload.Socket(),
			Folder:       staticDir,
			StripPrefix:  "/static/",
			ReloadPolicy: petra.StaticReloadAssets,
			Logger:       opts.Logger.With("component", "petra_static"),
		})
		a.static = a.staticFiles
		return a, nil
	}

	if err := tmpl.ParseFS(appFS, "templates"); err != nil {
		return app{}, err
	}
	a.static = petra.StaticFS(appFS, "/static/")
	return a, nil
}

func componentPlugins(opts appOptions) petra.Plugins {
	if opts.Dev {
		return petra.Plugins{
			ui.DevComponents("UI", filepath.Join(opts.RootDir, "ui")),
		}
	}
	return petra.Plugins{
		ui.Components("UI"),
	}
}

func (a app) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/static/", a.static)
	if a.hotReload != nil {
		mux.Handle("/_reload/", a.hotReload.Handler())
	}
	mux.HandleFunc("GET /{$}", a.index)
	mux.HandleFunc("GET /summary", a.summary)
	return mux
}

func (a app) Close() error {
	var err error
	if a.staticFiles != nil {
		err = errors.Join(err, a.staticFiles.Close())
	}
	if a.hotReload != nil {
		err = errors.Join(err, a.hotReload.Close())
	}
	return err
}

func (a app) index(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, "index", http.StatusOK, a.pageData())
}

func (a app) summary(w http.ResponseWriter, r *http.Request) {
	var body bytes.Buffer
	err := a.templates.Exec(&body, `{{UI.Stat "Open tickets" "12" "rendered through Exec"}}`, nil)
	if err != nil {
		a.renderError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(body.Bytes())
}

func (a app) render(w http.ResponseWriter, r *http.Request, name string, status int, data pageData) {
	var body bytes.Buffer
	if err := a.templates.ExecuteTemplate(&body, name, data); err != nil {
		a.renderError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
}

func (a app) renderError(w http.ResponseWriter, r *http.Request, err error) {
	if a.logger != nil {
		a.logger.Error("render failed", "path", r.URL.Path, "error", err)
	}
	if petra.RenderDebugError(w, r, err, petra.DebugOptions{
		Enabled:        a.dev,
		IncludeGoStack: a.dev,
		Title:          "Component set example error",
	}) {
		return
	}
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

func (a app) pageData() pageData {
	return pageData{
		Title: "Petra component set example",
		Dev:   a.dev,
		Metrics: []metric{
			{Label: "Published components", Value: "4", Detail: "Stat, Link, Badge, Notice"},
			{Label: "Private helpers", Value: "1", Detail: "statDetail stays internal"},
			{Label: "Watched roots", Value: "2", Detail: "templates + ui/components"},
		},
		Updates: []update{
			{
				Title: "Component set mount",
				Body:  `App templates call namespace-qualified components such as UI.Stat while source files stay namespace-free.`,
				Tone:  "info",
			},
			{
				Title: "Hot reload path",
				Body:  `In dev mode Petra watches templates and the live component-set folder. Component edits trigger a full template reparse.`,
				Tone:  "warning",
			},
		},
	}
}
