package cmd

import (
	"context"
	"embed"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/illyabusigin/petra"
	"github.com/illyabusigin/petra/examples/mvcweb/controllers"

	"github.com/go-chi/chi/v5"
)

//go:embed templates/* static/*
var webFS embed.FS

type Web struct {
	Addr    string
	Dev     bool
	Verbose bool
	RootDir string
}

type closeable interface {
	Close() error
}

func (w Web) Run(ctx context.Context) error {
	if w.Addr == "" {
		w.Addr = ":8080"
	}
	if w.RootDir == "" {
		w.RootDir = "."
	}

	level := slog.LevelInfo
	if w.Verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	tmpl := petra.NewWithOptions(petra.Options{
		IncludeDir: "components",
		Plugins:    petra.Plugins{petra.HTML()},
		Logger:     logger.With("component", "petra"),
	})

	r := chi.NewRouter()
	r.Use(requestLogger(logger))
	closers := []closeable{}

	if w.Dev {
		templatesDir := filepath.Join(w.RootDir, "cmd", "templates")
		staticDir := filepath.Join(w.RootDir, "cmd", "static")

		if err := tmpl.ParseDir(templatesDir); err != nil {
			return err
		}

		hotReload := petra.NewHotReloadControllerWithOptions(petra.HotReloadOptions{
			Template: tmpl,
			Folders:  []string{templatesDir},
			Logger:   logger.With("component", "petra_hot_reload"),
		})
		static := petra.NewStaticWithOptions(petra.StaticOptions{
			Socket:      hotReload.Socket(),
			Folder:      staticDir,
			StripPrefix: "/static/",
			Logger:      logger.With("component", "petra_static"),
		})
		closers = append(closers, static, hotReload)

		r.Mount("/_reload", hotReload.Handler())
		r.Handle("/static/*", static)
	} else {
		if err := tmpl.ParseFS(webFS, "templates"); err != nil {
			return err
		}

		r.Handle("/static/*", petra.StaticFS(webFS, "/static/"))
	}

	env := controllers.Env{
		Templates: tmpl,
		Log:       logger,
		Dev:       w.Dev,
	}

	r.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	r.Mount("/about", controllers.Template(&env, "marketing/about", "About"))
	r.Mount("/", controllers.Home(&env))

	server := http.Server{
		Addr:              w.Addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errs := make(chan error, 1)
	go func() {
		logger.Info("started", "addr", w.Addr, "dev", w.Dev, "verbose", w.Verbose)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
		close(errs)
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := server.Shutdown(shutdownCtx)
	for _, closer := range closers {
		err = errors.Join(err, closer.Close())
	}
	logger.Info("stopped")
	return err
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rec, r)

			logger.Info(
				"request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration", time.Since(start),
				"remote", r.RemoteAddr,
			)
		})
	}
}
