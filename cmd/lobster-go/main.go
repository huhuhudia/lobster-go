package main

import (
	"os"

	"github.com/huhuhudia/lobster-go/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
