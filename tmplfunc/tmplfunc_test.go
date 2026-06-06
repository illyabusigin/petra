// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tmplfunc

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	htmltemplate "html/template"
	texttemplate "text/template"
)

var tests = []struct {
	in  string
	out string
}{
	{`{{define "hello"}}hello {{.}}{{end}}{{template "hello" "world"}}`, "hello world"},
	{`{{define "hello"}}hello {{.}}{{end}}{{hello "world"}}`, "hello world"},
	{`{{define "hello who"}}hello {{.who}}{{end}}{{hello "world"}}`, "hello world"},
	{`{{define "hello who"}}hello {{.who}}{{end}}{{hello}}`,
		"EXEC: template: :1:45: executing \"\" at <hello>: error calling hello: too few arguments in call to template hello",
	},
	{`{{define "hello who?"}}hello {{.who}}{{end}}{{hello}}`, "hello"},
	{`{{define "hello who?"}}hello {{.who}}{{end}}{{hello "world"}}`, "hello world"},
	{`{{define "hello who..."}}hello {{.who}}{{end}}{{hello}}`, "hello []"},
	{`{{define "hello who..."}}hello {{.who}}{{end}}{{hello "world"}}`, "hello [world]"},
	{`{{define "UI.TextField name label type attrs error?"}}<label>{{.label}}<input name="{{.name}}" type="{{.type}}"></label>{{if .error}}<span>{{.error}}</span>{{end}}{{end}}{{UI.TextField "email" "Email" "email" nil "required"}}`, `<label>Email<input name="email" type="email"></label><span>required</span>`},
	{`{{define "UI.Label text"}}<label>{{.text}}</label>{{end}}{{define "UI.Field text"}}{{UI.Label .text}}<input>{{end}}{{UI.Field "Email"}}`, `<label>Email</label><input>`},
}

func TestText(t *testing.T) {
	for i, tt := range tests {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			tmpl := texttemplate.New("")
			err := Parse(tmpl, tt.in)
			var out string
			if err != nil {
				out = "PARSE: " + err.Error()
			} else {
				var buf bytes.Buffer
				err := tmpl.Execute(&buf, nil)
				if err != nil {
					out = "EXEC: " + err.Error()
				} else {
					out = strings.ReplaceAll(buf.String(), "<no value>", "") // text generates these but html does not
					out = strings.TrimSpace(out)
				}
			}
			if out != tt.out {
				t.Errorf("have: %s\nwant: %s", out, tt.out)
			}
		})
	}
}

func TestHTML(t *testing.T) {
	for i, tt := range tests {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			tmpl := htmltemplate.New("")
			err := Parse(tmpl, tt.in)
			var out string
			if err != nil {
				out = "PARSE: " + err.Error()
			} else {
				var buf bytes.Buffer
				err := tmpl.Execute(&buf, nil)
				if err != nil {
					out = "EXEC: " + err.Error()
				} else {
					out = strings.TrimSpace(buf.String())
				}
			}
			if out != tt.out {
				t.Errorf("have: %s\nwant: %s", out, tt.out)
			}
		})
	}
}

func TestGlob(t *testing.T) {
	tmpl := texttemplate.New("")
	MustParseGlob(tmpl, "testdata/*.tmpl")
	texttemplate.Must(tmpl.Parse("{{x .}}"))

	var buf bytes.Buffer
	must(tmpl.Execute(&buf, []int{1, 2, 3}))
	out := strings.TrimSpace(buf.String())
	if out != "y" {
		t.Fatalf("out = %q, want %q", out, "y")
	}
}

func TestFuncs(t *testing.T) {
	tmpl := htmltemplate.New("")
	MustParseGlob(tmpl, "testdata/*.tmpl")
	htmltemplate.Must(tmpl.Parse("{{x .}}"))

	tmpl2 := htmltemplate.Must(tmpl.Clone())
	if err := Funcs(tmpl2); err != nil {
		t.Fatal(err)
	}
	tmpl2.Execute(new(bytes.Buffer), nil)

	if _, err := tmpl.Clone(); err != nil {
		// Happens if you forget to call Funcs above:
		//	cannot Clone "" after it has executed
		t.Fatal(err)
	}
}

func TestNamespacedComponentPreservesUnknownFunctionParseErrors(t *testing.T) {
	tmpl := htmltemplate.New("")
	err := Parse(tmpl, `{{define "UI.Text"}}text{{end}}{{missingFunc}}`)
	if err == nil {
		t.Fatal("Parse() error = nil")
	}
	if !strings.Contains(err.Error(), `function "missingFunc" not defined`) {
		t.Fatalf("Parse() error = %v", err)
	}
}

func TestNamespacedComponentRejectsMissingMember(t *testing.T) {
	tmpl := htmltemplate.New("")
	err := Parse(tmpl, `{{define "UI.Text"}}text{{end}}{{UI.Missing}}`)
	if err == nil {
		t.Fatal("Parse() error = nil")
	}
	if !strings.Contains(err.Error(), `function "UI.Missing" not defined`) {
		t.Fatalf("Parse() error = %v", err)
	}
}

func TestNamespacedComponentRejectsRootCall(t *testing.T) {
	tmpl := htmltemplate.New("")
	err := Parse(tmpl, `{{define "UI.Text"}}text{{end}}{{UI}}`)
	if err == nil {
		t.Fatal("Parse() error = nil")
	}
	if !strings.Contains(err.Error(), `function "UI" not defined`) {
		t.Fatalf("Parse() error = %v", err)
	}
}
