package textfile

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

func TestNewlineIndent(t *testing.T) {
	for _, tc := range []struct {
		text           string
		cursor, anchor int
		auto           bool
		want           string
	}{
		{"\t  hello", 8, -1, true, "\t  hello\n\t  "},
		{"    hello", 2, -1, true, "  \n    hello"},
		{"    hello", 0, -1, true, "\n    hello"},
		{"  first\n    second", 18, 4, true, "  fi\n  "},
		{"  hello", 4, -1, false, "  he\nllo"},
	} {
		b := New(tc.text)
		b.Cursor = tc.cursor
		b.Anchor = tc.anchor
		b.Overwrite = true
		b.Newline(tc.auto)
		if b.Text != tc.want {
			t.Errorf("%q => %q, want %q", tc.text, b.Text, tc.want)
		}
		b.Undo()
		if b.Text != tc.text {
			t.Fatal("newline undo")
		}
		b.Redo()
		if b.Text != tc.want {
			t.Fatal("newline redo")
		}
	}
}
func TestBlockIndentSelection(t *testing.T) {
	b := New("a\nb\nc")
	b.Anchor = 0
	b.Cursor = 4
	b.IndentWithOptions(false, 2, true)
	if b.Text != "  a\n  b\nc" || b.Cursor != 8 || b.Anchor != 2 {
		t.Fatalf("%q %d %d", b.Text, b.Cursor, b.Anchor)
	}
	b.IndentWithOptions(true, 2, true)
	if b.Text != "a\nb\nc" || b.Cursor != 4 || b.Anchor != 0 {
		t.Fatalf("%q %d %d", b.Text, b.Cursor, b.Anchor)
	}
	b.Anchor = -1
	b.Cursor = 0
	b.IndentWithOptions(false, 8, false)
	if b.Text != "\ta\nb\nc" {
		t.Fatal(b.Text)
	}
}
func TestIncrementalLineIndex(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	b := New("abc\ndef\n")
	parts := []string{"", "x", "\n", "\n\n", "a\nb\n"}
	check := func() {
		t.Helper()
		fresh := New(b.Text)
		if !reflect.DeepEqual(b.Lines(), fresh.Lines()) {
			t.Fatalf("index %v want %v for %q", b.Lines(), fresh.Lines(), b.Text)
		}
	}
	for i := 0; i < 1000; i++ {
		check()
		start := rng.Intn(len(b.Text) + 1)
		end := start + rng.Intn(len(b.Text)-start+1)
		b.Replace(start, end, parts[rng.Intn(len(parts))])
		check()
		b.Undo()
		check()
		b.Redo()
		check()
	}
}
func TestUndoBudgetAndBackingSlots(t *testing.T) {
	b := New("")
	chunk := strings.Repeat("x", 1<<20)
	for i := 0; i < 40; i++ {
		b.Replace(0, len(b.Text), chunk+string(rune('A'+i)))
	}
	if b.historyBytes > MaxUndoBytes || len(b.UndoStack) >= 40 {
		t.Fatalf("unbounded undo: %d/%d", b.historyBytes, len(b.UndoStack))
	}
	old := len(b.UndoStack)
	b.Undo()
	slot := b.UndoStack[:old][old-1]
	if slot.Old != "" || slot.New != "" {
		t.Fatal("popped history slot retains text")
	}
	b.Replace(0, len(b.Text), "small")
	if len(b.RedoStack) != 0 || b.historyBytes > MaxUndoBytes {
		t.Fatal("branching history budget")
	}
}
func BenchmarkEditLargeFile(b *testing.B) {
	doc := New(strings.Repeat("a fairly short line of text\n", 200000))
	doc.Lines()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc.Replace(0, 1, "b")
		doc.Undo()
	}
}
