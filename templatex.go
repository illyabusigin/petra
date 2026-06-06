package petra

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/illyabusigin/petra/tmplfunc"
)

// ParseError represents an error which can occur when trying to parse a template.
type ParseError struct {
	Path string
	Err  error
}

func (e ParseError) Error() string {
	return fmt.Sprintf(`error while parsing "%v": %v`, e.Path, e.Err)
}

func (e ParseError) Unwrap() error {
	return e.Err
}

// NotFoundError represents an error which can occur when trying to execute a template,
// which does not exist.
type NotFoundError struct {
	Template string
}

func (e NotFoundError) Error() string {
	return "template not found: " + e.Template
}

// ExecuteError represents an error which can occur while trying to execute a template.
type ExecuteError struct {
	Template string
	Err      error
}

func (e ExecuteError) Error() string {
	return fmt.Sprintf(`error executing template "%v": %v`, e.Template, e.Err)
}

func (e ExecuteError) Unwrap() error {
	return e.Err
}

// New creates a new Template with sane default values for directories like:
// templates/
//
//	layout.html
//
//	includes/
//	  header.html
//	  footer.html
//
//	profile/
//	  view.html
//	  edit.html
func New() *Template {
	return NewWithOptions(Options{})
}

// Options contains Template construction settings.
type Options struct {
	Layout         string
	IncludeDir     string
	PageExtensions []string
	FuncMap        template.FuncMap
	Plugins        Plugins
	ReloadObserver ReloadObserver
	Logger         *slog.Logger
}

// NewWithOptions creates a new Template with defaults applied before options.
func NewWithOptions(opts Options) *Template {
	t := &Template{
		Layout:     "layout.html",
		IncludeDir: "includes",
	}

	if opts.Layout != "" {
		t.Layout = opts.Layout
	}
	if opts.IncludeDir != "" {
		t.IncludeDir = opts.IncludeDir
	}
	t.PageExtensions = append([]string{}, opts.PageExtensions...)
	t.FuncMap = opts.FuncMap
	t.Plugins = opts.Plugins
	t.ReloadObserver = opts.ReloadObserver
	t.Logger = opts.Logger

	return t
}

// Template represents a container for multiple templates parsed from a directory
type Template struct {
	// Layout specifies the filename of the layout files in a directory
	// Most commonly: "layout.html" or "base.html"
	Layout string

	// IncludeDir specifies the directory name where partial templates can be found
	// Most commonly: "includes", "include" or "inc"
	IncludeDir string

	// PageExtensions optionally limits page template discovery to file
	// extensions such as ".html". When empty, Petra treats every non-layout
	// file outside component directories as a page template.
	PageExtensions []string

	// FuncMap is a map of functions, given to the templates while parsing
	FuncMap template.FuncMap

	// Plugins is list of plugins, given to templates
	Plugins Plugins

	// ReloadObserver receives structured reload diagnostics when Reload is used.
	ReloadObserver ReloadObserver

	// Logger enables internal parse and reload metrics when set. Petra is quiet
	// by default; pass a slog logger to turn these logs on.
	Logger *slog.Logger

	mu sync.RWMutex

	// templates is a map of template identifiers to executable templates
	templates map[string]*template.Template

	// used for Exec
	components *template.Template

	graph   *templateGraph
	rootDir string
	source  fs.FS
}

// ParseDir parses all templates inside a given directory
func (t *Template) ParseDir(dir string) error {
	start := time.Now()
	root, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		t.logError("parse_dir_failed", err, slog.String("dir", dir), slog.Duration("duration", time.Since(start)))
		return err
	}

	source := os.DirFS(root)
	templates, components, graph, err := parseFS(source, ".", t.IncludeDir, t.Layout, t.PageExtensions, t.FuncMap, t.Plugins)
	if err != nil {
		err = newDebugError(DebugErrorInfo{
			Kind:      DebugErrorKindParse,
			Operation: "ParseDir",
			Path:      root,
		}, err)
		t.logError("parse_dir_failed", err, slog.String("dir", root), slog.Duration("duration", time.Since(start)))
		return err
	}

	t.swap(templates, components, graph, root, source)
	t.logDebug(
		"parse_dir_complete",
		slog.String("dir", root),
		slog.Duration("duration", time.Since(start)),
		slog.Int("pages", len(graph.pageIDs)),
		slog.Int("component_dirs", len(graph.componentDirList)),
		slog.Int("templates", len(templates)),
		slog.Bool("reloadable", true),
	)
	return nil
}

