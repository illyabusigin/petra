package plugins

import (
	"fmt"
	"html/template"
	"net/url"
	"strings"
)

func HTML() *_html {
	return &_html{}
}

type _html struct {
}

func (s _html) Funcs() (template.FuncMap, error) {
	return template.FuncMap{
		"attrs": safeAttr,
		"html": func(doc string) template.HTML {
			return template.HTML(doc)
		},
		"js": func(doc string) template.JS {
			return template.JS(doc)
		},
	}, nil
}

func safeAttr(attr, value string) (template.HTMLAttr, error) {
	if !isSafeAttrName(attr) {
		return "", fmt.Errorf("unsafe attribute name %q", attr)
	}
	if err := validateAttrValue(attr, value); err != nil {
		return "", err
	}

	return template.HTMLAttr(fmt.Sprintf(`%s="%s"`, attr, template.HTMLEscapeString(value))), nil
}

func isSafeAttrName(attr string) bool {
	if attr == "" {
		return false
	}

	for _, r := range attr {
		if r >= 'a' && r <= 'z' ||
			r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' ||
			r == '_' || r == ':' || r == '.' || r == '-' {
			continue
		}
		return false
	}

	attr = strings.ToLower(attr)
	if strings.HasPrefix(attr, "on") || attr == "style" {
		return false
	}

	return true
}

func validateAttrValue(attr, value string) error {
	attr = strings.ToLower(attr)
	if !urlAttrNames[attr] {
		return nil
	}

	if strings.ContainsFunc(value, func(r rune) bool {
		return r >= 0 && r < 0x20 || r == 0x7f
	}) {
		return fmt.Errorf("unsafe URL attribute %q contains control characters", attr)
	}

	value = strings.TrimSpace(value)
	if value == "" ||
		strings.HasPrefix(value, "./") ||
		strings.HasPrefix(value, "../") ||
		strings.HasPrefix(value, "#") ||
		strings.HasPrefix(value, "?") {
		return nil
	}
	if strings.HasPrefix(value, "//") {
		return fmt.Errorf("unsafe protocol-relative URL in attribute %q", attr)
	}
	if strings.HasPrefix(value, "/") {
		return nil
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("unsafe URL attribute %q: %w", attr, err)
	}
	if parsed.Scheme == "" {
		if parsed.Host != "" {
			return fmt.Errorf("unsafe protocol-relative URL in attribute %q", attr)
		}
		return nil
	}

	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "mailto", "tel":
		return nil
	default:
		return fmt.Errorf("unsafe URL scheme %q in attribute %q", parsed.Scheme, attr)
	}
}

var urlAttrNames = map[string]bool{
	"action":     true,
	"formaction": true,
	"href":       true,
	"poster":     true,
	"src":        true,
}

func (s _html) Apply(t *template.Template) error {

	return nil
}
