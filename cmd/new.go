package main

import (
	"fmt"
	"path/filepath"

	"github.com/gookit/color"
)

type NewCmd struct {
	SkipNpm   bool
	SkipGit   bool
	SkipGoMod bool
	Name      string `arg:"" required:""`
	Directory string `long:"dir"`
}

func (r *NewCmd) dir(ctx *Context) string {
	if r.Directory != "" {
		return filepath.Join(ctx.wd, r.Directory)
	} else {
		return filepath.Join(ctx.wd, r.Name)
	}
}

func (r *NewCmd) Run(ctx *Context) error {
	if err := ctx.inflate(""); err != nil {
		return err
	}

	ctx.header()

	if err := r.createFiles(ctx); err != nil {
		return err
	}

	if err := r.runInit(ctx); err != nil {
		return err
	}

	return r.notify(ctx)
}

func (r *NewCmd) runInit(ctx *Context) error {
	init := InitCmd{
		subCommand: true,
		SkipNpm:    r.SkipNpm,
		SkipGit:    r.SkipGit,
		SkipGoMod:  r.SkipGoMod,
		Directory:  r.dir(ctx),
	}

	return init.Run(ctx)
}

func (r *NewCmd) createFiles(ctx *Context) error {
	yellow := color.FgYellow.Render
	fmt.Printf("\n✨  Creating a new Petra app in %v:\n", yellow(filepath.Join(ctx.wd, r.Name)))

	tasks := tasks{}

	tasks.queue(
		ctx.createDir(r.dir(ctx)),
	)

	if err := tasks.run(); err != nil {
		return err
	}
	return nil
}

func (r *NewCmd) notify(ctx *Context) error {
	yellow := color.FgYellow.Render
	gray := color.FgGray.Render
	cyan := color.FgCyan.Render

	fmt.Printf("\n🎉  Successfully created project %v:", yellow(r.Name))
	fmt.Printf("\n👉  Get started by typing:\n\n")
	fmt.Printf("\t%v %v\n", gray("$"), cyan("cd ", r.Name))
	fmt.Printf("\t%v %v\n\n", gray("$"), cyan("petra serve"))
	fmt.Printf("Happy coding!\n\n")

	return nil
}
