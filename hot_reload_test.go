package petra

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"
	"github.com/olahol/melody"
)

func TestHotReloadControllerCloseIsIdempotent(t *testing.T) {
	dir := writeReloadFixture(t)

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components"}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	controller := NewHotReloadController(tmpl, dir)
	_ = controller.Handler()
	waitForHotReloadWatchers(t, controller)

	if err := controller.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := controller.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestNewHotReloadControllerWithOptions(t *testing.T) {
	dir := writeReloadFixture(t)
	tmpl := &Template{Layout: "layout.html", IncludeDir: "components"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	controller := NewHotReloadControllerWithOptions(HotReloadOptions{
		Template:           tmpl,
		Folders:            []string{dir},
		Logger:             logger,
		Debounce:           5 * time.Millisecond,
		MaxWait:            20 * time.Millisecond,
		MountPath:          "/dev_reload",
		SocketPath:         "socket",
		ClientScriptPath:   "client.js",
		ReconnectBaseDelay: 2 * time.Second,
		ReconnectMaxDelay:  45 * time.Second,
	})
	defer controller.Close()

	if controller.t != tmpl {
		t.Fatal("template was not retained")
	}
	if controller.logger != logger {
		t.Fatal("logger was not retained")
	}
	if len(controller.folders) != 1 || controller.folders[0] != dir {
		t.Fatalf("folders = %v", controller.folders)
	}
	if controller.debounce != 5*time.Millisecond {
		t.Fatalf("debounce = %s", controller.debounce)
	}
	if controller.maxWait != 20*time.Millisecond {
		t.Fatalf("maxWait = %s", controller.maxWait)
	}
	if controller.mountPath != "/dev_reload" {
		t.Fatalf("mountPath = %q", controller.mountPath)
	}
	if controller.socketPath != "/socket" {
		t.Fatalf("socketPath = %q", controller.socketPath)
	}
	if controller.clientScriptPath != "/client.js" {
		t.Fatalf("clientScriptPath = %q", controller.clientScriptPath)
	}

	handler := controller.Handler()
	for _, requestPath := range []string{"/client.js", "/dev_reload/client.js"} {
		req := httptest.NewRequest(http.MethodGet, requestPath, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d", requestPath, rec.Code)
		}
		body := rec.Body.String()
		for _, want := range []string{
			`"/dev_reload/client.js"`,
			`const socketPath = "/socket"`,
			"reconnectBaseDelay = 2000",
			"reconnectMaxDelay = 45000",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s client script missing %q", requestPath, want)
			}
		}
	}
}

func TestStaticFileServerCloseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app.css"), `body { color: black; }`)

	server := NewStatic(melody.New(), dir, "/static/")
	if err := server.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := server.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestNewStaticWithOptions(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app.css"), `body { color: black; }`)
	socket := melody.New()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	server := NewStaticWithOptions(StaticOptions{
		Socket:       socket,
		Folder:       dir,
		StripPrefix:  "/assets/",
		Logger:       logger,
		Debounce:     5 * time.Millisecond,
		MaxWait:      20 * time.Millisecond,
		ReloadPolicy: StaticReloadPage,
	})
	defer server.Close()

	if server.socket != socket {
		t.Fatal("socket was not retained")
	}
	if server.logger != logger {
		t.Fatal("logger was not retained")
	}
	if server.folder != dir {
		t.Fatalf("folder = %q", server.folder)
	}
	if server.stripPrefix != "/assets/" {
		t.Fatalf("stripPrefix = %q", server.stripPrefix)
	}
	if server.debounce != 5*time.Millisecond {
		t.Fatalf("debounce = %s", server.debounce)
	}
	if server.maxWait != 20*time.Millisecond {
		t.Fatalf("maxWait = %s", server.maxWait)
	}
	if server.reloadPolicy != StaticReloadPage {
		t.Fatalf("reloadPolicy = %v", server.reloadPolicy)
	}
	if message := server.reloadMessage(); message != "reload" {
		t.Fatalf("reload message = %q", message)
	}
}

func TestNewStaticWithOptionsCanDisableReloads(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app.css"), `body { color: black; }`)

	server := NewStaticWithOptions(StaticOptions{
		Socket:       melody.New(),
		Folder:       dir,
		ReloadPolicy: StaticReloadDisabled,
	})
	defer server.Close()

	if message := server.reloadMessage(); message != "" {
		t.Fatalf("reload message = %q", message)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := server.waitForWatchers(ctx); err != nil {
		t.Fatalf("waitForWatchers() error = %v", err)
	}
}

func TestStaticFileServerClassifiesStaticReloads(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app.css"), `body { color: black; }`)
	writeFile(t, filepath.Join(dir, "app.js"), `console.log("app")`)

	server := NewStaticWithOptions(StaticOptions{
		Folder:      dir,
		StripPrefix: "/static/",
	})
	defer server.Close()

	cases := []struct {
		name    string
		events  []fsnotify.Event
		message string
		paths   []string
		noop    bool
	}{
		{
			name:    "css write refreshes assets",
			events:  []fsnotify.Event{{Name: filepath.Join(dir, "app.css"), Op: fsnotify.Write}},
			message: "reload_assets",
			paths:   []string{"/static/app.css"},
		},
		{
			name:    "js write reloads page",
			events:  []fsnotify.Event{{Name: filepath.Join(dir, "app.js"), Op: fsnotify.Write}},
			message: "reload",
			paths:   []string{"/static/app.js"},
		},
		{
			name:    "css remove reloads page",
			events:  []fsnotify.Event{{Name: filepath.Join(dir, "app.css"), Op: fsnotify.Remove}},
			message: "reload",
			paths:   []string{"/static/app.css"},
		},
		{
			name:   "chmod is ignored",
			events: []fsnotify.Event{{Name: filepath.Join(dir, "app.css"), Op: fsnotify.Chmod}},
			noop:   true,
		},
		{
			name:   "noise is ignored",
			events: []fsnotify.Event{{Name: filepath.Join(dir, ".DS_Store"), Op: fsnotify.Write}},
			noop:   true,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			decision := server.reloadDecision(tt.events)
			if decision.noop != tt.noop {
				t.Fatalf("noop = %v, want %v", decision.noop, tt.noop)
			}
			if decision.message != tt.message {
				t.Fatalf("message = %q, want %q", decision.message, tt.message)
			}
			if !slices.Equal(decision.paths, tt.paths) {
				t.Fatalf("paths = %v, want %v", decision.paths, tt.paths)
			}
		})
	}
}

func TestStaticReloadPolicyOverridesClassification(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app.css"), `body { color: black; }`)
	writeFile(t, filepath.Join(dir, "app.js"), `console.log("app")`)

	for _, tt := range []struct {
		name    string
		policy  StaticReloadPolicy
		file    string
		message string
		noop    bool
	}{
		{
			name:    "assets policy keeps js asset reload",
			policy:  StaticReloadAssets,
			file:    "app.js",
			message: "reload_assets",
		},
		{
			name:    "page policy reloads for css",
			policy:  StaticReloadPage,
			file:    "app.css",
			message: "reload",
		},
		{
			name:   "disabled policy ignores css",
			policy: StaticReloadDisabled,
			file:   "app.css",
			noop:   true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := NewStaticWithOptions(StaticOptions{
				Folder:       dir,
				StripPrefix:  "/static/",
				ReloadPolicy: tt.policy,
			})
			defer server.Close()

			decision := server.reloadDecision([]fsnotify.Event{{Name: filepath.Join(dir, tt.file), Op: fsnotify.Write}})
			if decision.noop != tt.noop {
				t.Fatalf("noop = %v, want %v", decision.noop, tt.noop)
			}
			if decision.message != tt.message {
				t.Fatalf("message = %q, want %q", decision.message, tt.message)
			}
		})
	}
}

