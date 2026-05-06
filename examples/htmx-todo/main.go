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
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	htmx "github.com/donseba/go-htmx"
	"github.com/illyabusigin/petra"
)

//go:embed templates/* templates/components/* static/*
var appFS embed.FS

type Todo struct {
	ID    int
	Title string
	Done  bool
}

type pageData struct {
	Title     string
	Todos     []Todo
	OpenCount int
	Total     int
	FormError string
}

type todoStore struct {
	mu     sync.Mutex
	nextID int
	todos  []Todo
}

type app struct {
	templates *petra.Template
	htmx      *htmx.HTMX
	store     *todoStore
}

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	flag.Parse()

	a, err := newApp()
	if err != nil {
		slog.Error("build app", "error", err)
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
		slog.Info("started", "addr", *addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("listen", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown", "error", err)
	}
}

func newApp() (app, error) {
	tmpl := petra.NewWithOptions(petra.Options{
		IncludeDir: "components",
	})
	if err := tmpl.ParseFS(appFS, "templates"); err != nil {
		return app{}, err
	}

	return app{
		templates: tmpl,
		htmx:      htmx.New(),
		store:     newTodoStore(),
	}, nil
}

func (a app) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/static/", petra.StaticFS(appFS, "/static/"))
	mux.HandleFunc("GET /{$}", a.index)
	mux.HandleFunc("POST /todos", a.create)
	mux.HandleFunc("PATCH /todos/{id}/toggle", a.toggle)
	mux.HandleFunc("DELETE /todos/{id}", a.delete)
	return mux
}

func (a app) index(w http.ResponseWriter, r *http.Request) {
	a.renderPage(w, r, http.StatusOK, "")
}

func (a app) create(w http.ResponseWriter, r *http.Request) {
	h := a.htmx.NewHandler(w, r)
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		if !h.RenderPartial() {
			a.renderPage(w, r, http.StatusUnprocessableEntity, "Todo title is required.")
			return
		}

		h.ReTarget("#todo-form-errors")
		h.ReSwap("innerHTML")
		h.TriggerError("Todo title is required.")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = h.WriteString(`<span class="error">Todo title is required.</span>`)
		return
	}

	todo := a.store.add(title)
	a.triggerTodoChanged(h, "todo:created")
	if !h.RenderPartial() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	h.ReTarget("#todos")
	h.ReSwap("outerHTML")
	h.TriggerSuccess("Todo created.", map[string]any{"id": todo.ID})
	a.renderTodos(w, http.StatusCreated)
}

func (a app) toggle(w http.ResponseWriter, r *http.Request) {
	h := a.htmx.NewHandler(w, r)
	id, err := todoID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !a.store.toggle(id) {
		http.NotFound(w, r)
		return
	}

	a.triggerTodoChanged(h, "todo:toggled")
	if !h.RenderPartial() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	h.ReTarget("#todos")
	h.ReSwap("outerHTML")
	a.renderTodos(w, http.StatusOK)
}

func (a app) delete(w http.ResponseWriter, r *http.Request) {
	h := a.htmx.NewHandler(w, r)
	id, err := todoID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !a.store.delete(id) {
		http.NotFound(w, r)
		return
	}

	a.triggerTodoChanged(h, "todo:deleted")
	if !h.RenderPartial() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	h.ReTarget("#todos")
	h.ReSwap("outerHTML")
	a.renderTodos(w, http.StatusOK)
}

func (a app) renderPage(w http.ResponseWriter, r *http.Request, status int, formError string) {
	var body bytes.Buffer
	if err := a.templates.ExecuteTemplate(&body, "index", a.pageData(formError)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
}

func (a app) renderTodos(w http.ResponseWriter, status int) {
	var body bytes.Buffer
	// Components are rendered as Petra inline templates for HTMX partials.
	// The full page uses the same component through {{ Todos . }}.
	if err := a.templates.Exec(&body, "{{ Todos . }}", a.pageData("")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
}

func (a app) pageData(formError string) pageData {
	todos := a.store.list()
	open := 0
	for _, todo := range todos {
		if !todo.Done {
			open++
		}
	}
	return pageData{
		Title:     "Petra HTMX todo",
		Todos:     todos,
		OpenCount: open,
		Total:     len(todos),
		FormError: formError,
	}
}

func (a app) triggerTodoChanged(h *htmx.Handler, event string) {
	data := a.pageData("")
	trigger := htmx.NewTrigger().AddEventObject(event, map[string]any{
		"open":  data.OpenCount,
		"total": data.Total,
	})
	h.TriggerAfterSwapWithObject(trigger)
}

func todoID(r *http.Request) (int, error) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 1 {
		return 0, errors.New("invalid todo id")
	}
	return id, nil
}

func newTodoStore() *todoStore {
	return &todoStore{
		nextID: 4,
		todos: []Todo{
			{ID: 1, Title: "Render full pages with Petra"},
			{ID: 2, Title: "Swap todo partials with HTMX"},
			{ID: 3, Title: "Keep server state boring", Done: true},
		},
	}
}

func (s *todoStore) list() []Todo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.todos)
}

func (s *todoStore) add(title string) Todo {
	s.mu.Lock()
	defer s.mu.Unlock()

	todo := Todo{ID: s.nextID, Title: title}
	s.nextID++
	s.todos = append(s.todos, todo)
	return todo
}

func (s *todoStore) toggle(id int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.todos {
		if s.todos[i].ID == id {
			s.todos[i].Done = !s.todos[i].Done
			return true
		}
	}
	return false
}

func (s *todoStore) delete(id int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	before := len(s.todos)
	s.todos = slices.DeleteFunc(s.todos, func(todo Todo) bool {
		return todo.ID == id
	})
	return len(s.todos) != before
}
