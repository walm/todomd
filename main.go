package main

import (
	"os"

	"github.com/walm/todomd/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
