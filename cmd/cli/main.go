// Package main is the CLI entry point for OpsAgent.
// Equivalent to the root cmd/main.go, kept here so `make build-cli`
// (which targets ./cmd/cli) works.
package main

import (
	"fmt"
	"os"

	"github.com/Havens-blog/OpsAgent/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
