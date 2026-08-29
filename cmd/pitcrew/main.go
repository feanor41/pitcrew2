package main

import (
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/cli"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		root = "."
	}
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, root))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, root string) int {
	return cli.Run(args, cli.Dependencies{Stdin: stdin, Stdout: stdout, Stderr: stderr, ProjectRoot: root, DataHome: dataHome(), Now: time.Now})
}

func dataHome() string {
	if configured := os.Getenv("XDG_DATA_HOME"); configured != "" {
		return configured
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}
