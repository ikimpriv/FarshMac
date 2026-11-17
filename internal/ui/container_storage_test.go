package ui

import (
	"farshmac/internal/kube"
	"farshmac/internal/podtoast"
	"os"
	"strings"
	"testing"
)

func TestContainerMountMarkersAndLabels(t *testing.T) {
	entries := []Entry{{Name: "data", Dir: true, Info: podtoast.Entry{Filename: "data", FileMode: os.ModeDir, Mount: &podtoast.MountLocation{Path: "/data", Filesystem: "ext4"}, MountPoint: true}}, {Name: "file", Info: podtoast.Entry{Filename: "file", Mount: &podtoast.MountLocation{Path: "/data", Filesystem: "ext4"}}}}
	classifyContainerEntries(entries, "/", []kube.StorageMount{{Path: "/data", Kind: "Persistent volume", Claim: "db"}}, true)
	if !entries[0].Info.(storageFileInfo).mount || entries[1].Info.(storageFileInfo).mount {
		t.Fatal("descendant marked as mount")
	}
	if !strings.Contains(entries[1].Info.(storageFileInfo).label, "PVC db") {
		t.Fatal("missing PVC identity")
	}
	a := &App{}
	a.Panels[0] = Panel{Entries: entries, Cursor: 1, panelOptions: panelOptions{View: "Full"}}
	g := NewGrid(100, 12)
	a.drawPanel(g, 0, 0, 0, 100, 12)
	marker := false
	for _, c := range g.Cells {
		if c.Text == "↳" {
			marker = true
			if c.FG != 0xffaa55 {
				t.Fatalf("mount color: %x", c.FG)
			}
		}
	}
	if !marker {
		t.Fatal("missing mount marker")
	}
}

func TestManifestStorageColumnWithoutAgent(t *testing.T) {
	mounts := []kube.StorageMount{{Path: "/data", Kind: "Persistent volume", Claim: "database"}, {Path: "/cache", Kind: "Ephemeral · emptyDir", Volume: "cache"}, {Path: "/secret", Kind: "Secret", Volume: "credentials", ReadOnly: true}}
	entries := []Entry{}
	for _, name := range []string{"data", "cache", "secret", "ordinary"} {
		entries = append(entries, Entry{Name: name, Dir: true, Info: podtoast.Entry{Filename: name, FileMode: os.ModeDir}})
	}
	classifyContainerEntries(entries, "/", mounts, true)
	for i, want := range []string{"PVC: database", "emptyDir: cache", "Secret: credentials (ro)", "Container FS"} {
		info := entries[i].Info.(storageFileInfo)
		if info.column != want || info.mount != (i < 3) {
			t.Fatalf("%s: %+v", want, info)
		}
	}
	for _, width := range []int{30, 60, 100} {
		a := &App{Active: 1}
		a.Panels[0] = Panel{Entries: entries, panelOptions: panelOptions{View: "Full"}}
		g := NewGrid(width+2, 12)
		a.drawPanel(g, 0, 0, 0, width, 12)
		if !strings.Contains(gridLine(g, 1), "Storage") || !strings.Contains(gridLine(g, 2), "PVC:") {
			t.Fatalf("storage column missing at width %d: %s", width, gridLine(g, 2))
		}
		for y := 0; y < 12; y++ {
			if g.Cells[y*g.W+width].Text != " " {
				t.Fatalf("column overflow at width %d row %d", width, y)
			}
		}
	}
	// Observed parent root must not erase a mount explicitly declared by Kubernetes.
	entries = []Entry{{Name: "data", Dir: true, Info: podtoast.Entry{Filename: "data", FileMode: os.ModeDir, Mount: &podtoast.MountLocation{Path: "/", Filesystem: "overlay"}}}}
	classifyContainerEntries(entries, "/", mounts, true)
	if info := entries[0].Info.(storageFileInfo); !info.mount || info.column != "PVC: database" {
		t.Fatal("manifest mount was hidden", info)
	}
}
