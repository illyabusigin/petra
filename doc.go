// Package petra provides a small site-oriented layer over html/template.
//
// Petra parses a template tree into executable page templates, supports nested
// layouts and component directories, and lets defined templates be called like
// functions through the bundled tmplfunc package.
//
// Plugins can add template functions and helper templates. Helpers that return
// template.HTML, template.JS, or template.HTMLAttr are trust boundaries: use
// them only after the application has produced safe output.
package petra
