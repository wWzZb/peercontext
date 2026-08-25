package main

import (
	"os"

	"github.com/wWzZb/peercontext/internal/cli"
)

func main() {
	os.Exit(int(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)))
}
