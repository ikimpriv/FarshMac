package ui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"farshmac/internal/fsops"
	"farshmac/internal/kube"
)

type searchOptions struct {
	mask, needle                  string
	groups                        map[string]string
	folders, recursive, sensitive bool
	hideHidden                    bool
}
type searchEvent struct {
	hit       *searchHit
	namespace string
	snapshot  *kube.Snapshot
	done      bool
	stats     browseStats
	err       error
}
type searchTreeResult struct {
	hits     []searchHit
	labels   []string
	revision int
	limited  bool
	err      error
}

// Only the engine queue owns this state. Workers communicate over bounded channels.
type searchSession struct {
	source                 Panel
	title                  string
	op                     *fsops.Operation
	events                 chan searchEvent
	abandoned              chan struct{}
	form                   *Form
	hits, displayed        []searchHit
	labels                 []string
	snapshots              map[string]*kube.Snapshot
	done, stopped, tree    bool
	stats                  browseStats
	err                    error
	revision, treeRevision int
	treeOp                 *fsops.Operation
	treeDone               chan searchTreeResult
	treeAt                 time.Time
}

func (a *App) closeSearch() {
	if s := a.searchSession; s != nil {
		s.op.Cancel()
		if s.treeOp != nil {
			s.treeOp.Cancel()
		}
		close(s.abandoned)
		if a.searchResults == s.form {
			a.searchResults = nil
		}
		a.searchSession = nil
	}
}
func (a *App) startSearch(source Panel, index int, title string, opts searchOptions, factory func(*fsops.Operation) (browseStrategy, error)) {
	a.closeSearch()
	// Settings can be edited while an operation is in flight.
	groups := map[string]string{}
	for k, v := range opts.groups {
		groups[k] = v
	}
	opts.groups = groups
	opts.hideHidden = !a.Settings.ShowHidden
	s := &searchSession{source: source, title: title, op: fsops.NewOperation("Searching " + source.Path), events: make(chan searchEvent, 128), abandoned: make(chan struct{}), snapshots: map[string]*kube.Snapshot{}, treeRevision: -1}
	s.op.IgnoreDSStore = a.Settings.IgnoreDSStore
	a.searchSession = s
	s.form = &Form{SearchResults: true, Choice: []string{"Searching…"}, DisabledChoices: map[int]bool{0: true}}
	a.Form = s.form
	s.form.Cancel = func() {
		if !s.done && !s.stopped {
			s.stop()
			a.Form = s.form
			a.updateSearchCaption(s)
		} else {
			a.closeSearch()
		}
	}
	s.form.Select = func(i int) {
		if i < 0 || i >= len(s.displayed) {
			a.Form = s.form
			return
		}
		hit := s.displayed[i]
		a.closeSearch()
		p := &a.Panels[index]
		if p.label() != source.label() || p.Kube != source.Kube || p.Docker != source.Docker || p.Archive != source.Archive {
			a.Status = "Search source changed; search again"
			return
		}
		if err := p.Load(filepath.Dir(hit.path)); err != nil {
			a.error(err)
			return
		}
		if !p.pendingPosition(filepath.Base(hit.path), "") {
			for j, e := range p.Entries {
				if e.Name == filepath.Base(hit.path) {
					p.Cursor = j
					break
				}
			}
		}
	}
	s.form.OnKey = func(m Message) bool {
		if m.Meta || m.Ctrl || m.Alt || m.Shift {
			return false
		}
		switch m.Key {
		case "F2":
			s.stop()
			a.updateSearchCaption(s)
			return true
		case "Escape":
			if s.form.Query != "" {
				return false
			}
			if !s.done && !s.stopped {
				s.stop()
				a.updateSearchCaption(s)
				return true
			}
		case "F5":
			if source.Kube == nil || source.endpoint().Opener != nil {
				return false
			}
			s.tree = !s.tree
			s.form.OwnershipTree = s.tree
			if !s.tree {
				if s.treeOp != nil {
					s.treeOp.Cancel()
				}
				a.applySearchRows(s, s.hits, s.labels)
			} else {
				s.treeRevision = -1
				s.treeAt = time.Time{}
			}
			a.updateSearchCaption(s)
			return true
		case "F3":
			if !s.form.choiceSelectable(s.form.Focus) {
				return true
			}
			i := s.form.choiceIndex(s.form.Focus)
			if i < 0 || i >= len(s.displayed) {
				return true
			}
			a.searchResults = s.form
			a.Form = nil
			a.viewSearchHit(source, s.displayed[i])
			a.dirty = true
			return true
		}
		return false
	}
	a.updateSearchCaption(s)
	if factory == nil {
		factory = func(o *fsops.Operation) (browseStrategy, error) { return source.browse(o) }
	}
	go s.collect(opts, factory)
}
func (s *searchSession) stop() {
	if !s.done {
		s.stopped = true
		s.op.Cancel()
	}
}
func (s *searchSession) collect(opts searchOptions, factory func(*fsops.Operation) (browseStrategy, error)) {
	final := searchEvent{done: true}
	defer func() {
		select {
		case s.events <- final:
		case <-s.abandoned:
		}
	}()
	b, err := factory(s.op)
	if err != nil {
		final.err = err
		return
	}
	defer b.Close()
	skipped, count := 0, 0
	final.stats, final.err = walkBrowse(s.source.Path, b, s.op, browseOptions{Recursive: opts.recursive}, func(path string, e Entry) error {
		if opts.hideHidden && hiddenEntry(e.Name, e.Info) {
			return fs.SkipDir
		}
		if !matchesMask(e.Name, opts.mask, opts.groups) || opts.folders && !e.Dir {
			return nil
		}
		match := opts.needle == ""
		object := false
		if ke, ok := e.Info.(*kubeEntry); ok && ke.object != nil {
			object = true
		}
		if !match && (!e.Dir || object || s.source.Docker != nil && s.source.endpoint().Opener == nil) {
			if e.Info != nil && !e.Info.Mode().IsRegular() && !object && s.source.Docker == nil {
				return nil
			}
			r, err := b.Open(path)
			if err == nil {
				match, err = readerContains(r, opts.needle, opts.sensitive, s.op)
				r.Close()
			}
			if err != nil {
				if s.op.Check() != nil {
					return s.op.Check()
				}
				skipped++
			}
		}
		if !match {
			return nil
		}
		ev := searchEvent{hit: &searchHit{path: path, entry: e}}
		if kb, ok := b.(*kubeBrowse); ok {
			if ke, ok := e.Info.(*kubeEntry); ok && ke.object != nil {
				ev.namespace = ke.object.Namespace
				ev.snapshot = kb.snapshots[ev.namespace]
			}
		}
		select {
		case s.events <- ev:
		case <-s.op.Context().Done():
			return s.op.Check()
		case <-s.abandoned:
			return context.Canceled
		}
		count++
		if count >= 10000 {
			return fs.SkipAll
		}
		return nil
	})
	final.stats.Skipped += skipped
	final.stats.Limited = final.stats.Limited || count >= 10000
}
func (a *App) updateSearchCaption(s *searchSession) {
	state := "Searching"
	if s.done {
		state = "Complete"
	}
	if s.stopped {
		state = "Stopped"
	}
	if s.err != nil {
		state = "Failed"
	}
	mode := "List"
	if s.tree {
		mode = "Tree"
	}
	s.form.Title = fmt.Sprintf("%s · %s · %d found · %s", s.title, state, len(s.hits), mode)
	s.form.Message = "F3 View · Enter Go to · F2 Stop · Esc Close"
	if s.source.Kube != nil && s.source.endpoint().Opener == nil {
		s.form.Message = "F3 View · Enter Go to · F2 Stop · F5 List/tree · Esc Close"
	}
	if !s.done && !s.stopped {
		s.form.Message = strings.Replace(s.form.Message, "Esc Close", "Esc Stop", 1)
	}
	if s.done || s.stopped {
		s.form.Message = strings.Replace(s.form.Message, "F2 Stop · ", "", 1)
	}
	if s.stats.Limited {
		s.form.Title += " (limit)"
	}
	if a.Form == s.form {
		a.Status = fmt.Sprintf("%s: %d unreadable, %d boundaries skipped", s.title, s.stats.Skipped, s.stats.Boundaries)
		if s.err != nil {
			a.Status += " · " + s.err.Error()
		}
	}
	a.dirty = true
}
func (a *App) applySearchRows(s *searchSession, hits []searchHit, labels []string) {
	f := s.form
	selected := ""
	old := f.choiceIndex(f.Focus)
	if old >= 0 && old < len(s.displayed) {
		selected = s.displayed[old].path
	}
	scroll, focus, free := f.Scroll, f.Focus, f.freeScroll
	s.displayed = hits
	f.Choice = nil
	f.index = nil
	f.all = labels
	f.DisabledChoices = nil
	for i, label := range labels {
		if f.Query == "" || strings.Contains(strings.ToLower(label), strings.ToLower(f.Query)) {
			if hits[i].path == selected {
				focus = len(f.Choice)
			}
			f.Choice = append(f.Choice, label)
			f.index = append(f.index, i)
		}
	}
	if len(f.Choice) == 0 {
		message := "No matches"
		if !s.done && !s.stopped {
			message = "Searching…"
		}
		f.Choice = []string{message}
		f.index = []int{-1}
		f.DisabledChoices = map[int]bool{-1: true}
		focus = 0
	}
	f.Focus = max(0, min(focus, len(f.Choice)-1))
	f.Scroll = scroll
	f.freeScroll = free
	a.dirty = true
}
func (a *App) pollSearch() {
	s := a.searchSession
	if s == nil {
		return
	}
	changed := false
	// Bounded work per frame; arrival order remains stable in list mode.
	for n := 0; n < 256; n++ {
		select {
		case ev := <-s.events:
			changed = true
			if ev.hit != nil {
				s.hits = append(s.hits, *ev.hit)
				label, _ := filepath.Rel(s.source.Path, ev.hit.path)
				s.labels = append(s.labels, label)
				s.revision++
				if ev.snapshot != nil {
					s.snapshots[ev.namespace] = ev.snapshot
				}
			}
			if ev.done {
				s.done = true
				s.stats = ev.stats
				if ev.err != nil && !errors.Is(ev.err, context.Canceled) && !errors.Is(ev.err, fsops.ErrCancelled) {
					s.err = ev.err
				}
			}
		default:
			goto received
		}
	}
received:
	if changed {
		if !s.tree {
			a.applySearchRows(s, s.hits, s.labels)
		}
		a.updateSearchCaption(s)
	}
	if s.treeDone != nil {
		select {
		case result := <-s.treeDone:
			s.treeOp.Cancel()
			s.treeOp = nil
			s.treeDone = nil
			if s.tree && result.err == nil {
				s.treeRevision = result.revision
				a.applySearchRows(s, result.hits, result.labels)
				if result.limited {
					s.form.Title += " (tree limit)"
				}
			}
		default:
		}
	}
	if s.tree && s.treeDone == nil && s.treeRevision != s.revision && time.Since(s.treeAt) >= 250*time.Millisecond {
		s.treeAt = time.Now()
		s.treeOp = fsops.NewOperation("Building ownership tree")
		s.treeDone = make(chan searchTreeResult, 1)
		b := &kubeBrowse{o: s.treeOp, snapshots: map[string]*kube.Snapshot{}}
		for ns, snapshot := range s.snapshots {
			b.snapshots[ns] = snapshot
		}
		hits := append([]searchHit(nil), s.hits...)
		revision := s.revision
		done := s.treeDone
		go func() {
			rows, labels, limited, err := b.ownershipResults(hits)
			done <- searchTreeResult{rows, labels, revision, limited, err}
		}()
	}
}
