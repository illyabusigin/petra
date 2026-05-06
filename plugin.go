package petra

import (
	"html/template"

	"github.com/illyabusigin/petra/plugins"
)

// Plugin extends parsed templates with functions and optional helper templates.
//
// Petra calls Funcs while building each new template set. Caches captured by
// those functions therefore reset when ParseDir, ParseFS, or a successful reload
// builds a replacement template set.
type Plugin interface {
	Funcs() (template.FuncMap, error)
	Apply(t *template.Template) error
}

// Plugins is an ordered plugin list. Plugin functions override same-named
// functions from Template.FuncMap, and later plugins override earlier plugins
// when they return the same function name.
type Plugins []Plugin

// Built-in plugins
var (
	// SVG allows you to load SVGs in your template from file and customize the
	// class. For example:
	//
	//	{{SVG "path/to/test" "h-6 w-6 stroke-red-500"}}
	//
	// This plugin caches the results on first lookup for faster
	// subsequent lookups. SVG files and class strings are trusted input; this
	// plugin is not a sanitizer.
	SVG = plugins.SVG

	// Markdown allows you to load markdown from a file. This plugin is usually
	// used in conjunction with the @tailwindcss/typography plugin to make
	// your markdown beautiful. For example:
	//
	//	{{Markdown "path/to/privacy_policy"}}
	//
	// This plugin caches the results on first lookup for faster
	// subsequent lookups. Rendered Markdown is returned as trusted HTML; do not
	// use this plugin as a sanitizer for user-authored content.
	Markdown = plugins.Markdown

	// HTML adds helpers for trusted HTML, trusted JavaScript, and safe attribute
	// construction.
	HTML = plugins.HTML
)
