package petra

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/olahol/melody"
)

const (
	defaultHotReloadMountPath        = "/_reload"
	defaultHotReloadSocketPath       = "/ws"
	defaultHotReloadClientScriptPath = "/client.js"
	defaultReconnectBaseDelay        = time.Second
	defaultReconnectMaxDelay         = 30 * time.Second
)

// HotReloadOptions configures the development hot reload controller.
type HotReloadOptions struct {
	// Template is reloaded when watched template files change.
	Template *Template

	// Folders are watched recursively for template file changes.
	Folders []string

	// Logger receives hot reload watcher and broadcast logs. When nil, Petra
	// falls back to Template.Logger.
	Logger *slog.Logger

	// Debounce is the quiet period before a batch of file events is reloaded.
	// Zero uses Petra's default.
	Debounce time.Duration

	// MaxWait is the longest Petra waits before flushing a noisy event batch.
	// Zero uses Petra's default.
	MaxWait time.Duration

	// MountPath is the path where the reload handler is mounted. Petra uses it
	// for direct net/http mounting and as the fallback client script URL.
	// The default is /_reload.
	MountPath string

	// SocketPath is the websocket path below MountPath. The default is /ws.
	SocketPath string

	// ClientScriptPath is the browser client path below MountPath. The default
	// is /client.js.
	ClientScriptPath string

	// ReconnectBaseDelay is the first browser reconnect delay. Zero uses the
	// default of one second.
	ReconnectBaseDelay time.Duration

	// ReconnectMaxDelay caps browser reconnect backoff. Zero uses the default
	// of thirty seconds.
	ReconnectMaxDelay time.Duration
}

type HotReloadController struct {
	t                  *Template
	m                  *melody.Melody
	logger             *slog.Logger
	folders            []string
	debounce           time.Duration
	maxWait            time.Duration
	mountPath          string
	socketPath         string
	clientScriptPath   string
	clientScriptSource string

	counter       atomic.Int64
	startWatchers sync.Once
	closeOnce     sync.Once
	closed        atomic.Bool
	done          chan struct{}
	wg            sync.WaitGroup
	watchersReady *watcherReadiness
	closeErr      error
}

// NewHotReloadController creates a hot reload controller with default options.
func NewHotReloadController(t *Template, folders ...string) *HotReloadController {
	return NewHotReloadControllerWithOptions(HotReloadOptions{
		Template: t,
		Folders:  folders,
	})
}

// NewHotReloadControllerWithOptions creates a hot reload controller with
// explicit development settings.
func NewHotReloadControllerWithOptions(opts HotReloadOptions) *HotReloadController {
	debounce, maxWait := normalizeReloadTimings(opts.Debounce, opts.MaxWait)
	baseDelay, maxDelay := normalizeReconnectTimings(opts.ReconnectBaseDelay, opts.ReconnectMaxDelay)
	mountPath := normalizeHTTPPath(opts.MountPath, defaultHotReloadMountPath)
	socketPath := normalizeHTTPPath(opts.SocketPath, defaultHotReloadSocketPath)
	clientScriptPath := normalizeHTTPPath(opts.ClientScriptPath, defaultHotReloadClientScriptPath)

	c := &HotReloadController{
		folders:            append([]string{}, opts.Folders...),
		t:                  opts.Template,
		m:                  melody.New(),
		logger:             opts.Logger,
		debounce:           debounce,
		maxWait:            maxWait,
		mountPath:          mountPath,
		socketPath:         socketPath,
		clientScriptPath:   clientScriptPath,
		clientScriptSource: buildReloadClientScript(mountPath, socketPath, clientScriptPath, baseDelay, maxDelay),
		done:               make(chan struct{}),
	}
	c.watchersReady = newWatcherReadiness(len(c.folders))

	c.m.HandleConnect(func(s *melody.Session) {
		if c.counter.Add(1) == 1 {
			// Server re-booted, send a reload signal
			c.m.Broadcast([]byte("reload"))
		}
	})

	return c
}

func (c *HotReloadController) waitForWatchers(ctx context.Context) error {
	if c == nil {
		return nil
	}
	return c.watchersReady.wait(ctx)
}

func (c *HotReloadController) Socket() *melody.Melody {
	return c.m
}

