// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tmplfunc

import (
	"bytes"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	htmltemplate "html/template"
	texttemplate "text/template"
	"text/template/parse"
)

var validNameRE = regexp.MustCompile(`\A[_\pL][_\pL\p{Nd}]*\z`)
var validArgNameRE = regexp.MustCompile(`\A[_\pL][_\pL\p{Nd}]*(\.\.\.|\?)?\z`)

const generatedFuncPrefix = "__tmplfunc_"

// ExecuteError wraps an error raised while invoking a named template through
// its generated function.
type ExecuteError struct {
	Template string
	Function string
	Err      error
}

func (e ExecuteError) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e ExecuteError) Unwrap() error {
	return e.Err
}

// Funcs installs functions for all the templates in the set containing t.
// After using t.Clone it is necessary to call Funcs on the result to arrange
// for the functions to invoke the cloned templates and not the originals.
func Funcs(t Template) error {
	funcs := make(map[string]any)
	seen := map[string]string{}
	for _, name := range existingTemplateNames(t) {
		if err := addFunc(t, name, funcs, nil, seen); err != nil {
			return err
		}
	}
	installFuncs(t, funcs)
	return nil
}

func parseIntoTemplate(t Template, names, texts []string) error {
	discovered, err := parseTemplateTexts(t, names, texts, parse.SkipFuncCheck, nil)
	if err != nil {
		return err
	}

	funcs, aliases, err := templateFuncs(t, discovered)
	if err != nil {
		return err
	}
	installFuncs(t, funcs)

	funcNames := parseFuncNames(t)
	for name := range funcs {
		funcNames[name] = true
	}
	for name := range builtinFuncNames {
		funcNames[name] = true
	}
	namespaceRoots := namespaceRootNames(aliases)
	for root := range namespaceRoots {
		funcNames[root] = true
	}

	checked, err := parseTemplateTexts(t, names, texts, 0, funcNameMap(funcNames))
	if err != nil {
		return err
	}
	if err := rewriteNamespacedCalls(checked, aliases, namespaceRoots); err != nil {
		return err
	}
	return addParseTrees(t, checked)
}

func parseAssociatedIntoTemplate(t Template, names, texts []string) error {
	funcs, aliases, err := templateFuncs(t, nil)
	if err != nil {
		return err
	}

	funcNames := parseFuncNames(t)
	for name := range funcs {
		funcNames[name] = true
	}
	for name := range builtinFuncNames {
		funcNames[name] = true
	}
	namespaceRoots := namespaceRootNames(aliases)
	for root := range namespaceRoots {
		funcNames[root] = true
	}

	checked, err := parseTemplateTexts(t, names, texts, 0, funcNameMap(funcNames))
	if err != nil {
		return err
	}
	if err := rewriteNamespacedCalls(checked, aliases, namespaceRoots); err != nil {
		return err
	}
	return addParseTrees(t, checked)
}

func templateFuncs(t Template, discovered map[string]*parse.Tree) (map[string]any, map[string]string, error) {
	funcs := make(map[string]any)
	aliases := map[string]string{}
	seen := map[string]string{}

	for _, name := range existingTemplateNames(t) {
		if err := addFunc(t, name, funcs, aliases, seen); err != nil {
			return nil, nil, err
		}
	}
	for name, tree := range discovered {
		if parse.IsEmptyTree(tree.Root) {
			continue
		}
		if err := addFunc(t, name, funcs, aliases, seen); err != nil {
			return nil, nil, err
		}
	}

	return funcs, aliases, nil
}

