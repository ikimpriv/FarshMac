package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"

	"farshmac/internal/highlight"
	"farshmac/internal/native"
	"farshmac/internal/preview"
	"farshmac/internal/ui"
)

func main() {
	if (len(os.Args) == 3 || len(os.Args) == 4) && os.Args[1] == preview.HelperFlag {
		pixels := 0
		if len(os.Args) == 4 {
			pixels, _ = strconv.Atoi(os.Args[3])
		}
		os.Exit(preview.HelperMain(os.Args[2], pixels))
	}
	if len(os.Args) == 3 && os.Args[1] == highlight.HelperFlag {
		os.Exit(highlight.HelperMain(os.Args[2]))
	}
	runtime.LockOSThread()
	home, _ := os.UserHomeDir()
	left := flag.String("left", home, "left panel directory")
	right := flag.String("right", home, "right panel directory")
	flag.Parse()
	if !native.PrepareDiskAccess() {
		return
	}
	a, err := ui.NewAsync(*left, *right)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	a.LoadSetup()
	defer a.Close()
	restore := true
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "left" || f.Name == "right" {
			restore = false
		}
	})
	if err = native.Run(a, restore); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
