package cmd

import (
	"bytes"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/illyabusigin/petra"
	"github.com/illyabusigin/petra/examples/mvcweb/controllers"
)

func TestEmbeddedTemplatesRender(t *testing.T) {
	tmpl := newExampleTemplate(t)
	if err := tmpl.ParseFS(webFS, "templates"); err != nil {
		t.Fatalf("ParseFS() error = %v", err)
	}

	var b bytes.Buffer
	if err := tmpl.ExecuteTemplate(&b, "marketing/home", examplePageData(false)); err != nil {
		t.Fatalf("ExecuteTemplate() error = %v", err)
	}

	got := b.String()
	for _, want := range []string{
		"Petra MVC",
		"A small MVC web app using Petra.",
		`href="/"`,
		`href="/about"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered home missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "/_reload/client.js") {
		t.Fatal("production render included hot reload client")
	}
}

func TestDiskTemplatesRenderDevClient(t *testing.T) {
	tmpl := newExampleTemplate(t)
	if err := tmpl.ParseDir("templates"); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	var b bytes.Buffer
	if err := tmpl.ExecuteTemplate(&b, "marketing/about", examplePageData(true)); err != nil {
		t.Fatalf("ExecuteTemplate() error = %v", err)
	}

	got := b.String()
	if !strings.Contains(got, "What this example is meant to show.") {
		t.Fatalf("rendered about missing page content:\n%s", got)
	}
	if !strings.Contains(got, "/_reload/client.js") {
		t.Fatal("dev render did not include hot reload client")
	}
}

func TestEmbeddedStaticAssetExists(t *testing.T) {
	if _, err := fs.Stat(webFS, "static/app.css"); err != nil {
		t.Fatalf("embedded static asset missing: %v", err)
	}
}

func TestEmbeddedStaticHandlerServesAsset(t *testing.T) {
	handler := petra.StaticFS(webFS, "/static/")
	req := httptest.NewRequest(http.MethodGet, "/static/app.css", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "--bg") {
		t.Fatalf("static CSS response missing expected content")
	}
}

func TestRequestLoggerWritesBasicSlogFields(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	handler := requestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))

	req := httptest.NewRequest(http.MethodPost, "/submit", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	got := logs.String()
	for _, want := range []string{
		"msg=request",
		"method=POST",
		"path=/submit",
		"status=202",
		"duration=",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("request log missing %q:\n%s", want, got)
		}
	}
}

func newExampleTemplate(t *testing.T) *petra.Template {
	t.Helper()

	return petra.NewWithOptions(petra.Options{
		IncludeDir: "components",
		Plugins:    petra.Plugins{petra.HTML()},
	})
}

func examplePageData(dev bool) controllers.PageData {
	return controllers.PageData{
		Title:       "Petra MVC example",
		CurrentPath: "/",
		Dev:         dev,
		Nav: []controllers.NavItem{
			{Label: "Home", Path: "/"},
			{Label: "About", Path: "/about"},
		},
	}
}
