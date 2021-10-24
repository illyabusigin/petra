package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/gookit/color"
	"github.com/janeczku/go-spinner"
)

type InitCmd struct {
	SkipNpm   bool
	SkipGit   bool
	SkipGoMod bool
	Directory string `long:"dir" `

	subCommand bool
}

func (r *InitCmd) dir(ctx *Context) string {
	if r.Directory != "" {
		return r.Directory
	}

	return ctx.wd
}

func (r *InitCmd) dest(ctx *Context, dst string) string {
	return filepath.Join(r.dir(ctx), dst)
}

func (r *InitCmd) Run(ctx *Context) error {
	if err := ctx.inflate(""); err != nil {
		return err
	}

	if !r.subCommand {
		ctx.header()
	}

	if err := r.createFiles(ctx); err != nil {
		return err
	}

	if err := r.installDeps(ctx); err != nil {
		return err
	}

	return r.notify(ctx)
}

func (r *InitCmd) createFiles(ctx *Context) error {
	yellow := color.FgYellow.Render
	name := filepath.Base(filepath.Join(ctx.wd, r.dir(ctx)))
	if !r.subCommand {
		fmt.Printf("\n✨  Creating a new Petra app in %v:\n", yellow(name))
	}

	tasks := tasks{}

	tasks.queue(
		ctx.createFile(".petra", r.dest(ctx, ".petra")),
		ctx.createFile(".air.toml", r.dest(ctx, ".air.toml")),
		ctx.createFileFromTemplate("go.modt", filepath.Join(r.dir(ctx), "go.mod"), name),
		ctx.createFileFromTemplate("package.json", filepath.Join(r.dir(ctx), "package.json"), name),
		ctx.createFile("tailwind.config.js", filepath.Join(r.dir(ctx), "tailwind.config.js")),
		ctx.createDir(filepath.Join(r.dir(ctx), "components")),
		ctx.createFile("components/.gitkeep", filepath.Join(r.dir(ctx), "components/.gitkeep")),
		ctx.createDir(filepath.Join(r.dir(ctx), "routes")),
		ctx.createFile("routes/.gitkeep", filepath.Join(r.dir(ctx), "routes/.gitkeep")),
		ctx.createDir(filepath.Join(r.dir(ctx), "assets")),
		ctx.createFile("assets/.gitkeep", filepath.Join(r.dir(ctx), "assets/.gitkeep")),
		ctx.createFile("assets/app.css", filepath.Join(r.dir(ctx), "assets/app.css")),
		ctx.createFileFromTemplate("index.html", filepath.Join(r.dir(ctx), "index.html"), name),
		ctx.createFile("main.go", filepath.Join(r.dir(ctx), "main.go")),
	)

	if err := tasks.run(); err != nil {
		return err
	}
	return nil
}

func (r *InitCmd) depCheck(ctx *Context) error {
	if ctx.DryRun {
		return nil
	}

	npm := exec.Command("which", "npm")
	npm.Dir = r.dir(ctx)

	var out bytes.Buffer
	npm.Stdout = &out

	if err := npm.Run(); err != nil {
		return fmt.Errorf("which npm failed: %w", err)
	}

	if out.String() == "" {
		return fmt.Errorf("npm not found. Please install npm, see https://docs.npmjs.com/downloading-and-installing-node-js-and-npm")
	}

	whichGo := exec.Command("which", "go")
	whichGo.Dir = r.dir(ctx)

	out = bytes.Buffer{}
	whichGo.Stdout = &out

	if err := whichGo.Run(); err != nil {
		return fmt.Errorf("which go failed: %w", err)
	}

	if out.String() == "" {
		return fmt.Errorf("go not found. Please install Go, see https://golang.org/doc/install")
	}

	return nil
}

func (r *InitCmd) installDeps(ctx *Context) error {
	if err := r.depCheck(ctx); err != nil {
		return err
	}

	if ctx.DryRun {
		fmt.Printf("\n🚧   Installing dependencies...\n")
		color.Green.Println("\nnpm: Installed dependencies")
		color.Green.Println("go: Installed modules")
		return nil
	}

	green := color.FgGreen.Render

	s := spinner.StartNew("🚧   Installing dependencies...")

	if !r.SkipNpm {
		npm := exec.Command("npm", "install")
		npm.Dir = r.dir(ctx)
		s.Title = green("npm: Installing dependencies ...")

		if err := npm.Run(); err != nil {
			return fmt.Errorf("npm install failed: %w", err)
		}

		s.Stop()
		color.Green.Println("\nnpm: Installed dependencies")
	}

	if !r.SkipGoMod {
		s.Title = green("go: Downloading modules ...")
		s.Start()

		gomod := exec.Command("go", "mod", "download")
		gomod.Dir = r.dir(ctx)

		if err := gomod.Run(); err != nil {
			return fmt.Errorf("go mod download failed: %w", err)
		}

		s.Stop()
		color.Green.Println("\ngo: Installed modules")
	}

	if !r.SkipGit {
		// TODO: Git init
	}

	return nil
}

func (r *InitCmd) notify(ctx *Context) error {
	if r.subCommand {
		return nil
	}

	name := filepath.Base(r.dir(ctx))

	yellow := color.FgYellow.Render
	gray := color.FgGray.Render
	cyan := color.FgCyan.Render

	fmt.Printf("\n🎉  Successfully created project %v:", yellow(name))
	fmt.Printf("\n👉  Get started by typing:\n\n")
	fmt.Printf("\t%v %v\n\n", gray("$"), cyan("petra serve"))
	fmt.Printf("Happy coding!\n\n")

	return nil
}