// ParseFS parses all templates inside a given fs.FS
func (t *Template) ParseFS(files fs.FS, dir string) error {
	start := time.Now()
	templates, components, graph, err := parseFS(files, dir, t.IncludeDir, t.Layout, t.PageExtensions, t.FuncMap, t.Plugins)
	if err != nil {
		err = newDebugError(DebugErrorInfo{
			Kind:      DebugErrorKindParse,
			Operation: "ParseFS",
			Path:      dir,
		}, err)
		t.logError("parse_fs_failed", err, slog.String("dir", dir), slog.Duration("duration", time.Since(start)))
		return err
	}

	t.swap(templates, components, graph, "", files)
	t.logDebug(
		"parse_fs_complete",
		slog.String("dir", dir),
		slog.Duration("duration", time.Since(start)),
		slog.Int("pages", len(graph.pageIDs)),
		slog.Int("component_dirs", len(graph.componentDirList)),
		slog.Int("templates", len(templates)),
		slog.Bool("reloadable", false),
	)
	return nil
}

func (t *Template) swap(templates map[string]*template.Template, components *template.Template, graph *templateGraph, rootDir string, source fs.FS) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.templates = templates
	t.components = components
	t.graph = graph
	t.rootDir = rootDir
	t.source = source
}

// Exec executes an inline template string against the parsed component template set.
func (t *Template) Exec(w io.Writer, tmplt string, data any) error {
	t.mu.RLock()
	components := t.components
	t.mu.RUnlock()

	if components == nil {
		return newDebugError(DebugErrorInfo{
			Kind:      DebugErrorKindExecute,
			Operation: "Exec",
			Component: "components",
		}, NotFoundError{Template: "components"})
	}

	clone, err := components.Clone()
	if err != nil {
		return newDebugError(DebugErrorInfo{
			Kind:      DebugErrorKindExecute,
			Operation: "Exec",
			Component: "components",
		}, err)
	}

	inline := clone.New(tmplt)
	if err := tmplfunc.Parse(inline, tmplt); err != nil {
		return newDebugError(DebugErrorInfo{
			Kind:           DebugErrorKindParse,
			Operation:      "Exec",
			DependencyRole: DebugDependencyRoleInline,
			Path:           "inline",
		}, err)
	}

	if err := inline.Execute(w, data); err != nil {
		return newDebugError(DebugErrorInfo{
			Kind:           DebugErrorKindExecute,
			Operation:      "Exec",
			DependencyRole: DebugDependencyRoleInline,
			Path:           "inline",
		}, err)
	}
	return nil
}

// ExecuteTemplate executes a template by its name to a io.Writer with any given data
func (t *Template) ExecuteTemplate(w io.Writer, name string, data any) error {
	t.mu.RLock()
	tmpl, ok := t.templates[name]
	var info templateInfo
	hasInfo := false
	source := t.source
	if t.graph != nil {
		info, hasInfo = t.graph.pagesByID[name]
	}
	t.mu.RUnlock()

	if !ok {
		return newDebugError(DebugErrorInfo{
			Kind:      DebugErrorKindExecute,
			Operation: "ExecuteTemplate",
			Page:      name,
		}, NotFoundError{Template: name})
	}
	if err := tmpl.Execute(w, data); err != nil {
		debugInfo := DebugErrorInfo{
			Kind:      DebugErrorKindExecute,
			Operation: "ExecuteTemplate",
			Page:      name,
		}
		if hasInfo {
			debugInfo.Path = info.path
			debugInfo.Files = append([]string{}, info.files...)
		}
		wrapped := newDebugError(debugInfo, ExecuteError{Template: name, Err: err})
		if hasInfo {
			wrapped = withDebugSource(wrapped, source, append(append([]string{}, info.files...), info.componentFiles...), nil)
			wrapped = withDebugContext(wrapped, func(debugInfo *DebugErrorInfo) {
				applyTemplateInfoDebugRole(debugInfo, info)
			})
		}
		return wrapped
	}
	return nil
}

func (t *Template) logDebug(msg string, attrs ...slog.Attr) {
	if t.Logger == nil {
		return
	}
	t.Logger.LogAttrs(context.Background(), slog.LevelDebug, msg, attrs...)
}

