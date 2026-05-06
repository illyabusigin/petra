package main

import (
	"bytes"
	"context"
	"embed"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/illyabusigin/petra"
)

//go:embed templates/* static/*
var appFS embed.FS

type pageData struct {
	Title string
	Form  contactForm
}

type contactForm struct {
	Name      string
	Email     string
	Message   string
	Errors    map[string]string
	Submitted bool
}

type app struct {
	templates *petra.Template
}

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	flag.Parse()

	tmpl := petra.New()
	if err := tmpl.ParseFS(appFS, "templates"); err != nil {
		slog.Error("parse templates", "error", err)
		os.Exit(1)
	}

	a := app{templates: tmpl}
	mux := http.NewServeMux()
	mux.Handle("/static/", petra.StaticFS(appFS, "/static/"))
	mux.HandleFunc("/", a.contact)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	server := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("started", "addr", *addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("listen", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}

func (a app) contact(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		a.render(w, r, contactForm{}, http.StatusOK)
	case http.MethodPost:
		form, err := formFromRequest(r)
		if err != nil {
			form.Errors = map[string]string{"message": "The form could not be read."}
			a.render(w, r, form, http.StatusBadRequest)
			return
		}
		form.Errors = validate(form)
		if len(form.Errors) > 0 {
			a.render(w, r, form, http.StatusUnprocessableEntity)
			return
		}

		// A real application would send mail or write to storage here. This
		// example stops at validation so the render cycle remains obvious.
		form.Submitted = true
		form.Message = ""
		a.render(w, r, form, http.StatusOK)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (a app) render(w http.ResponseWriter, r *http.Request, form contactForm, status int) {
	var body bytes.Buffer
	err := a.templates.ExecuteTemplate(&body, "index", pageData{
		Title: "Petra forms example",
		Form:  form,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
}

func formFromRequest(r *http.Request) (contactForm, error) {
	if err := r.ParseForm(); err != nil {
		return contactForm{}, err
	}

	return contactForm{
		Name:    strings.TrimSpace(r.Form.Get("name")),
		Email:   strings.TrimSpace(r.Form.Get("email")),
		Message: strings.TrimSpace(r.Form.Get("message")),
	}, nil
}

func validate(form contactForm) map[string]string {
	errors := map[string]string{}
	if form.Name == "" {
		errors["name"] = "Name is required."
	}
	if form.Email == "" {
		errors["email"] = "Email is required."
	} else if !strings.Contains(form.Email, "@") {
		errors["email"] = "Email must contain an @ sign."
	}
	if len(form.Message) < 12 {
		errors["message"] = "Message must be at least 12 characters."
	}
	return errors
}
