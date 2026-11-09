package fsops

import (
	"context"
	"errors"
	"os"
)

var ErrRetryMetadata = errors.New("retry metadata")

var ErrSkipped = errors.New("item skipped")

type ErrorAction int

const (
	ErrorCancel ErrorAction = iota
	ErrorRetry
	ErrorSkip
)

type Failure struct {
	Operation, Source, Destination string
	Err                            error
}
type Collision struct {
	Source, Destination         string
	SourceInfo, DestinationInfo os.FileInfo
}
type Resolution struct {
	Action      int
	Destination string
}

const (
	Replace = iota
	Skip
	Cancel
	Append
	Rename
)

// Attempt retries only the supplied step. In particular, a failed source deletion
// after a committed move must never repeat the copy or append.
func (o *Operation) Attempt(operation, src, dst string, fn func() error) error {
	for {
		if err := o.Check(); err != nil {
			return err
		}
		err := fn()
		if errors.Is(err, context.Canceled) {
			return ErrCancelled
		}
		if err == nil || errors.Is(err, ErrCancelled) || errors.Is(err, ErrSkipped) {
			return err
		}
		if o == nil || o.Failure == nil {
			return err
		}
		switch o.Failure(Failure{operation, src, dst, err}) {
		case ErrorRetry:
			continue
		case ErrorSkip:
			o.SkipItem(src)
			return ErrSkipped
		default:
			return ErrCancelled
		}
	}
}
func (o *Operation) SkipItem(src string) {
	if o == nil {
		return
	}
	if o.parent != nil {
		o.parent.SkipItem(src)
		return
	}
	o.mu.Lock()
	o.state.Skipped++
	o.mu.Unlock()
	o.CompleteTree(src)
}
func IgnoreSkipped(err error) error {
	if errors.Is(err, ErrSkipped) {
		return nil
	}
	return err
}
func (o *Operation) Resolve(c Collision, legacy Conflict) Resolution {
	if o != nil && o.Collision != nil {
		return o.Collision(c)
	}
	if legacy != nil {
		return Resolution{Action: legacy(c.Source, c.Destination)}
	}
	return Resolution{Action: Cancel}
}