func (t *Template) logError(msg string, err error, attrs ...slog.Attr) {
	if t.Logger == nil {
		return
	}
	attrs = append(attrs, slog.Any("error", err))
	t.Logger.LogAttrs(context.Background(), slog.LevelError, msg, attrs...)
}

func (t *Template) logEnabled(level slog.Level) bool {
	return t != nil && t.Logger != nil && t.Logger.Enabled(context.Background(), level)
}

func buildFuncMap(tmplt *template.Template, funcMap template.FuncMap, plugins Plugins) (*template.Template, error) {
	componentMounts := collectComponentMounts(plugins)
	requiredPlugins, err := collectComponentRequiredPlugins(componentMounts)
	if err != nil {
		return nil, err
	}

	m := template.FuncMap{}
	for key, f := range funcMap {
		m[key] = f
	}

	for _, p := range requiredPlugins {
		funcs, err := p.Funcs()
		if err != nil {
			return nil, err
		}

		for key, f := range funcs {
			m[key] = f
		}
	}

	for _, p := range plugins {
		if _, ok := p.(componentMountProvider); ok {
			continue
		}

		funcs, err := p.Funcs()
		if err != nil {
			return nil, err
		}

		for key, f := range funcs {
			m[key] = f
		}
	}

	populated := tmplt.Funcs(m)

	for _, p := range requiredPlugins {
		if err := p.Apply(populated); err != nil {
			return nil, err
		}
	}

	for _, p := range plugins {
		if _, ok := p.(componentMountProvider); ok {
			continue
		}

		if err := p.Apply(populated); err != nil {
			return nil, err
		}
	}

	if err := applyComponentMounts(populated, componentMounts); err != nil {
		return nil, err
	}

	return populated, nil
}

const pathSeparator = "/"

// parseDir builds templates inside a given directory
func parseFS(files fs.FS, dir, includeDir, layout string, pageExtensions []string, funcMap template.FuncMap, plugins Plugins) (map[string]*template.Template, *template.Template, *templateGraph, error) {
	templates := map[string]*template.Template{}

	// Collect template parsing information of the given directory
	graph, err := buildGraph(files, dir, includeDir, layout, pageExtensions)
	if err != nil {
		return nil, nil, nil, err
	}

	for _, pageID := range graph.pageIDs {
		info := graph.pagesByID[pageID]
		t, err := parsePage(files, info, layout, funcMap, plugins)
		if err != nil {
			return nil, nil, nil, err
		}

		// Add the parsed template to the template map
		templates[info.id] = t
	}

	componentTemplate, err := parseComponents(files, graph.componentDirList, funcMap, plugins)
	if err != nil {
		return nil, nil, nil, err
	}

	return templates, componentTemplate, graph, nil
}

// templateInfo contains all template information necessary to parse a
// final template with its dependencies (layout templates, include templates)
// It also contains an identifier for the resulting template to execute
type templateInfo struct {
	id             string
	path           string
	includes       []string
	files          []string
	componentFiles []string
}

func applyTemplateInfoDebugRole(debugInfo *DebugErrorInfo, info templateInfo) {
	if debugInfo == nil {
		return
	}

	if debugInfo.Source != nil {
		sourcePath := pathClean(debugInfo.Source.Path)
		debugInfo.Path = sourcePath
		debugInfo.DependencyRole = info.roleForFile(sourcePath)
	}

	if debugInfo.DependencyRole == "" && debugInfo.Path != "" {
		debugInfo.DependencyRole = info.roleForFile(debugInfo.Path)
	}

	switch debugInfo.DependencyRole {
	case DebugDependencyRoleLayout:
		debugInfo.Layout = debugInfo.Path
	case DebugDependencyRoleComponent:
		if debugInfo.Component == "" {
			debugInfo.Component = debugInfo.Path
		}
	case "":
		if debugInfo.Path != "" && pathClean(debugInfo.Path) == pathClean(info.path) {
			debugInfo.DependencyRole = DebugDependencyRolePage
		}
	}
}