func addFunc(t Template, name string, funcs map[string]any, aliases map[string]string, seen map[string]string) error {
	fn, internalFn, bundle, err := bundler(name)
	if err != nil {
		return err
	}
	if fn == "" {
		return nil
	}
	if other, ok := seen[internalFn]; ok && other != fn {
		return fmt.Errorf("template function name collision: %q and %q both map to %q", other, fn, internalFn)
	}
	seen[internalFn] = fn
	if aliases != nil && fn != internalFn {
		aliases[fn] = internalFn
	}

	switch t := t.(type) {
	case *texttemplate.Template:
		funcs[internalFn] = func(args ...interface{}) (string, error) {
			t := t.Lookup(name)
			if t == nil {
				return "", ExecuteError{Template: name, Function: fn, Err: fmt.Errorf("lost template %q", name)}
			}
			arg, err := bundle(args)
			if err != nil {
				return "", ExecuteError{Template: name, Function: fn, Err: err}
			}
			var buf bytes.Buffer
			err = t.Execute(&buf, arg)
			if err != nil {
				return "", ExecuteError{Template: name, Function: fn, Err: err}
			}
			return buf.String(), nil
		}
	case *htmltemplate.Template:
		funcs[internalFn] = func(args ...interface{}) (htmltemplate.HTML, error) {
			t := t.Lookup(name)
			if t == nil {
				return "", ExecuteError{Template: name, Function: fn, Err: fmt.Errorf("lost template %q", name)}
			}
			arg, err := bundle(args)
			if err != nil {
				return "", ExecuteError{Template: name, Function: fn, Err: err}
			}
			var buf bytes.Buffer
			err = t.Execute(&buf, arg)
			if err != nil {
				return "", ExecuteError{Template: name, Function: fn, Err: err}
			}
			return htmltemplate.HTML(buf.String()), nil
		}
	}
	return nil
}

func bundler(name string) (fn, internalFn string, bundle func(args []interface{}) (interface{}, error), err error) {
	f := strings.Fields(name)
	if len(f) == 0 || !validCallName(f[0]) {
		return "", "", nil, nil
	}

	fn = f[0]
	internalFn = internalFuncName(fn)
	if len(f) == 1 {
		bundle = func(args []interface{}) (interface{}, error) {
			if len(args) == 0 {
				return nil, nil
			}
			if len(args) == 1 {
				return args[0], nil
			}
			return nil, fmt.Errorf("too many arguments in call to template %s", fn)
		}
	} else {
		sawQ := false
		for i, argName := range f[1:] {
			if !validArgNameRE.MatchString(argName) {
				return "", "", nil, fmt.Errorf("invalid template name %q: invalid argument name %s", name, argName)
			}
			if strings.HasSuffix(argName, "...") {
				if i != len(f)-2 {
					return "", "", nil, fmt.Errorf("invalid template name %q: %s is not last argument", name, argName)
				}
				break
			}
			if strings.HasSuffix(argName, "?") {
				sawQ = true
				continue
			}
			if sawQ {
				return "", "", nil, fmt.Errorf("invalid template name %q: required %s after optional %s", name, argName, f[i])
			}
		}

		bundle = func(args []interface{}) (interface{}, error) {
			m := make(map[string]interface{})
			for _, argName := range f[1:] {
				if strings.HasSuffix(argName, "...") {
					m[strings.TrimSuffix(argName, "...")] = args
					args = nil
					break
				}
				if strings.HasSuffix(argName, "?") {
					prefix := strings.TrimSuffix(argName, "?")
					if len(args) == 0 {
						m[prefix] = nil
					} else {
						m[prefix], args = args[0], args[1:]
					}
					continue
				}
				if len(args) == 0 {
					return nil, fmt.Errorf("too few arguments in call to template %s", fn)
				}
				m[argName], args = args[0], args[1:]
			}
			if len(args) > 0 {
				return nil, fmt.Errorf("too many arguments in call to template %s", fn)
			}
			return m, nil
		}
	}

	return fn, internalFn, bundle, nil
}

func validCallName(name string) bool {
	if name == "" {
		return false
	}
	for _, part := range strings.Split(name, ".") {
		if !validNameRE.MatchString(part) {
			return false
		}
	}
	return true
}

func internalFuncName(name string) string {
	if !strings.Contains(name, ".") {
		return name
	}
	return generatedFuncPrefix + strings.ReplaceAll(name, ".", "__")
}

func parseTemplateTexts(t Template, names, texts []string, mode parse.Mode, funcs map[string]any) (map[string]*parse.Tree, error) {
	leftDelim, rightDelim, err := templateDelims(t)
	if err != nil {
		return nil, err
	}

	trees := make(map[string]*parse.Tree)
	for i, text := range texts {
		tree := parse.New(names[i])
		tree.Mode = mode
		if _, err := tree.Parse(text, leftDelim, rightDelim, trees, funcs); err != nil {
			return nil, err
		}
	}
	return trees, nil
}

