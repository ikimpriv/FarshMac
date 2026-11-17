//go:build visualqa

package main

import (
	"farshmac/internal/native"
	"farshmac/internal/ui"
	"os"
	"runtime"
)

func main() {
	runtime.LockOSThread()
	root, err := os.MkdirTemp("", "farshmac-visualqa-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", root)
	a, err := ui.NewSearchVisualQA(root)
	if err != nil {
		panic(err)
	}
	defer a.Close()
	if err = native.Run(a, false); err != nil {
		panic(err)
	}
}
