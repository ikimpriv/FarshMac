package ui

import (
	"context"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"farshmac/internal/fsops"
	"farshmac/internal/kube"
	"farshmac/internal/remotefs"
)

// kubePanel is the state of a panel connected to a cluster: the client and
// the cached namespace snapshots. It is shared with the worker copy of the
// panel during an asynchronous load, hence the mutex.
type kubePanel struct {
	profile kube.Profile
	client  *kube.Client
	ttl     time.Duration
	hidden  map[string]bool

	mu           sync.Mutex
	namespaces   []*kube.Object
	namespacesAt time.Time
	snapshots    map[string]*kube.Snapshot
	pods         map[string]kube.PodSummary // "ns/pod" -> containers, for the containers level
	probes       map[string]kube.Probe      // "ns/pod/container" -> shell and tools found (C6)
	executor     kube.Executor              // Tests inject a local shell; nil = the client's.
	last         kubePath                   // Level of the path last listed, for the key bar.
	lastPath     string
	watches      map[string]*kubeNamespaceWatch // Namespace ("" = the list of namespaces) -> its watches.
	unwatched    map[string]string              // "ns/folder" -> why its watch ended (kept as listed).
	reloading    map[string]bool                // Namespaces being reloaded in the background.
	changed      atomic.Bool                    // Watches changed a snapshot; the panel re-lists.
	reloadedAt   time.Time
}

// cachedFor reports whether the level at path can be listed from memory
// alone, so the panel can be refreshed in place without a reading indicator.
func (k *kubePanel) cachedFor(p string) bool {
	segs := kubeSegments(p)
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(segs) == 0 {
		return k.namespaces != nil
	}
	snap := k.snapshots[segs[0]]
	if snap == nil {
		return false
	}
	if _, watched := k.watches[segs[0]]; !watched && time.Since(snap.Taken) >= k.ttl {
		return false
	}
	// A pod segment switches to the containers listing, which needs the
	// pod from the server unless it is cached; deeper is a container.
	for i := 1; i+1 < len(segs); i += 2 {
		if segs[i] == "pods" {
			_, ok := k.pods[segs[0]+"/"+segs[i+1]]
			return ok && i+2 == len(segs)
		}
	}
	return true
}

func newKubePanel(profile kube.Profile, client *kube.Client, namespaces []*kube.Object, s Settings) *kubePanel {
	k := &kubePanel{profile: profile, client: client, ttl: 30 * time.Second, hidden: map[string]bool{}, snapshots: map[string]*kube.Snapshot{}, pods: map[string]kube.PodSummary{}, probes: map[string]kube.Probe{}, unwatched: map[string]string{}, reloading: map[string]bool{}}
	for _, name := range strings.Split(s.KubeHiddenTypes, ",") {
		if name = strings.TrimSpace(name); name != "" {
			k.hidden[name] = true
		}
	}
	if s.KubeSnapshotTTL > 0 {
		k.ttl = time.Duration(s.KubeSnapshotTTL) * time.Second
	}
	if namespaces != nil {
		k.namespaces, k.namespacesAt = namespaces, time.Now()
	}
	return k
}
func (k *kubePanel) label() string { return k.profile.Label() }
func (k *kubePanel) close() {
	k.stopWatches()
	if k.client != nil {
		k.client.Close()
	}
}
func sortKubeObjects(list []*kube.Object) {
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
}

// noteChanged is called after this app changed the cluster: a watched
// namespace receives the change as an event, an unwatched one is re-read.
func (k *kubePanel) noteChanged(ns string) {
	if k.watching(ns) {
		return
	}
	k.invalidate(ns)
}

