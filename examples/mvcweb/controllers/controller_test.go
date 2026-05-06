package controllers

import (
	"errors"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/illyabusigin/petra"
)

func TestHTMLShowsDebugErrorInDev(t *testing.T) {
	controller := brokenRenderController(t, true)
	req := httptest.NewRequest(http.MethodGet, "/broken", nil)
	rec := httptest.NewRecorder()
	ctx := controller.MarketingContext(req)
	ctx.SetTitle("Broken")

	controller.HTML(rec, "index", ctx)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	got := rec.Body.String()
	for _, want := range []string{
		"Petra MVC template error",
		"Petra debug",
		"component exploded",
		"Go Stack",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("dev debug page missing %q:\n%s", want, got)
		}
	}
}

func TestHTMLHidesDebugErrorInProduction(t *testing.T) {
	controller := brokenRenderController(t, false)
	req := httptest.NewRequest(http.MethodGet, "/broken", nil)
	rec := httptest.NewRecorder()
	ctx := controller.MarketingContext(req)
	ctx.SetTitle("Broken")

	controller.HTML(rec, "index", ctx)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	got := rec.Body.String()
	if !strings.Contains(got, http.StatusText(http.StatusInternalServerError)) {
		t.Fatalf("production body = %q, want generic 500", got)
	}
	if strings.Contains(got, "component exploded") || strings.Contains(got, "Petra debug") {
		t.Fatalf("production body exposed debug details:\n%s", got)
	}
}

func TestExecRendersInlineComponent(t *testing.T) {
	tmpl := petra.NewWithOptions(petra.Options{
		IncludeDir: "components",
	})
	if err := tmpl.ParseFS(fstest.MapFS{
		"layout.html":           {Data: []byte(`{{block "content" .}}{{end}}`)},
		"index.html":            {Data: []byte(`{{define "content"}}{{end}}`)},
		"components/badge.html": {Data: []byte(`{{define "Badge"}}<span class="badge">{{.}}</span>{{end}}`)},
	}, "."); err != nil {
		t.Fatalf("ParseFS() error = %v", err)
	}

	controller := testController(t, tmpl, true)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	ctx := controller.MarketingContext(req)
	ctx.SetTitle("Inline badge")

	controller.Exec(rec, `{{ Badge .Title }}`, ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != `<span class="badge">Inline badge</span>` {
		t.Fatalf("Exec body = %q", got)
	}
}

func TestMarketingContextSetsRequestAndTitle(t *testing.T) {
	controller := brokenRenderController(t, true)
	req := httptest.NewRequest(http.MethodGet, "/about", nil)

	ctx := controller.MarketingContext(req)
	ctx.SetTitle("About")

	if ctx.Title != "About" {
		t.Fatalf("Title = %q, want About", ctx.Title)
	}
	if ctx.CurrentPath != "/about" {
		t.Fatalf("CurrentPath = %q, want /about", ctx.CurrentPath)
	}
	if ctx.HTTPRequest() != req {
		t.Fatal("HTTPRequest() did not return the original request")
	}
}

func brokenRenderController(t *testing.T, dev bool) *Controller {
	t.Helper()

	tmpl := petra.NewWithOptions(petra.Options{
		FuncMap: template.FuncMap{
			"fail": func() (string, error) {
				return "", errors.New("component exploded")
			},
		},
	})
	if err := tmpl.ParseFS(fstest.MapFS{
		"layout.html": {Data: []byte(`{{block "content" .}}{{end}}`)},
		"index.html":  {Data: []byte(`{{define "content"}}{{fail}}{{end}}`)},
	}, "."); err != nil {
		t.Fatalf("ParseFS() error = %v", err)
	}

	return testController(t, tmpl, dev)
}

func testController(t *testing.T, tmpl *petra.Template, dev bool) *Controller {
	t.Helper()

	env := &Env{
		Templates: tmpl,
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Dev:       dev,
	}
	return NewController(env, "test")
}
