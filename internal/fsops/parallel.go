package fsops

import "context"

// Each file worker owns its metadata/progress maps. Only aggregate counters and
// the serialized UI question channel are shared with the parent operation.
func (o *Operation) Fork(ctx context.Context) *Operation {
	child := NewOperation(o.Snapshot().Label)
	child.parent = o
	child.ctx = ctx
	child.IgnoreDSStore = o.IgnoreDSStore
	if o.CopyMetadata != nil {
		options := *o.CopyMetadata
		child.CopyMetadata = &options
	}
	child.MetadataFailure = o.MetadataFailure
	child.MetadataLimitation = o.MetadataLimitation
	child.Failure = o.Failure
	child.BeforeDelete = o.BeforeDelete
	child.Collision = o.Collision
	return child
}
func (o *Operation) Parallel(workers int)               { o.mu.Lock(); o.state.Workers = workers; o.mu.Unlock() }
func (o *Operation) Worker(delta int)                   { o.mu.Lock(); o.state.Active += delta; o.mu.Unlock() }
func ApplyMetadata(src, dst string, o *Operation) error { return o.preserveMetadata(src, dst) }
