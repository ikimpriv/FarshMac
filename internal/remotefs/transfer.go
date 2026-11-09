package remotefs

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"farshmac/internal/fsops"
)

type Endpoint struct{ Remote *Config }

func (e Endpoint) Open(ctx context.Context) (FS, error) {
	if e.Remote != nil {
		return e.Remote.Dial(ctx)
	}
	return Local{}, nil
}
func (e Endpoint) Same(other Endpoint) bool {
	if e.Remote == nil || other.Remote == nil {
		return e.Remote == nil && other.Remote == nil
	}
	return e.Remote.Label() == other.Remote.Label()
}

type Pair struct{ Source, Destination string }
type CopyOptions struct {
	PermissionsMode     *os.FileMode
	ExpectedDestination os.FileInfo
	Workers             int
	Metadata            fsops.MetadataOptions
	Conflict            fsops.Conflict
}
type job struct {
	Pair
	Info os.FileInfo
}

// Copy transfers files through a bounded queue. Remote workers have independent
// sessions (FTP control connections must never be shared concurrently).
func Copy(source, target Endpoint, pairs []Pair, options CopyOptions, o *fsops.Operation) error {
	metadata := options.Metadata
	o.CopyMetadata = &metadata
	workers := max(1, min(8, options.Workers))
	o.Parallel(workers)
	o.Streaming()
	ctx, cancel := context.WithCancel(o.Context())
	defer cancel()
	scan, err := source.Open(ctx)
	if err != nil {
		return err
	}
	defer scan.Close()
	dest, err := target.Open(ctx)
	if err != nil {
		return err
	}
	defer dest.Close()
	queue := make(chan job, workers*2)
	var wg sync.WaitGroup
	var first error
	var errMu sync.Mutex
	fail := func(err error) {
		if err == nil {
			return
		}
		errMu.Lock()
		if first == nil {
			first = err
		}
		errMu.Unlock()
		cancel()
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := source.Open(ctx)
			if err != nil {
				fail(err)
				return
			}
			defer s.Close()
			d, err := target.Open(ctx)
			if err != nil {
				fail(err)
				return
			}
			defer d.Close()
			for task := range queue {
				if ctx.Err() != nil {
					return
				}
				child := o.Fork(ctx)
				o.Worker(1)
				if source.Remote == nil && target.Remote == nil {
					err = fsops.Transfer(task.Source, task.Destination, false, options.Conflict, child)
				} else {
					err = copyFile(s, d, source, target, task, options, child)
				}
				o.Worker(-1)
				child.Cancel()
				if err != nil {
					fail(fmt.Errorf("%w (copy %s to %s)", err, task.Source, task.Destination))
					return
				}
			}
		}()
	}
	var directories []job
	count := 0
	var walk func(Pair, int, os.FileInfo) error
	walk = func(pair Pair, depth int, info os.FileInfo) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := o.Check(); err != nil {
			return err
		}
		if o.Ignored(pair.Source) {
			return nil
		}
		count++
		if count > 1000000 || depth > 256 {
			return fmt.Errorf("transfer tree limit exceeded")
		}
		var err error
		if info == nil {
			info, err = scan.Lstat(pair.Source)
			if err != nil {
				return err
			}
		}
		task := job{pair, info}
		size := int64(0)
		if info.Mode().IsRegular() {
			size = info.Size()
		}
		o.Add(pair.Source, size)
		if !info.IsDir() {
			select {
			case queue <- task:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		existing, err := dest.Lstat(pair.Destination)
		created := false
		if os.IsNotExist(err) {
			if target.Remote == nil && options.Metadata.Permissions {
				err = os.Mkdir(pair.Destination, 0700)
			} else {
				err = dest.Mkdir(pair.Destination)
			}
			created = err == nil
		} else if err == nil && !existing.IsDir() {
			return fmt.Errorf("destination is not a directory: %s", pair.Destination)
		}
		if err != nil {
			return err
		}
		if created {
			directories = append(directories, task)
		}
		visit := func(entry os.FileInfo) error {
			if entry.Name() == "." || entry.Name() == ".." {
				return nil
			}
			if (source.Remote != nil || target.Remote != nil) && !ValidName(entry.Name()) {
				return fmt.Errorf("unsafe filename from server")
			}
			return walk(Pair{path.Join(pair.Source, entry.Name()), path.Join(pair.Destination, entry.Name())}, depth+1, entry)
		}
		if source.Remote == nil {
			err = fsops.ReadDirectory(pair.Source, o, func(entry os.DirEntry) error {
				info, err := entry.Info()
				if err != nil {
					return err
				}
				return visit(info)
			})
		} else {
			var entries []os.FileInfo
			entries, err = scan.ReadDir(pair.Source)
			if err == nil && len(entries) > 100000 {
				err = fmt.Errorf("remote directory exceeds 100000 entries")
			}
			if err == nil {
				for _, entry := range entries {
					if err = visit(entry); err != nil {
						break
					}
				}
			}
		}
		if err != nil {
			return err
		}
		o.Complete(pair.Source)
		return nil
	}
	for _, pair := range pairs {
		if source.Remote != nil && target.Remote != nil {
			if err = validateRemotePair(scan, dest, pair); err != nil {
				break
			}
		}
		if source.Same(target) {
			a, b := path.Clean(pair.Source), path.Clean(pair.Destination)
			if a == b || strings.HasPrefix(b, a+"/") {
				err = fmt.Errorf("destination is inside source")
				break
			}
		}
		// Local Copy retains its real-path/symlink checks even for parallel trees.
		if source.Remote == nil && target.Remote == nil {
			if err = validateLocalPair(pair); err != nil {
				break
			}
		}
		if err = walk(pair, 0, nil); err != nil {
			break
		}
	}
	if err != nil {
		fail(err)
	}
	close(queue)
	o.Ready()
	wg.Wait()
	errMu.Lock()
	err = first
	errMu.Unlock()
	if err != nil {
		return err
	}
	for i := len(directories) - 1; i >= 0; i-- {
		j := directories[i]
		if source.Remote == nil && target.Remote == nil {
			err = fsops.ApplyMetadata(j.Source, j.Destination, o)
		} else {
			err = applyPortableMetadata(dest, j.Source, j.Destination, j.Info, options, o)
		}
		if err != nil {
			return err
		}
	}
	return o.Check()
}

