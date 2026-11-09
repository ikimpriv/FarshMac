package textfile

import (
	"bytes"
	"strings"
	"testing"
)

func TestUnicodeEditingUndoRedo(t *testing.T) {
	b := New("Привет\r\n世界\n")
	b.Cursor = b.At(0, 6)
	b.Insert("!")
	b.Cursor = b.At(1, 1)
	b.Replace(b.Cursor, b.Next(b.Cursor), "界!")
	if b.Text != "Привет!\n世界!\n" {
		t.Fatal(b.Text)
	}
	b.Undo()
	b.Undo()
	if b.Text != "Привет\n世界\n" {
		t.Fatal("undo", b.Text)
	}
	b.Redo()
	b.Redo()
	if b.Text != "Привет!\n世界!\n" {
		t.Fatal("redo", b.Text)
	}
	b.Undo()
	b.Insert("x")
	if len(b.RedoStack) != 0 {
		t.Fatal("redo survived branching edit")
	}
}
func TestSelectionOverwriteAndReadOnly(t *testing.T) {
	b := New("one two\nthree")
	b.Move(4, false, false)
	b.Move(7, true, false)
	if b.Selected() != "two" {
		t.Fatal(b.Selected())
	}
	b.Insert("2")
	if b.Text != "one 2\nthree" {
		t.Fatal(b.Text)
	}
	b.Cursor = 0
	b.Overwrite = true
	b.Insert("ONE")
	if b.Text != "ONE 2\nthree" {
		t.Fatal(b.Text)
	}
	b.ReadOnly = true
	before := b.Text
	b.Insert("changed")
	b.Undo()
	if b.Text != before {
		t.Fatal("read-only mutated")
	}
	if _, err := b.ReplaceAll(Search{Query: "ONE"}, "one"); err == nil {
		t.Fatal("read-only replace accepted")
	}
}
func TestRectangularBlockAndIndent(t *testing.T) {
	b := New("abcd\n1234\nwxyz")
	b.Cursor = b.At(0, 1)
	b.Move(b.At(2, 3), true, true)
	if b.Selected() != "bc\n23\nxy" {
		t.Fatal(b.Selected())
	}
	b.Insert("!")
	if b.Text != "a!d\n1!4\nw!z" {
		t.Fatal(b.Text)
	}
	b.Undo()
	if b.Text != "abcd\n1234\nwxyz" {
		t.Fatal("rectangle undo", b.Text)
	}
	b.Anchor = 0
	b.Cursor = len(b.Text)
	b.Indent(false)
	if b.Text != "    abcd\n    1234\n    wxyz" {
		t.Fatal(b.Text)
	}
	b.Undo()
	if b.Text != "abcd\n1234\nwxyz" {
		t.Fatal(b.Text)
	}
}
func TestSearchAndRegexReplacement(t *testing.T) {
	b := New("cat scatter CAT\nname=42\nname=7")
	q := Search{Query: "cat", Whole: true}
	found, err := b.Find(q, false)
	if err != nil || !found || b.Selected() != "cat" {
		t.Fatal(found, err, b.Selected())
	}
	b.Find(q, false)
	if b.Selected() != "CAT" {
		t.Fatal("next", b.Selected())
	}
	b.Find(q, true)
	if b.Selected() != "cat" {
		t.Fatal("previous", b.Selected())
	}
	n, err := b.ReplaceAll(Search{Query: `^name=(\d+)$`, Regex: true}, "number:$1")
	if err != nil || n != 2 || !strings.Contains(b.Text, "number:42\nnumber:7") {
		t.Fatal(n, err, b.Text)
	}
	b.Undo()
	if !strings.Contains(b.Text, "name=42") {
		t.Fatal("replace-all not one undo transaction")
	}
	n, err = b.ReplaceAll(Search{Query: `\bcat\b`, Regex: true}, "dog")
	if err != nil || n != 2 || !strings.HasPrefix(b.Text, "dog scatter dog") {
		t.Fatal(n, err, b.Text)
	}
	if _, err = b.Find(Search{Query: "[", Regex: true}, false); err == nil {
		t.Fatal("invalid regex accepted")
	}
}
func TestCodepagesRoundTripAndRejectLoss(t *testing.T) {
	for _, cp := range Codepages {
		sample := "Hello\r\n"
		if cp == "UTF-8" || cp == "UTF-16LE" || cp == "UTF-16BE" || cp == "Windows-1251" || cp == "CP866" || cp == "KOI8-R" {
			sample = "Привет\r\n"
		}
		for _, bom := range []bool{false, true} {
			raw, err := Encode(sample, cp, bom)
			if err != nil {
				t.Fatal(cp, err)
			}
			decoded, gotBOM, err := Decode(raw, cp)
			if err != nil || decoded != sample {
				t.Fatal(cp, decoded, err)
			}
			again, err := Encode(decoded, cp, gotBOM)
			if err != nil || !bytes.Equal(raw, again) {
				t.Fatal(cp, "round trip")
			}
		}
	}
	if _, err := Encode("😀", "CP866", false); err == nil {
		t.Fatal("unrepresentable symbol silently lost")
	}
	if _, _, err := Decode([]byte{0xff}, "UTF-8"); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	if _, _, err := Decode([]byte{0xff, 0xfe, 0x12}, "UTF-16LE"); err == nil {
		t.Fatal("partial UTF16 accepted")
	}
}
