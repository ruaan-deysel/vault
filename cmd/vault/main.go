package main

import (
	"os"

	"github.com/ruaandeysel/vault/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
