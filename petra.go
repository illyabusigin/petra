package petra

import (
	"embed"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"os"

	"github.com/go-chi/chi/v5"
)

//go:embed static
var static embed.FS

func New(opts ...Option) *Application {
	app := &Application{
		Router:     chi.NewRouter(),
		static:     &static,
		components: map[string]ComponentInitializer{},
	}

	app.init(opts...)
	// listComponentFuncs()

	return app
}

func listComponentFuncs() {
	set := token.NewFileSet()

	packs, err := parser.ParseDir(set, "components", nil, 0)
	if err != nil {
		fmt.Println("Failed to parse package:", err)
		os.Exit(1)
	}

	funcs := []*ast.FuncDecl{}
	for _, pack := range packs {
		for _, f := range pack.Files {
			for _, d := range f.Decls {
				if fn, isFn := d.(*ast.FuncDecl); isFn {
					funcs = append(funcs, fn)
					fmt.Println("func:", fn.Name.String(), fn.Recv)
				}
			}
		}
	}

	fmt.Printf("all funcs: %#v\n", funcs)

	pkg, err := importer.Default().Import("github.com/illyabusigin/petra/_examples/dev/components")
	if err != nil {
		fmt.Printf("error: %s\n", err.Error())
		return
	}
	for _, declName := range pkg.Scope().Names() {
		fmt.Println("declar:", declName)
	}
}
