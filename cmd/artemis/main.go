// Artemis - headless browser engine for AI agents.
// Copyright (C) 2026 Artemis contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

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
  serve     run the JSON-over-WebSocket steering server
  version   print version
  help      print this help

Run 'artemis <command> --help' for subcommand-specific flags.
`, Version)
}