// invalidate forgets cached listings: one namespace, or everything when ns is "".
func (k *kubePanel) invalidate(ns string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if ns == "" {
		for name, w := range k.watches {
			w.cancel()
			delete(k.watches, name)
		}
		k.namespaces, k.namespacesAt = nil, time.Time{}
		k.snapshots = map[string]*kube.Snapshot{}
		k.pods = map[string]kube.PodSummary{}
		return
	}
	if w, ok := k.watches[ns]; ok {
		w.cancel()
		delete(k.watches, ns)
	}
	delete(k.snapshots, ns)
	for key := range k.pods {
		if strings.HasPrefix(key, ns+"/") {
			delete(k.pods, key)
		}
	}
	for key := range k.probes {
		if strings.HasPrefix(key, ns+"/") {
			delete(k.probes, key)
		}
	}
}
func (k *kubePanel) cachedPod(ns, name string) (kube.PodSummary, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	s, ok := k.pods[ns+"/"+name]
	return s, ok
}
func (k *kubePanel) namespaceList(ctx context.Context) ([]*kube.Object, error) {
	k.mu.Lock()
	cached, at := k.namespaces, k.namespacesAt
	_, watched := k.watches[""]
	k.mu.Unlock()
	if cached != nil && (watched || time.Since(at) < k.ttl) {
		return cached, nil
	}
	list, rv, err := k.client.NamespacesRV(ctx)
	if err != nil {
		return nil, err
	}
	k.mu.Lock()
	k.namespaces, k.namespacesAt = list, time.Now()
	k.mu.Unlock()
	if rv != "" {
		k.startWatches("", nil, rv)
	}
	return list, nil
}
func (k *kubePanel) snapshot(ctx context.Context, ns string) (*kube.Snapshot, error) {
	k.mu.Lock()
	cached := k.snapshots[ns]
	_, watched := k.watches[ns]
	k.mu.Unlock()
	if cached != nil && (watched || time.Since(cached.Taken) < k.ttl) {
		return cached, nil
	}
	snap, err := k.client.LoadSnapshot(ctx, ns, k.hidden)
	if err != nil {
		return nil, err
	}
	k.mu.Lock()
	k.snapshots[ns] = snap
	k.mu.Unlock()
	k.startWatches(ns, snap, "")
	return snap, nil
}
func (k *kubePanel) cachedSnapshot(ns string) *kube.Snapshot {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.snapshots[ns]
}
func (k *kubePanel) podSummary(ctx context.Context, ns, name string) (kube.PodSummary, error) {
	key := ns + "/" + name
	k.mu.Lock()
	cached, ok := k.pods[key]
	k.mu.Unlock()
	if ok {
		return cached, nil
	}
	pod, err := k.client.Pod(ctx, ns, name)
	if err != nil {
		return kube.PodSummary{}, err
	}
	summary := kube.PodSummaryOf(pod)
	k.mu.Lock()
	k.pods[key] = summary
	k.mu.Unlock()
	return summary, nil
}

// kubeLevel is where a virtual path points (5.2).
type kubeLevel uint8

const (
	kubeRoot        kubeLevel = iota // namespaces
	kubeNamespace                    // resource type folders
	kubeTypeFolder                   // objects of one type (top level or inside an owner)
	kubeObject                       // child type folders of an object
	kubeContainers                   // containers of a pod
	kubeContainerFS                  // files inside a container
)

// kubePath is a resolved virtual path.
type kubePath struct {
	Level     kubeLevel
	Namespace string
	Folder    string         // Type folder (typeFolder level).
	Parent    *kube.Object   // Owner of the type folder, nil at the namespace level.
	Object    *kube.Object   // The object (object, containers and containerFS levels).
	Objects   []*kube.Object // Objects listed by a type folder.
	Container string
	FSPath    string
	Prefix    string // Virtual path of the container root (containerFS level).
	Snapshot  *kube.Snapshot
	Unknown   bool // Resolved without a snapshot: level guessed from the segment count.
}

func isPodObject(o *kube.Object) bool {
	return o != nil && o.GVR.Group == "" && o.GVR.Resource == "pods"
}
func kubeSegments(p string) []string {
	p = path.Clean("/" + p)
	if p == "/" {
		return nil
	}
	return strings.Split(strings.TrimPrefix(p, "/"), "/")
}