func copyFile(s, d FS, source, target Endpoint, j job, options CopyOptions, o *fsops.Operation) (err error) {
	before, statErr := d.Lstat(j.Destination)
	if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}
	exists := statErr == nil
	if want := options.ExpectedDestination; want != nil {
		if !exists || before.Size() != want.Size() || !before.ModTime().Equal(want.ModTime()) || before.Mode() != want.Mode() {
			return fmt.Errorf("remote file changed; reopen before saving")
		}
	}
	if exists {
		if before.IsDir() {
			return fmt.Errorf("cannot replace a directory with a file")
		}
		answer := 2
		if options.Conflict != nil {
			answer = options.Conflict(j.Source, j.Destination)
		}
		if answer == 1 {
			o.Complete(j.Source)
			o.Advance(j.Info.Size())
			return nil
		}
		if answer != 0 {
			return fsops.ErrCancelled
		}
	}
	tmp := path.Join(path.Dir(j.Destination), ".farshmac-part-"+rand.Text())
	published, created := false, false
	defer func() {
		if !published && created {
			if cleanupErr := d.Remove(tmp, false); cleanupErr != nil && target.Remote != nil {
				// The cancelled data session is already closed. A short independent
				// cleanup session can remove only our unpredictable temporary name.
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if cleanup, e := target.Open(ctx); e == nil {
					_ = cleanup.Remove(tmp, false)
					cleanup.Close()
				}
			}
		}
	}()
	if j.Info.Mode()&os.ModeSymlink != 0 {
		link, e := s.Readlink(j.Source)
		if e != nil {
			return e
		}
		if err = d.Symlink(link, tmp); err != nil {
			return err
		}
		created = true
	} else {
		if !j.Info.Mode().IsRegular() {
			return fmt.Errorf("unsupported remote special file: %s", j.Source)
		}
		in, e := s.Open(j.Source)
		if e != nil {
			return e
		}
		out, e := d.Create(tmp)
		if e != nil {
			in.Close()
			return e
		}
		created = true
		o.Begin(j.Source, j.Info.Size())
		buffer := make([]byte, 256*1024)
		n, e := io.CopyBuffer(struct{ io.Writer }{out}, o.Reader(in), buffer)
		rclose := in.Close()
		wclose := out.Close()
		if e != nil {
			return e
		}
		if rclose != nil {
			return rclose
		}
		if wclose != nil {
			return wclose
		}
		if n != j.Info.Size() {
			return fmt.Errorf("source size changed while copying %s", j.Source)
		}
	}
	if err = applyPortableMetadata(d, j.Source, tmp, j.Info, options, o); err != nil {
		return err
	}
	if err = o.Check(); err != nil {
		return err
	}
	current, e := d.Lstat(j.Destination)
	if exists {
		if e != nil || current.Size() != before.Size() || !current.ModTime().Equal(before.ModTime()) || current.Mode() != before.Mode() {
			return fmt.Errorf("destination changed during transfer")
		}
	} else if e == nil || !os.IsNotExist(e) {
		return fmt.Errorf("destination appeared during transfer")
	}
	if err = d.Rename(tmp, j.Destination); err != nil {
		return err
	}
	published = true
	o.Complete(j.Source)
	return nil
}

