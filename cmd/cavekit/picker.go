package main

import (
	"fmt"
	"os"

	"github.com/JuliusBrussee/cavekit/internal/picker"
)

func runPicker(args []string) {
	_ = args
	root := picker.ProjectRoot()
	selected, err := picker.Run(root, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := picker.WriteSelection(selected, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