func TestHotReloadWebsocketBroadcastsReload(t *testing.T) {
	dir := writeReloadFixture(t)

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components"}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	controller := NewHotReloadController(tmpl, dir)
	defer controller.Close()

	router := reloadTestMux(controller)
	server := httptest.NewServer(router)
	defer server.Close()
	waitForHotReloadWatchers(t, controller)

	conn := dialReloadSocket(t, server.URL)
	defer conn.Close()
	messages := readWebSocketMessages(conn)
	expectInitialReload(t, messages)

	writeFile(t, filepath.Join(dir, "about.html"), `{{define "content"}}about websocket{{end}}`)
	if got := waitForWebSocketMessage(t, messages, 3*time.Second, func(msg string) bool {
		return msg == "reload"
	}); got != "reload" {
		t.Fatalf("message = %q, want reload", got)
	}

	if got := executeTemplate(t, tmpl, "about"); got != "header root about websocket" {
		t.Fatalf("updated about render = %q", got)
	}
}

func TestHotReloadWebsocketBroadcastsReloadError(t *testing.T) {
	dir := writeReloadFixture(t)

	tmpl := &Template{Layout: "layout.html", IncludeDir: "components"}
	if err := tmpl.ParseDir(dir); err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	controller := NewHotReloadController(tmpl, dir)
	defer controller.Close()

	router := reloadTestMux(controller)
	server := httptest.NewServer(router)
	defer server.Close()
	waitForHotReloadWatchers(t, controller)

	conn := dialReloadSocket(t, server.URL)
	defer conn.Close()
	messages := readWebSocketMessages(conn)
	expectInitialReload(t, messages)

	writeFile(t, filepath.Join(dir, "about.html"), `{{define "content"}}broken {{end`)
	raw := waitForWebSocketMessage(t, messages, 3*time.Second, func(msg string) bool {
		return strings.Contains(msg, `"type":"reload_error"`)
	})

	var payload reloadErrorPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v", raw, err)
	}
	if payload.Type != "reload_error" {
		t.Fatalf("payload type = %q", payload.Type)
	}
	if len(payload.Paths) != 1 || payload.Paths[0] != "about.html" {
		t.Fatalf("payload paths = %v", payload.Paths)
	}
	if payload.Debug == nil {
		t.Fatal("payload debug = nil")
	}
	if payload.Debug.Kind != DebugErrorKindParse {
		t.Fatalf("payload debug kind = %q, want %q", payload.Debug.Kind, DebugErrorKindParse)
	}
	if payload.Debug.Operation != "Reload" {
		t.Fatalf("payload debug operation = %q, want Reload", payload.Debug.Operation)
	}
	if payload.Debug.Page != "about" {
		t.Fatalf("payload debug page = %q, want about", payload.Debug.Page)
	}
	if payload.Debug.DependencyRole != DebugDependencyRolePage {
		t.Fatalf("payload debug role = %q, want %q", payload.Debug.DependencyRole, DebugDependencyRolePage)
	}
	if len(payload.Debug.ChangedPaths) != 1 || payload.Debug.ChangedPaths[0] != "about.html" {
		t.Fatalf("payload debug changed paths = %v", payload.Debug.ChangedPaths)
	}
	if len(payload.Debug.AffectedPages) != 1 || payload.Debug.AffectedPages[0] != "about" {
		t.Fatalf("payload debug affected pages = %v", payload.Debug.AffectedPages)
	}
	if payload.Debug.Source == nil || payload.Debug.Source.Path != "about.html" {
		t.Fatalf("payload debug source = %#v, want about.html", payload.Debug.Source)
	}

	writeFile(t, filepath.Join(dir, "about.html"), `{{define "content"}}about fixed{{end}}`)
	if got := waitForWebSocketMessage(t, messages, 3*time.Second, func(msg string) bool {
		return msg == "reload"
	}); got != "reload" {
		t.Fatalf("message = %q, want reload", got)
	}
}

