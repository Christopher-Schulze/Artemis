package main

import (
	"os"

	"github.com/Christopher-Schulze/Artemis/smoke"
)

func main() {
	os.Exit(smoke.Command(os.Args[1:]))
}
