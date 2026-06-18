// Package main is the entry point for OpsAgent.
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
