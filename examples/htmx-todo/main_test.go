package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestIndexRendersHTMXTodoApp(t *testing.T) {
	a := testApp(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	a.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	got := rec.Body.String()
	for _, want := range []string{
		"Petra HTMX todo",
		`hx-post="/todos"`,
		`hx-patch="/todos/1/toggle"`,
		`hx-delete="/todos/1"`,
		"Render full pages with Petra",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("index response missing %q:\n%s", want, got)
		}
	}
}

func TestCreateTodoHTMXReturnsTodoPartial(t *testing.T) {
	a := testApp(t)
	form := url.Values{"title": {"Ship the HTMX example"}}
	req := httptest.NewRequest(http.MethodPost, "/todos", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()

	a.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if got := rec.Header().Get("HX-Retarget"); got != "#todos" {
		t.Fatalf("HX-Retarget = %q, want #todos", got)
	}
	if got := rec.Header().Get("HX-Reswap"); got != "outerHTML" {
		t.Fatalf("HX-Reswap = %q, want outerHTML", got)
	}
	if got := rec.Header().Get("HX-Trigger-After-Swap"); !strings.Contains(got, "todo:created") {
		t.Fatalf("HX-Trigger-After-Swap = %q, want todo:created event", got)
	}
	if got := rec.Body.String(); !strings.Contains(got, `id="todos"`) || !strings.Contains(got, "Ship the HTMX example") {
		t.Fatalf("partial response did not include updated todo list:\n%s", got)
	}
}

func TestCreateTodoHTMXValidationRetargetsErrors(t *testing.T) {
	a := testApp(t)
	req := httptest.NewRequest(http.MethodPost, "/todos", strings.NewReader(url.Values{"title": {""}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()

	a.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if got := rec.Header().Get("HX-Retarget"); got != "#todo-form-errors" {
		t.Fatalf("HX-Retarget = %q, want #todo-form-errors", got)
	}
	if got := rec.Body.String(); !strings.Contains(got, "Todo title is required.") {
		t.Fatalf("validation response = %q", got)
	}
}

func TestToggleTodoHTMXReturnsUpdatedPartial(t *testing.T) {
	a := testApp(t)
	req := httptest.NewRequest(http.MethodPatch, "/todos/1/toggle", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()

	a.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	got := rec.Body.String()
	if !strings.Contains(got, "completed") {
		t.Fatalf("toggle response did not mark todo as completed:\n%s", got)
	}
	if !strings.Contains(rec.Header().Get("HX-Trigger-After-Swap"), "todo:toggled") {
		t.Fatalf("HX-Trigger-After-Swap = %q, want todo:toggled event", rec.Header().Get("HX-Trigger-After-Swap"))
	}
}

func TestDeleteTodoHTMXReturnsUpdatedPartial(t *testing.T) {
	a := testApp(t)
	req := httptest.NewRequest(http.MethodDelete, "/todos/1", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()

	a.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	got := rec.Body.String()
	if strings.Contains(got, "Render full pages with Petra") {
		t.Fatalf("deleted todo still rendered:\n%s", got)
	}
	if !strings.Contains(rec.Header().Get("HX-Trigger-After-Swap"), "todo:deleted") {
		t.Fatalf("HX-Trigger-After-Swap = %q, want todo:deleted event", rec.Header().Get("HX-Trigger-After-Swap"))
	}
}

func TestCreateTodoWithoutHTMXRedirects(t *testing.T) {
	a := testApp(t)
	form := url.Values{"title": {"Plain form fallback"}}
	req := httptest.NewRequest(http.MethodPost, "/todos", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	a.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/" {
		t.Fatalf("Location = %q, want /", got)
	}
}

func TestCreateTodoWithoutHTMXValidationRendersFullPage(t *testing.T) {
	a := testApp(t)
	req := httptest.NewRequest(http.MethodPost, "/todos", strings.NewReader(url.Values{"title": {""}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	a.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	got := rec.Body.String()
	for _, want := range []string{"Petra HTMX todo", "Todo title is required.", `hx-post="/todos"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("validation fallback missing %q:\n%s", want, got)
		}
	}
}

func testApp(t *testing.T) app {
	t.Helper()

	a, err := newApp()
	if err != nil {
		t.Fatalf("newApp() error = %v", err)
	}
	return a
}
