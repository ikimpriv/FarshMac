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
	// Fatal marks a step the operation cannot continue without (connecting,
	// opening the destination): only Retry or Cancel make sense.
	Fatal bool
}
type Collision struct {
	Source, Destination         string
	SourceInfo, DestinationInfo os.FileInfo
	// DestinationLink is the target of an existing symbolic link (file or broken).
	DestinationLink   string
	DestinationBroken bool
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
	return o.attempt(operation, src, dst, false, fn)
}

// AttemptRequired is Attempt for a step without which nothing can be done.
// It never reports ErrSkipped, so a failed step can't end as "Done".
func (o *Operation) AttemptRequired(operation, src, dst string, fn func() error) error {
	return o.attempt(operation, src, dst, true, fn)
}
func (o *Operation) attempt(operation, src, dst string, fatal bool, fn func() error) error {
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
		switch action := o.Failure(Failure{operation, src, dst, err, fatal}); {
		case action == ErrorRetry:
			continue
		case action == ErrorSkip && !fatal:
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
