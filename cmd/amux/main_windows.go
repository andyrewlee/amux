//go:build windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "amux is not supported on Windows. It requires tmux and is supported on Linux/macOS.")
	os.Exit(1)
}
