package petra

import (
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ReloadOp uint8

const (
	ReloadWrite ReloadOp = 1 << iota
	ReloadCreate
	ReloadRemove
	ReloadRename
	ReloadChmod
)

type ReloadFileEvent struct {
	Path string
	Op   ReloadOp
}

type ReloadResult struct {
	FullReload        bool
	RebuiltPages      []string
	RebuiltComponents bool
	ChangedPaths      []string
	Duration          time.Duration
	FallbackReason    string
	Noop              bool
}

type ReloadEvent struct {
	ChangedPaths      []string
	RebuiltPages      []string
	RebuiltComponents bool
	FullReload        bool
	FallbackReason    string
	Noop              bool
	Duration          time.Duration
	Err               error
}

type ReloadObserver interface {
	ObserveReload(ReloadEvent)
}

type reloadPlan struct {
	fullReload        bool
	rebuildComponents bool
	fallbackReason    string
	pageIDs           map[string]struct{}
	changedPaths      []string
	noop              bool
}

// Reload rebuilds the parts of a mutable template directory affected by the
// supplied file events. It requires a prior successful ParseDir call.
func (t *Template) Reload(events ...ReloadFileEvent) (result ReloadResult, err error) {
	start := time.Now()
	defer func() {
		result.Duration = time.Since(start)
		t.observeReload(result, err)
	}()

	t.mu.RLock()
	rootDir := t.rootDir
	graph := t.graph
	oldTemplates := maps.Clone(t.templates)
	oldComponents := t.components
	t.mu.RUnlock()

	if rootDir == "" {
		err = newDebugError(DebugErrorInfo{
			Kind:      DebugErrorKindReload,
			Operation: "Reload",
		}, fmt.Errorf("petra reload requires ParseDir"))
		return result, err
	}

	if graph == nil {
		result.FallbackReason = "missing template graph"
		return t.fullReload(rootDir, start, result)
	}

	plan, planErr := graph.planReload(rootDir, events)
	if planErr != nil {
		err = withReloadDebugContext(planErr, result, graph, nil, nil)
		return result, err
	}

	result.ChangedPaths = plan.changedPaths
	result.FallbackReason = plan.fallbackReason

	if plan.noop {
		result.Noop = true
		return result, nil
	}

	if plan.fullReload {
		result.FullReload = true
		return t.fullReload(rootDir, start, result)
	}

	files := os.DirFS(rootDir)
	nextTemplates := maps.Clone(oldTemplates)
	nextComponents := oldComponents

	pageIDs := sortedSet(plan.pageIDs)
	for _, pageID := range pageIDs {
		info, ok := graph.pagesByID[pageID]
		if !ok {
			result.FullReload = true
			result.FallbackReason = "affected page missing from graph"
			return t.fullReload(rootDir, start, result)
		}

		tmpl, parseErr := parsePage(files, info, t.Layout, t.FuncMap, t.Plugins)
		if parseErr != nil {
			err = withReloadDebugContext(parseErr, result, graph, pageIDs, files)
			return result, err
		}
		nextTemplates[pageID] = tmpl
	}

	if plan.rebuildComponents {
		components, parseErr := parseComponents(files, graph.componentDirList, t.FuncMap, t.Plugins)
		if parseErr != nil {
			err = withReloadDebugContext(parseErr, result, graph, pageIDs, files)
			return result, err
		}
		nextComponents = components
		result.RebuiltComponents = true
	}

	t.mu.Lock()
	t.templates = nextTemplates
	t.components = nextComponents
	t.mu.Unlock()

	result.RebuiltPages = pageIDs
	return result, nil
}

// ReloadDir treats every supplied path as a write event. It is intended for
// tests and manual reload callers; HotReloadController uses Reload.
func (t *Template) ReloadDir(paths ...string) (ReloadResult, error) {
	events := make([]ReloadFileEvent, 0, len(paths))
	for _, path := range paths {
		events = append(events, ReloadFileEvent{Path: path, Op: ReloadWrite})
	}
	return t.Reload(events...)
}

func (t *Template) fullReload(rootDir string, start time.Time, result ReloadResult) (ReloadResult, error) {
	result.FullReload = true

	source := os.DirFS(rootDir)
	templates, components, graph, err := parseFS(source, ".", t.IncludeDir, t.Layout, t.PageExtensions, t.FuncMap, t.Plugins)
	if err != nil {
		return result, withReloadDebugContext(err, result, nil, nil, source)
	}

	t.swap(templates, components, graph, rootDir, source)

	result.RebuiltPages = append([]string{}, graph.pageIDs...)
	result.RebuiltComponents = true
	result.Duration = time.Since(start)
	return result, nil
}

func withReloadDebugContext(err error, result ReloadResult, graph *templateGraph, affectedPages []string, source fs.FS) error {
	wrapped := withDebugContext(err, func(info *DebugErrorInfo) {
		if info.Kind == "" || info.Kind == DebugErrorKindUnknown {
			info.Kind = DebugErrorKindReload
		}
		info.Operation = "Reload"
		if len(info.ChangedPaths) == 0 {
			info.ChangedPaths = append([]string{}, result.ChangedPaths...)
		}
		if len(info.AffectedPages) == 0 {
			info.AffectedPages = append([]string{}, affectedPages...)
		}
		if info.FallbackReason == "" {
			info.FallbackReason = result.FallbackReason
		}
		if graph != nil && len(result.ChangedPaths) == 1 {
			role := graph.roleForFile(result.ChangedPaths[0])
			if role != "" {
				info.DependencyRole = role
				info.Path = result.ChangedPaths[0]
				switch role {
				case DebugDependencyRoleLayout:
					info.Layout = result.ChangedPaths[0]
				case DebugDependencyRoleComponent:
					if info.Component == "" {
						info.Component = result.ChangedPaths[0]
					}
				}
			}
		}
	})
	if source != nil {
		wrapped = withDebugSource(wrapped, source, debugSourceCandidatesFromError(wrapped), result.ChangedPaths)
	}
	return wrapped
}

func (t *Template) observeReload(result ReloadResult, err error) {
	if err != nil {
		if t.logEnabled(slog.LevelError) {
			t.logError("reload_failed", err, reloadLogAttrs(result)...)
		}
	} else if t.logEnabled(slog.LevelDebug) {
		t.logDebug("reload_complete", reloadLogAttrs(result)...)
	}

	if t.ReloadObserver == nil {
		return
	}

	t.ReloadObserver.ObserveReload(ReloadEvent{
		ChangedPaths:      append([]string{}, result.ChangedPaths...),
		RebuiltPages:      append([]string{}, result.RebuiltPages...),
		RebuiltComponents: result.RebuiltComponents,
		FullReload:        result.FullReload,
		FallbackReason:    result.FallbackReason,
		Noop:              result.Noop,
		Duration:          result.Duration,
		Err:               err,
	})
}

func reloadLogAttrs(result ReloadResult) []slog.Attr {
	attrs := []slog.Attr{
		slog.Duration("duration", result.Duration),
		slog.Bool("full_reload", result.FullReload),
		slog.Bool("rebuilt_components", result.RebuiltComponents),
		slog.Bool("noop", result.Noop),
		slog.Int("changed_path_count", len(result.ChangedPaths)),
		slog.Int("rebuilt_page_count", len(result.RebuiltPages)),
		slog.Any("changed_paths", append([]string{}, result.ChangedPaths...)),
		slog.Any("rebuilt_pages", append([]string{}, result.RebuiltPages...)),
		slog.String("fallback_reason", result.FallbackReason),
	}
	return attrs
}

func (g *templateGraph) planReload(rootDir string, events []ReloadFileEvent) (reloadPlan, error) {
	plan := reloadPlan{
		pageIDs: map[string]struct{}{},
	}

	if len(events) == 0 {
		plan.noop = true
		return plan, nil
	}

	changed := map[string]struct{}{}

	for _, event := range events {
		if event.Path == "" {
			continue
		}

		rel, err := normalizeTemplatePath(rootDir, event.Path)
		if err != nil {
			if isNoisePath(event.Path) {
				continue
			}
			plan.fullReload = true
			plan.fallbackReason = "path outside template root"
			continue
		}

		if isNoisePath(rel) {
			continue
		}

		if event.Op == 0 {
			event.Op = ReloadWrite
		}

		if event.Op&(ReloadRemove|ReloadRename) != 0 {
			plan.fullReload = true
			plan.fallbackReason = "structural file event"
			changed[rel] = struct{}{}
			continue
		}

		kind, known := g.allFiles[rel]
		if event.Op&ReloadCreate != 0 && !known {
			plan.fullReload = true
			plan.fallbackReason = "new template path"
			changed[rel] = struct{}{}
			continue
		}

		if event.Op == ReloadChmod {
			if known {
				continue
			}
			plan.fullReload = true
			plan.fallbackReason = "unknown chmod path"
			changed[rel] = struct{}{}
			continue
		}

		if !known {
			plan.fullReload = true
			plan.fallbackReason = "unknown template path"
			changed[rel] = struct{}{}
			continue
		}

		changed[rel] = struct{}{}

		switch kind {
		case fileKindPage:
			if pageID, ok := g.pageIDByFile[rel]; ok {
				plan.pageIDs[pageID] = struct{}{}
			} else {
				plan.fullReload = true
				plan.fallbackReason = "page missing from graph"
			}
		case fileKindLayout:
			for pageID := range g.layoutsByFile[rel] {
				plan.pageIDs[pageID] = struct{}{}
			}
		case fileKindComponent:
			componentDir, ok := g.componentDirByFile[rel]
			if !ok {
				plan.fullReload = true
				plan.fallbackReason = "component missing from graph"
				break
			}
			plan.rebuildComponents = true
			for pageID := range g.includesByDir[componentDir] {
				plan.pageIDs[pageID] = struct{}{}
			}
		default:
			plan.fullReload = true
			plan.fallbackReason = "unknown file kind"
		}
	}

	plan.changedPaths = sortedKeys(changed)
	if len(plan.changedPaths) == 0 && !plan.fullReload {
		plan.noop = true
	}

	return plan, nil
}

func normalizeTemplatePath(rootDir, value string) (string, error) {
	rootAbs, err := filepath.Abs(filepath.Clean(rootDir))
	if err != nil {
		return "", err
	}

	cleanValue := filepath.Clean(value)
	var candidateAbs string

	if filepath.IsAbs(cleanValue) {
		candidateAbs = cleanValue
	} else {
		fromCWD, err := filepath.Abs(cleanValue)
		if err == nil && isPathWithinRoot(rootAbs, fromCWD) {
			candidateAbs = fromCWD
		} else {
			candidateAbs = filepath.Join(rootAbs, cleanValue)
		}
	}

	candidateAbs, err = filepath.Abs(filepath.Clean(candidateAbs))
	if err != nil {
		return "", err
	}

	if !isPathWithinRoot(rootAbs, candidateAbs) {
		return "", fmt.Errorf("path %q outside template root %q", value, rootDir)
	}

	rel, err := filepath.Rel(rootAbs, candidateAbs)
	if err != nil {
		return "", err
	}
	if rel == "." {
		return "", fmt.Errorf("path %q is template root", value)
	}

	return filepath.ToSlash(rel), nil
}

func isPathWithinRoot(rootAbs, pathAbs string) bool {
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return false
	}
	return rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func isNoisePath(value string) bool {
	base := filepath.Base(value)
	if base == ".DS_Store" {
		return true
	}
	if strings.HasPrefix(base, ".#") {
		return true
	}
	if strings.HasSuffix(base, "~") || strings.HasSuffix(base, ".swp") || strings.HasSuffix(base, ".swo") || strings.HasSuffix(base, ".tmp") {
		return true
	}
	return false
}

func sortedSet(values map[string]struct{}) []string {
	return sortedKeys(values)
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
