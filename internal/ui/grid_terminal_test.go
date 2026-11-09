package ui

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"testing"
)

func TestGridUnicodeAlignmentInBundledTerminal(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for the bundled xterm parser regression")
	}
	type frame struct {
		ANSI  string   `json:"ansi"`
		Cells []string `json:"cells"`
	}
	input := struct {
		Width  int     `json:"width"`
		Height int     `json:"height"`
		Frames []frame `json:"frames"`
	}{Width: 60, Height: 15}
	appendFrame := func(g *Grid) {
		f := frame{ANSI: g.ANSI()}
		for _, c := range g.Cells {
			f.Cells = append(f.Cells, c.Text)
		}
		input.Frames = append(input.Frames, f)
	}
	// Start from occupied cells to catch stale content under emoji continuation
	// cells, then repaint different names as happens when scrolling a tree.
	g := NewGrid(input.Width, input.Height)
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			g.Set(x, y, "X", LightCyan, Blue)
		}
	}
	appendFrame(g)
	for _, names := range [][]string{
		{"├─😀folder", "│ ├─日本語", "│ └─📁résumé", "└─e\u0301cole"},
		{"├─🦊📁😀", "│ └─файлы", "├─plain name", "└─文書"},
	} {
		g = NewGrid(input.Width, input.Height)
		g.Box(0, 0, 30, 15, LightCyan, Blue)
		g.Box(30, 0, 30, 15, LightCyan, Blue)
		for y, name := range names {
			g.Text(2, y+2, name, White, Blue, 26)
		}
		g.Text(58, 12, "😀", White, Blue, 2)
		appendFrame(g)
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "../native/tests/grid.cjs")
	cmd.Stdin = bytes.NewReader(data)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("terminal grid mismatch: %v\n%s", err, output)
	}
}
