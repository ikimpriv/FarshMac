package shellsession

import (
	"context"
	"fmt"
	"sync"
)

// Deferred owns resolution and exec, without ever forwarding input to
// the local PTY while connecting or after a remote failure.
type Deferred struct {
	input      chan []byte
	mu         sync.Mutex
	inner      Stream
	cols, rows int
	cancel     context.CancelFunc
	out        chan []byte
	done       chan struct{}
	err        error
}

func NewDeferred(cols, rows int, open func(context.Context) (Stream, error)) *Deferred {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Deferred{cols: cols, rows: rows, cancel: cancel, input: make(chan []byte, 32), out: make(chan []byte, 16), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		defer close(s.out)
		inner, err := open(ctx)
		if err != nil {
			s.err = err
			return
		}
		defer inner.Close()
		stopClose := context.AfterFunc(ctx, inner.Close)
		defer stopClose()
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case data := <-s.input:
					if inner.Write(data) != nil {
						return
					}
				}
			}
		}()
		defer cancel()
		if prefixed, ok := inner.(*prefixedStream); ok {
			select {
			case s.out <- prefixed.prefix:
			case <-ctx.Done():
				return
			}
		}
		s.mu.Lock()
		s.inner = inner
		_ = inner.Resize(s.cols, s.rows)
		s.mu.Unlock()
		for {
			select {
			case <-ctx.Done():
				return
			case data, ok := <-inner.Output():
				if !ok {
					s.err = inner.Err()
					return
				}
				select {
				case s.out <- data:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return s
}
func (s *Deferred) Write(data []byte) error {
	s.mu.Lock()
	ready := s.inner != nil
	s.mu.Unlock()
	if !ready {
		if string(data) == "\x1b" || string(data) == "\x03" {
			s.cancel()
		}
		return nil
	}
	if len(data) > 64<<10 {
		return fmt.Errorf("remote terminal input exceeds 64 KiB")
	}
	select {
	case <-s.done:
		return fmt.Errorf("remote shell closed")
	default:
	}
	select {
	case s.input <- append([]byte(nil), data...):
		return nil
	default:
		return fmt.Errorf("remote terminal input queue is full")
	}
}
func (s *Deferred) Resize(cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cols, s.rows = cols, rows
	if s.inner != nil {
		return s.inner.Resize(cols, rows)
	}
	return nil
}
func (s *Deferred) Output() <-chan []byte { return s.out }
func (s *Deferred) Done() <-chan struct{} { return s.done }
func (s *Deferred) Err() error            { <-s.done; return s.err }
func (s *Deferred) Close()                { s.cancel() }

func (s *Deferred) Connected() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.inner != nil }

type prefixedStream struct {
	Stream
	prefix []byte
}

func WithPrefix(s Stream, prefix []byte) Stream { return &prefixedStream{Stream: s, prefix: prefix} }
