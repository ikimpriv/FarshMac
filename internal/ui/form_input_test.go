package ui

import (
	"encoding/json"
	"testing"
)

func paste(a *App, text string) {
	b, _ := json.Marshal(Message{Type: "paste", Text: text})
	a.Handle(string(b))
}
func TestFormPasteStripsControlCharacters(t *testing.T) {
	a := formTestApp(t)
	a.form("Create link", []Field{{Label: "Link name"}}, func([]Field) {})
	paste(a, "first\r\nsecond\tthird\x1b\n")
	if v := a.Form.Fields[0].Value; v != "first second third " || a.Status != "Pasted as a single line" {
		t.Fatalf("%q %q", v, a.Status)
	}
	a.form("Create link", []Field{{Label: "Link name"}}, func([]Field) {})
	a.Status = ""
	paste(a, "name\n")
	if a.Form.Fields[0].Value != "name" || a.Status != "" {
		t.Fatal("trailing newline kept or reported", a.Form.Fields[0].Value)
	}
}
func TestSecretPasteKeepsTabs(t *testing.T) {
	a := formTestApp(t)
	a.form("Login", []Field{{Label: "Password", Secret: true}}, func([]Field) {})
	paste(a, "pa\tss\r\n")
	if a.Form.Fields[0].Value != "pa\tss" {
		t.Fatalf("%q", a.Form.Fields[0].Value)
	}
}
