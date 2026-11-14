package kube

import (
	"context"
	"errors"
	"io"
	"sync"

	"k8s.io/client-go/tools/remotecommand"
)

// ExecStream is an interactive command in a container with a TTY: keyboard
// bytes go to Write, output arrives on Output in order, Resize follows the
// window. It feeds the terminal area while a container command runs (C10).
type ExecStream struct {
	ctx    context.Context
	cancel context.CancelFunc
	stdin  *io.PipeWriter
	sizes  chan remotecommand.TerminalSize
	output chan []byte
	done   chan struct{}

	mu     sync.Mutex
	err    error
	closed bool
}

// StartExec runs command with a TTY of cols×rows. The stream ends when the
// command exits, the context is cancelled or Close is called.
func StartExec(ctx context.Context, x Executor, ref ContainerRef, command []string, cols, rows int) (*ExecStream, error) {
	if len(command) == 0 {
		return nil, errors.New("empty command")
	}
	ctx, cancel := context.WithCancel(ctx)
	pr, pw := io.Pipe()
	s := &ExecStream{
		ctx:    ctx,
		cancel: cancel,
		stdin:  pw,
		sizes:  make(chan remotecommand.TerminalSize, 16),
		output: make(chan []byte, 64),
		done:   make(chan struct{}),
	}
	// The initial size is the first item in the queue, as kubectl does.
	s.sizes <- remotecommand.TerminalSize{Width: uint16(cols), Height: uint16(rows)}
	go func() {
		err := x.Stream(ctx, ref, ExecOptions{Command: command, Stdin: pr, Stdout: &chanWriter{ctx: ctx, ch: s.output}, TTY: true, Resize: s.sizes})
		s.mu.Lock()
		if err != nil && ctx.Err() != nil && !isExitError(err) {
			err = ctx.Err()
		}
		s.err = err
		s.mu.Unlock()
		_ = pr.CloseWithError(io.EOF)
		close(s.output)
		close(s.done)
		cancel()
	}()
	return s, nil
}

func isExitError(err error) bool {
	_, ok := ExitCode(err)
	return ok
}

// Write sends keyboard bytes to the command's stdin. Control characters are
// plain bytes: the TTY in the container turns 0x03 into SIGINT.
func (s *ExecStream) Write(p []byte) error {
	select {
	case <-s.done:
		return io.ErrClosedPipe
	default:
	}
	_, err := s.stdin.Write(p)
	return err
}

// Resize queues a new terminal size; it never blocks the caller.
func (s *ExecStream) Resize(cols, rows int) error {
	select {
	case <-s.done:
		return io.ErrClosedPipe
	default:
	}
	size := remotecommand.TerminalSize{Width: uint16(cols), Height: uint16(rows)}
	select {
	case s.sizes <- size:
	default:
		// The queue is full: drop the oldest pending size and keep the newest.
		select {
		case <-s.sizes:
		default:
		}
		select {
		case s.sizes <- size:
		default:
		}
	}
	return nil
}

// Output delivers stdout and stderr merged by the TTY; closed when the
// stream ends.
func (s *ExecStream) Output() <-chan []byte { return s.output }

// Done is closed when the command has finished for any reason.
func (s *ExecStream) Done() <-chan struct{} { return s.done }

// Err is nil for a clean exit, a context error after cancellation, or the
// exit error (see ExitCode) of a command that failed.
func (s *ExecStream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Close ends the command: stdin is closed and the context cancelled.
func (s *ExecStream) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	_ = s.stdin.Close()
	s.cancel()
}

// chanWriter hands output chunks to the consumer; it gives up when the
// stream's context ends so a vanished reader cannot block the exec.
type chanWriter struct {
	ctx context.Context
	ch  chan []byte
}

func (w *chanWriter) Write(p []byte) (int, error) {
	chunk := make([]byte, len(p))
	copy(chunk, p)
	select {
	case w.ch <- chunk:
		return len(p), nil
	case <-w.ctx.Done():
		return 0, w.ctx.Err()
	}
}
