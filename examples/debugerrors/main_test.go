package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHomeRendersDebugExampleLinks(t *testing.T) {
	a := testApp(t, false)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	a.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	got := rec.Body.String()
	for _, want := range []string{"Petra debug errors", "/broken-page", "/broken-component"} {
		if !strings.Contains(got, want) {
			t.Fatalf("home response missing %q:\n%s", want, got)
		}
	}
}

func TestBrokenPageRendersPetraDebugPageInDev(t *testing.T) {
	a := testApp(t, true)
	req := httptest.NewRequest(http.MethodGet, "/broken-page", nil)
	rec := httptest.NewRecorder()

	a.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	got := rec.Body.String()
	for _, want := range []string{
		"Petra debug",
		"Petra debug error example",
		"page data lookup failed",
		"broken-page",
		"Source Excerpt",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("debug page missing %q:\n%s", want, got)
		}
	}
}

func TestBrokenComponentRendersPetraDebugPageInDev(t *testing.T) {
	a := testApp(t, true)
	req := httptest.NewRequest(http.MethodGet, "/broken-component", nil)
	rec := httptest.NewRecorder()

	a.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	got := rec.Body.String()
	for _, want := range []string{
		"Petra debug",
		"component data loader failed",
		"Component",
		"ExplodingCard",
		"Template Stack",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("component debug page missing %q:\n%s", want, got)
		}
	}
}

func TestBrokenPageHidesDebugDetailsInProduction(t *testing.T) {
	a := testApp(t, false)
	req := httptest.NewRequest(http.MethodGet, "/broken-page", nil)
	rec := httptest.NewRecorder()

	a.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	got := rec.Body.String()
	if strings.Contains(got, "Petra debug") || strings.Contains(got, "page data lookup failed") {
		t.Fatalf("production response exposed debug details:\n%s", got)
	}
	if got != "Internal Server Error\n" {
		t.Fatalf("production response = %q", got)
	}
}

func testApp(t *testing.T, dev bool) app {
	t.Helper()

	a, err := newApp(dev, nil)
	if err != nil {
		t.Fatalf("newApp() error = %v", err)
	}
	return a
}
