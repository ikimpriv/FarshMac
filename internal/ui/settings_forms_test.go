package ui

import (
	"strings"
	"testing"
)

func TestInvalidFontSizeReopensFormWithValues(t *testing.T) {
	setupHome(t)
	a := formTestApp(t)
	a.settingsMenu("General settings")
	a.Form.Fields[0].Value = "99"
	a.Form.Fields[1].Checked = false
	a.Form.Focus = 3
	a.key(Message{Key: "Enter"})
	if a.Form == nil || a.Form.Title != "General settings" || a.Form.Focus != 0 || !a.Form.Selected || a.Form.Fields[1].Checked || a.Form.Fields[0].Value != "99" || a.Status != "Font size must be 10–24" {
		t.Fatal("form not reopened with values", a.Form, a.Status)
	}
	a.key(Message{Key: "Escape"})
	if a.Form != nil || a.Settings.FontSize != 14 || !a.Settings.ShowKeys {
		t.Fatal("cancel after retry applied values")
	}
}
func TestPromptValidationReopens(t *testing.T) {
	setupHome(t)
	a := formTestApp(t)
	a.Settings.RememberDialogValues = true
	a.settingsMenu("Tree settings")
	a.Dialog.Input = "99"
	a.key(Message{Key: "Enter"})
	if a.Dialog == nil || a.Dialog.Input != "99" || a.Status != "Enter a number from 1 to 12" || a.dialogValues["Tree settings"] == "99" {
		t.Fatal("prompt not reopened or invalid value remembered", a.Status)
	}
	a.Dialog.Input = "5"
	a.key(Message{Key: "Enter"})
	if a.Dialog != nil || a.Settings.TreeDepth != 5 || a.dialogValues["Tree settings"] != "5" {
		t.Fatal("valid value not applied")
	}
}
func TestEditUserMenuKeepsOrder(t *testing.T) {
	setupHome(t)
	a := formTestApp(t)
	a.Settings.UserMenu = []UserCommand{{"one", "echo 1"}, {"two", "echo 2"}, {"three", "echo 3"}}
	a.editUserMenu()
	a.Form.Select(1)
	a.Form.Fields[1].Value = "echo one"
	a.key(Message{Key: "Enter"})
	if a.Settings.UserMenu[0] != (UserCommand{"one", "echo one"}) || len(a.Settings.UserMenu) != 3 {
		t.Fatal("edit moved entry", a.Settings.UserMenu)
	}
	a.editUserMenu()
	a.Form.Select(2)
	a.Form.Fields[1].Value = ""
	a.key(Message{Key: "Enter"})
	if len(a.Settings.UserMenu) != 2 || a.Settings.UserMenu[1].Name != "three" {
		t.Fatal("empty command did not delete", a.Settings.UserMenu)
	}
}
func TestUserMenuRequiresLabel(t *testing.T) {
	setupHome(t)
	a := formTestApp(t)
	a.Settings.UserMenu = []UserCommand{{"one", "echo 1"}}
	a.editUserMenu()
	a.Form.Select(1)
	a.Form.Fields[0].Value = ""
	a.key(Message{Key: "Enter"})
	if a.Form == nil || a.Status != "Enter a label" || len(a.Settings.UserMenu) != 1 || a.Form.Fields[1].Value != "echo 1" {
		t.Fatal("empty label deleted silently", a.Settings.UserMenu, a.Status)
	}
}
func TestEditAssociationKeepsPriority(t *testing.T) {
	setupHome(t)
	a := formTestApp(t)
	a.Settings.Associations = []Association{{Mask: "*.a", Command: "a"}, {Mask: "*.b", Command: "b"}}
	a.fileAssociations()
	a.Form.Select(2)
	a.Form.Fields[1].Value = "edited"
	a.key(Message{Key: "Enter"})
	if a.Settings.Associations[0].Command != "edited" || len(a.Settings.Associations) != 2 {
		t.Fatal("association moved", a.Settings.Associations)
	}
	a.fileAssociations()
	a.Form.Select(2)
	a.Form.Fields[0].Value = ""
	a.key(Message{Key: "Enter"})
	if a.Form == nil || a.Status != "Enter file masks" || a.Settings.Associations[0].Mask != "*.a" {
		t.Fatal("empty mask accepted", a.Status)
	}
}
func TestMaskGroupNameWithEqualsIsEditable(t *testing.T) {
	setupHome(t)
	a := formTestApp(t)
	a.Settings.MaskGroups = map[string]string{"a = b": "*.x"}
	a.settingsMenu("Groups of file masks")
	a.Form.Select(1)
	if a.Form.Fields[0].Value != "a = b" || a.Form.Fields[1].Value != "*.x" {
		t.Fatal("group parsed from label", a.Form.Fields)
	}
	a.Form.Fields[1].Value = ""
	a.key(Message{Key: "Enter"})
	if len(a.Settings.MaskGroups) != 0 {
		t.Fatal("group with = cannot be deleted", a.Settings.MaskGroups)
	}
	a.settingsMenu("Groups of file masks")
	a.Form.Select(0)
	a.Form.Fields[0].Value = "x=y"
	a.Form.Fields[1].Value = "*.y"
	a.key(Message{Key: "Enter"})
	if a.Form == nil || a.Status != "Invalid group name" || len(a.Settings.MaskGroups) != 0 {
		t.Fatal("name with = accepted")
	}
}
func TestDescriptionFileRejectsDots(t *testing.T) {
	setupHome(t)
	a := formTestApp(t)
	for _, bad := range []string{".", "..", "a/b", "  "} {
		a.settingsMenu("File descriptions")
		a.Dialog.Input = bad
		a.key(Message{Key: "Enter"})
		if a.Dialog == nil || a.Status != "Enter a file name without a path" || a.Settings.DescriptionFile != "descript.ion" {
			t.Fatalf("%q accepted", bad)
		}
		a.Dialog = nil
	}
	a.settingsMenu("File descriptions")
	a.Dialog.Input = " notes.txt "
	a.key(Message{Key: "Enter"})
	if a.Settings.DescriptionFile != "notes.txt" {
		t.Fatal("name not trimmed", a.Settings.DescriptionFile)
	}
	a.settingsMenu("Folder description files")
	a.Dialog.Input = "README, .."
	a.key(Message{Key: "Enter"})
	if a.Dialog == nil || strings.Contains(a.Settings.FolderDescriptions, "..") {
		t.Fatal("folder description .. accepted")
	}
}
func TestPromptCursorEditing(t *testing.T) {
	a := formTestApp(t)
	got := ""
	a.prompt("Make folder", "Name", "middle", func(s string) { got = s })
	a.Dialog.Selected = false
	a.key(Message{Key: "Home"})
	a.key(Message{Key: "<"})
	a.key(Message{Key: "End"})
	a.key(Message{Key: ">"})
	a.key(Message{Key: "ArrowLeft"})
	a.key(Message{Key: "ArrowLeft"})
	a.key(Message{Key: "Delete"})
	paste(a, "!!")
	if a.Dialog.Input != "<middl!!>" {
		t.Fatalf("%q", a.Dialog.Input)
	}
	a.key(Message{Key: "Home"})
	a.key(Message{Key: "Delete"})
	a.key(Message{Key: "Backspace"})
	a.key(Message{Key: "Enter"})
	if got != "middl!!>" {
		t.Fatalf("%q", got)
	}
}
