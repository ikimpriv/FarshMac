package fsops

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type Progress struct {
	Workers, Active                                           int
	Label, Path                                               string
	Bytes, TotalBytes, Files, TotalFiles, FileBytes, FileSize int64
	Scanning, Streaming                                       bool
	Started                                                   time.Time
}
type Operation struct {
	Questions sync.Mutex
	parent    *Operation
	// Set before starting a worker. Visibility of other hidden files is UI-only.
	IgnoreDSStore bool
	CopyMetadata  *MetadataOptions
	// MetadataFailure is invoked by the worker before accepting a lossy move.
	// Return nil to continue, or an error to retain the source and stop.
	MetadataFailure func(src, dst string, err error) error
	metadataCopier  func(src, dst string, o *Operation) error
	moving          bool
	hardlinks       map[fileIdentity]copiedLink
	ctx             context.Context
	stop            context.CancelFunc
	pauseWake       chan struct{}
	mu              sync.Mutex
	state           Progress
	sizes           map[string]int64
	completed       map[string]bool
	cancel          atomic.Bool
}

// MetadataOptions are independent; nil retains the legacy copy policy.
type MetadataOptions struct {
	Permissions bool // mode, owner/group and ACL
	Attributes  bool // extended attributes, BSD flags and timestamps
}

func (o *Operation) metadataOptions() (MetadataOptions, bool) {
	if o == nil {
		return MetadataOptions{}, false
	}
	if o.moving {
		return MetadataOptions{Permissions: true, Attributes: true}, true
	}
	if o.CopyMetadata != nil {
		return *o.CopyMetadata, true
	}
	return MetadataOptions{}, false
}

func NewOperation(label string) *Operation {
	ctx, stop := context.WithCancel(context.Background())
	return &Operation{ctx: ctx, stop: stop, state: Progress{Label: label, Scanning: true, Started: time.Now()}, sizes: map[string]int64{}, completed: map[string]bool{}}
}
func (o *Operation) Context() context.Context {
	if o == nil {
		return context.Background()
	}
	return o.ctx
}
func (o *Operation) Cancel() {
	if o != nil {
		o.cancel.Store(true)
		if o.stop != nil {
			o.stop()
		}
		o.Resume()
	}
}
func (o *Operation) Pause() {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.pauseWake == nil {
		o.pauseWake = make(chan struct{})
	}
}
func (o *Operation) Resume() {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.pauseWake != nil {
		close(o.pauseWake)
		o.pauseWake = nil
	}
}
func (o *Operation) Check() error {
	if o == nil {
		return nil
	}
	if o.parent != nil {
		if err := o.parent.Check(); err != nil {
			return err
		}
	}
	if o.ctx != nil && o.ctx.Err() != nil {
		return ErrCancelled
	}
	for {
		if o.cancel.Load() {
			return ErrCancelled
		}
		o.mu.Lock()
		wake := o.pauseWake
		o.mu.Unlock()
		if wake == nil {
			return nil
		}
		<-wake
	}
}

