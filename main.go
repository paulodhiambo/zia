package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintf(os.Stderr, "use 'go run ./cmd/api' instead\n")
	os.Exit(1)
}
