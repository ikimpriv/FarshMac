package ui

import (
	"context"
	"time"
)

// contextLike is what cached cluster reads outside a background task use:
// an unbounded context, since they only consult caches or short calls.
type contextLike = context.Context

type backgroundContext struct{}

func (backgroundContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (backgroundContext) Done() <-chan struct{}       { return nil }
func (backgroundContext) Err() error                  { return nil }
func (backgroundContext) Value(any) any               { return nil }