func templateDelims(t Template) (string, string, error) {
	switch t := t.(type) {
	case nil:
		return "", "", fmt.Errorf("tmplfunc: nil Template")
	default:
		return "", "", fmt.Errorf("tmplfunc: non-template type %T", t)
	case *texttemplate.Template:
		v := reflect.ValueOf(t).Elem()
		return v.FieldByName("leftDelim").String(), v.FieldByName("rightDelim").String(), nil
	case *htmltemplate.Template:
		v := reflect.ValueOf(t).Elem().FieldByName("text").Elem()
		return v.FieldByName("leftDelim").String(), v.FieldByName("rightDelim").String(), nil
	}
}

func installFuncs(t Template, funcs map[string]any) {
	if len(funcs) == 0 {
		return
	}
	switch t := t.(type) {
	case *texttemplate.Template:
		t.Funcs(funcs)
	case *htmltemplate.Template:
		t.Funcs(funcs)
	}
}

func addParseTrees(t Template, trees map[string]*parse.Tree) error {
	for name, tree := range trees {
		if name == t.Name() && parse.IsEmptyTree(tree.Root) {
			continue
		}

		var err error
		switch t := t.(type) {
		case *texttemplate.Template:
			var parsed *texttemplate.Template
			parsed, err = t.AddParseTree(name, tree)
			if err == nil && name == t.Name() {
				*t = *parsed
			}
		case *htmltemplate.Template:
			var parsed *htmltemplate.Template
			parsed, err = t.AddParseTree(name, tree)
			if err == nil && name == t.Name() {
				*t = *parsed
			}
		default:
			err = fmt.Errorf("tmplfunc: non-template type %T", t)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func existingTemplateNames(t Template) []string {
	switch t := t.(type) {
	case *texttemplate.Template:
		templates := t.Templates()
		names := make([]string, 0, len(templates))
		for _, tmpl := range templates {
			if tmpl.Tree == nil || parse.IsEmptyTree(tmpl.Tree.Root) {
				continue
			}
			names = append(names, tmpl.Name())
		}
		return names
	case *htmltemplate.Template:
		templates := t.Templates()
		names := make([]string, 0, len(templates))
		for _, tmpl := range templates {
			if tmpl.Tree == nil || parse.IsEmptyTree(tmpl.Tree.Root) {
				continue
			}
			names = append(names, tmpl.Name())
		}
		return names
	default:
		return nil
	}
}

func parseFuncNames(t Template) map[string]bool {
	names := map[string]bool{}
	var parseFuncs reflect.Value
	switch t := t.(type) {
	case *texttemplate.Template:
		common := reflect.ValueOf(t).Elem().FieldByName("common")
		if common.IsNil() {
			return names
		}
		parseFuncs = common.Elem().FieldByName("parseFuncs")
	case *htmltemplate.Template:
		text := reflect.ValueOf(t).Elem().FieldByName("text")
		if text.IsNil() {
			return names
		}
		common := text.Elem().FieldByName("common")
		if common.IsNil() {
			return names
		}
		parseFuncs = common.Elem().FieldByName("parseFuncs")
	default:
		return names
	}

	for _, key := range parseFuncs.MapKeys() {
		names[key.String()] = true
	}
	return names
}

func funcNameMap(names map[string]bool) map[string]any {
	out := make(map[string]any, len(names))
	for name := range names {
		out[name] = true
	}
	return out
}

func namespaceRootNames(aliases map[string]string) map[string]bool {
	roots := map[string]bool{}
	for call := range aliases {
		root, _, ok := strings.Cut(call, ".")
		if ok {
			roots[root] = true
		}
	}
	return roots
}

var builtinFuncNames = map[string]bool{
	"and":      true,
	"call":     true,
	"html":     true,
	"index":    true,
	"slice":    true,
	"js":       true,
	"len":      true,
	"not":      true,
	"or":       true,
	"print":    true,
	"printf":   true,
	"println":  true,
	"urlquery": true,
	"eq":       true,
	"ge":       true,
	"gt":       true,
	"le":       true,
	"lt":       true,
	"ne":       true,
}

func rewriteNamespacedCalls(trees map[string]*parse.Tree, aliases map[string]string, namespaceRoots map[string]bool) error {
	if len(aliases) == 0 {
		return nil
	}
	for _, tree := range trees {
		if err := rewriteNode(tree, tree.Root, aliases, namespaceRoots); err != nil {
			return err
		}
	}
	return nil
}

func rewriteNode(tree *parse.Tree, node parse.Node, aliases map[string]string, namespaceRoots map[string]bool) error {
	switch node := node.(type) {
	case nil:
	case *parse.ListNode:
		if node == nil {
			return nil
		}
		for _, child := range node.Nodes {
			if err := rewriteNode(tree, child, aliases, namespaceRoots); err != nil {
				return err
			}
		}
	case *parse.ActionNode:
		if node == nil {
			return nil
		}
		return rewritePipe(tree, node.Pipe, aliases, namespaceRoots)
	case *parse.IfNode:
		if node == nil {
			return nil
		}
		return rewriteBranch(tree, &node.BranchNode, aliases, namespaceRoots)
	case *parse.RangeNode:
		if node == nil {
			return nil
		}
		return rewriteBranch(tree, &node.BranchNode, aliases, namespaceRoots)
	case *parse.WithNode:
		if node == nil {
			return nil
		}
		return rewriteBranch(tree, &node.BranchNode, aliases, namespaceRoots)
	case *parse.TemplateNode:
		if node == nil {
			return nil
		}
		return rewritePipe(tree, node.Pipe, aliases, namespaceRoots)
	}
	return nil
}

func rewriteBranch(tree *parse.Tree, branch *parse.BranchNode, aliases map[string]string, namespaceRoots map[string]bool) error {
	if err := rewritePipe(tree, branch.Pipe, aliases, namespaceRoots); err != nil {
		return err
	}
	if err := rewriteNode(tree, branch.List, aliases, namespaceRoots); err != nil {
		return err
	}
	return rewriteNode(tree, branch.ElseList, aliases, namespaceRoots)
}

func rewritePipe(tree *parse.Tree, pipe *parse.PipeNode, aliases map[string]string, namespaceRoots map[string]bool) error {
	if pipe == nil {
		return nil
	}
	for _, cmd := range pipe.Cmds {
		if err := rewriteCommand(tree, cmd, aliases, namespaceRoots); err != nil {
			return err
		}
	}
	return nil
}

func rewriteCommand(tree *parse.Tree, cmd *parse.CommandNode, aliases map[string]string, namespaceRoots map[string]bool) error {
	if cmd == nil {
		return nil
	}
	if len(cmd.Args) > 0 {
		alias, name, root, ok := namespacedCommandAlias(cmd.Args[0], aliases)
		switch {
		case ok:
			cmd.Args[0] = parse.NewIdentifier(alias).SetTree(tree).SetPos(cmd.Args[0].Position())
		case namespaceRoots[root]:
			if name == root {
				return fmt.Errorf("function %q not defined", root)
			}
			return fmt.Errorf("function %q not defined", name)
		}
	}
	for _, arg := range cmd.Args {
		if pipe, ok := arg.(*parse.PipeNode); ok {
			if err := rewritePipe(tree, pipe, aliases, namespaceRoots); err != nil {
				return err
			}
		}
	}
	return nil
}

func namespacedCommandAlias(node parse.Node, aliases map[string]string) (alias, name, root string, ok bool) {
	switch node := node.(type) {
	case *parse.IdentifierNode:
		return "", node.Ident, node.Ident, false
	case *parse.ChainNode:
		if len(node.Field) == 0 {
			return "", "", "", false
		}
		ident, ok := node.Node.(*parse.IdentifierNode)
		if !ok {
			return "", "", "", false
		}

		parts := make([]string, 0, len(node.Field)+1)
		parts = append(parts, ident.Ident)
		parts = append(parts, node.Field...)
		name := strings.Join(parts, ".")
		alias, ok := aliases[name]
		return alias, name, ident.Ident, ok
	default:
		return "", "", "", false
	}
}
