package petra

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
	"text/template/parse"
	"unicode"

	"github.com/illyabusigin/petra/tmplfunc"
)

// ComponentSet is a reusable collection of Petra components.
//
// Component source files define namespace-free templates such as
// {{define "TextField name label id attrs error?"}}. Mounting the set with
// Components("UI", set) exposes exported definitions as UI.TextField.
type ComponentSet struct {
	id       string
	files    fs.FS
	root     string
	imports  []componentImport
	requires Plugins
}

type componentImport struct {
	alias string
	set   *ComponentSet
}

// ComponentSetOption configures a ComponentSet.
type ComponentSetOption func(*componentSetConfig)

type componentSetConfig struct {
	imports  []componentImport
	requires Plugins
}

// NewComponentSet creates a component set from templates under root.
//
// The id must be stable across releases. Petra uses it to give private
// component definitions deterministic names.
func NewComponentSet(id string, files fs.FS, root string, opts ...ComponentSetOption) *ComponentSet {
	cfg := componentSetConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	return &ComponentSet{
		id:       id,
		files:    files,
		root:     root,
		imports:  append([]componentImport(nil), cfg.imports...),
		requires: append(Plugins(nil), cfg.requires...),
	}
}

// Import makes another component set available privately in this set's
// templates under alias, for example {{ Base.Button "Save" }}.
func Import(alias string, set *ComponentSet) ComponentSetOption {
	return func(cfg *componentSetConfig) {
		cfg.imports = append(cfg.imports, componentImport{alias: alias, set: set})
	}
}

// Requires declares render-time Petra plugins needed by the component set.
//
// This is intentionally limited to server-side template/plugin dependencies.
// Client assets, JavaScript modules, and models are outside the v1 component
// set contract.
func Requires(plugins ...Plugin) ComponentSetOption {
	return func(cfg *componentSetConfig) {
		cfg.requires = append(cfg.requires, plugins...)
	}
}

// Components mounts a component set into the app template namespace.
//
// Only definitions whose first rune is uppercase are exported. Lowercase and
// underscore-prefixed definitions remain private implementation details.
func Components(namespace string, set *ComponentSet) Plugin {
	return componentsPlugin{namespace: namespace, set: set}
}

type componentsPlugin struct {
	namespace string
	set       *ComponentSet
}

func (p componentsPlugin) Funcs() (template.FuncMap, error) {
	return nil, nil
}

func (p componentsPlugin) Apply(*template.Template) error {
	return nil
}

func (p componentsPlugin) componentMount() componentMount {
	return componentMount{namespace: p.namespace, set: p.set}
}

type componentMountProvider interface {
	componentMount() componentMount
}

type componentMount struct {
	namespace string
	set       *ComponentSet
}

func collectComponentMounts(plugins Plugins) []componentMount {
	var mounts []componentMount
	for _, plugin := range plugins {
		if provider, ok := plugin.(componentMountProvider); ok {
			mounts = append(mounts, provider.componentMount())
		}
	}
	return mounts
}

func collectComponentRequiredPlugins(mounts []componentMount) (Plugins, error) {
	collector := componentRequirementCollector{
		seen:     map[string]*ComponentSet{},
		visiting: map[string]bool{},
	}
	for _, mount := range mounts {
		if err := validateComponentNamespace(mount.namespace); err != nil {
			return nil, err
		}
		if err := collector.collect(mount.set); err != nil {
			return nil, err
		}
	}
	return collector.plugins, nil
}

type componentRequirementCollector struct {
	plugins  Plugins
	seen     map[string]*ComponentSet
	visiting map[string]bool
}

func (c *componentRequirementCollector) collect(set *ComponentSet) error {
	if err := validateComponentSet(set); err != nil {
		return err
	}
	if prev := c.seen[set.id]; prev != nil {
		if prev != set {
			return fmt.Errorf("petra: component set %q registered with multiple definitions", set.id)
		}
		return nil
	}
	if c.visiting[set.id] {
		return fmt.Errorf("petra: component set import cycle includes %q", set.id)
	}

	c.visiting[set.id] = true
	for _, imp := range set.imports {
		if err := validateComponentImport(imp); err != nil {
			return err
		}
		if err := c.collect(imp.set); err != nil {
			return err
		}
	}
	delete(c.visiting, set.id)

	c.seen[set.id] = set
	c.plugins = append(c.plugins, set.requires...)
	return nil
}

