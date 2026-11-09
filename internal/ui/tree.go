package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"unicode"
)

const treeNodeLimit = 50000
const treeEntryLimit = 1000000

type treeResult struct {
	entries []Entry
	labels  map[string]string
	limited bool
	skipped int
	err     error
}

// Only the worker owns entries and filesystem handles. The UI receives an
// immutable result through a buffered channel, never a shared Panel or slice.
type treeLoad struct {
	cancel  context.CancelFunc
	done    chan treeResult
	state   panelState
	visited atomic.Int64
	shown   int64
}

type treeScanOptions struct {
	depth, nodeLimit, entryLimit int
	hideHidden                   bool
	ignoreDSStore                bool
	forcedVisible                string
	check                        func() error
	progress                     func(string, int)
	sort                         func([]Entry)
}

// Read directories in bounded batches. In particular, never stat ordinary files
// or enumerate descendants at the depth boundary. Symlinks are not traversed.
func scanTree(root string, opts treeScanOptions) treeResult {
	var result treeResult
	reserved, visited := 0, 0
	stopped := false
	check := func() bool {
		if stopped {
			return false
		}
		if err := opts.check(); err != nil {
			result.err, stopped = err, true
			return false
		}
		return true
	}
	read := func(rel string) []Entry {
		if !check() {
			return nil
		}
		if result.limited {
			return nil
		}
		if reserved >= opts.nodeLimit || visited >= opts.entryLimit {
			result.limited = true
			return nil
		}
		path := filepath.Join(root, rel)
		f, err := os.Open(path)
		if err != nil {
			if rel == "" {
				result.err = err
			} else {
				result.skipped++
			}
			return nil
		}
		defer f.Close()
		var dirs []Entry
		for check() {
			batch, err := f.ReadDir(256)
			for _, d := range batch {
				visited++
				name := filepath.Join(rel, d.Name())
				if d.IsDir() && !(opts.ignoreDSStore && d.Name() == ".DS_Store") && !(opts.hideHidden && strings.HasPrefix(d.Name(), ".") && filepath.Join(root, name) != opts.forcedVisible) {
					if reserved >= opts.nodeLimit {
						result.limited = true
						break
					}
					if info, e := d.Info(); e == nil {
						if !opts.hideHidden || !hiddenEntry(d.Name(), info) || filepath.Join(root, name) == opts.forcedVisible {
							dirs = append(dirs, Entry{Name: name, Info: info, Dir: true})
							reserved++
						}
					} else {
						result.skipped++
					}
				}
				if visited >= opts.entryLimit {
					result.limited = true
					break
				}
			}
			if opts.progress != nil {
				opts.progress(path, visited)
			}
			if result.limited {
				break
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					if rel == "" {
						result.err = err
					} else {
						result.skipped++
					}
				}
				break
			}
		}
		opts.sort(dirs)
		return dirs
	}
	var visit func([]Entry, int)
	visit = func(dirs []Entry, depth int) {
		for _, dir := range dirs {
			result.entries = append(result.entries, dir)
			if depth < opts.depth && check() {
				visit(read(dir.Name), depth+1)
			}
		}
	}
	visit(read(""), 1)
	return result
}

func (p *Panel) treeOptions() treeScanOptions {
	// Sorting runs off the engine queue: copy its mutable maps too.
	config := Panel{panelOptions: p.panelOptions, SortGroups: p.SortGroups, Marked: cloneMarks(p.Marked)}
	config.MaskGroups = make(map[string]string, len(p.MaskGroups))
	for k, v := range p.MaskGroups {
		config.MaskGroups[k] = v
	}
	return treeScanOptions{depth: max(1, min(12, p.TreeDepth)), nodeLimit: treeNodeLimit,
		entryLimit: treeEntryLimit, hideHidden: p.HideHidden, ignoreDSStore: p.IgnoreDSStore,
		forcedVisible: p.ForcedVisiblePath, sort: config.sortEntries}
}

func (p *Panel) cancelTree() {
	if p.tree != nil {
		p.tree.cancel()
		p.tree = nil
	}
}

func (p *Panel) loadTree(abs string) error {
	p.startTree(abs, p.treeOptions())
	return nil
}

func (p *Panel) startTree(abs string, options treeScanOptions) {
	state := p.beginLoad(abs)
	p.cancelTree()
	p.cancelPanelLoad()
	ctx, cancel := context.WithCancel(context.Background())
	job := &treeLoad{cancel: cancel, done: make(chan treeResult, 1), state: state}
	p.tree = job
	p.TotalBytes, p.ItemCount = 0, 0
	p.Err = "Reading tree… Esc stops"
	check := options.check
	options.check = func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if check != nil {
			return check()
		}
		return nil
	}
	options.progress = func(_ string, visited int) { job.visited.Store(int64(visited)) }
	go func() {
		result := scanTree(abs, options)
		result.labels = treeLabels(result.entries)
		job.done <- result
	}()
}

func (r treeResult) status(depth int) string {
	text := fmt.Sprintf("%d folders · depth %d", len(r.entries), depth)
	if r.err != nil {
		if errors.Is(r.err, context.Canceled) {
			text = "Stopped · partial tree"
		} else {
			text = r.err.Error()
		}
	} else if r.limited {
		text = "Partial tree · limit reached; open a subfolder"
	}
	if r.skipped > 0 {
		text += fmt.Sprintf(" · %d unreadable", r.skipped)
	}
	return text
}

func (p *Panel) pollTree() bool {
	job := p.tree
	if job == nil {
		return false
	}
	select {
	case result := <-job.done:
		job.cancel()
		p.tree = nil
		p.installEntries(result.entries, job.state)
		p.ItemCount = len(result.entries)
		p.treeLabels = result.labels
		p.Err = result.status(max(1, min(12, p.TreeDepth)))
		return true
	default:
		visited := job.visited.Load()
		if visited != job.shown {
			job.shown = visited
			p.Err = fmt.Sprintf("Reading tree… %d entries · Esc stops", visited)
			return true
		}
		return false
	}
}

// Keep filesystem paths untouched; display control characters explicitly so
// filenames cannot masquerade as indentation or terminal control sequences.
func treeDisplayName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch r {
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// Precompute branch labels once in O(nodes * depth), not while painting rows.
func treeLabels(entries []Entry) map[string]string {
	last := make(map[string]string)
	for _, e := range entries {
		last[filepath.Dir(e.Name)] = e.Name
	}
	labels := make(map[string]string, len(entries))
	for _, e := range entries {
		parts := strings.Split(e.Name, string(filepath.Separator))
		var b strings.Builder
		for level := 0; level < len(parts)-1; level++ {
			ancestor := filepath.Join(parts[:level+1]...)
			if last[filepath.Dir(ancestor)] == ancestor {
				b.WriteString("  ")
			} else {
				b.WriteString("│ ")
			}
		}
		if last[filepath.Dir(e.Name)] == e.Name {
			b.WriteString("└─")
		} else {
			b.WriteString("├─")
		}
		b.WriteString(treeDisplayName(filepath.Base(e.Name)))
		labels[e.Name] = b.String()
	}
	return labels
}
