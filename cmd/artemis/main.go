// Artemis - headless browser engine for AI agents.
// Copyright (c) 2026 Christopher Schulze.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
)

const Version = "0.0.1-dev"

func main() {
	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version", "-v", "--version":
		fmt.Println(Version)
	case "help", "-h", "--help":
		printUsage(os.Stdout)
	case "fetch":
		os.Exit(cmdFetch(os.Args[2:]))
	case "run":
		os.Exit(cmdRun(os.Args[2:]))
	case "serve":
		os.Exit(cmdServe(os.Args[2:]))
	default:
		errf("unknown command %q", os.Args[1])
		printUsage(os.Stderr)
		os.Exit(2)
	}
}

func printUsage(w *os.File) {
	fmt.Fprintf(w, `artemis %s - headless browser engine

Usage:
  artemis <command> [flags] [args]

Commands:
  fetch     fetch a URL and dump html / markdown / text / title / links / structured / semantic
  run       load a JavaScript file and execute it in the page context (--script FILE <url>)
  serve     run the JSON-over-WebSocket steering server
  version   print version
  help      print this help

Run 'artemis <command> --help' for subcommand-specific flags.
`, Version)
}
