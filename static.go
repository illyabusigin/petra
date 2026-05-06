package petra

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/olahol/melody"
	"github.com/vearutop/statigz"
	"github.com/vearutop/statigz/brotli"
)

// StaticReloadPolicy controls what browser message Petra broadcasts after a
// static asset changes during development.
type StaticReloadPolicy uint8

const (
	// StaticReloadDefault preserves Petra's current asset reload behavior.
	StaticReloadDefault StaticReloadPolicy = iota

	// StaticReloadAssets asks the browser client to refresh stylesheet assets.
	StaticReloadAssets

	// StaticReloadPage asks the browser client to reload the whole page.
	StaticReloadPage

	// StaticReloadDisabled disables static file watching and browser broadcasts.
	StaticReloadDisabled
)

// StaticOptions configures the development static file server.
type StaticOptions struct {
	// Socket receives browser reload broadcasts. When nil, Petra serves files
	// without starting a watcher.
	Socket *melody.Melody

	// Folder is the local filesystem directory to serve.
	Folder string

	// StripPrefix is removed from request paths before serving files.
	StripPrefix string

	// Logger receives static watcher and broadcast logs.
	Logger *slog.Logger

	// Debounce is the quiet period before a batch of file events is broadcast.
	// Zero uses Petra's default.
	Debounce time.Duration

	// MaxWait is the longest Petra waits before flushing a noisy event batch.
	// Zero uses Petra's default.
	MaxWait time.Duration

	// ReloadPolicy controls the browser message sent after static asset changes.
	ReloadPolicy StaticReloadPolicy
}

type StaticFileServer struct {
	socket       *melody.Melody
	logger       *slog.Logger
	dev          bool
	folder       string
	fs           fs.FS
	stripPrefix  string
	debounce     time.Duration
	maxWait      time.Duration
	reloadPolicy StaticReloadPolicy

	server http.Handler
	done   chan struct{}
	wg     sync.WaitGroup
	ready  *watcherReadiness

	closeOnce sync.Once
	closed    atomic.Bool
	closeErr  error
}

type staticReloadPayload struct {
	Type  string   `json:"type"`
	Paths []string `json:"paths,omitempty"`
}

type staticReloadDecision struct {
	message string
	paths   []string
	noop    bool
}

func Static(socket *melody.Melody, folder, stripPrefix string) http.Handler {
	return NewStatic(socket, folder, stripPrefix)
}

// NewStatic creates a development static file server with default options.
func NewStatic(socket *melody.Melody, folder, stripPrefix string) *StaticFileServer {
	return NewStaticWithOptions(StaticOptions{
		Socket:      socket,
		Folder:      folder,
		StripPrefix: stripPrefix,
	})
}

// NewStaticWithLogger creates a development static file server with a logger.
func NewStaticWithLogger(socket *melody.Melody, folder, stripPrefix string, logger *slog.Logger) *StaticFileServer {
	return NewStaticWithOptions(StaticOptions{
		Socket:      socket,
		Folder:      folder,
		StripPrefix: stripPrefix,
		Logger:      logger,
	})
}

// NewStaticWithOptions creates a development static file server with explicit
// watcher and reload settings.
func NewStaticWithOptions(opts StaticOptions) *StaticFileServer {
	debounce, maxWait := normalizeReloadTimings(opts.Debounce, opts.MaxWait)
	watcherCount := 0
	if opts.Socket != nil && opts.ReloadPolicy != StaticReloadDisabled {
		watcherCount = 1
	}

	s := &StaticFileServer{
		socket:       opts.Socket,
		logger:       opts.Logger,
		folder:       opts.Folder,
		stripPrefix:  opts.StripPrefix,
		debounce:     debounce,
		maxWait:      maxWait,
		reloadPolicy: opts.ReloadPolicy,
		server:       http.FileServer(http.Dir(opts.Folder)),
		done:         make(chan struct{}),
		ready:        newWatcherReadiness(watcherCount),
	}

	if watcherCount > 0 {
		s.wg.Go(s.watch)
	}

	return s
}

func StaticFS(fs fs.ReadDirFS, stripPrefix string) http.Handler {
	options := []func(*statigz.Server){
		brotli.AddEncoding,
		statigz.EncodeOnInit,
	}
	if stripPrefix != "" {
		options = append(options, statigz.FSPrefix(stripPrefix))
	}

	s := &StaticFileServer{
		dev:         false,
		fs:          fs,
		stripPrefix: stripPrefix,
		server:      statigz.FileServer(fs, options...),
		done:        make(chan struct{}),
		ready:       newWatcherReadiness(0),
	}

	return s
}

func (s *StaticFileServer) waitForWatchers(ctx context.Context) error {
	if s == nil {
		return nil
	}
	return s.ready.wait(ctx)
}

func (s *StaticFileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.stripPrefix != "" {
		http.StripPrefix(s.stripPrefix, s.server).ServeHTTP(w, r)
	} else {
		s.server.ServeHTTP(w, r)
	}
}

func (s *StaticFileServer) Close() error {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		close(s.done)
		s.wg.Wait()
		if s.socket != nil && s.socket.IsClosed() {
			s.closeErr = nil
		}
		s.logDebug("static_watcher_closed", slog.String("folder", s.folder))
	})
	return s.closeErr
}

