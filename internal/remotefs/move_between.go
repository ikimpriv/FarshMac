package remotefs

import (
	"fmt"
	"os"
	"path"

	"farshmac/internal/fsops"
)

// MoveBetween uses rename on one endpoint. Across endpoints it copies first,
// then removes only the recorded source entries. Failed/skipped copies never
// delete their source tree, and newly created children are never swept up.
func MoveBetween(source, target Endpoint, src, dst string, o *fsops.Operation) error {
	s, err := openTransferEndpoint(source, o.Context(), o)
	if err != nil {
		return err
	}
	defer s.Close()
	if source.Same(target) {
		if mapper, ok := source.Opener.(PathMapper); ok {
			dst, err = mapper.PathFrom(target.Opener, dst)
			if err != nil {
				return err
			}
		}
		return Move(s, source, src, dst, o)
	}
	type entry struct {
		name string
		info os.FileInfo
	}
	var entries []entry
	var scan func(string, int) error
	scan = func(name string, depth int) error {
		if err := o.Check(); err != nil {
			return err
		}
		if depth > 256 || len(entries) >= 1000000 {
			return fmt.Errorf("transfer tree limit exceeded")
		}
		info, err := s.Lstat(name)
		if err != nil {
			return err
		}
		entries = append(entries, entry{name, info})
		if info.IsDir() {
			children, err := s.ReadDir(name)
			if err != nil {
				return err
			}
			for _, child := range children {
				if child.Name() == "." || child.Name() == ".." {
					continue
				}
				if !ValidName(child.Name()) {
					return fmt.Errorf("unsafe server filename")
				}
				if err := scan(path.Join(name, child.Name()), depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err = scan(src, 0); err != nil {
		return err
	}
	skipped := o.Snapshot().Skipped
	o.IgnoreDSStore = false
	if err = Copy(source, target, []Pair{{src, dst}}, CopyOptions{Workers: 1, Metadata: fsops.MetadataOptions{Permissions: true, Attributes: true}}, o); err != nil {
		return err
	}
	if o.Snapshot().Skipped != skipped {
		return nil
	}
	unchanged := func(e entry) error {
		current, err := s.Lstat(e.name)
		if err != nil {
			return err
		}
		if current.Mode() != e.info.Mode() || current.Size() != e.info.Size() || !current.ModTime().Equal(e.info.ModTime()) {
			return fmt.Errorf("destination copied; source changed, retained: %s", e.name)
		}
		return nil
	}
	// Verify the whole tree before removing any entry.
	for _, e := range entries {
		if err = unchanged(e); err != nil {
			return err
		}
	}
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if err = o.Check(); err != nil {
			return err
		}
		if !e.info.IsDir() {
			if err = unchanged(e); err != nil {
				return err
			}
		}
		if err = o.Attempt("delete copied source", e.name, "", func() error { return s.Remove(e.name, e.info.IsDir()) }); err != nil {
			return fmt.Errorf("destination copied; source removal incomplete: %w", err)
		}
	}
	return nil
}
