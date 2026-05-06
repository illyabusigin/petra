package petra

import (
	"bytes"
	"cmp"
	"errors"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	texttemplate "text/template"

	"github.com/illyabusigin/petra/tmplfunc"
)

// DebugErrorKind describes the broad class of template failure.
type DebugErrorKind string

const (
	// DebugErrorKindUnknown is used when Petra did not capture a more specific kind.
	DebugErrorKindUnknown DebugErrorKind = "unknown"

	// DebugErrorKindParse marks a template parse failure.
	DebugErrorKindParse DebugErrorKind = "parse"

	// DebugErrorKindReload marks a reload failure not tied to a narrower parse error.
	DebugErrorKindReload DebugErrorKind = "reload"

	// DebugErrorKindExecute marks a page or inline template execution failure.
	DebugErrorKindExecute DebugErrorKind = "execute"

	// DebugErrorKindComponent marks a component-style template call failure.
	DebugErrorKindComponent DebugErrorKind = "component"
)

// DebugDependencyRole describes how the failing file participates in a page.
type DebugDependencyRole string

const (
	DebugDependencyRolePage      DebugDependencyRole = "page"
	DebugDependencyRoleLayout    DebugDependencyRole = "layout"
	DebugDependencyRoleComponent DebugDependencyRole = "component"
	DebugDependencyRoleFuncMap   DebugDependencyRole = "funcmap"
	DebugDependencyRoleInline    DebugDependencyRole = "inline"
)