func applyComponentMounts(t *template.Template, mounts []componentMount) error {
	if len(mounts) == 0 {
		return nil
	}

	compiler := componentCompiler{
		t:                t,
		setsByID:         map[string]*ComponentSet{},
		compiledInternal: map[string]*compiledComponentSet{},
		compiling:        map[string]bool{},
		mounted:          map[string]string{},
	}
	for _, mount := range mounts {
		if err := compiler.mount(mount.namespace, mount.set); err != nil {
			return err
		}
	}
	return nil
}

type componentCompiler struct {
	t                *template.Template
	setsByID         map[string]*ComponentSet
	compiledInternal map[string]*compiledComponentSet
	compiling        map[string]bool
	mounted          map[string]string
}

type compiledComponentSet struct {
	set         *ComponentSet
	defs        map[string]componentDefinition
	localFuncs  map[string]string
	importFuncs map[string]string
	importRoots map[string]bool
}

type componentDefinition struct {
	name     string
	args     []string
	tree     *parse.Tree
	exported bool
}

func (c *componentCompiler) mount(namespace string, set *ComponentSet) error {
	if err := validateComponentNamespace(namespace); err != nil {
		return err
	}
	if existing, ok := c.mounted[namespace]; ok {
		if set != nil && existing == set.id {
			return nil
		}
		return fmt.Errorf("petra: component namespace %q is already mounted by set %q", namespace, existing)
	}

	compiled, err := c.compileInternal(set)
	if err != nil {
		return err
	}

	c.mounted[namespace] = compiled.set.id
	return c.compilePublic(namespace, compiled)
}

func (c *componentCompiler) compileInternal(set *ComponentSet) (*compiledComponentSet, error) {
	if err := validateComponentSet(set); err != nil {
		return nil, err
	}
	if prev := c.setsByID[set.id]; prev != nil && prev != set {
		return nil, fmt.Errorf("petra: component set %q registered with multiple definitions", set.id)
	}
	c.setsByID[set.id] = set

	if compiled := c.compiledInternal[set.id]; compiled != nil {
		return compiled, nil
	}
	if c.compiling[set.id] {
		return nil, fmt.Errorf("petra: component set import cycle includes %q", set.id)
	}

	c.compiling[set.id] = true
	defer delete(c.compiling, set.id)

	importFuncs := map[string]string{}
	importRoots := map[string]bool{}
	for _, imp := range set.imports {
		if err := validateComponentImport(imp); err != nil {
			return nil, err
		}
		importRoots[imp.alias] = true

		imported, err := c.compileInternal(imp.set)
		if err != nil {
			return nil, err
		}
		for name, def := range imported.defs {
			if !def.exported {
				continue
			}
			importFuncs[imp.alias+"."+name] = imported.localFuncs[name]
		}
	}

	defs, err := discoverComponentDefinitions(set)
	if err != nil {
		return nil, err
	}

	localFuncs := map[string]string{}
	for name := range defs {
		localFuncs[name] = componentInternalFuncName(set.id, name)
	}

	compiled := &compiledComponentSet{
		set:         set,
		defs:        defs,
		localFuncs:  localFuncs,
		importFuncs: importFuncs,
		importRoots: importRoots,
	}

	aliases := compiled.componentAliases()
	if err := c.parseDefinitions(compiled, aliases, func(def componentDefinition) string {
		return componentSignature(localFuncs[def.name], def.args)
	}, allComponentDefinitions); err != nil {
		return nil, err
	}

	c.compiledInternal[set.id] = compiled
	return compiled, nil
}

func (c *componentCompiler) compilePublic(namespace string, compiled *compiledComponentSet) error {
	aliases := compiled.componentAliases()
	return c.parseDefinitions(compiled, aliases, func(def componentDefinition) string {
		return componentSignature(namespace+"."+def.name, def.args)
	}, exportedComponentDefinitions)
}

type componentDefinitionSelector func(componentDefinition) bool

func allComponentDefinitions(componentDefinition) bool {
	return true
}

func exportedComponentDefinitions(def componentDefinition) bool {
	return def.exported
}

