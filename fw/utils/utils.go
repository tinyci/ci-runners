// Package utils contains useful one-off tools for implementing runners.
package utils

import (
	"fmt"
	"os"
)

// ErrOut is just a simple way to barf out info before exiting.
func ErrOut(err interface{}) {
	fmt.Fprintf(os.Stderr, "Fatal Error during runner execution: %v\n", err)
	os.Exit(1)
}