// resolve walks the snapshot along the path. With a nil ctx only cached data
// is used and the level is guessed when the snapshot is missing.
func (k *kubePanel) resolve(ctx context.Context, p string) (kubePath, error) {
	segs := kubeSegments(p)
	if len(segs) == 0 {
		return kubePath{Level: kubeRoot}, nil
	}
	r := kubePath{Namespace: segs[0]}
	var snap *kube.Snapshot
	var err error
	if ctx == nil {
		snap = k.cachedSnapshot(segs[0])
	} else {
		snap, err = k.snapshot(ctx, segs[0])
	}
	if err != nil {
		return r, err
	}
	rest := segs[1:]
	if snap == nil {
		r.Unknown = true
		switch {
		case len(rest) == 0:
			r.Level = kubeNamespace
		case len(rest)%2 == 1:
			r.Level, r.Folder = kubeTypeFolder, rest[len(rest)-1]
		default:
			r.Level = kubeObject
		}
		return r, nil
	}
	r.Snapshot = snap
	var parent *kube.Object
	i := 0
	for {
		if i == len(rest) {
			if parent == nil {
				r.Level = kubeNamespace
			} else {
				r.Level, r.Object = kubeObject, parent
			}
			return r, nil
		}
		folder := rest[i]
		i++
		var objects []*kube.Object
		var known bool
		if parent == nil {
			objects, known = snap.ByFolder[folder]
		} else {
			objects, known = snap.Children(parent)[folder]
		}
		if !known {
			return r, fmt.Errorf("no such folder: %s", path.Join(segs[:i+1]...))
		}
		if i == len(rest) {
			r.Level, r.Folder, r.Parent, r.Objects = kubeTypeFolder, folder, parent, objects
			return r, nil
		}
		name := rest[i]
		i++
		var obj *kube.Object
		for _, o := range objects {
			if o.Name == name {
				obj = o
				break
			}
		}
		if obj == nil {
			return r, fmt.Errorf("no such object: %s", path.Join(segs[:i+1]...))
		}
		if isPodObject(obj) {
			r.Object = obj
			// C3: a pod with exactly one ordinary container opens straight
			// into its file system; otherwise the next segment names one.
			summary, known := k.cachedPod(r.Namespace, obj.Name)
			if !known && ctx != nil {
				if summary, err = k.podSummary(ctx, r.Namespace, obj.Name); err != nil {
					return r, err
				}
				known = true
			}
			single, direct := "", false
			if known {
				single, direct = summary.SingleContainer()
			}
			switch {
			case direct:
				r.Level, r.Container, r.FSPath = kubeContainerFS, single, "/"+path.Join(rest[i:]...)
				r.Prefix = "/" + path.Join(segs[:i+1]...)
			case i == len(rest):
				r.Level = kubeContainers
			default:
				r.Level, r.Container, r.FSPath = kubeContainerFS, containerName(rest[i]), "/"+path.Join(rest[i+1:]...)
				r.Prefix = "/" + path.Join(segs[:i+2]...)
			}
			return r, nil
		}
		parent = obj
	}
}

// kubeEntry is the FileInfo of a panel entry. Objects that own other
// objects (and pods) are folders; an object without children is a file whose
// Enter shows the manifest. The panel columns show kind, status and age.
type kubeEntry struct {
	name    string
	dir     bool
	kind    string
	modTime time.Time
	column  string // Status, state or object count.
	object  *kube.Object
}

func (e *kubeEntry) Name() string { return e.name }
func (e *kubeEntry) Size() int64  { return 0 }
func (e *kubeEntry) Mode() os.FileMode {
	if e.dir {
		return os.ModeDir | 0555
	}
	return 0444
}
func (e *kubeEntry) ModTime() time.Time { return e.modTime }
func (e *kubeEntry) IsDir() bool        { return e.dir }
func (e *kubeEntry) Sys() any           { return e.object }

