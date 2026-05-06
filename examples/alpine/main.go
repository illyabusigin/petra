package main

import (
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
)

//go:embed templates/* static/*
var appFS embed.FS

type pageData struct {
	Title string
	Dev   bool
}

type appConfig struct {
	Dev     bool
	RootDir string
	Logger  *slog.Logger
}

type closeable interface {
	Close() error
}

type app struct {
	Handler http.Handler
	closers []closeable
}

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	dev := flag.Bool("dev", false, "load templates and static assets from disk with Petra hot reload")
	root := flag.String("root", ".", "example app root directory")
	verbose := flag.Bool("verbose", false, "enable Petra debug logs for reload and static asset events")
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	app, err := newApp(appConfig{
		Dev:     *dev,
		RootDir: *root,
		Logger:  logger,
	})
	if err != nil {
		logger.Error("build app", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	server := &http.Server{
		Addr:              *addr,
		Handler:           app.Handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("started", "addr", *addr, "dev", *dev, "verbose", *verbose)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listen", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = server.Shutdown(shutdownCtx)
	err = errors.Join(err, app.Close())
	if err != nil {
		logger.Error("shutdown", "error", err)
	}
}

func newApp(cfg appConfig) (*app, error) {
	if cfg.RootDir == "" {
		cfg.RootDir = "."
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	tmpl := petra.New()

	mux := http.NewServeMux()
	closers := []closeable{}

	if cfg.Dev {
		templatesDir := filepath.Join(cfg.RootDir, "templates")
		staticDir := filepath.Join(cfg.RootDir, "static")

		if err := tmpl.ParseDir(templatesDir); err != nil {
			return nil, err
		}

		hotReload := petra.NewHotReloadControllerWithOptions(petra.HotReloadOptions{
			Template: tmpl,
			Folders:  []string{templatesDir},
			Logger:   cfg.Logger.With("component", "petra_hot_reload"),
		})
		static := petra.NewStaticWithOptions(petra.StaticOptions{
			Socket:       hotReload.Socket(),
			Folder:       staticDir,
			StripPrefix:  "/static/",
			Logger:       cfg.Logger.With("component", "petra_static"),
			ReloadPolicy: petra.StaticReloadDefault,
		})
		closers = append(closers, static, hotReload)

		// Alpine is bundled to static/app.js. Petra's default static policy
		// reloads the page for JavaScript changes so Alpine state starts clean.
		mux.Handle("/_reload/", http.StripPrefix("/_reload", hotReload.Handler()))
		mux.Handle("/static/", static)
	} else {
		if err := tmpl.ParseFS(appFS, "templates"); err != nil {
			return nil, err
		}

		mux.Handle("/static/", petra.StaticFS(appFS, "/static/"))
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Keep the server side boring so the Alpine integration is easy to see.
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if err := tmpl.ExecuteTemplate(w, "index", pageData{Title: "Petra Alpine example", Dev: cfg.Dev}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	return &app{Handler: mux, closers: closers}, nil
}

func (a *app) Close() error {
	var err error
	for _, closer := range a.closers {
		err = errors.Join(err, closer.Close())
	}
	return err
}