func (c *componentCompiler) parseDefinitions(compiled *compiledComponentSet, aliases map[string]string, signature func(componentDefinition) string, selector componentDefinitionSelector) error {
	defs := make([]componentDefinition, 0, len(compiled.defs))
	for _, def := range compiled.defs {
		if selector(def) {
			defs = append(defs, def)
		}
	}
	if len(defs) == 0 {
		return nil
	}

	sort.Slice(defs, func(i, j int) bool {
		return defs[i].name < defs[j].name
	})

	var source strings.Builder
	for _, def := range defs {
		tree := def.tree.Copy()
		if err := rewriteComponentCalls(tree.Root, tree, aliases, compiled.importRoots); err != nil {
			return fmt.Errorf("petra: component set %q component %q: %w", compiled.set.id, def.name, err)
		}

		source.WriteString("{{define ")
		source.WriteString(strconv.Quote(signature(def)))
		source.WriteString("}}")
		source.WriteString(tree.Root.String())
		source.WriteString("{{end}}\n")
	}

	if err := tmplfunc.Parse(c.t, source.String()); err != nil {
		return fmt.Errorf("petra: component set %q: %w", compiled.set.id, err)
	}
	return nil
}

func (c compiledComponentSet) componentAliases() map[string]string {
	aliases := make(map[string]string, len(c.localFuncs)+len(c.importFuncs))
	for call, fn := range c.localFuncs {
		aliases[call] = fn
	}
	for call, fn := range c.importFuncs {
		aliases[call] = fn
	}
	return aliases
}