func kubeObjectEntry(o *kube.Object, dir bool) Entry {
	return Entry{Name: o.Name, Dir: dir, Info: &kubeEntry{name: o.Name, dir: dir, kind: o.Kind, modTime: o.Created, column: o.ShortStatus(), object: o}}
}
func kubeFolderEntry(name string, count int) Entry {
	return Entry{Name: name, Dir: true, Info: &kubeEntry{name: name, dir: true, column: strconv.Itoa(count) + " objects"}}
}

// kubeAge formats an age like kubectl: 45s, 12m, 3h, 5d, 2y.
func kubeAge(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < 0:
		return "0s"
	case d < 2*time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < 2*time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	case d < 2*365*24*time.Hour:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	}
	return strconv.Itoa(int(d.Hours()/24/365)) + "y"
}

// panelColumns renders the metadata columns of a Kubernetes entry for the
// panel view: kind, status and age (Detailed: the creation time instead).
func (e *kubeEntry) panelColumns(view string) string {
	switch view {
	case "Wide":
		return fmt.Sprintf("%-12.12s", e.column)
	case "Detailed":
		created := ""
		if !e.modTime.IsZero() {
			created = e.modTime.Local().Format("02.01.06 15:04")
		}
		return fmt.Sprintf("%-14.14s %-16.16s %s", e.kind, e.column, created)
	}
	return fmt.Sprintf("%-11.11s %-12.12s %5s", e.kind, e.column, kubeAge(e.modTime))
}

