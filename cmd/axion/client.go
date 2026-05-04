package main

import (
	"flag"
	"fmt"
	"os"
)

func runClient(args []string) {
	fs := flag.NewFlagSet("client", flag.ExitOnError)
	// v0.2.0 では flag を持たない。v0.2.1 で --server, --root, --id-file, --psk-file 追加
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: axion client [flags]")
		fmt.Fprintln(fs.Output(), "\nFlags:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	fmt.Fprintln(os.Stderr, "axion client: not yet implemented (v0.2.1)")
	os.Exit(0)
}
