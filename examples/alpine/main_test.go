package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/illyabusigin/petra"

	"github.com/gorilla/websocket"
)

func TestTemplatesRenderAlpineHooks(t *testing.T) {
	tmpl := petra.New()
	if err := tmpl.ParseFS(appFS, "templates"); err != nil {
		t.Fatalf("ParseFS() error = %v", err)
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "index", pageData{Title: "Alpine"}); err != nil {
		t.Fatalf("ExecuteTemplate() error = %v", err)
	}

	got := out.String()
	for _, want := range []string{`src="/static/app.js"`, `x-data="disclosure()"`, "Toggle details"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered page missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "/_reload/client.js") {
		t.Fatalf("production render should not include Petra reload client:\n%s", got)
	}
}

func TestStaticJSIsEmbedded(t *testing.T) {
	if _, err := fs.Stat(appFS, "static/app.js"); err != nil {
		t.Fatalf("static/app.js missing: %v", err)
	}

	handler := petra.StaticFS(appFS, "/static/")
	req := httptest.NewRequest(http.MethodGet, "/static/app.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, "Alpine") || !strings.Contains(got, "disclosure") {
		t.Fatalf("static JS does not look like the built Alpine asset:\n%s", got)
	}
}

func TestDevAppServesReloadClientAndDiskJS(t *testing.T) {
	app, err := newApp(appConfig{Dev: true, RootDir: "."})
	if err != nil {
		t.Fatalf("newApp() error = %v", err)
	}
	defer func() {
		if err := app.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	pageReq := httptest.NewRequest(http.MethodGet, "/", nil)
	pageRec := httptest.NewRecorder()
	app.Handler.ServeHTTP(pageRec, pageReq)
	if pageRec.Code != http.StatusOK {
		t.Fatalf("page status = %d, body = %q", pageRec.Code, pageRec.Body.String())
	}
	if got := pageRec.Body.String(); !strings.Contains(got, "/_reload/client.js") {
		t.Fatalf("development render should include Petra reload client:\n%s", got)
	}

	reloadReq := httptest.NewRequest(http.MethodGet, "/_reload/client.js", nil)
	reloadRec := httptest.NewRecorder()
	app.Handler.ServeHTTP(reloadRec, reloadReq)
	if reloadRec.Code != http.StatusOK {
		t.Fatalf("reload client status = %d, body = %q", reloadRec.Code, reloadRec.Body.String())
	}
	if got := reloadRec.Body.String(); !strings.Contains(got, "reload_assets") || !strings.Contains(got, "WebSocket") {
		t.Fatalf("reload client does not look like Petra's browser client:\n%s", got)
	}

	jsReq := httptest.NewRequest(http.MethodGet, "/static/app.js", nil)
	jsRec := httptest.NewRecorder()
	app.Handler.ServeHTTP(jsRec, jsReq)
	if jsRec.Code != http.StatusOK {
		t.Fatalf("JS status = %d, body = %q", jsRec.Code, jsRec.Body.String())
	}
	if got := jsRec.Body.String(); !strings.Contains(got, "disclosure") {
		t.Fatalf("development static handler did not serve built Alpine JS:\n%s", got)
	}
}

func TestDevAppBroadcastsAlpineJSPageReload(t *testing.T) {
	root := copyDevRoot(t, "static/app.js")
	app, err := newApp(appConfig{Dev: true, RootDir: root})
	if err != nil {
		t.Fatalf("newApp() error = %v", err)
	}
	defer func() {
		if err := app.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	server := httptest.NewServer(app.Handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/_reload/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial(%q) error = %v", wsURL, err)
	}
	defer conn.Close()

	messages := readWebSocketMessages(t, conn)
	jsPath := filepath.Join(root, "static", "app.js")
	ticker := time.NewTicker(75 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(3 * time.Second)

	for writes := 0; ; {
		select {
		case msg, ok := <-messages:
			if !ok {
				t.Fatal("reload websocket closed before Alpine JS change was broadcast")
			}
			if strings.Contains(msg, `"type":"reload"`) && strings.Contains(msg, `"/static/app.js"`) {
				return
			}
		case <-ticker.C:
			writes++
			next := fmt.Sprintf("window.__alpineExampleReload = %d;\n", writes)
			if err := os.WriteFile(jsPath, []byte(next), 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
		case <-deadline:
			t.Fatal("timed out waiting for reload broadcast for /static/app.js")
		}
	}
}

func copyDevRoot(t *testing.T, files ...string) string {
	t.Helper()

	root := t.TempDir()
	for _, name := range append([]string{"templates/layout.html", "templates/index.html"}, files...) {
		src := filepath.Clean(name)
		dst := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", src, err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", dst, err)
		}
	}
	return root
}

func readWebSocketMessages(t *testing.T, conn *websocket.Conn) <-chan string {
	t.Helper()

	messages := make(chan string, 8)
	go func() {
		defer close(messages)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			messages <- string(data)
		}
	}()
	return messages
}