func (s *StaticFileServer) watch() {
	if s.reloadPolicy == StaticReloadDisabled || s.socket == nil {
		return
	}

	debouncer := newEventDebouncer(s.debounce, s.maxWait, func(events []fsnotify.Event) {
		if s.closed.Load() {
			return
		}
		decision := s.reloadDecision(events)
		if decision.noop {
			return
		}
		payload, err := json.Marshal(staticReloadPayload{
			Type:  decision.message,
			Paths: decision.paths,
		})
		if err != nil {
			payload = []byte(decision.message)
		}
		if err := s.socket.Broadcast(payload); err != nil {
			s.logError("static_reload_broadcast_failed", err, slog.String("folder", s.folder))
			return
		}
		s.logDebug(
			"static_reload_broadcast",
			slog.String("folder", s.folder),
			slog.String("message", decision.message),
			slog.Int("event_count", len(events)),
			slog.Int("path_count", len(decision.paths)),
			slog.Any("paths", append([]string{}, decision.paths...)),
		)
	})
	defer debouncer.Close()

	s.watchFolder(s.folder, debouncer.Add)
}

func (s *StaticFileServer) watchFolder(folderPath string, do func(fsnotify.Event)) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		s.logError("static_watcher_create_failed", err, slog.String("folder", folderPath))
		s.ready.markFailed("static", folderPath, err)
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
		s.logError("static_watcher_walk_failed", err, slog.String("folder", folderPath))
		s.ready.markFailed("static", folderPath, err)
		return
	}
	s.logDebug("static_watcher_started", slog.String("folder", folderPath))
	s.ready.markReady()

	for {
		select {
		case <-s.done:
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
						s.logError("static_created_folder_walk_failed", err, slog.String("folder", event.Name))
					}
				}
			}

			do(event)
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			s.logError("static_watcher_error", err, slog.String("folder", folderPath))
		}
	}
}

func (s *StaticFileServer) logDebug(msg string, attrs ...slog.Attr) {
	if s.logger == nil {
		return
	}
	s.logger.LogAttrs(context.Background(), slog.LevelDebug, msg, attrs...)
}

func (s *StaticFileServer) logError(msg string, err error, attrs ...slog.Attr) {
	if s.logger == nil {
		return
	}
	attrs = append(attrs, slog.Any("error", err))
	s.logger.LogAttrs(context.Background(), slog.LevelError, msg, attrs...)
}

func (s *StaticFileServer) reloadMessage() string {
	switch s.reloadPolicy {
	case StaticReloadDisabled:
		return ""
	case StaticReloadPage:
		return "reload"
	case StaticReloadDefault, StaticReloadAssets:
		return "reload_assets"
	default:
		return "reload_assets"
	}
}

func (s *StaticFileServer) reloadDecision(events []fsnotify.Event) staticReloadDecision {
	if s.reloadPolicy == StaticReloadDisabled {
		return staticReloadDecision{noop: true}
	}

	paths, classifiedMessage := s.classifyStaticEvents(events)
	if len(paths) == 0 {
		return staticReloadDecision{noop: true}
	}

	message := classifiedMessage
	switch s.reloadPolicy {
	case StaticReloadAssets:
		message = "reload_assets"
	case StaticReloadPage:
		message = "reload"
	case StaticReloadDefault:
	default:
		if message == "" {
			message = "reload"
		}
	}

	return staticReloadDecision{
		message: message,
		paths:   paths,
	}
}

func (s *StaticFileServer) classifyStaticEvents(events []fsnotify.Event) ([]string, string) {
	paths := map[string]struct{}{}
	message := "reload_assets"

	for _, event := range events {
		if staticEventIgnored(event) {
			continue
		}

		requestPath, ok := s.staticRequestPath(event.Name)
		if !ok {
			continue
		}
		paths[requestPath] = struct{}{}

		if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 || staticAssetReloadMessage(event.Name) == "reload" {
			message = "reload"
		}
	}

	return sortedKeys(paths), message
}

func staticEventIgnored(event fsnotify.Event) bool {
	if event.Name == "" {
		return true
	}
	if isNoisePath(event.Name) {
		return true
	}
	return event.Op != 0 && event.Op&^fsnotify.Chmod == 0
}

func staticAssetReloadMessage(file string) string {
	if strings.EqualFold(filepath.Ext(file), ".css") {
		return "reload_assets"
	}
	return "reload"
}

func (s *StaticFileServer) staticRequestPath(file string) (string, bool) {
	rootAbs, err := filepath.Abs(filepath.Clean(s.folder))
	if err != nil {
		return "", false
	}
	fileAbs, err := filepath.Abs(filepath.Clean(file))
	if err != nil {
		return "", false
	}
	if !isPathWithinRoot(rootAbs, fileAbs) {
		return "", false
	}

	rel, err := filepath.Rel(rootAbs, fileAbs)
	if err != nil || rel == "." {
		return "", false
	}

	prefix := s.staticRequestPrefix()
	return path.Join(prefix, filepath.ToSlash(rel)), true
}

func (s *StaticFileServer) staticRequestPrefix() string {
	if s.stripPrefix == "" {
		return "/"
	}
	prefix := s.stripPrefix
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	return path.Clean(prefix)
}
