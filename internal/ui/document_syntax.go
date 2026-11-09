package ui

import (
	"context"
	"time"

	"farshmac/internal/highlight"
)

type syntaxReply struct {
	result highlight.Result
	err    error
}
type syntaxJob struct {
	generation uint64
	cancel     context.CancelFunc
	done       chan syntaxReply
}
type syntaxState struct {
	doc        *Document
	text, path string
	generation uint64
	due        time.Time
	attempted  bool
	large      bool
	job        *syntaxJob
	result     highlight.Result
	status     string
}

func (a *App) stopSyntax() {
	if a.syntax.job != nil {
		a.syntax.job.cancel()
	}
	a.syntax = syntaxState{}
}
func (a *App) pollSyntax() {
	if a.closed {
		return
	}
	s := &a.syntax
	d := a.Doc
	if !a.Settings.EditorSyntax || d == nil || d.Help || d.File != nil || d.Mode != "text" {
		d = nil
	}
	// Never retain the contents of a large document in the syntax scheduler.
	text, path := "", ""
	if d != nil {
		path = d.displayPath()
		if len(d.Text) <= highlight.MaxBytes {
			text = d.Text
		}
	}
	large := d != nil && len(d.Text) > highlight.MaxBytes
	if s.doc != d || s.text != text || s.path != path || large != s.large {
		if s.job != nil {
			s.job.cancel()
		}
		s.doc, s.text, s.path = d, text, path
		s.large = large
		s.generation++
		s.result = highlight.Result{}
		s.attempted = false
		s.due = time.Now().Add(200 * time.Millisecond)
		s.status = ""
		if large {
			s.status = "syntax off: >1 MiB"
			s.attempted = true
		}
		a.dirty = true
	}
	if s.job != nil {
		select {
		case reply := <-s.job.done:
			s.job.cancel()
			if s.job.generation == s.generation {
				s.result = reply.result
				s.status = reply.result.Language
				if reply.err != nil {
					s.status = "syntax paused (limit/error)"
				}
				a.dirty = true
			}
			s.job = nil
		default:
			return
		}
	}
	if d == nil || s.attempted || time.Now().Before(s.due) {
		return
	}
	s.attempted = true
	ctx, cancel := context.WithCancel(context.Background())
	job := &syntaxJob{generation: s.generation, cancel: cancel, done: make(chan syntaxReply, 1)}
	s.job = job
	// Snapshot immutable strings only. Workers never touch Document or App.
	run := a.syntaxRun
	if run == nil {
		run = highlight.Run
	}
	go func() { result, err := run(ctx, path, text); job.done <- syntaxReply{result, err} }()
}
func (a *App) documentSyntax(d *Document) highlight.Result {
	s := &a.syntax
	if a.Settings.EditorSyntax && s.doc == d && s.text == d.Text && s.path == d.displayPath() {
		return s.result
	}
	return highlight.Result{}
}
