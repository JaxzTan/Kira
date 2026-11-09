// Command kira runs the Kira server. Subcommands (built by kira-api in Phase 2):
//
//	kira serve   start the HTTP server
//	kira verify  recompute the ledger hash chain and exit non-zero if broken
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "usage: kira serve | kira verify (not implemented yet)")
	os.Exit(2)
}