func applyPortableMetadata(d FS, src, dst string, info os.FileInfo, options CopyOptions, o *fsops.Operation) error {
	var problems []error
	if info.Mode()&os.ModeSymlink != 0 {
		// Chmod/Chtimes would follow the link and change its target.
		if !options.Metadata.Permissions && !options.Metadata.Attributes {
			return nil
		}
		err := fmt.Errorf("remote transfer cannot preserve symbolic-link metadata")
		if o.MetadataFailure != nil {
			return o.MetadataFailure(src, dst, err)
		}
		return err
	}
	if options.Metadata.Permissions {
		mode := info.Mode().Perm()
		if options.PermissionsMode != nil {
			mode = *options.PermissionsMode
		}
		if err := d.Chmod(dst, mode); err != nil {
			problems = append(problems, err)
		}
		problems = append(problems, fmt.Errorf("remote transfer cannot preserve owner/group and ACL"))
	}
	if options.Metadata.Attributes {
		if err := d.Chtimes(dst, info.ModTime()); err != nil {
			problems = append(problems, err)
		}
		problems = append(problems, fmt.Errorf("remote transfer cannot preserve xattrs, flags and creation time"))
	}
	if err := errors.Join(problems...); err != nil {
		if o.MetadataFailure != nil {
			return o.MetadataFailure(src, dst, err)
		}
		return err
	}
	return nil
}

type canonicalFS interface {
	Identity() string
	Canonical(string) (string, error)
}

func validateRemotePair(source, target FS, pair Pair) error {
	s, sok := source.(canonicalFS)
	d, dok := target.(canonicalFS)
	if !sok || !dok || s.Identity() != d.Identity() {
		return nil
	}
	i, err := source.Lstat(pair.Source)
	if err != nil {
		return err
	}
	if !i.IsDir() {
		return nil
	}
	a, err := s.Canonical(pair.Source)
	if err != nil {
		return err
	}
	b, err := d.Canonical(path.Dir(pair.Destination))
	if err != nil {
		return err
	}
	b = path.Join(b, path.Base(pair.Destination))
	if a == b || strings.HasPrefix(b, a+"/") {
		return fmt.Errorf("destination is inside source")
	}
	return nil
}

func RemoveTree(f FS, p string, o *fsops.Operation) error {
	return removeTree(f, p, o, 0)
}
func removeTree(f FS, p string, o *fsops.Operation, depth int) error {
	if depth > 256 {
		return fmt.Errorf("remote tree depth exceeds 256")
	}
	if err := o.Check(); err != nil {
		return err
	}
	info, err := f.Lstat(p)
	if err != nil {
		return err
	}
	if info.IsDir() {
		entries, err := f.ReadDir(p)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.Name() == "." || entry.Name() == ".." {
				continue
			}
			if !ValidName(entry.Name()) {
				return fmt.Errorf("unsafe server filename")
			}
			if err = removeTree(f, path.Join(p, entry.Name()), o, depth+1); err != nil {
				return err
			}
		}
	}
	if err = o.Check(); err != nil {
		return err
	}
	err = f.Remove(p, info.IsDir())
	if err == nil {
		o.Complete(p)
	}
	return err
}
