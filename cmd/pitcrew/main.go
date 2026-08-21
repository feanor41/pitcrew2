package main

import (
	"io"
	"os"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/cli"
)

var version = "dev"

func main() {
	root, err := os.Getwd()
	if err != nil {
		root = "."
	}
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, root))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, root string) int {
	return cli.Run(args, cli.Dependencies{Stdin: stdin, Stdout: stdout, Stderr: stderr, ProjectRoot: root, Version: version, Now: time.Now})
}