// Handler returns the hot reload HTTP handler.
//
// Mount it at the same path used by the browser client, usually /_reload.
func (c *HotReloadController) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, socketPath := range c.handlerPaths(c.socketPath) {
		mux.HandleFunc("GET "+socketPath, c.upgrade)
	}
	for _, clientScriptPath := range c.handlerPaths(c.clientScriptPath) {
		mux.HandleFunc("GET "+clientScriptPath, c.clientScript)
	}

	c.startWatchers.Do(func() {
		for _, folder := range c.folders {
			c.wg.Go(func() {
				c.watchFolder(folder)
			})
		}
	})

	return mux
}

// Close stops template watchers and closes active websocket sessions.
func (c *HotReloadController) Close() error {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		close(c.done)
		c.wg.Wait()
		if err := c.m.Close(); err != nil && !errors.Is(err, melody.ErrClosed) {
			c.closeErr = err
		}
	})
	return c.closeErr
}

func (c *HotReloadController) upgrade(w http.ResponseWriter, r *http.Request) {
	c.m.HandleRequestWithKeys(w, r, map[string]any{})
}

func (c *HotReloadController) clientScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	_, _ = w.Write([]byte(c.clientScriptSource))
}

func (c *HotReloadController) watchFolder(folderPath string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		c.logError("hot_reload_watcher_create_failed", err, slog.String("folder", folderPath))
		c.watchersReady.markFailed("hot reload", folderPath, err)
		return
	}
	defer watcher.Close()

	err = filepath.Walk(folderPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return watcher.Add(path)
		}
		return nil
	})
	if err != nil {
		c.logError("hot_reload_watcher_walk_failed", err, slog.String("folder", folderPath))
		c.watchersReady.markFailed("hot reload", folderPath, err)
		return
	}
	c.logDebug("hot_reload_watcher_started", slog.String("folder", folderPath))
	c.watchersReady.markReady()

	debouncer := newEventDebouncer(c.debounce, c.maxWait, c.handleTemplateEvents)
	defer debouncer.Close()

	for {
		select {
		case <-c.done:
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			if event.Op&fsnotify.Create == fsnotify.Create {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if err := filepath.Walk(event.Name, func(path string, info os.FileInfo, err error) error {
						if err != nil {
							return err
						}
						if info.IsDir() {
							return watcher.Add(path)
						}
						return nil
					}); err != nil {
						c.logError("hot_reload_created_folder_walk_failed", err, slog.String("folder", event.Name))
					}
				}
			}

			debouncer.Add(event)
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			c.logError("hot_reload_watcher_error", err, slog.String("folder", folderPath))
		}
	}
}

func (c *HotReloadController) handleTemplateEvents(events []fsnotify.Event) {
	if c.closed.Load() {
		return
	}

	reloadEvents := make([]ReloadFileEvent, 0, len(events))
	for _, event := range events {
		reloadEvents = append(reloadEvents, ReloadFileEvent{
			Path: event.Name,
			Op:   reloadOpFromFSNotify(event.Op),
		})
	}

	if c.t == nil {
		c.logError("hot_reload_template_missing", errors.New("petra hot reload requires a template"))
		return
	}

	result, err := c.t.Reload(reloadEvents...)
	if err != nil {
		c.broadcastReloadError(result, err)
		return
	}
	if result.Noop || c.closed.Load() {
		return
	}

	if err := c.m.Broadcast([]byte("reload")); err != nil && !errors.Is(err, melody.ErrClosed) {
		c.logError("hot_reload_broadcast_failed", err)
		return
	}
	if c.logEnabled(slog.LevelDebug) {
		attrs := []slog.Attr{slog.String("message", "reload")}
		attrs = append(attrs, reloadLogAttrs(result)...)
		c.logDebug("hot_reload_broadcast", attrs...)
	}
}

type reloadErrorPayload struct {
	Type    string          `json:"type"`
	Message string          `json:"message"`
	Paths   []string        `json:"paths,omitempty"`
	Debug   *DebugErrorInfo `json:"debug,omitempty"`
}

func (c *HotReloadController) broadcastReloadError(result ReloadResult, err error) {
	if c.closed.Load() {
		return
	}

	debugInfo, _ := DebugInfo(err)
	if len(debugInfo.ChangedPaths) == 0 {
		debugInfo.ChangedPaths = append([]string{}, result.ChangedPaths...)
	}

	payload, marshalErr := json.Marshal(reloadErrorPayload{
		Type:    "reload_error",
		Message: err.Error(),
		Paths:   append([]string{}, result.ChangedPaths...),
		Debug:   &debugInfo,
	})
	if marshalErr != nil {
		payload = []byte(`{"type":"reload_error","message":"failed to encode reload error"}`)
	}

	if err := c.m.Broadcast(payload); err != nil && !errors.Is(err, melody.ErrClosed) {
		c.logError("hot_reload_error_broadcast_failed", err)
		return
	}
	if c.logEnabled(slog.LevelDebug) {
		attrs := []slog.Attr{slog.String("message", "reload_error")}
		attrs = append(attrs, reloadLogAttrs(result)...)
		c.logDebug("hot_reload_broadcast", attrs...)
	}
}

