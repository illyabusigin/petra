package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/aymerick/raymond"
	"github.com/gookit/color"
)

type Context struct {
	Debug  bool
	DryRun bool

	wd string
}

func (r *Context) header() {
	logo := ` 
   ________  _______  _________  ________  ________     
  |\   __  \|\  ___ \|\___   ___\\   __  \|\   __  \    
  \ \  \|\  \ \   __/\|___ \  \_\ \  \|\  \ \  \|\  \   
   \ \   ____\ \  \_|/__  \ \  \ \ \   _  _\ \   __  \  
    \ \  \___|\ \  \_|\ \  \ \  \ \ \  \\  \\ \  \ \  \ 
     \ \__\    \ \_______\  \ \__\ \ \__\\ _\\ \__\ \__\
      \|__|     \|_______|   \|__|  \|__|\|__|\|__|\|__|
																
  `
	color.Yellow.Println(logo)
}

func (r *Context) inflate(dir string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	r.wd = wd

	if r.DryRun {
		color.Yellow.Println("You specified the dry-run flag, so no changes will be written.")
	}

	return nil
}

func (r *Context) createDir(name string) task {
	return func() error {
		blue := color.FgBlue.Render
		fmt.Printf("\t%v %v/\n", blue("create"), filepath.Base(name))

		if r.DryRun {
			return nil
		}

		return os.Mkdir(name, os.ModePerm)
	}
}

func (r *Context) createFile(src, dst string) task {
	return func() error {
		src := filepath.Join("static", src)

		data, err := static.ReadFile(src)
		if err != nil {
			return err
		}

		return r.writeFile(dst, data)()
	}
}

func (r *Context) writeFile(dst string, data []byte) task {
	return func() error {
		if !r.DryRun {

			if err := os.WriteFile(dst, data, os.ModePerm); err != nil {
				return err
			}
		}

		green := color.FgGreen.Render
		fmt.Printf("\t%v %v\n", green("create"), filepath.Base(dst))

		return nil
	}
}

func (r *Context) createFileFromTemplate(src, dst, name string) task {
	return func() error {

		if !r.DryRun {
			src := filepath.Join("static", src)

			data, err := static.ReadFile(src)
			if err != nil {
				return err
			}

			ctx := map[string]interface{}{
				"name":    name,
				"rootURL": "{{rootURL}}",
			}

			tmplt := raymond.MustParse(string(data))

			tmplt.RegisterHelper("content-for", func(value string) raymond.SafeString {
				return raymond.SafeString(fmt.Sprintf(`{{content-for "%v"}}`, value))
			})

			r := tmplt.MustExec(ctx)

			if err := os.WriteFile(dst, []byte(r), os.ModePerm); err != nil {
				return fmt.Errorf("failed to write %v: %w", dst, err)
			}
		}

		green := color.FgGreen.Render
		fmt.Printf("\t%v %v\n", green("create"), filepath.Base(src))

		return nil
	}
}
