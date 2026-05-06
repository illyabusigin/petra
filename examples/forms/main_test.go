package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/illyabusigin/petra"
)

func TestFormGetRendersPage(t *testing.T) {
	a := testApp(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	a.contact(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	got := rec.Body.String()
	for _, want := range []string{"Forms example", `method="post"`, `href="/static/app.css"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("GET response missing %q:\n%s", want, got)
		}
	}
}

func TestFormPostValidationErrors(t *testing.T) {
	a := testApp(t)
	form := url.Values{
		"name":    {""},
		"email":   {"broken"},
		"message": {"short"},
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	a.contact(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	got := rec.Body.String()
	for _, want := range []string{"Name is required.", "Email must contain an @ sign.", "Message must be at least 12 characters."} {
		if !strings.Contains(got, want) {
			t.Fatalf("validation response missing %q:\n%s", want, got)
		}
	}
}

func TestFormPostSuccess(t *testing.T) {
	a := testApp(t)
	form := url.Values{
		"name":    {"Ilya"},
		"email":   {"ilya@example.com"},
		"message": {"This is a real message."},
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	a.contact(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Body.String(); !strings.Contains(got, "Thanks, Ilya.") {
		t.Fatalf("success response = %q", got)
	}
}

func testApp(t *testing.T) app {
	t.Helper()

	tmpl := petra.New()
	if err := tmpl.ParseFS(appFS, "templates"); err != nil {
		t.Fatalf("ParseFS() error = %v", err)
	}
	return app{templates: tmpl}
}
