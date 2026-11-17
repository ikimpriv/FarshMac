package ui

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"farshmac/internal/kube"
)

// foreignStream is a byte stream shown in the terminal area instead of the
// local shell: an interactive SSH/container exec, or followed logs. Keyboard
// input and resizes go to it while it runs; the shell waits (C10, G13).
// startForeign shows the terminal and routes it to s until s ends; done
// runs afterwards on the engine queue (for example to refresh a panel).
func (a *App) startForeign(s foreignStream, label string, done func()) {
	a.activateTerminal("stream")
	a.installForeign(s, label, done)
}
func (a *App) installForeign(s foreignStream, label string, done func()) {
	if a.Foreign != nil {
		a.Foreign.Close()
	}
	a.Shells.ClearRemote()
	a.foreignEnded = false
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
			a.acceptForeignOutput(chunk)
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
		a.acceptForeignOutput(chunk)
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
	if m := a.Shells.Remote(); m != nil {
		r := m.End()
		a.pending = append(a.pending, r.Output...)
		if r.Completed {
			a.pending = append(a.pending, ("\r\n" + status + "\r\n")...)
			a.TermVisible, a.foreignEnded = false, false
			a.Status, a.dirty = status, true
			if done != nil {
				done()
			}
			return
		}
	}
	a.pending = append(a.pending, "\r\n\x1b[7m "+status+" · any key returns to the panels \x1b[0m\r\n"...)
	a.foreignEnded = true
	if a.Shells.RemoteKey() != "" {
		a.Status = status
	} else {
		a.setShellStatus(status)
	}
	a.dirty = true
	if done != nil {
		done()
	}
}

// foreignReturn consumes the first key after a remote command ended and
// returns to the panels, instead of passing the key to the local shell.
func (a *App) foreignReturn() bool {
	if !a.foreignEnded {
		return false
	}
	a.foreignEnded = false
	if a.TermVisible {
		a.TermVisible = false
		a.dirty = true
	}
	return true
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
func (s *logStream) Write(p []byte) error {
	if bytes.IndexByte(p, 0x03) >= 0 { // Ctrl+C ends the stream like ⌘.
		s.cancel()
	}
	return nil
}
func (s *logStream) Resize(int, int) error { return nil }
func (s *logStream) Output() <-chan []byte { return s.out }
func (s *logStream) Done() <-chan struct{} { return s.done }
func (s *logStream) Err() error            { return s.err }
func (s *logStream) Close()                { s.cancel() }