// loadKube lists one level of the cluster into the panel.
func (p *Panel) loadKube(abs string) error {
	k := p.Kube
	o := p.readOperation
	if o == nil {
		o = fsops.NewOperation("Reading cluster")
		defer o.Cancel()
	}
	ctx := o.Context()
	r, err := k.resolve(ctx, abs)
	if err != nil {
		return err
	}
	var files []Entry
	info := k.label()
	switch r.Level {
	case kubeRoot:
		list, err := k.namespaceList(ctx)
		if err != nil {
			return err
		}
		for _, ns := range list {
			files = append(files, kubeObjectEntry(ns, true))
		}
		info += "\nNamespaces · server " + k.client.ServerVersion()
	case kubeNamespace:
		for _, folder := range r.Snapshot.Folders() {
			files = append(files, kubeFolderEntry(folder, len(r.Snapshot.ByFolder[folder])))
		}
		info += "\nNamespace " + r.Namespace + " · resource types"
		if n := len(r.Snapshot.Skipped); n > 0 {
			info += fmt.Sprintf("\n%d resource types skipped (%s)", n, r.Snapshot.Skipped[0].Reason)
		}
	case kubeTypeFolder:
		for _, obj := range r.Objects {
			// Owners of other objects and pods are folders; the rest are
			// files whose Enter shows the manifest.
			dir := isPodObject(obj) || len(r.Snapshot.Children(obj)) > 0
			files = append(files, kubeObjectEntry(obj, dir))
		}
		info += "\n" + r.Folder
		if r.Parent != nil {
			info += " owned by " + r.Parent.Kind + " " + r.Parent.Name
			if r.Parent.GVR.Resource == "services" {
				info += " (matched by selector)"
			}
		}
	case kubeObject:
		children := r.Snapshot.Children(r.Object)
		var folders []string
		for folder := range children {
			folders = append(folders, folder)
		}
		for _, folder := range kube.FolderOrder(folders) {
			files = append(files, kubeFolderEntry(folder, len(children[folder])))
		}
		info += "\n" + r.Object.Kind + " " + r.Object.Name + " · owned objects"
	case kubeContainers:
		summary, err := k.podSummary(ctx, r.Namespace, r.Object.Name)
		if err != nil {
			return err
		}
		for _, c := range summary.Containers {
			name := c.Name
			if c.Kind != "" {
				name += " (" + c.Kind + ")"
			}
			kind := "Container"
			if c.Kind != "" {
				kind = strings.ToUpper(c.Kind[:1]) + c.Kind[1:] + " container"
			}
			files = append(files, Entry{Name: name, Dir: true, Info: &kubeEntry{name: name, dir: true, kind: kind, modTime: c.Started, column: c.State, object: r.Object}})
		}
		info += "\nPod " + r.Object.Name + " · " + summary.Phase + " · containers"
	case kubeContainerFS:
		f, err := r.container(k).Open(ctx)
		if err != nil {
			return err
		}
		defer f.Close()
		var size int64
		if files, size, err = p.listFS(f, abs, o); err != nil {
			return err
		}
		probe, _ := k.probe(ctx, kube.ContainerRef{Namespace: r.Namespace, Pod: r.Object.Name, Container: r.Container})
		info += "\nPod " + r.Object.Name + " · container " + r.Container + " · shell " + probe.Shell
		if !probe.Tar {
			info += " · no tar"
		}
		if !probe.Cat {
			info += " · no cat: files cannot be written"
		}
		if pf, ok := f.(prefixFS); ok {
			if cfs, ok := pf.FS.(*kube.ContainerFS); ok && cfs.ListMode() == "builtin" {
				info += "\nNo ls, stat or find: names and types only, sizes and dates unknown"
			}
		}
		info += "\nCommands run in the container; the shell stays local"
		state := p.beginLoad(abs)
		p.cancelPanelLoad()
		p.cancelTree()
		p.sortEntries(files)
		p.installEntries(files, state)
		p.TotalBytes, p.ItemCount = size, len(files)
		p.Descriptions = nil
		p.InfoText = info
		k.mu.Lock()
		k.last, k.lastPath = r, abs
		k.mu.Unlock()
		return nil
	}
	if err := o.Check(); err != nil {
		return err
	}
	filtered := files[:0]
	for _, e := range files {
		if matchesMask(e.Name, p.activeFilter(), p.MaskGroups) || e.Dir && p.activeFilter() == "" {
			filtered = append(filtered, e)
		}
	}
	state := p.beginLoad(abs)
	p.cancelPanelLoad()
	p.cancelTree()
	p.sortEntries(filtered)
	p.installEntries(filtered, state)
	p.TotalBytes = 0
	p.ItemCount = len(filtered)
	p.Descriptions = nil
	p.InfoText = info
	k.mu.Lock()
	k.last, k.lastPath = r, abs
	k.mu.Unlock()
	return nil
}

// level is the resolved level of the panel's current path from cached data
// only (never the network): what the key bar and capabilities need.
func (p *Panel) kubeLevel() kubePath {
	k := p.Kube
	k.mu.Lock()
	last, lastPath := k.last, k.lastPath
	k.mu.Unlock()
	if lastPath == p.Path {
		return last
	}
	r, err := k.resolve(nil, p.Path)
	if err != nil {
		r.Unknown = true
	}
	return r
}

// kubeCurrent is the object under the cursor, if the entry is an object.
func (p *Panel) kubeCurrent() *kube.Object {
	if len(p.Entries) == 0 || p.Entries[p.Cursor].Name == ".." {
		return nil
	}
	if e, ok := p.Entries[p.Cursor].Info.(*kubeEntry); ok {
		return e.object
	}
	return nil
}

// kubeBackend: a Kubernetes cluster shown as namespaces, resource types,
// objects and their owned children (docs/KUBERNETES_PANEL_SPEC.md).
type kubeBackend struct{}

