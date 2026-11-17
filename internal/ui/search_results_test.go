package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchResultsF3PreservesFilteredList(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("first"), 0600)
	os.WriteFile(filepath.Join(root, "b.txt"), []byte("second"), 0600)
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.findFiles(false)
	a.Form.Fields[0].Value = "*.txt"
	a.key(Message{Key: "Enter"})
	waitWork(t, a)
	if a.Form == nil || len(a.Form.Choice) != 2 {
		t.Fatal("no results", a.Status)
	}
	a.setListFilter("b.txt")
	saved := a.Form
	a.key(Message{Key: "F3"})
	waitWork(t, a)
	if a.Form != nil || a.Doc == nil || a.Doc.Text != "second" {
		t.Fatal("F3 did not open filtered hit", a.Status)
	}
	a.key(Message{Key: "Escape"})
	a.Poll()
	if a.Form != saved || a.Form.Query != "b.txt" || len(a.Form.Choice) != 1 {
		t.Fatal("result list was not restored")
	}
}

func TestSearchResultsKubeF3OpensManifestWithoutPanelNavigation(t *testing.T) {
	a, _ := kubeApp(t)
	enterKube(t, a, "shop")
	original := a.Panels[0].Path
	a.findFiles(false)
	a.Form.Fields[0].Value = "*"
	a.key(Message{Key: "Enter"})
	waitWork(t, a)
	if a.Form == nil {
		t.Fatal("no results", a.Status)
	}
	selected := -1
	for i, label := range a.Form.Choice {
		if strings.Contains(label, "pods/") {
			selected = i
			break
		}
	}
	if selected < 0 {
		t.Fatal("missing pod result", a.Form.Choice)
	}
	a.Form.Focus = selected
	saved := a.Form
	a.key(Message{Key: "F3"})
	waitWork(t, a)
	if a.Doc == nil || !strings.Contains(a.Doc.Text, "kind: Pod") || a.Panels[0].Path != original {
		t.Fatal("F3 did not open pod manifest in place", a.Status)
	}
	a.key(Message{Key: "Escape"})
	a.Poll()
	if a.Form != saved || a.Form.Focus != selected {
		t.Fatal("lost result selection")
	}
}

func TestSearchResultsF3OnOwnershipContext(t *testing.T) {
	a, _ := kubeApp(t)
	enterKube(t, a, "shop")
	a.findFiles(false)
	a.Form.Fields[0].Value = "app-new-x"
	a.key(Message{Key: "Enter"})
	waitWork(t, a)
	a.key(Message{Key: "F5"})
	waitWork(t, a)
	if a.Form == nil || len(a.Form.Choice) != 3 || !strings.Contains(a.Form.Choice[0], "Deployment app [owner]") {
		t.Fatal("missing ownership chain", a.Status, a.Form)
	}
	results := a.Form
	a.Form.Focus = 0
	a.key(Message{Key: "F3"})
	waitWork(t, a)
	if a.Doc == nil || !strings.Contains(a.Doc.Text, "kind: Deployment") {
		t.Fatal("cannot view ancestor", a.Status)
	}
	a.key(Message{Key: "Escape"})
	a.Poll()
	if a.Form != results {
		t.Fatal("lost ownership results")
	}
	a.key(Message{Key: "F5"})
	a.Poll()
	if a.Form.OwnershipTree || len(a.Form.Choice) != 1 || !strings.Contains(a.Form.Choice[0], "app-new-x") {
		t.Fatal("flat view must contain only actual matches")
	}

}
