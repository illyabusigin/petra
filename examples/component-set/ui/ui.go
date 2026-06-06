package ui

import (
	"embed"
	"os"

	"github.com/illyabusigin/petra"
)

const setID = "github.com/illyabusigin/petra/examples/component-set/ui"

//go:embed components/*.html
var files embed.FS

// Set is the production component set backed by embedded templates.
var Set = petra.NewComponentSet(
	setID,
	files,
	"components",
	petra.Requires(petra.HTML()),
)

// Components mounts the embedded component set under the app's namespace.
func Components(namespace string) petra.Plugin {
	return petra.Components(namespace, Set)
}

// DevSet returns a component set backed by a live filesystem. The root should
// be the example's ui directory, not ui/components.
func DevSet(root string) *petra.ComponentSet {
	return petra.NewComponentSet(
		setID,
		os.DirFS(root),
		"components",
		petra.Requires(petra.HTML()),
	)
}

// DevComponents mounts a live component set for local component development.
func DevComponents(namespace, root string) petra.Plugin {
	return petra.Components(namespace, DevSet(root))
}