func discoverComponentDefinitions(set *ComponentSet) (map[string]componentDefinition, error) {
	files, err := componentTemplateFiles(set.files, set.root)
	if err != nil {
		return nil, fmt.Errorf("petra: component set %q: %w", set.id, err)
	}
	if len(files) == 0 {
		return nil, nil
	}

	defs := map[string]componentDefinition{}
	for _, file := range files {
		source, err := fs.ReadFile(set.files, file)
		if err != nil {
			return nil, fmt.Errorf("petra: component set %q: read %s: %w", set.id, file, err)
		}

		treeSet := map[string]*parse.Tree{}
		tree := parse.New(path.Base(file))
		tree.Mode = parse.SkipFuncCheck
		if _, err := tree.Parse(string(source), "{{", "}}", treeSet, nil); err != nil {
			return nil, fmt.Errorf("petra: component set %q: parse %s: %w", set.id, file, err)
		}

		names := make([]string, 0, len(treeSet))
		for name := range treeSet {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, signature := range names {
			tree := treeSet[signature]
			if parse.IsEmptyTree(tree.Root) {
				continue
			}

			componentName, args, err := parseComponentSignature(signature)
			if err != nil {
				return nil, fmt.Errorf("petra: component set %q: %s: %w", set.id, signature, err)
			}
			if existing, ok := defs[componentName]; ok {
				return nil, fmt.Errorf("petra: component set %q: component %q defined multiple times with signatures %q and %q", set.id, componentName, componentSignature(existing.name, existing.args), signature)
			}

			defs[componentName] = componentDefinition{
				name:     componentName,
				args:     args,
				tree:     tree,
				exported: isExportedComponent(componentName),
			}
		}
	}

	return defs, nil
}

func parseComponentSignature(signature string) (string, []string, error) {
	fields := strings.Fields(signature)
	if len(fields) == 0 {
		return "", nil, errors.New("component definition name is required")
	}

	name := fields[0]
	if strings.Contains(name, ".") {
		return "", nil, fmt.Errorf("component name %q must be namespace-free", name)
	}
	if !validTemplateIdentifier(name) {
		return "", nil, fmt.Errorf("component name %q is not a valid template identifier", name)
	}
	return name, fields[1:], nil
}

func validateComponentNamespace(namespace string) error {
	if namespace == "" {
		return errors.New("petra: component namespace is required")
	}
	if !validTemplateIdentifier(namespace) {
		return fmt.Errorf("petra: component namespace %q is not a valid template identifier", namespace)
	}
	return nil
}

func validateComponentSet(set *ComponentSet) error {
	if set == nil {
		return errors.New("petra: component set is required")
	}
	if set.id == "" {
		return errors.New("petra: component set id is required")
	}
	if set.files == nil {
		return fmt.Errorf("petra: component set %q files are required", set.id)
	}

	aliases := map[string]bool{}
	for _, imp := range set.imports {
		if err := validateComponentImport(imp); err != nil {
			return err
		}
		if aliases[imp.alias] {
			return fmt.Errorf("petra: component set %q imports alias %q multiple times", set.id, imp.alias)
		}
		aliases[imp.alias] = true
	}

	return nil
}

func validateComponentImport(imp componentImport) error {
	if imp.alias == "" {
		return errors.New("petra: component import alias is required")
	}
	if !validTemplateIdentifier(imp.alias) {
		return fmt.Errorf("petra: component import alias %q is not a valid template identifier", imp.alias)
	}
	if imp.set == nil {
		return fmt.Errorf("petra: component import %q set is required", imp.alias)
	}
	return nil
}

func validTemplateIdentifier(name string) bool {
	for i, r := range name {
		if i == 0 {
			if r != '_' && !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return name != ""
}

func isExportedComponent(name string) bool {
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
}

func componentSignature(name string, args []string) string {
	if len(args) == 0 {
		return name
	}
	return name + " " + strings.Join(args, " ")
}

func componentInternalFuncName(setID, name string) string {
	sum := sha256.Sum256([]byte(setID))
	return "__petra_component_" + hex.EncodeToString(sum[:8]) + "__" + name
}

func rewriteComponentCalls(node parse.Node, tree *parse.Tree, aliases map[string]string, importRoots map[string]bool) error {
	switch node := node.(type) {
	case *parse.ListNode:
		for _, child := range node.Nodes {
			if err := rewriteComponentCalls(child, tree, aliases, importRoots); err != nil {
				return err
			}
		}
	case *parse.ActionNode:
		return rewriteComponentCalls(node.Pipe, tree, aliases, importRoots)
	case *parse.IfNode:
		return rewriteComponentBranch(&node.BranchNode, tree, aliases, importRoots)
	case *parse.RangeNode:
		return rewriteComponentBranch(&node.BranchNode, tree, aliases, importRoots)
	case *parse.WithNode:
		return rewriteComponentBranch(&node.BranchNode, tree, aliases, importRoots)
	case *parse.TemplateNode:
		if node.Pipe != nil {
			return rewriteComponentCalls(node.Pipe, tree, aliases, importRoots)
		}
	case *parse.PipeNode:
		for _, cmd := range node.Cmds {
			if err := rewriteComponentCalls(cmd, tree, aliases, importRoots); err != nil {
				return err
			}
		}
	case *parse.CommandNode:
		if len(node.Args) == 0 {
			return nil
		}
		if err := rewriteComponentCall(node, tree, aliases, importRoots); err != nil {
			return err
		}
		for _, arg := range node.Args[1:] {
			if pipe, ok := arg.(*parse.PipeNode); ok {
				if err := rewriteComponentCalls(pipe, tree, aliases, importRoots); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func rewriteComponentBranch(branch *parse.BranchNode, tree *parse.Tree, aliases map[string]string, importRoots map[string]bool) error {
	if branch.Pipe != nil {
		if err := rewriteComponentCalls(branch.Pipe, tree, aliases, importRoots); err != nil {
			return err
		}
	}
	if branch.List != nil {
		if err := rewriteComponentCalls(branch.List, tree, aliases, importRoots); err != nil {
			return err
		}
	}
	if branch.ElseList != nil {
		if err := rewriteComponentCalls(branch.ElseList, tree, aliases, importRoots); err != nil {
			return err
		}
	}
	return nil
}

func rewriteComponentCall(cmd *parse.CommandNode, tree *parse.Tree, aliases map[string]string, importRoots map[string]bool) error {
	switch first := cmd.Args[0].(type) {
	case *parse.IdentifierNode:
		if replacement, ok := aliases[first.Ident]; ok {
			cmd.Args[0] = parse.NewIdentifier(replacement).SetTree(tree)
		}
	case *parse.ChainNode:
		root, ok := first.Node.(*parse.IdentifierNode)
		if !ok {
			return nil
		}

		name := root.Ident + "." + strings.Join(first.Field, ".")
		if replacement, ok := aliases[name]; ok {
			cmd.Args[0] = parse.NewIdentifier(replacement).SetTree(tree)
			return nil
		}
		if importRoots[root.Ident] {
			return fmt.Errorf("component %q is not exported by import %q", name, root.Ident)
		}
	}
	return nil
}
