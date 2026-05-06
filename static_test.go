package petra

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestStaticFSServesEmbeddedStaticPrefix(t *testing.T) {
	handler := StaticFS(fstest.MapFS{
		"static/app.css": {Data: []byte("body { color: black; }")},
	}, "/static/")

	rec := requestStatic(t, handler, "/static/app.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, "color: black") {
		t.Fatalf("body = %q", got)
	}
}

func TestStaticFSUsesConfiguredStripPrefixAsEmbeddedPrefix(t *testing.T) {
	files := fstest.MapFS{
		"assets/app.css": {Data: []byte("body { color: white; }")},
	}

	for _, prefix := range []string{"/assets/", "/assets"} {
		t.Run(prefix, func(t *testing.T) {
			handler := StaticFS(files, prefix)

			rec := requestStatic(t, handler, "/assets/app.css")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
			}
			if got := rec.Body.String(); !strings.Contains(got, "color: white") {
				t.Fatalf("body = %q", got)
			}
		})
	}
}

func TestStaticFSRejectsRequestsOutsideStripPrefix(t *testing.T) {
	handler := StaticFS(fstest.MapFS{
		"assets/app.css": {Data: []byte("body { color: white; }")},
	}, "/assets/")

	rec := requestStatic(t, handler, "/static/app.css")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestStaticFSServesRootEmbeddedFilesWithoutStripPrefix(t *testing.T) {
	handler := StaticFS(fstest.MapFS{
		"app.css": {Data: []byte("body { color: blue; }")},
	}, "")

	rec := requestStatic(t, handler, "/app.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, "color: blue") {
		t.Fatalf("body = %q", got)
	}
}

func requestStatic(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
