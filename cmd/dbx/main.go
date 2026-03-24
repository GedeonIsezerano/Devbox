package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "dbx v0.0.1-dev")
	os.Exit(0)
}
