package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/gookit/color"
)

type ServeCmd struct {
	ConfigPath string `short:"c"`
	Assets     string `help:"Assets folder for auto-generated assets. Defaults to ./assets" short:"c"`

	stop bool
}

func (r *ServeCmd) Run(ctx *Context) error {
	if err := ctx.inflate(""); err != nil {
		return err
	}

	ctx.header()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	color.Yellow.Printf("Starting file watchers...\n\n")
	go r.runTailwind(ctx, &r.stop)
	go r.runAir(ctx, &r.stop)

	go func() {
		time.Sleep(time.Second)
		color.Green.Printf("\nPetra watching for changes...\n\n")
	}()

	go func() {
		<-sigs
		r.stop = true
	}()

	defer func() {
		if e := recover(); e != nil {
			log.Fatalf("%v %+v", color.FgRed.Render("Petra panic:"), e)
		}
	}()

	<-sigs
	r.stop = true
	// tailwind.Wait()

	return nil
}

func (r *ServeCmd) assets(ctx *Context) string {
	if r.Assets == "" {
		return "./assets/app.css"
	}

	return r.Assets
}

func (r *ServeCmd) tailwind(ctx *Context) (*exec.Cmd, error) {
	cmd := exec.Command("npx", "tailwindcss", "--watch", "--output", r.assets(ctx))
	return cmd, nil
}

func (r *ServeCmd) runTailwind(ctx *Context, stop *bool) error {
	tailwind, err := r.tailwind(ctx)
	if err != nil {
		return err
	}
	tailwindOut, _ := tailwind.StdoutPipe()
	tailwind.Stderr = tailwind.Stdout

	tailwind.StdinPipe()
	tailwind.Start()

	scanner := bufio.NewScanner(tailwindOut)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		if *stop {
			fmt.Println("Stopping Air watcher")
			break
		}
		m := scanner.Text()
		if m != "" {
			fmt.Printf("%v %v\n", color.FgCyan.Render("tailwindcss:"), m)
		}
	}

	if err := tailwind.Wait(); err != nil {
		return err
	}

	fmt.Println("Stopping TailwindCSS watche2ßr")

	return nil
}

func (r *ServeCmd) runAir(ctx *Context, stop *bool) error {
	air := exec.Command("air")

	out, _ := air.StdoutPipe()
	air.Stderr = air.Stdout

	air.StdinPipe()
	air.Start()

	scanner := bufio.NewScanner(out)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		if *stop {
			fmt.Println("Stopping Air watcher")
			break
		}
		m := scanner.Text()
		if m != "" {
			fmt.Printf("%v %v\n", color.FgBlue.Render("air:"), m)
		}
	}

	if err := air.Wait(); err != nil {
		return err
	}

	fmt.Println("Stopping air watche2ßr")

	return nil
}
