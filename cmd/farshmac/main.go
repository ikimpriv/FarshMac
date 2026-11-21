package main

import (
	"farshmac/internal/aispaces"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"

	"farshmac/internal/cloudfiles"
	"farshmac/internal/highlight"
	"farshmac/internal/native"
	"farshmac/internal/preview"
	"farshmac/internal/ui"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == aispaces.GatewayFlag {
		os.Exit(aispaces.GatewayMain())
	}
	if len(os.Args) == 2 && os.Args[1] == aispaces.OperatorFlag {
		os.Exit(aispaces.OperatorMain())
	}
	if len(os.Args) == 3 && os.Args[1] == cloudfiles.HelperFlag {
		os.Exit(cloudfiles.HelperMain(os.Args[2]))
	}
	if (len(os.Args) >= 3 && len(os.Args) <= 5) && os.Args[1] == preview.HelperFlag {
		pixels := 0
		if len(os.Args) >= 4 {
			pixels, _ = strconv.Atoi(os.Args[3])
		}
		page := 1
		if len(os.Args) == 5 {
			page, _ = strconv.Atoi(os.Args[4])
		}
		os.Exit(preview.HelperMain(os.Args[2], pixels, page))
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
	go aispaces.Shared() // serves the AI Spaces MCP for the user's own agents from the start
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
