package plugins

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func TestHTMLHelpersReturnTrustedTypes(t *testing.T) {
	funcs, err := HTML().Funcs()
	if err != nil {
		t.Fatalf("Funcs() error = %v", err)
	}

	htmlFn, ok := funcs["html"].(func(string) template.HTML)
	if !ok {
		t.Fatalf("html helper type = %T", funcs["html"])
	}
	if got := htmlFn("<strong>trusted</strong>"); got != template.HTML("<strong>trusted</strong>") {
		t.Fatalf("html helper output = %q", got)
	}

	jsFn, ok := funcs["js"].(func(string) template.JS)
	if !ok {
		t.Fatalf("js helper type = %T", funcs["js"])
	}
	if got := jsFn("window.__x = 1"); got != template.JS("window.__x = 1") {
		t.Fatalf("js helper output = %q", got)
	}

	attrsFn, ok := funcs["attrs"].(func(string, string) (template.HTMLAttr, error))
	if !ok {
		t.Fatalf("attrs helper type = %T", funcs["attrs"])
	}
	if got, err := attrsFn("class", "button"); err != nil || got != template.HTMLAttr(`class="button"`) {
		t.Fatalf("attrs helper output = %q, error = %v", got, err)
	}
}

func TestAttrsEscapesValues(t *testing.T) {
	funcs, err := HTML().Funcs()
	if err != nil {
		t.Fatalf("Funcs() error = %v", err)
	}
	attrs := funcs["attrs"].(func(string, string) (template.HTMLAttr, error))

	got, err := attrs("title", `a "quoted" <value> & more`)
	if err != nil {
		t.Fatalf("attrs() error = %v", err)
	}
	want := template.HTMLAttr(`title="a &#34;quoted&#34; &lt;value&gt; &amp; more"`)
	if got != want {
		t.Fatalf("attrs() = %q, want %q", got, want)
	}
}

func TestAttrsAllowsCommonSafeAttributes(t *testing.T) {
	funcs, err := HTML().Funcs()
	if err != nil {
		t.Fatalf("Funcs() error = %v", err)
	}
	attrs := funcs["attrs"].(func(string, string) (template.HTMLAttr, error))

	for _, tc := range []struct {
		name  string
		value string
	}{
		{name: "class", value: "btn primary"},
		{name: "data-state", value: "open"},
		{name: "aria-expanded", value: "true"},
		{name: "href", value: "/products"},
		{name: "href", value: "https://example.com"},
		{name: "href", value: "mailto:hello@example.com"},
		{name: "href", value: "tel:+15551234567"},
		{name: "src", value: "./app.css"},
		{name: "action", value: "?next=/docs"},
	} {
		t.Run(tc.name+"="+tc.value, func(t *testing.T) {
			if _, err := attrs(tc.name, tc.value); err != nil {
				t.Fatalf("attrs(%q, %q) error = %v", tc.name, tc.value, err)
			}
		})
	}
}

func TestAttrsRejectsUnsafeAttributeNames(t *testing.T) {
	funcs, err := HTML().Funcs()
	if err != nil {
		t.Fatalf("Funcs() error = %v", err)
	}
	attrs := funcs["attrs"].(func(string, string) (template.HTMLAttr, error))

	for _, name := range []string{
		"",
		"class name",
		`class"onclick`,
		"class=onclick",
		"class<onclick",
		"class>onclick",
		"onclick",
		"onClick",
		"style",
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := attrs(name, "x"); err == nil {
				t.Fatalf("attrs(%q, %q) = %q, want error", name, "x", got)
			}
		})
	}
}

func TestAttrsRejectsUnsafeURLValues(t *testing.T) {
	funcs, err := HTML().Funcs()
	if err != nil {
		t.Fatalf("Funcs() error = %v", err)
	}
	attrs := funcs["attrs"].(func(string, string) (template.HTMLAttr, error))

	for _, value := range []string{
		"javascript:alert(1)",
		"JaVaScRiPt:alert(1)",
		"vbscript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"//evil.example/path",
		"http://exa\nmple.com",
	} {
		t.Run(value, func(t *testing.T) {
			if got, err := attrs("href", value); err == nil {
				t.Fatalf("attrs(%q, %q) = %q, want error", "href", value, got)
			}
		})
	}
}

func TestAttrsTemplateExecutionFailsForUnsafeInput(t *testing.T) {
	funcs, err := HTML().Funcs()
	if err != nil {
		t.Fatalf("Funcs() error = %v", err)
	}

	tmpl := template.Must(template.New("test").Funcs(funcs).Parse(`<a {{ attrs .Name .Value }}>link</a>`))
	var b bytes.Buffer
	err = tmpl.Execute(&b, map[string]string{
		"Name":  "href",
		"Value": "javascript:alert(1)",
	})
	if err == nil {
		t.Fatal("Execute() error = nil")
	}
	if !strings.Contains(err.Error(), "unsafe URL scheme") {
		t.Fatalf("Execute() error = %v", err)
	}
}
