package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"github.com/illyabusigin/petra/examples/mvcweb/cmd"
)

func main() {
	web := cmd.Web{}
	flag.StringVar(&web.Addr, "addr", ":8080", "address to listen on")
	flag.BoolVar(&web.Dev, "dev", false, "load templates and static assets from disk")
	flag.BoolVar(&web.Verbose, "verbose", false, "enable debug logs, including Petra parse and reload metrics")
	flag.StringVar(&web.RootDir, "root", ".", "example app root directory")
	flag.Parse()

	if err := web.Run(context.Background()); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
