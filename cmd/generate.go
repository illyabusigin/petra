package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/aymerick/raymond"
	"github.com/iancoleman/strcase"
	"github.com/jinzhu/inflection"
)

type GenerateCmd struct {
	Blueprint string `arg:"" required:""`
	Name      string `arg:"" required:""`
	Overwrite bool   `short:"o" help:"Overwrite existing file(s)."`
	Package   string `short:"p" help:"Override the package name for the blueprint."`

	Directory string `name:"in" short:"d" help:"Runs a blueprint against the provided directory. A path is expected, relative to the root of the project."`
}

func (r *GenerateCmd) dir(ctx *Context) (string, error) {
	if r.Directory != "" {
		dir := filepath.Join(ctx.wd, r.Directory)

		if _, err := os.Stat(dir); os.IsNotExist(err) {
			if err = os.MkdirAll(dir, os.ModePerm); err != nil {
				return "", err
			}
		}

		return dir, nil
	}

	switch r.Blueprint {
	case "component":
		return filepath.Join(ctx.wd, "/components/"), nil
	case "route":
		return filepath.Join(ctx.wd, "/routes/"), nil
	}

	return ctx.wd, nil
}

func (r *GenerateCmd) name() string {
	return filepath.Base(r.Name)
}

func (r *GenerateCmd) Run(ctx *Context) error {
	if err := ctx.inflate(""); err != nil {
		return err
	}

	fmt.Printf("Generate %v %v:\n", r.Blueprint, r.Name)

	src := filepath.Join("static/blueprints", r.Blueprint+".hbs")

	data, err := static.ReadFile(src)
	if err != nil {
		return err
	}

	env := r.templateContext(ctx)

	tmplt, err := raymond.Parse(string(data))
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	result, err := tmplt.Exec(env)
	if err != nil {
		return err
	}

	return r.createFiles(ctx, result)
}

func (r *GenerateCmd) templateContext(ctx *Context) map[string]interface{} {
	env := map[string]interface{}{
		"package": inflection.Plural(r.Blueprint),
		"name":    strcase.ToCamel(r.name()),
	}

	return env
}

func (r *GenerateCmd) createFiles(ctx *Context, result string) error {

	dir, err := r.dir(ctx)
	if err != nil {
		return err
	}

	// Create .go file
	dst := filepath.Join(dir, r.name()+".go")
	if _, err := os.Stat(dst); !os.IsNotExist(err) && !r.Overwrite {
		return fmt.Errorf("%v already exists: Use --overwrite to overwrite existing files", filepath.Base(dst))
	}

	if err := ctx.writeFile(dst, []byte(result))(); err != nil {
		return err
	}

	// Create.hbs file
	dst = filepath.Join(dir, r.name()+".hbs")
	if _, err := os.Stat(dst); !os.IsNotExist(err) && !r.Overwrite {
		return fmt.Errorf("%v already exists: Use --overwrite to overwrite existing files", filepath.Base(dst))
	}

	if err := ctx.writeFile(dst, []byte{})(); err != nil {
		return err
	}

	return nil
}