func TestStaticFileServerBroadcastsReloadAssets(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app.css"), `body { color: black; }`)
	writeFile(t, filepath.Join(dir, "app.js"), `console.log("black")`)

	tmpl := New()
	controller := NewHotReloadController(tmpl)
	defer controller.Close()

	static := NewStatic(controller.Socket(), dir, "/static/")
	defer static.Close()
	waitForStaticWatchers(t, static)

	router := reloadTestMux(controller)
	router.Handle("/static/", static)
	server := httptest.NewServer(router)
	defer server.Close()

	conn := dialReloadSocket(t, server.URL)
	defer conn.Close()
	messages := readWebSocketMessages(conn)
	expectInitialReload(t, messages)

	writeFile(t, filepath.Join(dir, "app.css"), `body { color: white; }`)
	raw := waitForWebSocketMessage(t, messages, 3*time.Second, func(msg string) bool {
		return strings.Contains(msg, `"type":"reload_assets"`)
	})
	var payload staticReloadPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v", raw, err)
	}
	if payload.Type != "reload_assets" {
		t.Fatalf("payload type = %q, want reload_assets", payload.Type)
	}
	if !slices.Equal(payload.Paths, []string{"/static/app.css"}) {
		t.Fatalf("payload paths = %v", payload.Paths)
	}

	writeFile(t, filepath.Join(dir, "app.js"), `console.log("white")`)
	raw = waitForWebSocketMessage(t, messages, 3*time.Second, func(msg string) bool {
		return strings.Contains(msg, `"type":"reload"`)
	})
	payload = staticReloadPayload{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v", raw, err)
	}
	if payload.Type != "reload" {
		t.Fatalf("payload type = %q, want reload", payload.Type)
	}
	if !slices.Equal(payload.Paths, []string{"/static/app.js"}) {
		t.Fatalf("payload paths = %v", payload.Paths)
	}
}

