package ui

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"farshmac/internal/kube"
)

// foreignStream is a byte stream shown in the terminal area instead of the
// local shell: an interactive exec in a container, or followed logs. Keyboard
// input and resizes go to it while it runs; the shell waits (C10, G13).
type foreignStream interface {
	Write(p []byte) error
	Resize(cols, rows int) error
	Output() <-chan []byte
	Done() <-chan struct{}
	Err() error
	Close()
}

// startForeign shows the terminal and routes it to s until s ends; done
// runs afterwards on the engine queue (for example to refresh a panel).
func (a *App) startForeign(s foreignStream, label string, done func()) {
	if a.Foreign != nil {
		a.Foreign.Close()
	}
	a.Foreign, a.foreignLabel, a.foreignDone = s, label, done
	a.TermVisible = true
	a.Status = label + " · ⌘. stops"
	a.dirty = true
	a.error(s.Resize(a.Cols, a.Rows-1))
}

// stopForeign ends the stream from the UI side (⌘. in panels, quitting).
func (a *App) stopForeign() {
	if a.Foreign != nil {
		a.Foreign.Close()
	}
}

// pollForeign moves stream output to the renderer and notices the end.
func (a *App) pollForeign() {
	s := a.Foreign
	if s == nil {
		return
	}
	for i := 0; i < 64 && len(a.pending) < 256*1024; i++ {
		select {
		case chunk, ok := <-s.Output():
			if !ok {
				a.finishForeign()
				return
			}
			a.pending = append(a.pending, chunk...)
			a.dirty = true
		default:
			select {
			case <-s.Done():
				a.finishForeign()
			default:
			}
			return
		}
	}
}
func (a *App) finishForeign() {
	s := a.Foreign
	if s == nil {
		return
	}
	// Drain what is left so the final lines are not lost.
	for chunk := range s.Output() {
		a.pending = append(a.pending, chunk...)
	}
	a.Foreign = nil
	done := a.foreignDone
	a.foreignDone = nil
	status := a.foreignLabel + " ended · ⌘O panels / terminal"
	if err := s.Err(); err != nil {
		if code, ok := kube.ExitCode(err); ok {
			status = fmt.Sprintf("%s: exit %d · ⌘O panels / terminal", a.foreignLabel, code)
		} else {
			status = a.foreignLabel + ": " + err.Error()
		}
	}
	a.pending = append(a.pending, "\r\n"...)
	a.setShellStatus(status)
	a.dirty = true
	if done != nil {
		done()
	}
}

// logStream follows a log in the terminal area: read-only, ⌘. stops it.
type logStream struct {
	cancel context.CancelFunc
	out    chan []byte
	done   chan struct{}
	err    error
}

func newLogStream(ctx context.Context, open func(context.Context) (io.ReadCloser, error)) *logStream {
	ctx, cancel := context.WithCancel(ctx)
	s := &logStream{cancel: cancel, out: make(chan []byte, 16), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		defer close(s.out)
		r, err := open(ctx)
		if err != nil {
			s.err = err
			return
		}
		defer r.Close()
		buf := make([]byte, 32*1024)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				chunk := bytes.ReplaceAll(buf[:n], []byte("\n"), []byte("\r\n"))
				select {
				case s.out <- chunk:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				if err != io.EOF && ctx.Err() == nil {
					s.err = err
				}
				return
			}
		}
	}()
	return s
}
func (s *logStream) Write([]byte) error    { return nil }
func (s *logStream) Resize(int, int) error { return nil }
func (s *logStream) Output() <-chan []byte { return s.out }
func (s *logStream) Done() <-chan struct{} { return s.done }
func (s *logStream) Err() error            { return s.err }
func (s *logStream) Close()                { s.cancel() }