func (info templateInfo) roleForFile(file string) DebugDependencyRole {
	file = pathClean(file)
	if file == pathClean(info.path) {
		return DebugDependencyRolePage
	}
	for _, candidate := range info.files {
		candidate = pathClean(candidate)
		if candidate == file && candidate != pathClean(info.path) {
			return DebugDependencyRoleLayout
		}
	}
	for _, candidate := range info.componentFiles {
		if pathClean(candidate) == file {
			return DebugDependencyRoleComponent
		}
	}
	for _, includeDir := range info.includes {
		if isWithinDir(file, includeDir) {
			return DebugDependencyRoleComponent
		}
	}
	return ""
}

func parsePage(files fs.FS, info templateInfo, layout string, funcMap template.FuncMap, plugins Plugins) (*template.Template, error) {
	// Create a new empty layout with the name of the layout file
	t := template.New(layout)

	t, err := buildFuncMap(t, funcMap, plugins)
	if err != nil {
		return nil, newDebugErrorWithSource(files, nil, DebugErrorInfo{
			Kind:           DebugErrorKindParse,
			Operation:      "parse page",
			DependencyRole: DebugDependencyRoleFuncMap,
			Page:           info.id,
			Path:           "funcMap",
		}, ParseError{Path: "funcMap", Err: err})
	}

	// Parse component templates from matching include directories.
	for _, f := range info.includes {
		gf, err := componentTemplateFiles(files, f)
		if err != nil {
			return nil, withDebugContext(err, func(debugInfo *DebugErrorInfo) {
				debugInfo.Kind = DebugErrorKindParse
				debugInfo.Operation = "parse page components"
				debugInfo.DependencyRole = DebugDependencyRoleComponent
				debugInfo.Page = info.id
				debugInfo.Path = f
			})
		}
		err = tmplfunc.ParseFilesFS(t, files, gf...)
		if err != nil {
			wrapped := newDebugErrorWithSource(files, gf, DebugErrorInfo{
				Kind:           DebugErrorKindParse,
				Operation:      "parse page components",
				DependencyRole: DebugDependencyRoleComponent,
				Page:           info.id,
				Path:           f,
				Files:          append([]string{}, gf...),
			}, ParseError{Path: f, Err: err})
			return nil, withDebugContext(wrapped, func(debugInfo *DebugErrorInfo) {
				if debugInfo.Source != nil {
					debugInfo.Path = debugInfo.Source.Path
				}
			})
		}
	}

	// Parse the rest of the templates
	if err := tmplfunc.ParseAssociatedFilesFS(t, files, info.files...); err != nil {
		wrapped := newDebugErrorWithSource(files, info.files, DebugErrorInfo{
			Kind:      DebugErrorKindParse,
			Operation: "parse page",
			Page:      info.id,
			Files:     append([]string{}, info.files...),
		}, ParseError{Path: fmt.Sprintf("%v", info.files), Err: err})
		return nil, withDebugContext(wrapped, func(debugInfo *DebugErrorInfo) {
			applyTemplateInfoDebugRole(debugInfo, info)
		})
	}

	return t, nil
}

func parseComponents(files fs.FS, componentDirs []string, funcMap template.FuncMap, plugins Plugins) (*template.Template, error) {
	componentTemplate := template.New("")
	componentTemplate, err := buildFuncMap(componentTemplate, funcMap, plugins)
	if err != nil {
		return nil, newDebugErrorWithSource(files, nil, DebugErrorInfo{
			Kind:           DebugErrorKindParse,
			Operation:      "parse components",
			DependencyRole: DebugDependencyRoleFuncMap,
			Path:           "funcMap",
		}, fmt.Errorf("componentTemplate.buildFuncMap error: %w", err))
	}

	for _, compDir := range componentDirs {
		componentFiles, err := componentTemplateFiles(files, compDir)
		if err != nil {
			return nil, withDebugContext(err, func(debugInfo *DebugErrorInfo) {
				debugInfo.Kind = DebugErrorKindParse
				debugInfo.Operation = "parse components"
				debugInfo.DependencyRole = DebugDependencyRoleComponent
				debugInfo.Path = compDir
			})
		}
		if err := tmplfunc.ParseFilesFS(componentTemplate, files, componentFiles...); err != nil {
			wrapped := newDebugErrorWithSource(files, componentFiles, DebugErrorInfo{
				Kind:           DebugErrorKindParse,
				Operation:      "parse components",
				DependencyRole: DebugDependencyRoleComponent,
				Path:           compDir,
				Files:          append([]string{}, componentFiles...),
			}, fmt.Errorf("componentTemplate.ParseFilesFS %q error: %w", compDir, err))
			return nil, withDebugContext(wrapped, func(debugInfo *DebugErrorInfo) {
				if debugInfo.Source != nil {
					debugInfo.Path = debugInfo.Source.Path
				}
			})
		}
	}

	return componentTemplate, nil
}