func (o *Operation) Snapshot() Progress {
	if o == nil {
		return Progress{}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.state
}
func (o *Operation) Scan(paths []string) error {
	if o == nil {
		return nil
	}
	for _, root := range paths {
		err := WalkDir(root, o, func(path string, d fs.DirEntry, err error) error {
			if e := o.Check(); e != nil {
				return e
			}
			if err != nil {
				return err
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			size := int64(0)
			if info.Mode().IsRegular() {
				size = info.Size()
			}
			o.Add(path, size)
			return nil
		})
		if err != nil {
			return err
		}
	}
	o.Ready()
	return nil
}
func (o *Operation) Add(path string, size int64) {
	if o == nil {
		return
	}
	if o.parent != nil {
		o.parent.Add(path, size)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	key, _ := filepath.Abs(path)
	if _, ok := o.sizes[key]; ok {
		return
	}
	o.sizes[key] = size
	o.state.TotalBytes += size
	o.state.TotalFiles++
	o.state.Path = path
}

// ScanProgress reports aggregate-only scan progress, without retaining every
// pathname as Add does for copy/delete accounting. Safe for concurrent scanners.
func (o *Operation) ScanProgress(path string, items, bytes int64) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.state.Path = path
	o.state.TotalFiles += items
	o.state.TotalBytes += bytes
	o.state.Bytes += bytes
}
func (o *Operation) Ready() {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.state.Scanning = false
	o.state.Streaming = false
	o.mu.Unlock()
}

// Streaming reports progress without claiming that the total is known yet.
func (o *Operation) Streaming() {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.state.Scanning = false
	o.state.Streaming = true
	o.mu.Unlock()
}
func (o *Operation) Begin(path string, size int64) {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.state.Path = path
	o.state.FileBytes = 0
	o.state.FileSize = size
	o.mu.Unlock()
}
func (o *Operation) Advance(n int64) {
	if o == nil {
		return
	}
	if o.parent != nil {
		o.parent.Advance(n)
	}
	o.mu.Lock()
	o.state.Bytes += n
	o.state.FileBytes += n
	o.mu.Unlock()
}
func (o *Operation) Complete(path string) {
	if o == nil {
		return
	}
	if o.parent != nil {
		o.parent.Complete(path)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	key, _ := filepath.Abs(path)
	if o.completed[key] {
		return
	}
	o.completed[key] = true
	o.state.Files++
}

func (o *Operation) completedPath(path string) bool {
	key, _ := filepath.Abs(path)
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.completed[key]
}
func (o *Operation) CompleteTree(path string) {
	if o == nil {
		return
	}
	if o.parent != nil {
		o.parent.CompleteTree(path)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	path, _ = filepath.Abs(path)
	prefix := path + string(filepath.Separator)
	for p, size := range o.sizes {
		if !o.completed[p] && (p == path || strings.HasPrefix(p, prefix)) {
			o.completed[p] = true
			o.state.Bytes += size
			o.state.Files++
		}
	}
	o.state.Path = path
	o.state.FileBytes = o.sizes[path]
	o.state.FileSize = o.sizes[path]
}

type progressReader struct {
	r io.Reader
	o *Operation
}

func (o *Operation) Reader(r io.Reader) io.Reader { return progressReader{r, o} }

func (r progressReader) Read(b []byte) (int, error) {
	if e := r.o.Check(); e != nil {
		return 0, e
	}
	n, e := r.r.Read(b)
	r.o.Advance(int64(n))
	return n, e
}

var copyBuffers = sync.Pool{New: func() any { b := make([]byte, 256*1024); return &b }}

func copyProgress(w io.Writer, r io.Reader, o *Operation) (int64, error) {
	b := copyBuffers.Get().(*[]byte)
	defer copyBuffers.Put(b)
	// Hide ReaderFrom so os.File cannot bypass our reusable buffer.
	return io.CopyBuffer(struct{ io.Writer }{w}, progressReader{r, o}, *b)
}
func operation(ops []*Operation) *Operation {
	if len(ops) > 0 {
		return ops[0]
	}
	return nil
}

// Remove walks without following symbolic links and reports each removed entry.
func Remove(path string, o *Operation) error {
	if o.Ignored(path) {
		return o.Check()
	}
	return removeTree(path, o)
}

// A selected directory is removed completely, including its Finder metadata.
// Do not follow symlinks or include ignored metadata in progress totals.
func removeTree(path string, o *Operation) error {
	return removeTreeWith(path, o, os.Remove)
}
func removeTreeWith(path string, o *Operation, remove func(string) error) error {
	if e := o.Check(); e != nil {
		return e
	}
	info, e := os.Lstat(path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	for attempt := 0; ; attempt++ {
		if info.IsDir() {
			if e = readDirectory(path, o, func(child os.DirEntry) error { return removeTreeWith(filepath.Join(path, child.Name()), o, remove) }); e != nil && !os.IsNotExist(e) {
				return e
			}
		}
		if e = o.Check(); e != nil {
			return e
		}
		e = remove(path)
		if e == nil || os.IsNotExist(e) {
			break
		}
		if !info.IsDir() || !errors.Is(e, syscall.ENOTEMPTY) || attempt >= 2 {
			return e
		}
		// A live directory can gain Finder metadata or skip entries while being
		// enumerated. Reopen it, but never retry against a replacement directory.
		current, statErr := os.Lstat(path)
		if statErr != nil || !os.SameFile(info, current) {
			return e
		}
	}
	if o != nil && !o.Ignored(path) && !o.completedPath(path) {
		o.Begin(path, info.Size())
		if info.Mode().IsRegular() {
			o.Advance(info.Size())
		}
		o.Complete(path)
	}
	return nil
}