func TestStaticFileServerBroadcastsConfiguredReloadPolicy(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app.css"), `body { color: black; }`)

	tmpl := New()
	controller := NewHotReloadController(tmpl)
	defer controller.Close()

	static := NewStaticWithOptions(StaticOptions{
		Socket:       controller.Socket(),
		Folder:       dir,
		StripPrefix:  "/static/",
		Debounce:     5 * time.Millisecond,
		MaxWait:      20 * time.Millisecond,
		ReloadPolicy: StaticReloadPage,
	})
	defer static.Close()
	waitForStaticWatchers(t, static)

	router := reloadTestMux(controller)
	router.Handle("/static/", static)
	server := httptest.NewServer(router)
	defer server.Close()

	conn := dialReloadSocket(t, server.URL)
	defer conn.Close()
	messages := readWebSocketMessages(conn)
	expectInitialReload(t, messages)

	writeFile(t, filepath.Join(dir, "app.css"), `body { color: white; }`)
	raw := waitForWebSocketMessage(t, messages, 3*time.Second, func(msg string) bool {
		return strings.Contains(msg, `"type":"reload"`)
	})
	var payload staticReloadPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v", raw, err)
	}
	if payload.Type != "reload" {
		t.Fatalf("payload type = %q, want reload", payload.Type)
	}
	if !slices.Equal(payload.Paths, []string{"/static/app.css"}) {
		t.Fatalf("payload paths = %v", payload.Paths)
	}
}

func TestReloadClientScriptIsServed(t *testing.T) {
	controller := NewHotReloadController(New())
	defer controller.Close()

	req := httptest.NewRequest("GET", "/_reload/client.js", nil)
	rec := httptest.NewRecorder()

	controller.clientScript(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/javascript; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"reload_error",
		"new WebSocket",
		"petra-reload-disconnected",
		"nextReconnectDelay",
		"disconnectNoticeDelay = 2000",
		"clientScriptPath",
		"socketPath",
		"assetPath",
		"payload.type === \"reload_assets\"",
		"reconnectBaseDelay = 1000",
		"reconnectMaxDelay = 30000",
		"Template reload failed",
		"Go stack",
		"dependency_role",
		"Affected pages",
		"Source excerpt",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("client script missing %q", want)
		}
	}
	if strings.Contains(body, "setTimeout(connect, 1000)") {
		t.Fatal("client script still has fixed one-second reconnect loop")
	}
	if strings.Contains(body, "__PETRA_") {
		t.Fatal("client script still has unsubstituted Petra template tokens")
	}
}

func TestEventDebouncerCloseDropsPendingEvents(t *testing.T) {
	events := make(chan []fsnotify.Event, 1)
	debouncer := newEventDebouncer(10*time.Millisecond, 100*time.Millisecond, func(batch []fsnotify.Event) {
		events <- batch
	})

	debouncer.Add(fsnotify.Event{Name: "about.html", Op: fsnotify.Write})
	debouncer.Close()

	select {
	case batch := <-events:
		t.Fatalf("debouncer emitted after Close(): %v", batch)
	case <-time.After(150 * time.Millisecond):
	}
}

func dialReloadSocket(t *testing.T, serverURL string) *websocket.Conn {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/_reload/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial(%q) error = %v", wsURL, err)
	}
	return conn
}

func reloadTestMux(controller *HotReloadController) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/_reload/", http.StripPrefix("/_reload", controller.Handler()))
	return mux
}

type webSocketRead struct {
	message string
	err     error
}

func readWebSocketMessages(conn *websocket.Conn) <-chan webSocketRead {
	reads := make(chan webSocketRead, 8)
	go func() {
		defer close(reads)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				reads <- webSocketRead{err: err}
				return
			}
			reads <- webSocketRead{message: string(data)}
		}
	}()
	return reads
}

func waitForWebSocketMessage(t *testing.T, reads <-chan webSocketRead, timeout time.Duration, match func(string) bool) string {
	t.Helper()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case read, ok := <-reads:
			if !ok {
				t.Fatal("websocket reader closed")
			}
			if read.err != nil {
				t.Fatalf("ReadMessage() error = %v", read.err)
			}
			if match(read.message) {
				return read.message
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for websocket message")
		}
	}
}

func expectInitialReload(t *testing.T, reads <-chan webSocketRead) {
	t.Helper()

	if got := waitForWebSocketMessage(t, reads, 3*time.Second, func(msg string) bool {
		return msg == "reload"
	}); got != "reload" {
		t.Fatalf("initial websocket message = %q, want reload", got)
	}
}

func waitForHotReloadWatchers(t *testing.T, controller *HotReloadController) {
	t.Helper()
	waitForWatcherReadiness(t, controller.waitForWatchers)
}

func waitForStaticWatchers(t *testing.T, server *StaticFileServer) {
	t.Helper()
	waitForWatcherReadiness(t, server.waitForWatchers)
}

func waitForWatcherReadiness(t *testing.T, wait func(context.Context) error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	if err := wait(ctx); err != nil {
		t.Fatalf("watcher readiness: %v", err)
	}
}
