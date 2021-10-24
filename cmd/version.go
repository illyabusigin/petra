package main

import "fmt"

var (
	version string
	commit  string
)

type VersionCmd struct {
}

func (r *VersionCmd) Run(ctx *Context) error {
	ctx.header()
	fmt.Printf("petra v%v (%v)\n", version, commit)
	return nil
}
