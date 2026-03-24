package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "dbx-server v0.0.1-dev")
	os.Exit(0)
}