// DebugFrame describes one page or component frame Petra can identify.
type DebugFrame struct {
	Kind string `json:"kind,omitempty"`
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

// DebugLocation is a parsed Go template location when the underlying error
// includes one.
type DebugLocation struct {
	Template string `json:"template,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
}

// DebugSourceLine is one line in a source excerpt.
type DebugSourceLine struct {
	Number    int    `json:"number,omitempty"`
	Text      string `json:"text,omitempty"`
	Highlight bool   `json:"highlight,omitempty"`
}

// DebugSourceExcerpt shows source near the failing template location.
type DebugSourceExcerpt struct {
	Path      string            `json:"path,omitempty"`
	Line      int               `json:"line,omitempty"`
	Column    int               `json:"column,omitempty"`
	StartLine int               `json:"start_line,omitempty"`
	Lines     []DebugSourceLine `json:"lines,omitempty"`
}

// DebugErrorInfo is the structured form used by debug pages and hot reload
// overlays.
type DebugErrorInfo struct {
	Kind           DebugErrorKind      `json:"kind,omitempty"`
	Operation      string              `json:"operation,omitempty"`
	Message        string              `json:"message,omitempty"`
	DependencyRole DebugDependencyRole `json:"dependency_role,omitempty"`
	Page           string              `json:"page,omitempty"`
	Component      string              `json:"component,omitempty"`
	Layout         string              `json:"layout,omitempty"`
	Path           string              `json:"path,omitempty"`
	Files          []string            `json:"files,omitempty"`
	ChangedPaths   []string            `json:"changed_paths,omitempty"`
	AffectedPages  []string            `json:"affected_pages,omitempty"`
	FallbackReason string              `json:"fallback_reason,omitempty"`
	Frames         []DebugFrame        `json:"frames,omitempty"`
	Location       *DebugLocation      `json:"location,omitempty"`
	Source         *DebugSourceExcerpt `json:"source,omitempty"`
	GoStack        string              `json:"go_stack,omitempty"`
}

// DebugError wraps a template failure with structured development diagnostics.
// It unwraps to the original error.
type DebugError struct {
	Info DebugErrorInfo
	Err  error
}

func (e *DebugError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Info.Message
}

func (e *DebugError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// DebugOptions controls development debug-page rendering.
type DebugOptions struct {
	Enabled        bool
	IncludeGoStack bool
	Title          string
	StatusCode     int
}

// DebugInfo extracts Petra debug metadata from err. The boolean is true when
// err contains a *DebugError.
func DebugInfo(err error) (DebugErrorInfo, bool) {
	if err == nil {
		return DebugErrorInfo{}, false
	}

	if debugErr, ok := errors.AsType[*DebugError](err); ok {
		info := debugErr.Info
		if info.Message == "" {
			info.Message = debugErr.Error()
		}
		enrichDebugInfo(&info, err)
		return info, true
	}

	info := DebugErrorInfo{
		Kind:    DebugErrorKindUnknown,
		Message: err.Error(),
	}
	enrichDebugInfo(&info, err)
	return info, false
}

// RenderDebugError writes Petra's development error page when opts.Enabled is
// true. It returns whether it handled the response.
func RenderDebugError(w http.ResponseWriter, r *http.Request, err error, opts DebugOptions) bool {
	if !opts.Enabled || err == nil {
		return false
	}

	info, _ := DebugInfo(err)
	if info.Message == "" {
		info.Message = err.Error()
	}
	if info.Kind == "" {
		info.Kind = DebugErrorKindUnknown
	}
	if opts.IncludeGoStack && info.GoStack == "" {
		info.GoStack = string(debug.Stack())
	}
	if !opts.IncludeGoStack {
		info.GoStack = ""
	}

	status := opts.StatusCode
	if status == 0 {
		status = http.StatusInternalServerError
	}

	view := debugPageView{
		Title:       cmp.Or(opts.Title, "Petra template error"),
		Method:      requestMethod(r),
		Path:        requestPath(r),
		Status:      status,
		Info:        info,
		ShowGoStack: opts.IncludeGoStack && info.GoStack != "",
	}

	var body bytes.Buffer
	if renderErr := debugPageTemplate.Execute(&body, view); renderErr != nil {
		http.Error(w, info.Message, status)
		return true
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
	return true
}

func newDebugError(info DebugErrorInfo, err error) error {
	if err == nil {
		return nil
	}

	if debugErr, ok := errors.AsType[*DebugError](err); ok {
		mergeDebugInfo(&debugErr.Info, info)
		enrichDebugInfo(&debugErr.Info, err)
		return err
	}

	if info.Kind == "" {
		info.Kind = DebugErrorKindUnknown
	}
	if info.Message == "" {
		info.Message = err.Error()
	}
	if info.GoStack == "" {
		info.GoStack = string(debug.Stack())
	}

	enrichDebugInfo(&info, err)
	return &DebugError{Info: info, Err: err}
}

func withDebugContext(err error, update func(*DebugErrorInfo)) error {
	if err == nil {
		return nil
	}

	if debugErr, ok := errors.AsType[*DebugError](err); ok {
		update(&debugErr.Info)
		enrichDebugInfo(&debugErr.Info, err)
		return err
	}

	info := DebugErrorInfo{}
	update(&info)
	return newDebugError(info, err)
}

func mergeDebugInfo(dst *DebugErrorInfo, src DebugErrorInfo) {
	if dst.Kind == "" || dst.Kind == DebugErrorKindUnknown {
		dst.Kind = src.Kind
	}
	if dst.Operation == "" {
		dst.Operation = src.Operation
	}
	if dst.Message == "" {
		dst.Message = src.Message
	}
	if dst.DependencyRole == "" {
		dst.DependencyRole = src.DependencyRole
	}
	if dst.Page == "" {
		dst.Page = src.Page
	}
	if dst.Component == "" {
		dst.Component = src.Component
	}
	if dst.Layout == "" {
		dst.Layout = src.Layout
	}
	if dst.Path == "" {
		dst.Path = src.Path
	}
	if dst.FallbackReason == "" {
		dst.FallbackReason = src.FallbackReason
	}
	if len(dst.Files) == 0 {
		dst.Files = append([]string{}, src.Files...)
	}
	if len(dst.ChangedPaths) == 0 {
		dst.ChangedPaths = append([]string{}, src.ChangedPaths...)
	}
	if len(dst.AffectedPages) == 0 {
		dst.AffectedPages = append([]string{}, src.AffectedPages...)
	}
	if len(dst.Frames) == 0 {
		dst.Frames = append([]DebugFrame{}, src.Frames...)
	}
	if dst.Location == nil && src.Location != nil {
		location := *src.Location
		dst.Location = &location
	}
	if dst.Source == nil && src.Source != nil {
		source := *src.Source
		source.Lines = append([]DebugSourceLine{}, src.Source.Lines...)
		dst.Source = &source
	}
	if dst.GoStack == "" {
		dst.GoStack = src.GoStack
	}
}

func enrichDebugInfo(info *DebugErrorInfo, err error) {
	if info.Kind == "" {
		info.Kind = DebugErrorKindUnknown
	}
	if info.Message == "" && err != nil {
		info.Message = err.Error()
	}

	if componentErr, ok := errors.AsType[tmplfunc.ExecuteError](err); ok {
		info.Kind = DebugErrorKindComponent
		info.Component = cmp.Or(info.Component, componentErr.Function, componentErr.Template)
		appendDebugFrame(info, DebugFrame{
			Kind: "component",
			Name: cmp.Or(componentErr.Function, componentErr.Template),
		})
	}

	if info.Page != "" {
		appendDebugFrame(info, DebugFrame{Kind: "page", Name: info.Page, Path: info.Path})
	}
	if info.DependencyRole == DebugDependencyRoleLayout && info.Layout == "" {
		info.Layout = info.Path
	}

	if info.Location == nil {
		if location, ok := extractTemplateLocation(err); ok {
			info.Location = &location
		}
	}
}

func appendDebugFrame(info *DebugErrorInfo, frame DebugFrame) {
	if frame.Kind == "" && frame.Name == "" && frame.Path == "" {
		return
	}
	for _, existing := range info.Frames {
		if existing == frame {
			return
		}
	}
	info.Frames = append(info.Frames, frame)
}

var templateLocationPattern = regexp.MustCompile(`template: ([^:]+):([0-9]+)(?::([0-9]+))?:`)

func extractTemplateLocation(err error) (DebugLocation, bool) {
	if err == nil {
		return DebugLocation{}, false
	}

	if execErr, ok := errors.AsType[texttemplate.ExecError](err); ok {
		if location, found := parseTemplateLocation(execErr.Error()); found {
			return location, true
		}
	}

	return parseTemplateLocation(err.Error())
}

func parseTemplateLocation(message string) (DebugLocation, bool) {
	match := templateLocationPattern.FindStringSubmatch(message)
	if len(match) == 0 {
		return DebugLocation{}, false
	}

	line, _ := strconv.Atoi(match[2])
	column := 0
	if match[3] != "" {
		column, _ = strconv.Atoi(match[3])
	}

	return DebugLocation{
		Template: strings.TrimSpace(match[1]),
		Line:     line,
		Column:   column,
	}, true
}

func newDebugErrorWithSource(files fs.FS, candidates []string, info DebugErrorInfo, err error) error {
	wrapped := newDebugError(info, err)
	return withDebugSource(wrapped, files, candidates, nil)
}

func withDebugSource(err error, files fs.FS, candidates, preferred []string) error {
	if err == nil || files == nil {
		return err
	}

	if debugErr, ok := errors.AsType[*DebugError](err); ok {
		enrichDebugInfo(&debugErr.Info, err)
		attachSourceExcerpt(&debugErr.Info, files, candidates, preferred)
	}
	return err
}

func debugSourceCandidatesFromError(err error) []string {
	info, ok := DebugInfo(err)
	if !ok {
		return nil
	}

	candidates := []string{}
	candidates = append(candidates, info.Files...)
	candidates = append(candidates, info.ChangedPaths...)
	if info.Path != "" {
		candidates = append(candidates, info.Path)
	}
	if info.Source != nil && info.Source.Path != "" {
		candidates = append(candidates, info.Source.Path)
	}
	return candidates
}

func attachSourceExcerpt(info *DebugErrorInfo, files fs.FS, candidates, preferred []string) {
	if info == nil || info.Source != nil || info.Location == nil || info.Location.Line <= 0 {
		return
	}

	sourcePath, ok := resolveDebugSourcePath(info.Location.Template, candidates, preferred)
	if !ok {
		return
	}

	data, err := fs.ReadFile(files, sourcePath)
	if err != nil {
		return
	}

	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return
	}

	line := min(info.Location.Line, len(lines))
	start := max(1, line-2)
	end := min(len(lines), line+2)

	sourceLines := make([]DebugSourceLine, 0, end-start+1)
	for number := start; number <= end; number++ {
		sourceLines = append(sourceLines, DebugSourceLine{
			Number:    number,
			Text:      lines[number-1],
			Highlight: number == line,
		})
	}

	info.Source = &DebugSourceExcerpt{
		Path:      sourcePath,
		Line:      line,
		Column:    info.Location.Column,
		StartLine: start,
		Lines:     sourceLines,
	}
	if info.Path == "" {
		info.Path = sourcePath
	}
}

func resolveDebugSourcePath(templateName string, candidates, preferred []string) (string, bool) {
	name := path.Clean(templateName)
	if name == "." || name == "" {
		return "", false
	}

	if sourcePath, ok := resolveDebugSourcePathFrom(name, preferred); ok {
		return sourcePath, true
	}
	return resolveDebugSourcePathFrom(name, candidates)
}

func resolveDebugSourcePathFrom(templateName string, candidates []string) (string, bool) {
	cleanCandidates := make([]string, 0, len(candidates))
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		clean := path.Clean(filepathSlash(candidate))
		if clean == "." || clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		cleanCandidates = append(cleanCandidates, clean)
		if clean == templateName && strings.Contains(templateName, "/") {
			return clean, true
		}
	}

	matches := []string{}
	for _, candidate := range cleanCandidates {
		if path.Base(candidate) == path.Base(templateName) {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return "", false
}

func filepathSlash(value string) string {
	return strings.ReplaceAll(value, "\\", "/")
}

func requestMethod(r *http.Request) string {
	if r == nil {
		return ""
	}
	return r.Method
}

func requestPath(r *http.Request) string {
	if r == nil || r.URL == nil {
		return ""
	}
	return r.URL.Path
}

type debugPageView struct {
	Title       string
	Method      string
	Path        string
	Status      int
	Info        DebugErrorInfo
	ShowGoStack bool
}

var debugPageTemplate = template.Must(template.New("petra_debug_error").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <style>
    :root {
      color-scheme: dark;
      --bg: #111113;
      --panel: #18181b;
      --panel-2: #202024;
      --border: #3f3f46;
      --text: #f4f4f5;
      --muted: #a1a1aa;
      --accent: #fb7185;
      --accent-2: #fda4af;
      --code: #09090b;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background: var(--bg);
      color: var(--text);
      font: 15px/1.5 ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    main {
      width: min(1120px, calc(100vw - 32px));
      margin: 32px auto;
    }
    header {
      border: 1px solid var(--border);
      border-left: 4px solid var(--accent);
      border-radius: 8px;
      background: var(--panel);
      padding: 20px;
    }
    .eyebrow {
      color: var(--accent-2);
      font-size: 12px;
      font-weight: 700;
      letter-spacing: .08em;
      text-transform: uppercase;
    }
    h1 {
      margin: 6px 0 10px;
      font-size: clamp(28px, 4vw, 44px);
      line-height: 1.1;
      letter-spacing: 0;
    }
    .message {
      margin: 0;
      color: var(--text);
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
      white-space: pre-wrap;
      word-break: break-word;
    }
    .meta {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
      gap: 10px;
      margin: 16px 0;
    }
    .meta div, section {
      border: 1px solid var(--border);
      border-radius: 8px;
      background: var(--panel);
    }
    .meta div {
      padding: 12px;
      min-width: 0;
    }
    .label {
      display: block;
      color: var(--muted);
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: .06em;
    }
    .value {
      display: block;
      margin-top: 4px;
      overflow-wrap: anywhere;
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    }
    section {
      margin-top: 14px;
      overflow: hidden;
    }
    h2 {
      margin: 0;
      padding: 12px 14px;
      border-bottom: 1px solid var(--border);
      background: var(--panel-2);
      font-size: 14px;
      letter-spacing: 0;
    }
    pre, ol {
      margin: 0;
      padding: 14px;
      overflow: auto;
      background: var(--code);
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
      font-size: 13px;
      line-height: 1.55;
    }
    ol {
      list-style-position: inside;
    }
    .source-lines {
      list-style-position: outside;
      padding-left: 58px;
    }
    .source-lines li {
      padding: 0 10px;
      color: var(--muted);
    }
    .source-lines li.highlight {
      background: rgba(251, 113, 133, .18);
      color: var(--text);
    }
    .source-lines code {
      white-space: pre;
    }
    li + li { margin-top: 4px; }
    code {
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    }
  </style>
</head>
<body>
  <main>
    <header>
      <div class="eyebrow">Petra debug</div>
      <h1>{{.Title}}</h1>
      <p class="message">{{.Info.Message}}</p>
    </header>

    <div class="meta">
      <div><span class="label">Kind</span><span class="value">{{.Info.Kind}}</span></div>
      {{if .Info.Operation}}<div><span class="label">Operation</span><span class="value">{{.Info.Operation}}</span></div>{{end}}
      {{if .Info.DependencyRole}}<div><span class="label">Role</span><span class="value">{{.Info.DependencyRole}}</span></div>{{end}}
      {{if .Info.Page}}<div><span class="label">Page</span><span class="value">{{.Info.Page}}</span></div>{{end}}
      {{if .Info.Component}}<div><span class="label">Component</span><span class="value">{{.Info.Component}}</span></div>{{end}}
      {{if .Info.Layout}}<div><span class="label">Layout</span><span class="value">{{.Info.Layout}}</span></div>{{end}}
      {{if .Info.Path}}<div><span class="label">Path</span><span class="value">{{.Info.Path}}</span></div>{{end}}
      {{if .Info.FallbackReason}}<div><span class="label">Fallback</span><span class="value">{{.Info.FallbackReason}}</span></div>{{end}}
      {{if .Method}}<div><span class="label">Request</span><span class="value">{{.Method}} {{.Path}}</span></div>{{end}}
      <div><span class="label">Status</span><span class="value">{{.Status}}</span></div>
    </div>

    {{with .Info.Location}}
    <section>
      <h2>Template Location</h2>
      <pre>{{.Template}}{{if .Line}}:{{.Line}}{{if .Column}}:{{.Column}}{{end}}{{end}}</pre>
    </section>
    {{end}}

    {{with .Info.Source}}
    <section>
      <h2>Source Excerpt: {{.Path}}{{if .Line}}:{{.Line}}{{if .Column}}:{{.Column}}{{end}}{{end}}</h2>
      <ol class="source-lines" start="{{.StartLine}}">{{range .Lines}}<li class="{{if .Highlight}}highlight{{end}}"><code>{{.Text}}</code></li>{{end}}</ol>
    </section>
    {{end}}

    {{if .Info.AffectedPages}}
    <section>
      <h2>Affected Pages</h2>
      <pre>{{range .Info.AffectedPages}}{{.}}
{{end}}</pre>
    </section>
    {{end}}

    {{if .Info.Frames}}
    <section>
      <h2>Template Stack</h2>
      <ol>{{range .Info.Frames}}<li><code>{{.Kind}}</code> {{.Name}}{{if .Path}} <code>{{.Path}}</code>{{end}}</li>{{end}}</ol>
    </section>
    {{end}}

    {{if .Info.ChangedPaths}}
    <section>
      <h2>Changed Paths</h2>
      <pre>{{range .Info.ChangedPaths}}{{.}}
{{end}}</pre>
    </section>
    {{end}}

    {{if .Info.Files}}
    <section>
      <h2>Parsed Files</h2>
      <pre>{{range .Info.Files}}{{.}}
{{end}}</pre>
    </section>
    {{end}}

    {{if .ShowGoStack}}
    <section>
      <h2>Go Stack</h2>
      <pre>{{.Info.GoStack}}</pre>
    </section>
    {{end}}
  </main>
</body>
</html>`))
