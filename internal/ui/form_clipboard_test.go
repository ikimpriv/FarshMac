package ui

import "testing"

func formTestApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a
}
func TestFormCopyOnButtonDoesNotPanic(t *testing.T) {
	a := formTestApp(t)
	for _, kind := range []string{"copy", "cut"} {
		for _, extra := range []int{0, 1} {
			a.form("Test", []Field{{Label: "Name", Value: "keep"}, {Label: "Flag", Toggle: true}}, func([]Field) {})
			a.Form.Focus = len(a.Form.Fields) + extra
			a.Clipboard = nil
			a.Handle(`{"Type":"` + kind + `"}`)
			if a.Clipboard != nil || a.Form.Fields[0].Value != "keep" {
				t.Fatalf("%s on button %d changed clipboard or field", kind, extra)
			}
		}
	}
	a.form("Empty", nil, func([]Field) {})
	a.Form.Focus = 1
	a.Handle(`{"Type":"copy"}`)
	if a.Clipboard != nil {
		t.Fatal("empty form copied")
	}
}
func TestChooseCopyCopiesCurrentItem(t *testing.T) {
	a := formTestApp(t)
	a.choose("List", []string{"first", "second"}, func(int) {})
	a.Form.Focus = 1
	a.Handle(`{"Type":"cut"}`)
	if a.Clipboard == nil || *a.Clipboard != "second" || a.Form == nil || len(a.Form.Choice) != 2 {
		t.Fatal("choose copy did not copy current item")
	}
	a.confirm("Question", "full message", []string{"OK", "Cancel"}, func(int) {})
	a.Form.Remember = true
	a.Form.Focus = len(a.Form.Choice)
	a.Handle(`{"Type":"copy"}`)
	if *a.Clipboard != "full message" {
		t.Fatal("confirm copy did not copy message")
	}
}
func TestSecretFieldNeverReachesClipboard(t *testing.T) {
	a := formTestApp(t)
	a.form("Secret", []Field{{Label: "User", Value: "u"}, {Label: "Password", Value: "pw", Secret: true}}, func([]Field) {})
	a.Form.Focus = 1
	a.key(Message{Key: "a", Meta: true})
	a.Handle(`{"Type":"copy"}`)
	if a.Clipboard != nil || a.Form.Fields[1].Value != "pw" || a.Status != "Secret fields are not copied" {
		t.Fatal("secret copied")
	}
	a.Handle(`{"Type":"cut"}`)
	if a.Clipboard != nil || a.Form.Fields[1].Value != "" {
		t.Fatal("secret cut reached clipboard or kept value")
	}
}
