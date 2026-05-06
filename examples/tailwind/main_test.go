package main

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/illyabusigin/petra"

	"github.com/gorilla/websocket"
)

func TestTemplatesRenderTailwindAsset(t *testing.T) {
	assets, err := petra.NewAssets(petra.AssetOptions{
		Files:  appFS,
		Root:   "static",
		Prefix: "/static/",
	})
	if err != nil {
		t.Fatalf("NewAssets() error = %v", err)
	}

	tmpl := petra.NewWithOptions(petra.Options{
		FuncMap: template.FuncMap{
			"Asset": assets.URL,
		},
	})
	if err := tmpl.ParseFS(appFS, "templates"); err != nil {
		t.Fatalf("ParseFS() error = %v", err)
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "index", pageData{Title: "Tailwind"}); err != nil {
		t.Fatalf("ExecuteTemplate() error = %v", err)
	}

	got := out.String()
	for _, want := range []string{"Tailwind example", "Vite builds"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered page missing %q:\n%s", want, got)
		}
	}
	if !regexp.MustCompile(`href="/static/app-[0-9a-f]{64}\.css"`).MatchString(got) {
		t.Fatalf("rendered page missing hashed stylesheet:\n%s", got)
	}
	if strings.Contains(got, "/_reload/client.js") {
		t.Fatalf("production render should not include Petra reload client:\n%s", got)
	}
}

func TestStaticCSSIsEmbedded(t *testing.T) {
	if _, err := fs.Stat(appFS, "static/app.css"); err != nil {
		t.Fatalf("static/app.css missing: %v", err)
	}

	assets, err := petra.NewAssets(petra.AssetOptions{
		Files:  appFS,
		Root:   "static",
		Prefix: "/static/",
	})
	if err != nil {
		t.Fatalf("NewAssets() error = %v", err)
	}
	assetURL, err := assets.URL("app.css")
	if err != nil {
		t.Fatalf("URL() error = %v", err)
	}

	handler := assets.Handler()
	req := httptest.NewRequest(http.MethodGet, assetURL, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := rec.Body.String(); !strings.Contains(got, "tailwindcss") || !strings.Contains(got, ".button") {
		t.Fatalf("static CSS does not look like the built Tailwind asset:\n%s", got)
	}
}

func TestDevAppServesReloadClientAndDiskCSS(t *testing.T) {
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
	if got := pageRec.Body.String(); !strings.Contains(got, `href="/static/app.css?v=`) {
		t.Fatalf("development render should include versioned raw stylesheet:\n%s", got)
	}

	reloadReq := httptest.NewRequest(http.MethodGet, "/_reload/client.js", nil)
	reloadRec := httptest.NewRecorder()
	app.Handler.ServeHTTP(reloadRec, reloadReq)
	if reloadRec.Code != http.StatusOK {
		t.Fatalf("reload client status = %d, body = %q", reloadRec.Code, reloadRec.Body.String())
	}
	if got := reloadRec.Body.String(); !strings.Contains(got, "reload_assets") || !strings.Contains(got, "WebSocket") {
		t.Fatalf("reload client does not look like Petra's asset-aware client:\n%s", got)
	}

	cssReq := httptest.NewRequest(http.MethodGet, "/static/app.css", nil)
	cssRec := httptest.NewRecorder()
	app.Handler.ServeHTTP(cssRec, cssReq)
	if cssRec.Code != http.StatusOK {
		t.Fatalf("CSS status = %d, body = %q", cssRec.Code, cssRec.Body.String())
	}
	if got := cssRec.Body.String(); !strings.Contains(got, ".button") {
		t.Fatalf("development static handler did not serve built Tailwind CSS:\n%s", got)
	}
}

func TestDevAppBroadcastsTailwindCSSAssetReload(t *testing.T) {
	root := copyDevRoot(t, "static/app.css")
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
	cssPath := filepath.Join(root, "static", "app.css")
	ticker := time.NewTicker(75 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(3 * time.Second)

	for writes := 0; ; {
		select {
		case msg, ok := <-messages:
			if !ok {
				t.Fatal("reload websocket closed before Tailwind CSS change was broadcast")
			}
			if strings.Contains(msg, `"type":"reload_assets"`) && strings.Contains(msg, `"/static/app.css"`) {
				return
			}
		case <-ticker.C:
			writes++
			next := fmt.Sprintf("@import \"tailwindcss\";\n.button { color: rgb(%d 0 0); }\n", writes)
			if err := os.WriteFile(cssPath, []byte(next), 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
		case <-deadline:
			t.Fatal("timed out waiting for reload_assets broadcast for /static/app.css")
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