type fileKind int

const (
	fileKindUnknown fileKind = iota
	fileKindPage
	fileKindLayout
	fileKindComponent
)

type templateGraph struct {
	root       string
	includeDir string
	layout     string

	pageIDs      []string
	pagesByID    map[string]templateInfo
	pageIDByFile map[string]string

	layoutsByFile      map[string]map[string]struct{}
	includesByDir      map[string]map[string]struct{}
	componentDirs      map[string]struct{}
	componentDirByFile map[string]string
	componentDirList   []string

	allFiles map[string]fileKind
}

func (g *templateGraph) roleForFile(file string) DebugDependencyRole {
	if g == nil {
		return ""
	}

	switch g.allFiles[pathClean(file)] {
	case fileKindPage:
		return DebugDependencyRolePage
	case fileKindLayout:
		return DebugDependencyRoleLayout
	case fileKindComponent:
		return DebugDependencyRoleComponent
	default:
		return ""
	}
}

func buildGraph(files fs.FS, dir, includeDir, layout string, pageExtensions []string) (*templateGraph, error) {
	templateInfos, err := findTemplates(files, dir, includeDir, layout, pageExtensions)
	if err != nil {
		return nil, err
	}

	graph := &templateGraph{
		root:               path.Clean(dir),
		includeDir:         path.Clean(includeDir),
		layout:             layout,
		pagesByID:          map[string]templateInfo{},
		pageIDByFile:       map[string]string{},
		layoutsByFile:      map[string]map[string]struct{}{},
		includesByDir:      map[string]map[string]struct{}{},
		componentDirs:      map[string]struct{}{},
		componentDirByFile: map[string]string{},
		allFiles:           map[string]fileKind{},
	}

	for _, info := range templateInfos {
		graph.pageIDs = append(graph.pageIDs, info.id)
		graph.pagesByID[info.id] = info
		graph.pageIDByFile[info.path] = info.id
		graph.allFiles[info.path] = fileKindPage

		for _, f := range info.files {
			if f == info.path {
				continue
			}
			addGraphSet(graph.layoutsByFile, f, info.id)
			graph.allFiles[f] = fileKindLayout
		}

		for _, includeDir := range info.includes {
			addGraphSet(graph.includesByDir, includeDir, info.id)
			graph.componentDirs[includeDir] = struct{}{}
		}
	}

	graph.componentDirList = make([]string, 0, len(graph.componentDirs))
	for dir := range graph.componentDirs {
		graph.componentDirList = append(graph.componentDirList, dir)
	}
	sort.Strings(graph.componentDirList)

	for _, dir := range graph.componentDirList {
		componentFiles, err := componentTemplateFiles(files, dir)
		if err != nil {
			return nil, err
		}
		for _, componentFile := range componentFiles {
			graph.allFiles[componentFile] = fileKindComponent
			graph.componentDirByFile[componentFile] = dir
		}
	}

	return graph, nil
}

func addGraphSet(index map[string]map[string]struct{}, key, pageID string) {
	if index[key] == nil {
		index[key] = map[string]struct{}{}
	}
	index[key][pageID] = struct{}{}
}

