package main

import (
	"fmt"
	"os"

	"github.com/dnd-it/tamci/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "::error::%s\n", err)
		os.Exit(1)
	}
}
