package ui

import (
	"context"
	"errors"
	"time"

	"farshmac/internal/kube"
)

// Watches keep the namespace snapshots current the way an operator's
// informers do: after a namespace is listed, every type folder is watched
// from the list's resourceVersion and events are applied to the snapshot
// in memory. The panel re-lists from the snapshot when something changed.
// Watches of the most recently visited namespaces are kept (kubeWatchKeep);
// older ones are cancelled.
const kubeWatchKeep = 3

type kubeNamespaceWatch struct {
	cancel  context.CancelFunc
	started time.Time
}

// watching reports whether the namespace ("" = the namespace list) is
// kept current by watches, so its cached listing never goes stale.
func (k *kubePanel) watching(ns string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	_, ok := k.watches[ns]
	return ok
}

// startWatches begins watching every type folder of the snapshot (or the
// namespace list when snap is nil and ns is ""), replacing older watches.
func (k *kubePanel) startWatches(ns string, snap *kube.Snapshot, rootRV string) {
	k.mu.Lock()
	if _, ok := k.watches[ns]; ok {
		k.mu.Unlock()
		return
	}
	if k.watches == nil {
		k.watches = map[string]*kubeNamespaceWatch{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	k.watches[ns] = &kubeNamespaceWatch{cancel: cancel, started: time.Now()}
	// Drop the oldest namespace watches beyond the limit (the root stays).
	for len(k.watches) > kubeWatchKeep+1 {
		oldest, at := "", time.Time{}
		for name, w := range k.watches {
			if name != "" && name != ns && (oldest == "" || w.started.Before(at)) {
				oldest, at = name, w.started
			}
		}
		if oldest == "" {
			break
		}
		k.watches[oldest].cancel()
		delete(k.watches, oldest)
	}
	k.mu.Unlock()
	if ns == "" {
		go k.watchNamespaces(ctx, rootRV)
		return
	}
	for folder, rv := range snap.ResourceVersions {
		r, ok := k.client.ResourceByFolder(folder)
		if !ok {
			continue
		}
		go k.watchFolder(ctx, ns, r, rv)
	}
}

func (k *kubePanel) watchFolder(ctx context.Context, ns string, r kube.Resource, rv string) {
	events, result := k.client.WatchFolder(ctx, r, ns, rv)
	for ev := range events {
		k.mu.Lock()
		if snap := k.snapshots[ns]; snap != nil {
			k.snapshots[ns] = snap.Apply(ev)
		}
		if ev.Object != nil && isPodObject(ev.Object) {
			delete(k.pods, ns+"/"+ev.Object.Name)
		}
		k.mu.Unlock()
		k.changed.Store(true)
	}
	if err := result(); err != nil && ctx.Err() == nil {
		if errors.Is(err, kube.ErrWatchExpired) {
			// Too old to resume: reload the namespace in the background and
			// start its watches again; the panel keeps showing the old list.
			k.reloadNamespace(ns)
			return
		}
		// A type that cannot be watched (or a terminal API error) keeps the
		// snapshot as listed; the rest of the namespace stays live.
		k.mu.Lock()
		k.unwatched[ns+"/"+r.Folder] = err.Error()
		k.mu.Unlock()
	}
}

// reloadNamespace replaces a namespace snapshot from the server without
// touching the panel until the data is in, then restarts its watches.
func (k *kubePanel) reloadNamespace(ns string) {
	k.mu.Lock()
	if k.reloading[ns] {
		k.mu.Unlock()
		return
	}
	k.reloading[ns] = true
	if w, ok := k.watches[ns]; ok {
		w.cancel()
		delete(k.watches, ns)
	}
	k.mu.Unlock()
	go func() {
		defer func() {
			k.mu.Lock()
			delete(k.reloading, ns)
			k.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		snap, err := k.client.LoadSnapshot(ctx, ns, k.hidden)
		if err != nil {
			return // The TTL path reloads it on the next listing.
		}
		k.mu.Lock()
		k.snapshots[ns] = snap
		k.mu.Unlock()
		k.startWatches(ns, snap, "")
		k.changed.Store(true)
	}()
}

func (k *kubePanel) watchNamespaces(ctx context.Context, rv string) {
	events, result := k.client.WatchFolder(ctx, kube.NamespacesResource, "", rv)
	for ev := range events {
		if ev.Object == nil {
			continue
		}
		k.mu.Lock()
		list := make([]*kube.Object, 0, len(k.namespaces)+1)
		for _, o := range k.namespaces {
			if o.Name != ev.Object.Name {
				list = append(list, o)
			}
		}
		if ev.Type != kube.Deleted {
			list = append(list, ev.Object)
		}
		sortKubeObjects(list)
		k.namespaces, k.namespacesAt = list, time.Now()
		k.mu.Unlock()
		k.changed.Store(true)
	}
	if err := result(); err != nil && ctx.Err() == nil {
		k.mu.Lock()
		if w, ok := k.watches[""]; ok {
			w.cancel()
			delete(k.watches, "")
		}
		k.mu.Unlock()
		if errors.Is(err, kube.ErrWatchExpired) {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				list, rv, err := k.client.NamespacesRV(ctx)
				if err != nil {
					return
				}
				k.mu.Lock()
				k.namespaces, k.namespacesAt = list, time.Now()
				k.mu.Unlock()
				if rv != "" {
					k.startWatches("", nil, rv)
				}
				k.changed.Store(true)
			}()
		}
	}
}

func (k *kubePanel) stopWatches() {
	k.mu.Lock()
	defer k.mu.Unlock()
	for name, w := range k.watches {
		w.cancel()
		delete(k.watches, name)
	}
}

// takeChanged reports, once, that watches changed something since the last call.
func (k *kubePanel) takeChanged() bool { return k.changed.Swap(false) }

// pollKubeWatches re-lists Kubernetes panels whose snapshots changed: from
// the cache, synchronously, so the entries are replaced in place without a
// reading indicator; at most a few times per second and never over a
// listing in progress. Levels that need the server (container listings)
// are left alone until the user re-enters them.
func (a *App) pollKubeWatches() {
	for i := range a.Panels {
		p := &a.Panels[i]
		k := p.Kube
		if k == nil || p.load != nil || !k.takeChanged() {
			continue
		}
		if time.Since(k.reloadedAt) < 250*time.Millisecond {
			k.changed.Store(true) // Try again on a later poll.
			continue
		}
		k.reloadedAt = time.Now()
		if !k.cachedFor(p.Path) {
			continue
		}
		if err := p.loadKube(p.Path); err == nil {
			a.dirty = true
		}
	}
}
