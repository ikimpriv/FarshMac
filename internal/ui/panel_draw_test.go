package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"farshmac/internal/fsops"
)

type panelStatInfo struct {
	fsops.ArchiveMember
	stat syscall.Stat_t
}

func (i panelStatInfo) Sys() any { return &i.stat }

func TestPanelColumnsDoNotMoveWithValues(t *testing.T) {
	entries := []Entry{{Name: "..", Dir: true}}
	for i, n := range []uint16{3, 42, 984, 65535} {
		name := []string{"a", "folder with a long name", "日本語📁", "large"}[i]
		info := panelStatInfo{ArchiveMember: fsops.ArchiveMember{Path: name, Bytes: int64(i) * 123456789012, Modified: time.Unix(1700000000, 0)}, stat: syscall.Stat_t{Nlink: n, Uid: uint32(i) * 1234567890}}
		entries = append(entries, Entry{Name: name, Info: info, Dir: i < 3})
	}
	for _, view := range []string{"File links", "File owners", "Full", "Wide", "Detailed", "Alternative full", "Descriptions", "Long descriptions"} {
		for _, width := range []int{14, 35, 60, 120} {
			t.Run(fmt.Sprintf("%s/%d", view, width), func(t *testing.T) {
				a := &App{Active: 0}
				a.Panels[0] = Panel{Path: "/test", Entries: entries, Cursor: 2, panelOptions: panelOptions{View: view}, Descriptions: map[string]string{"a": "short", "large": strings.Repeat("description ", 50)}}
				g := NewGrid(width+6, 16)
				for i := range g.Cells {
					g.Cells[i].Text = "!"
				}
				a.drawPanel(g, 0, 2, 1, width, 12)
				separator := -1
				// Includes parent with no stat data and two empty rows after entries.
				for row := 0; row < 7; row++ {
					yy := 3 + row
					found := -1
					for x := 3; x < 2+width-1; x++ {
						if g.Cells[yy*g.W+x].Text == "│" {
							if found >= 0 {
								t.Fatal("extra separator")
							}
							found = x
						}
					}
					if found < 0 {
						t.Fatal("missing separator", row)
					}
					if separator < 0 {
						separator = found
					} else if separator != found {
						t.Fatalf("separator moved: row %d x=%d, want %d", row, found, separator)
					}
					if g.Cells[yy*g.W+2+width-1].Text != "║" || g.Cells[yy*g.W+2+width].Text != "!" {
						t.Fatal("painted outside panel")
					}
				}
				if g.Cells[(3+2)*g.W+separator].BG != Cyan {
					t.Fatal("selected row separator lost highlight")
				}
				if view == "File links" && width >= 60 {
					for row, n := range []string{"-", "3", "42", "984", "65535"} {
						var text strings.Builder
						for x := separator + 1; x < 2+width-1; x++ {
							text.WriteString(g.Cells[(3+row)*g.W+x].Text)
						}
						if !strings.HasSuffix(strings.TrimSpace(text.String()), n) {
							t.Fatalf("link count truncated: %q, want %s", text.String(), n)
						}
					}
				}
			})
		}
	}
}
func TestFileLinksCountsRealHardLinks(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	if err := os.WriteFile(first, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(first, filepath.Join(root, "second")); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.setView(0, "File links")
	g := NewGrid(100, 16)
	a.drawPanel(g, 0, 0, 0, 100, 16)
	for row, e := range a.Panels[0].Entries {
		if e.Name != "first" && e.Name != "second" {
			continue
		}
		var line strings.Builder
		for x := 0; x < 99; x++ {
			line.WriteString(g.Cells[(row+2)*g.W+x].Text)
		}
		if !strings.HasSuffix(strings.TrimSpace(line.String()), "2") {
			t.Fatal("hard link count missing", line.String())
		}
	}
}