func (kubeBackend) Kind() backendKind                        { return backendKube }
func (kubeBackend) Label(p *Panel) string                    { return p.Kube.label() }
func (kubeBackend) FilesystemPath(p *Panel) string           { return p.RemoteLocalPath }
func (kubeBackend) Virtual(*Panel, string) bool              { return true }
func (kubeBackend) HistoryEntry(p *Panel, abs string) string { return p.Kube.label() + abs }
func (kubeBackend) SizeSource(p *Panel) string               { return p.Kube.label() }
func (kubeBackend) InfoPlugin(p *Panel) string               { return p.Kube.label() + p.Path }
func (kubeBackend) Caps(p *Panel) panelCaps {
	switch p.kubeLevel().Level {
	case kubeRoot:
		return capMkdir | capDelete | capQuickView
	case kubeTypeFolder:
		return capEdit | capDelete | capQuickView
	case kubeContainers:
		return capQuickView
	case kubeContainerFS:
		return capEdit | capMkdir | capDelete | capRename | capCopyIn | capExecute
	}
	return 0
}
func (kubeBackend) Endpoint(p *Panel) remotefs.Endpoint {
	if r := p.kubeLevel(); r.Level == kubeContainerFS && r.Object != nil {
		return remotefs.Endpoint{Opener: r.container(p.Kube)}
	}
	return remotefs.Endpoint{}
}
func (kubeBackend) CommandPrompt(p *Panel) string {
	if r := p.kubeLevel(); r.Level == kubeContainerFS && r.Object != nil {
		return "pod:" + r.Namespace + "/" + r.Object.Name + "/" + r.Container + ":" + r.FSPath
	}
	return ""
}
func (kubeBackend) Execute(a *App, p *Panel, command string) bool {
	r := p.kubeLevel()
	if r.Level != kubeContainerFS || r.Object == nil {
		return false
	}
	a.kubeRunInContainer(p, r, command)
	return true
}
func (kubeBackend) KeyBar(p *Panel, mods modifiers) [10]string {
	plain := [10]string{"Help", "", "", "", "", "", "", "", "Menu", ""}
	shift := [10]string{}
	alt := [10]string{}
	ctrl := keyBarCtrl
	ctrl[7], ctrl[8], ctrl[9] = "", "", ""
	level := p.kubeLevel()
	switch level.Level {
	case kubeRoot:
		plain[2], plain[3], plain[4], plain[6], plain[7] = "Manif", "Edit", "Export", "NewNS", "Delete"
		shift[3] = "Descr"
	case kubeNamespace:
		plain[4] = "Export"
	case kubeTypeFolder:
		plain[2], plain[3], plain[4], plain[7] = "Manif", "Edit", "Copy", "Delete"
		shift[3], shift[7] = "Descr", "Orphan"
		if obj := p.kubeCurrent(); obj != nil && level.Snapshot != nil && level.Snapshot.HasPods(obj) {
			shift[2], alt[2] = "Logs", "Follow"
		}
	case kubeObject:
		plain[4] = "Export"
	case kubeContainers:
		plain[2] = "Manif"
		shift[2], shift[3], alt[2] = "Logs", "Descr", "Follow"
	case kubeContainerFS:
		plain = keyBarLocal
		plain[7] = "Delete"
		shift = keyBarShift
		shift[0], shift[1], shift[2], shift[8] = "", "", "", ""
	}
	return keyBarFor(mods, plain, shift, ctrl, alt)
}
func (kubeBackend) DocumentSource(p *Panel, path string) documentSource {
	if r := p.kubeLevel(); r.Level == kubeContainerFS && r.Object != nil {
		return &remoteSource{Opener: r.container(p.Kube), Label: p.Kube.label(), Path: path}
	}
	return p.manifestSource(path)
}
func (kubeBackend) Open(a *App, p *Panel, edit bool) bool {
	if len(p.Entries) == 0 || p.Entries[p.Cursor].Name == ".." {
		a.Status = "Choose an entry first"
		return true
	}
	if p.kubeLevel().Level == kubeContainerFS {
		if p.Entries[p.Cursor].Dir {
			return true
		}
		a.openSourceDocument(kubeBackend{}.DocumentSource(p, p.Current()), edit, a.showDocument, nil)
		return true
	}
	src := p.manifestSource(p.Current())
	if src == nil {
		a.Status = "No manifest here: choose a namespace, an object or a pod"
		return true
	}
	if m, ok := src.(*manifestSource); ok {
		m.edit = edit
	}
	a.openSourceDocument(src, edit, a.showDocument, nil)
	return true
}
func (kubeBackend) Enter(a *App, p *Panel) bool {
	if len(p.Entries) == 0 {
		return true
	}
	if e := p.Entries[p.Cursor]; !e.Dir {
		if p.kubeLevel().Level == kubeContainerFS && e.Info != nil && e.Info.Mode()&os.ModeSymlink != 0 {
			a.enterRemoteLink()
			return true
		}
		kubeBackend{}.Open(a, p, false)
		return true
	}
	a.error(p.Load(p.Current()))
	return true
}
func (kubeBackend) Mkdir(a *App, p *Panel) bool {
	if p.kubeLevel().Level == kubeContainerFS {
		a.remoteMkdir()
		return true
	}
	if p.kubeLevel().Level != kubeRoot {
		a.Status = "Namespaces are created at the cluster root; objects come from manifests (F5)"
		return true
	}
	a.kubeCreateNamespace(p)
	return true
}
func (kubeBackend) Delete(a *App, p *Panel, orphan bool) bool {
	if p.kubeLevel().Level == kubeContainerFS {
		a.remoteDelete()
		return true
	}
	a.kubeDelete(p, orphan)
	return true
}
func (kubeBackend) Move(a *App, p *Panel) bool {
	if p.kubeLevel().Level == kubeContainerFS {
		a.remoteMoveDialog()
		return true
	}
	a.Status = "Kubernetes objects cannot be renamed; copy the manifest and apply it under a new name"
	return true
}
func (kubeBackend) Copy(a *App, p *Panel) bool {
	if p.kubeLevel().Level == kubeContainerFS {
		return false // The transfer dialog copies files through the container endpoint.
	}
	a.kubeExport(p)
	return true
}
func (kubeBackend) Key(a *App, p *Panel, m Message) bool {
	cmd := m.Meta || m.Ctrl
	switch {
	case m.Key == "F3" && m.Shift && !cmd && !m.Alt:
		a.kubeLogs(p, false)
	case m.Key == "F3" && m.Alt && !cmd && !m.Shift:
		a.kubeLogs(p, true)
	case m.Key == "F4" && m.Shift && !cmd && !m.Alt:
		if p.kubeLevel().Level == kubeContainerFS {
			return false // Shift+F4 creates a file there.
		}
		a.kubeDescribe(p)
	case m.Key == "F1" && m.Shift, m.Key == "F2" && m.Shift, m.Key == "F3" && cmd && m.Shift, m.Key == "F4" && cmd && m.Shift:
		a.Status = "Not available in a Kubernetes panel"
	default:
		return false
	}
	return true
}
func (kubeBackend) MenuSection(a *App, p *Panel) *menuSection {
	return &menuSection{Name: "Kubernetes", Items: []menuItem{
		{"Logs…", "⇧F3", func() { a.kubeLogs(p, false) }},
		{"Follow logs in terminal", "⌥F3", func() { a.kubeLogs(p, true) }},
		{"Describe", "⇧F4", func() { a.kubeDescribe(p) }},
		{"Open shell in container", "", func() { a.kubeShell(p) }},
		{"Export manifests…", "F5", func() { a.kubeExport(p) }},
		{"Refresh snapshot", "⌘R", func() { a.rereadPanel(a.Active) }},
		{"Switch context…", "", func() { a.kubeConnectionForm(a.Active, p.Kube.profile) }},
		{"Disconnect", "", func() { a.disconnectRemote(a.Active) }},
	}}
}