// fileTemplates returns a list of all executable templates
// with their respective layout dependencies and include templates
func findTemplates(files fs.FS, dir, includeDir, layout string, pageExtensions []string) ([]templateInfo, error) {
	// Cleans trailing slashs from directories
	dir = path.Clean(filepath.ToSlash(dir))
	includeDir = path.Clean(filepath.ToSlash(includeDir))
	allowedPageExtensions := normalizePageExtensions(pageExtensions)

	// Slices to hold all found files and directories
	includeDirs := []string{}
	layouts := []string{}
	templates := []string{}

	// walkfn finds all files and directories inside of dir
	walkfn := func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		cleanPath := pathClean(path)
		if info.IsDir() {
			// Check if the found directory is an include directory
			if pathBase(cleanPath) == includeDir && !isWithinAnyDir(cleanPath, includeDirs) {
				includeDirs = append(includeDirs, cleanPath)
			}
			return nil
		}

		// Skip all templates found in component directories.
		if isNoisePath(cleanPath) || isWithinAnyDir(cleanPath, includeDirs) {
			return nil
		}

		// Determine if the template is a base/layout template or normal template
		if pathBase(cleanPath) == layout {
			layouts = append(layouts, cleanPath)
		} else if pageExtensionAllowed(cleanPath, allowedPageExtensions) {
			templates = append(templates, cleanPath)
		}

		return nil
	}
	if err := fs.WalkDir(files, dir, walkfn); err != nil {
		return nil, ParseError{Path: dir, Err: err}
	}

	// Sort all base/layout templates by their directory depth (shallow to deep)
	sort.Slice(layouts, func(i, j int) bool {
		return len(layouts[i]) < len(layouts[j])
	})

	// Sort all include directoSkipies by their directory depth (shallow to deep)
	sort.Slice(includeDirs, func(i, j int) bool {
		return len(includeDirs[i]) < len(includeDirs[j])
	})

	// For each found normal template, build a list of dependencies to parse
	templateInfos := []templateInfo{}
	componentFilesByDir := map[string][]string{}
	for _, t := range templates {
		parseFiles := []string{}
		includes := []string{}
		componentFiles := []string{}

		// Add all include directories which lie in the same directory hirachy
		for _, i := range includeDirs {
			if isWithinDir(t, pathDir(i)) || pathDir(i) == "." && pathBase(i) != ".DS_Store" {
				includes = append(includes, i)
				found, ok := componentFilesByDir[i]
				if !ok {
					var err error
					found, err = componentTemplateFiles(files, i)
					if err != nil {
						return nil, err
					}
					componentFilesByDir[i] = found
				}
				componentFiles = append(componentFiles, found...)
			}
		}

		// Add all base/layout templates which lie in the same directory hirachy
		for _, l := range layouts {
			if isWithinDir(t, pathDir(l)) || pathDir(l) == "." && pathBase(l) != ".DS_Store" {
				parseFiles = append(parseFiles, l)
			}
		}

		// Add the final template as the last entry
		parseFiles = append(parseFiles, t)

		// Build the template identifier based on the path of the final template
		// e.g. <dir>/profile/view.html -> profile/view
		id := strings.TrimPrefix(t, dir+string(pathSeparator))
		id = strings.TrimSuffix(id, path.Ext(id))

		templateInfos = append(templateInfos, templateInfo{
			id:             id,
			path:           t,
			includes:       includes,
			files:          parseFiles,
			componentFiles: componentFiles,
		})
	}

	return templateInfos, nil
}

func pathClean(value string) string {
	return path.Clean(filepath.ToSlash(value))
}

func pathDir(value string) string {
	return path.Dir(pathClean(value))
}

func pathBase(value string) string {
	return path.Base(pathClean(value))
}

func isWithinDir(file, dir string) bool {
	file = pathClean(file)
	dir = pathClean(dir)
	if dir == "." {
		return true
	}
	return file == dir || strings.HasPrefix(file, dir+string(pathSeparator))
}

func isWithinAnyDir(file string, dirs []string) bool {
	for _, dir := range dirs {
		if isWithinDir(file, dir) {
			return true
		}
	}
	return false
}

func componentTemplateFiles(files fs.FS, dir string) ([]string, error) {
	out := []string{}
	err := fs.WalkDir(files, dir, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		cleanPath := pathClean(path)
		if isNoisePath(cleanPath) {
			return nil
		}

		out = append(out, cleanPath)
		return nil
	})
	if err != nil {
		return nil, ParseError{Path: dir, Err: err}
	}
	sort.Strings(out)
	return out, nil
}

func normalizePageExtensions(extensions []string) map[string]struct{} {
	if len(extensions) == 0 {
		return nil
	}

	out := map[string]struct{}{}
	for _, ext := range extensions {
		if ext == "" {
			out[""] = struct{}{}
			continue
		}
		ext = strings.ToLower(ext)
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		out[ext] = struct{}{}
	}
	return out
}

func pageExtensionAllowed(file string, allowed map[string]struct{}) bool {
	if len(allowed) == 0 {
		return true
	}
	_, ok := allowed[strings.ToLower(path.Ext(file))]
	return ok
}