func (c *HotReloadController) logDebug(msg string, attrs ...slog.Attr) {
	if c == nil {
		return
	}
	if c.logger != nil {
		c.logger.LogAttrs(context.Background(), slog.LevelDebug, msg, attrs...)
		return
	}
	if c.t == nil {
		return
	}
	c.t.logDebug(msg, attrs...)
}

func (c *HotReloadController) logError(msg string, err error, attrs ...slog.Attr) {
	if c == nil {
		return
	}
	if c.logger != nil {
		attrs = append(attrs, slog.Any("error", err))
		c.logger.LogAttrs(context.Background(), slog.LevelError, msg, attrs...)
		return
	}
	if c.t == nil {
		return
	}
	c.t.logError(msg, err, attrs...)
}

func (c *HotReloadController) logEnabled(level slog.Level) bool {
	if c == nil {
		return false
	}
	if c.logger != nil {
		return c.logger.Enabled(context.Background(), level)
	}
	return c.t != nil && c.t.logEnabled(level)
}

func (c *HotReloadController) handlerPaths(routePath string) []string {
	routePath = normalizeHTTPPath(routePath, routePath)
	mountedPath := joinHTTPPath(c.mountPath, routePath)
	if mountedPath == routePath {
		return []string{routePath}
	}
	return []string{routePath, mountedPath}
}

func normalizeHTTPPath(value, fallback string) string {
	if value == "" {
		value = fallback
	}
	if value == "" {
		return "/"
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	clean := path.Clean(value)
	if clean == "." {
		return fallback
	}
	return clean
}

func joinHTTPPath(mountPath, routePath string) string {
	mountPath = normalizeHTTPPath(mountPath, defaultHotReloadMountPath)
	routePath = normalizeHTTPPath(routePath, routePath)
	if mountPath == "/" {
		return routePath
	}
	return path.Join(mountPath, routePath)
}

func normalizeReconnectTimings(baseDelay, maxDelay time.Duration) (time.Duration, time.Duration) {
	if baseDelay <= 0 {
		baseDelay = defaultReconnectBaseDelay
	}
	if maxDelay <= 0 {
		maxDelay = defaultReconnectMaxDelay
	}
	if maxDelay < baseDelay {
		maxDelay = baseDelay
	}
	return baseDelay, maxDelay
}

func buildReloadClientScript(mountPath, socketPath, clientScriptPath string, baseDelay, maxDelay time.Duration) string {
	fallbackPath := joinHTTPPath(mountPath, clientScriptPath)
	return strings.NewReplacer(
		"__PETRA_CLIENT_SCRIPT_FALLBACK__",
		strconv.Quote(fallbackPath),
		"__PETRA_CLIENT_SCRIPT_PATH__",
		strconv.Quote(clientScriptPath),
		"__PETRA_SOCKET_PATH__",
		strconv.Quote(socketPath),
		"__PETRA_RECONNECT_BASE_DELAY__",
		strconv.FormatInt(durationMilliseconds(baseDelay), 10),
		"__PETRA_RECONNECT_MAX_DELAY__",
		strconv.FormatInt(durationMilliseconds(maxDelay), 10),
	).Replace(reloadClientScriptTemplate)
}

func durationMilliseconds(duration time.Duration) int64 {
	return max(1, int64(duration/time.Millisecond))
}

func reloadOpFromFSNotify(op fsnotify.Op) ReloadOp {
	var reloadOp ReloadOp
	if op&fsnotify.Write == fsnotify.Write {
		reloadOp |= ReloadWrite
	}
	if op&fsnotify.Create == fsnotify.Create {
		reloadOp |= ReloadCreate
	}
	if op&fsnotify.Remove == fsnotify.Remove {
		reloadOp |= ReloadRemove
	}
	if op&fsnotify.Rename == fsnotify.Rename {
		reloadOp |= ReloadRename
	}
	if op&fsnotify.Chmod == fsnotify.Chmod {
		reloadOp |= ReloadChmod
	}
	return reloadOp
}

// reloadClientScriptTemplate is JavaScript with Petra placeholders replaced by
// buildReloadClientScript before serving.
//
//go:embed hot_reload_client.js
var reloadClientScriptTemplate string
