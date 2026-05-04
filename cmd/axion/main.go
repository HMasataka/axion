package main

import (
	"fmt"
	"os"
)

const usage = `axion - file synchronization tool

Usage:
  axion <command> [flags]

Commands:
  server   Run the axion server
  client   Run the axion client

Use "axion <command> --help" for command-specific flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	args := os.Args[2:]
	switch os.Args[1] {
	case "server":
		runServer(args)
	case "client":
		runClient(args)
	case "-h", "--help", "help":
		fmt.Fprint(os.Stdout, usage)
	default:
		fmt.Fprintf(os.Stderr, "axion: unknown command %q\n\n", os.Args[1])
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}
