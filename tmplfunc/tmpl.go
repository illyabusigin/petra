// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package tmplfunc provides an extension of Go templates
// in which templates can be invoked as if they were functions.
//
// For example, after parsing
//
//	{{define "link url text"}}<a href="{{.url}}">{{.text}}</a>{{end}}
//
// this package installs a function named link allowing the template
// to be invoked as
//
//	{{link "https://golang.org" "the Go language"}}
//
// instead of the longer-form (assuming an appropriate function named dict)
//
//	{{template "link" (dict "url" "https://golang.org" "text" "the Go language")}}
//
// # Function Definitions
//
// The function installed for a given template depends on the name of the
// defined template, which can include not just a function name but also
// a list of parameter names. The function name and parameter names must
// consist only of letters, digits, and underscores, with a leading non-digit.
//
// If there is no parameter list, then the function is expected to take
// at most one argument, made available in the template body as “.” (dot).
// If such a function is called with no arguments, dot will be a nil interface value.
//
// If there is a parameter list, then the function requires an argument for
// each parameter, except for optional and variadic parameters, explained below.
// Inside the template, the top-level value “.” is a map[string]interface{} in which
// each parameter name is mapped to the corresponding argument value.
// A parameter x can therefore be accessed as {{(index . "x")}} or, more concisely, {{.x}}.
//
// The first special case in parameter handling is that
// a parameter can be made optional by adding a “?” suffix after its name.
// If the argument list ends before that parameter, the corresponding map entry
// will be present and set to a nil value.
// The second special case is that a parameter can be made variadic
// by adding a “...” suffix after its name.
// The corresponding map entry contains a []interface{} holding the
// zero or more arguments corresponding to that parameter.
//
// In the parameter list, required parameters must precede optional parameters,
// which must in turn precede any variadic parameter.
//
// For example, we can revise the link template given earlier to make the
// link text optional, substituting the URL when the text is omitted:
//
//	{{define "link url text?"}}<a href="{{.url}}">{{or .text .url}}</a>{{end}}
//
//	The Go home page is {{link "https://golang.org"}}.
//
// # Usage
//
// This package is meant to be used with templates from either the
// text/template or html/template packages. Given a *template.Template
// variable t, substitute:
//
//	t.Parse(text) -> tmplfunc.Parse(t, text)
//	t.ParseFiles(list) -> tmplfunc.ParseFiles(t, list)
//	t.ParseGlob(pattern) -> tmplfunc.ParseGlob(t, pattern)
//
// Parse, ParseFiles, and ParseGlob parse the new templates but also add
// functions that invoke them, named according to the function signatures.
// Templates can only invoke functions for templates that have already been
// defined or that are being defined in the same Parse, ParseFiles, or ParseGlob call.
// For example, templates in two files x.tmpl and y.tmpl can call each other
// only if ParseFiles or ParseGlob is used to parse both files in a single call.
// Otherwise, the parsing of the first file will report that calls to templates in
// the second file are calling unknown functions.
//
// When used with the html/template package, all function-invoked template
// calls are treated as invoking templates producing HTML. In order to use a
// template that produces some other kind of text fragment, the template must
// be invoked directly using the {{template "name"}} form, not as a function call.
package tmplfunc

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// A Template is a *template.Template, where template refers to either
// the html/template or text/template package.
type Template interface {
	// Method here only to make most types that are not a *template.Template
	// not implement the interface. The requirement here is to be one of the two
	// template types, not just to have this single method.
	DefinedTemplates() string
	Name() string
}

// Parse is like t.Parse(text), adding functions for the templates defined in text.
func Parse(t Template, text string) error {
	return parseIntoTemplate(t, []string{t.Name()}, []string{text})
}

// ParseFiles is like t.ParseFiles(filenames...), adding functions for the parsed templates.
func ParseFiles(t Template, filenames ...string) error {
	return parseFiles(t, readFileOS, filenames...)
}

// ParseFilesFS is like t.ParseFiles(filenames...), adding functions for the parsed templates.
func ParseFilesFS(t Template, fsys fs.FS, filenames ...string) error {
	return parseFiles(t, readFileFS(fsys), filenames...)
}

// ParseAssociatedFilesFS is like t.ParseFS for templates associated with t.
// It allows those templates to call tmplfunc-generated functions already
// installed on t, including namespaced functions such as UI.TextField, but it
// does not install new functions for templates defined by filenames.
func ParseAssociatedFilesFS(t Template, fsys fs.FS, filenames ...string) error {
	return parseAssociatedFiles(t, readFileFS(fsys), filenames...)
}

// parseFiles is the helper for the method and function. If the argument
// template is nil, it is created from the first file.
func parseFiles(t Template, readFile func(string) (string, []byte, error), filenames ...string) error {
	if len(filenames) == 0 {
		// Not really a problem, but be consistent.
		return fmt.Errorf("tmplfunc: no files named in call to ParseFiles")
	}

	var names []string
	var texts []string
	for _, filename := range filenames {
		name, b, err := readFile(filename)
		if err != nil {
			return err
		}
		names = append(names, name)
		texts = append(texts, string(b))
	}

	return parseIntoTemplate(t, names, texts)
}

func parseAssociatedFiles(t Template, readFile func(string) (string, []byte, error), filenames ...string) error {
	if len(filenames) == 0 {
		return fmt.Errorf("tmplfunc: no files named in call to ParseAssociatedFiles")
	}

	var names []string
	var texts []string
	for _, filename := range filenames {
		name, b, err := readFile(filename)
		if err != nil {
			return err
		}
		names = append(names, name)
		texts = append(texts, string(b))
	}

	return parseAssociatedIntoTemplate(t, names, texts)
}

// ParseGlob is like t.ParseGlob(pattern), adding functions for the parsed templates.
func ParseGlob(t Template, pattern string) error {
	filenames, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	if len(filenames) == 0 {
		return fmt.Errorf("tmplfunc: pattern matches no files: %#q", pattern)
	}
	return parseFiles(t, readFileOS, filenames...)
}

// ParseGlob is like t.ParseGlob(pattern), adding functions for the parsed templates.
func ParseGlobFS(t Template, fsys fs.FS, pattern string) error {
	filenames, err := fs.Glob(fsys, pattern)
	if err != nil {
		return err
	}
	if len(filenames) == 0 {
		return fmt.Errorf("tmplfunc: pattern matches no files: %#q", pattern)
	}
	return parseFiles(t, readFileFS(fsys), filenames...)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

// MustParse is like Parse but panics on error.
func MustParse(t Template, text string) {
	must(Parse(t, text))
}

// MustParseFiles is like ParseFiles but panics on error.
func MustParseFiles(t Template, filenames ...string) {
	must(ParseFiles(t, filenames...))
}

// MustParseFiles is like ParseFiles but panics on error.
func MustParseFilesFS(t Template, fsys fs.FS, filenames ...string) {
	must(ParseFilesFS(t, fsys, filenames...))
}

// MustParseGlob is like ParseGlob but panics on error.
func MustParseGlob(t Template, pattern string) {
	must(ParseGlob(t, pattern))
}

// MustParseGlob is like ParseGlob but panics on error.
func MustParseGlobFS(t Template, fsys fs.FS, pattern string) {
	must(ParseGlobFS(t, fsys, pattern))
}

func readFileOS(file string) (name string, b []byte, err error) {
	name = filepath.Base(file)
	b, err = os.ReadFile(file)
	return
}

func readFileFS(fsys fs.FS) func(string) (string, []byte, error) {
	return func(file string) (name string, b []byte, err error) {
		name = filepath.Base(file)
		b, err = fs.ReadFile(fsys, file)
		return
	}
}
