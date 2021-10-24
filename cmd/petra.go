package main

import (
	"embed"

	"github.com/alecthomas/kong"
)

//go:embed static/*.* static/assets/*  static/routes/*  static/components/* static/blueprints/*
var static embed.FS

var cli struct {
	Debug  bool `help:"Enable debug mode."`
	DryRun bool `help:"Perform dry-run."`

	Version  VersionCmd  `cmd:"" help:"Print Petra version." aliases:"v"`
	Init     InitCmd     `cmd:"" help:"Reinitializes a new petra project in the current folder."`
	New      NewCmd      `cmd:"" help:"Creates a new directory and runs petra init in it."`
	Serve    ServeCmd    `cmd:"" help:"Builds and runs your app, rebuilding on file changes."`
	Generate GenerateCmd `cmd:"" help:"Generates new code from blueprints. Built-in blueprints include: component, route" aliases:"g"`
}

func main() {
	ctx := kong.Parse(&cli)
	err := ctx.Run(&Context{Debug: cli.Debug, DryRun: cli.DryRun})
	ctx.FatalIfErrorf(err)
}
