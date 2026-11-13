package ui

import (
	"fmt"
	"strings"

	"farshmac/internal/kube"
)

// The manifest editor knows the object's schema from the cluster (OpenAPI
// v3, which CRDs publish too): the text is checked after every change and
// lines with problems are marked; Ctrl+Space completes property names and
// enum values at the cursor.

const manifestCheckLimit = 512 << 10

// manifestSchema is the schema of a manifest open in the editor, or nil.
func (d *Document) manifestSchema() *kube.Schema {
	if s, ok := d.Source.(*manifestSource); ok {
		return s.schema
	}
	return nil
}

// pollManifestCheck re-checks the manifest when its text changed.
func (a *App) pollManifestCheck() {
	d := a.Doc
	if d == nil || !d.Edit || d.File != nil {
		return
	}
	s := d.manifestSchema()
	if s == nil {
		return
	}
	if d.checked && d.checkedText == d.Text {
		return
	}
	d.checked, d.checkedText = true, d.Text
	d.Problems, d.problemRows = nil, nil
	if len(d.Text) > manifestCheckLimit {
		return
	}
	d.Problems = kube.CheckManifest([]byte(d.Text), s)
	d.problemRows = map[int]bool{}
	for _, p := range d.Problems {
		d.problemRows[p.Line-1] = true
	}
	a.dirty = true
}

// problemAt is the first problem on a row, if any.
func (d *Document) problemAt(row int) *kube.Problem {
	for i := range d.Problems {
		if d.Problems[i].Line-1 == row {
			return &d.Problems[i]
		}
	}
	return nil
}

// manifestStatus is the title suffix: the problem under the cursor, or the count.
func (d *Document) manifestStatus(row int) string {
	if d.manifestSchema() == nil || !d.Edit {
		return ""
	}
	if p := d.problemAt(row); p != nil {
		return fmt.Sprintf("  ⚠ %s", p.Message)
	}
	switch n := len(d.Problems); {
	case n == 1:
		return fmt.Sprintf("  ⚠ 1 problem (line %d)", d.Problems[0].Line)
	case n > 1:
		return fmt.Sprintf("  ⚠ %d problems (first at line %d)", n, d.Problems[0].Line)
	case d.checked:
		return "  ✓ schema"
	}
	return ""
}

// nextProblem moves the cursor to the next problem after the cursor row.
func (a *App) nextProblem() {
	d := a.Doc
	if len(d.Problems) == 0 {
		a.Status = "No problems found"
		return
	}
	row := d.Pos(d.Cursor).Row
	target := d.Problems[0]
	for _, p := range d.Problems {
		if p.Line-1 > row {
			target = p
			break
		}
	}
	d.remember()
	d.Move(d.At(target.Line-1, target.Column), false, false)
	a.Status = target.Message
}

// manifestComplete offers schema completions at the cursor (Ctrl+Space).
func (a *App) manifestComplete() {
	d := a.Doc
	s := d.manifestSchema()
	if s == nil {
		a.Status = "No schema for this document"
		return
	}
	pos := d.Pos(d.Cursor)
	prefix, items := kube.CompleteManifest([]byte(d.Text), pos.Row+1, pos.Col, s)
	if len(items) == 0 {
		a.Status = "No completions here"
		return
	}
	insert := func(c kube.Completion) {
		d.Insert(c.Insert)
		d.modified()
		a.dirty = true
	}
	if len(items) == 1 {
		insert(items[0])
		return
	}
	labels := make([]string, len(items))
	for i, c := range items {
		labels[i] = c.Label
		if c.Detail != "" {
			labels[i] += "  " + c.Detail
		}
	}
	title := "Complete"
	if prefix != "" {
		title = "Complete " + strings.TrimSpace(prefix) + "…"
	}
	a.choose(title, labels, func(i int) { insert(items[i]) })
}
