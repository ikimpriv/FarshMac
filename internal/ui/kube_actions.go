package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"farshmac/internal/fsops"
	"farshmac/internal/kube"
	"farshmac/internal/terminal"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var namespaceGVR = schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}

// manifestSource is an object's manifest as a document: F3 shows it, F4
// edits it and saves with Update (C11), so a concurrent change surfaces as a
// conflict instead of being overwritten.
type manifestSource struct {
	k               *kubePanel
	ref             kube.Ref
	edit            bool // The editor gets the manifest without server-managed fields.
	resourceVersion string
	schema          *kube.Schema // From the cluster's OpenAPI, for checks and completion (editor).
}

// Display ends in .yaml so the viewer and editor highlight it as YAML.
func (s *manifestSource) Display() string { return s.k.label() + "/" + s.ref.String() + ".yaml" }
func (s *manifestSource) Name() string    { return s.ref.Name + ".yaml" }
func (s *manifestSource) Blocked(os.FileInfo) string {
	return ""
}
func (s *manifestSource) Fetch(o *fsops.Operation, dir string) (string, os.FileInfo, error) {
	obj, err := s.k.client.Get(o.Context(), s.ref)
	if err != nil {
		return "", nil, err
	}
	mode := kube.ExportFull
	if s.edit {
		mode = kube.ExportEdit
	}
	data, err := kube.ExportYAMLMode(obj, mode)
	if err != nil {
		return "", nil, err
	}
	s.resourceVersion = obj.GetResourceVersion()
	if s.edit {
		// Missing schema (no OpenAPI for this kind) only disables the checks.
		if schema, err := s.k.client.SchemaFor(o.Context(), obj.GroupVersionKind()); err == nil {
			s.schema = schema
		}
	}
	local := filepath.Join(dir, s.Name())
	if err = os.WriteFile(local, data, 0600); err != nil {
		return "", nil, err
	}
	return local, nil, nil
}
func (s *manifestSource) Store(o *fsops.Operation, staged string, _ os.FileInfo, force bool) (os.FileInfo, error) {
	data, err := os.ReadFile(staged)
	if err != nil {
		return nil, err
	}
	objs, err := kube.Decode(data)
	if err != nil {
		return nil, err
	}
	if len(objs) != 1 {
		return nil, fmt.Errorf("the manifest must contain exactly one object, found %d", len(objs))
	}
	obj := objs[0]
	if obj.GetName() != s.ref.Name {
		return nil, fmt.Errorf("the manifest names %q; this document is %s", obj.GetName(), s.ref.Name)
	}
	if obj.GetNamespace() == "" && s.ref.Namespace != "" {
		obj.SetNamespace(s.ref.Namespace)
	}
	version := s.resourceVersion
	if force {
		current, err := s.k.client.Get(o.Context(), s.ref)
		if err != nil {
			return nil, err
		}
		version = current.GetResourceVersion()
	}
	// The text keeps the resourceVersion of the first fetch; the one of the
	// last successful save is what the server compares against.
	obj.SetResourceVersion(version)
	updated, err := s.k.client.Update(o.Context(), obj)
	if apierrors.IsConflict(err) {
		return nil, errRemoteChanged
	}
	if err != nil {
		return nil, err
	}
	s.resourceVersion = updated.GetResourceVersion()
	s.k.invalidate(s.ref.Namespace)
	return nil, nil
}

// manifestSource of the entry at a virtual path: a namespace at the root, an
// object in a type folder, the pod of a containers listing; nil elsewhere.
func (p *Panel) manifestSource(virtual string) documentSource {
	k := p.Kube
	segs := kubeSegments(virtual)
	level := p.kubeLevel()
	switch {
	case len(segs) == 1:
		return &manifestSource{k: k, ref: kube.Ref{GVR: namespaceGVR, Name: segs[0]}}
	case level.Level == kubeTypeFolder && len(segs) >= 2:
		name := segs[len(segs)-1]
		for _, o := range level.Objects {
			if o.Name == name {
				return &manifestSource{k: k, ref: o.Ref()}
			}
		}
	case level.Level == kubeContainers && level.Object != nil:
		return &manifestSource{k: k, ref: level.Object.Ref()}
	}
	return nil
}

// showText opens read-only text (describe output, logs) as a document screen.
func (a *App) showText(title, text string) {
	d := newDocument(title, text)
	d.Help = true
	d.ReadOnly = true
	d.markSaved()
	d.Lines()
	a.showDocument(d)
}

func (a *App) kubeCreateNamespace(p *Panel) {
	k := p.Kube
	a.prompt("New namespace", "Name", "", func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		a.workProgress("Creating namespace "+name, func(o *fsops.Operation) error {
			o.Streaming()
			if err := k.client.CreateNamespace(o.Context(), name); err != nil {
				return err
			}
			k.invalidate("")
			return nil
		})
		a.Panels[a.Active].pendingPosition(name, "")
	})
}

// kubeDelete removes the selected objects (Background propagation, or
// Orphan when orphan is set) or, at the root, one namespace after its name
// is typed again (C8).
func (a *App) kubeDelete(p *Panel, orphan bool) {
	k := p.Kube
	level := p.kubeLevel()
	switch level.Level {
	case kubeRoot:
		ns := p.kubeCurrent()
		if ns == nil {
			return
		}
		a.prompt("Delete namespace "+ns.Name, "Type the namespace name to delete it with everything inside", "", func(s string) {
			if strings.TrimSpace(s) != ns.Name {
				a.Status = "Namespace not deleted: the name did not match"
				return
			}
			a.workProgress("Deleting namespace "+ns.Name, func(o *fsops.Operation) error {
				o.Streaming()
				if err := k.client.DeleteNamespace(o.Context(), ns.Name); err != nil {
					return err
				}
				k.invalidate("")
				return nil
			})
		})
	case kubeTypeFolder:
		var objects []*kube.Object
		for _, name := range p.Selected() {
			name = path.Base(name)
			for _, o := range level.Objects {
				if o.Name == name {
					objects = append(objects, o)
				}
			}
		}
		if len(objects) == 0 {
			return
		}
		lines := make([]string, 0, min(len(objects), 10)+1)
		for i, o := range objects {
			if i == 10 {
				lines = append(lines, fmt.Sprintf("… and %d more", len(objects)-10))
				break
			}
			lines = append(lines, o.Kind+" "+o.Name)
		}
		title, action, note := "Delete objects", "Delete", "Owned objects are deleted too (Background propagation)."
		if orphan {
			title, action, note = "Delete, keep children", "Orphan", "Owned objects are kept (Orphan propagation)."
		}
		a.confirm(title, fmt.Sprintf("Delete from namespace %s:\n%s\n\n%s", level.Namespace, strings.Join(lines, "\n"), note), []string{action, "Cancel"}, func(i int) {
			if i != 0 {
				return
			}
			a.workProgress("Deleting objects", func(o *fsops.Operation) error {
				o.Streaming()
				for _, obj := range objects {
					if err := o.Check(); err != nil {
						return err
					}
					o.Add(obj.Name, 0)
					if err := k.client.Delete(o.Context(), obj.Ref(), orphan); err != nil {
						return fmt.Errorf("%s %s: %w", obj.Kind, obj.Name, err)
					}
					o.Complete(obj.Name)
				}
				k.invalidate(level.Namespace)
				return nil
			})
		})
	default:
		a.Status = "Select objects inside a type folder to delete them"
	}
}

// exportItem is one manifest to write: the object and its file path under
// the destination folder.
type exportItem struct {
	obj  *kube.Object
	file string
}

// kubeExportPlan lists what F5 writes for the selection at this level: an
// object becomes <name>.yaml, a folder its contents recursively (5.3).
func (a *App) kubeExportPlan(p *Panel, level kubePath) ([]exportItem, error) {
	k := p.Kube
	var items []exportItem
	folderItems := func(snap *kube.Snapshot, dir string) {
		for _, folder := range snap.Folders() {
			if folder == "events" || k.hidden[folder] {
				continue
			}
			for _, o := range snap.ByFolder[folder] {
				items = append(items, exportItem{o, filepath.Join(dir, folder, o.Name+".yaml")})
			}
		}
	}
	selected := map[string]bool{}
	for _, s := range p.Selected() {
		selected[path.Base(s)] = true
	}
	switch level.Level {
	case kubeRoot:
		for name := range selected {
			snap, err := k.snapshot(a.exportContext(), name)
			if err != nil {
				return nil, err
			}
			folderItems(snap, name)
		}
	case kubeNamespace:
		for folder := range selected {
			for _, o := range level.Snapshot.ByFolder[folder] {
				items = append(items, exportItem{o, filepath.Join(folder, o.Name+".yaml")})
			}
		}
	case kubeTypeFolder:
		for _, o := range level.Objects {
			if selected[o.Name] {
				items = append(items, exportItem{o, o.Name + ".yaml"})
			}
		}
	case kubeObject:
		children := level.Snapshot.Children(level.Object)
		for folder := range selected {
			for _, o := range children[folder] {
				items = append(items, exportItem{o, filepath.Join(folder, o.Name+".yaml")})
			}
		}
	default:
		return nil, errors.New("containers cannot be copied; copy files inside them")
	}
	sort.Slice(items, func(i, j int) bool { return items[i].file < items[j].file })
	return items, nil
}

// exportContext is the context snapshots are loaded with while a dialog is
// being prepared; the operation that follows has its own.
func (a *App) exportContext() contextLike { return backgroundContext{} }

// kubeExport copies manifests of the selection into a local folder.
func (a *App) kubeExport(p *Panel) {
	other := &a.Panels[1-a.Active]
	if !other.can(capLocalOutput) {
		a.Status = "Export manifests into a local panel"
		return
	}
	k := p.Kube
	level := p.kubeLevel()
	items, err := a.kubeExportPlan(p, level)
	if err != nil {
		a.error(err)
		return
	}
	if len(items) == 0 {
		a.Status = "Nothing selected to export"
		return
	}
	secrets := false
	for _, it := range items {
		if it.obj.GVR.Resource == "secrets" && it.obj.GVR.Group == "" {
			secrets = true
		}
	}
	dest := other.filesystemPath()
	start := func(dir string, clean bool) {
		a.workProgress(fmt.Sprintf("Exporting %d manifest(s)", len(items)), func(o *fsops.Operation) error {
			for _, it := range items {
				o.Add(it.file, 0)
			}
			o.Ready()
			for _, it := range items {
				if err := o.Check(); err != nil {
					return err
				}
				target := filepath.Join(dir, it.file)
				obj, err := k.client.Get(o.Context(), it.obj.Ref())
				if err != nil {
					return fmt.Errorf("%s: %w", it.file, err)
				}
				data, err := kube.ExportYAML(obj, clean)
				if err != nil {
					return fmt.Errorf("%s: %w", it.file, err)
				}
				if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
					return err
				}
				if err = fsops.Create(target, data, 0644, o); err != nil {
					return fmt.Errorf("%s: %w", it.file, err)
				}
				o.Complete(it.file)
			}
			return nil
		})
	}
	ask := func() {
		a.form("Export manifests", []Field{
			{Label: fmt.Sprintf("Write %d manifest(s) to", len(items)), Value: dest},
			{Label: "Clean export (strip status and server fields)", Toggle: true, Checked: true},
		}, func(fields []Field) {
			start(expand(fields[0].Value, dest), fields[1].Checked)
		})
		a.Form.Primary = "Export"
	}
	if secrets {
		a.confirm("Secrets", "The selection includes Secrets. Their manifests contain the secret data (base64). Export anyway?", []string{"Export", "Cancel"}, func(i int) {
			if i == 0 {
				ask()
			}
		})
		return
	}
	ask()
}

// kubeTarget is the pod a logs/describe command applies to: the current
// object when it is a pod, a chosen pod among its owned pods otherwise.
func (a *App) kubePods(level kubePath, obj *kube.Object) []*kube.Object {
	if isPodObject(obj) {
		return []*kube.Object{obj}
	}
	if level.Snapshot == nil {
		return nil
	}
	var pods []*kube.Object
	seen := map[string]bool{}
	var walk func(o *kube.Object)
	walk = func(o *kube.Object) {
		for _, children := range level.Snapshot.Children(o) {
			for _, c := range children {
				if isPodObject(c) {
					if !seen[c.Name] {
						seen[c.Name] = true
						pods = append(pods, c)
					}
				} else {
					walk(c)
				}
			}
		}
	}
	walk(obj)
	sort.Slice(pods, func(i, j int) bool { return pods[i].Name < pods[j].Name })
	return pods
}

// kubeLogs shows the last lines of a container's log (Shift+F3). follow
// streams them in the terminal area, which arrives with container support.
func (a *App) kubeLogs(p *Panel, follow bool) {
	k := p.Kube
	level := p.kubeLevel()
	obj := p.kubeCurrent()
	var pods []*kube.Object
	container := ""
	switch level.Level {
	case kubeTypeFolder:
		pods = a.kubePods(level, obj)
	case kubeContainers:
		pods = []*kube.Object{level.Object}
		if len(p.Entries) > 0 && p.Entries[p.Cursor].Name != ".." {
			container, _, _ = strings.Cut(p.Entries[p.Cursor].Name, " (")
		}
	}
	if len(pods) == 0 {
		a.Status = "Logs: choose a pod, or an object that owns pods"
		return
	}
	withPod := func(pod *kube.Object) {
		withContainer := func(name string) {
			if follow {
				a.kubeFollowLogs(p, pod, name)
				return
			}
			tail := int64(a.Settings.KubeLogsTail)
			if tail <= 0 {
				tail = 1000
			}
			title := fmt.Sprintf("%s/%s/%s/%s.log", k.label(), pod.Namespace, pod.Name, name)
			a.backgroundTask("Reading logs of "+pod.Name, func(o *fsops.Operation) (func(), func(), error) {
				r, err := k.client.Logs(o.Context(), pod.Namespace, pod.Name, kube.LogOptions{Container: name, TailLines: tail})
				if err != nil {
					return nil, nil, err
				}
				defer r.Close()
				data, err := io.ReadAll(io.LimitReader(o.Reader(r), 16<<20))
				if err != nil && o.Check() == nil {
					return nil, nil, err
				}
				text := string(data)
				if text == "" {
					text = "(no log output)\n"
				}
				return func() { a.showText(title, text) }, nil, o.Check()
			})
			if a.task != nil {
				a.task.reportError = true
			}
		}
		if container != "" {
			withContainer(container)
			return
		}
		summary, err := k.podSummary(backgroundContext{}, pod.Namespace, pod.Name)
		if err != nil {
			a.error(err)
			return
		}
		names := summary.Names()
		if len(names) <= 1 {
			if len(names) == 1 {
				withContainer(names[0])
			} else {
				withContainer("")
			}
			return
		}
		a.choose("Container", names, func(i int) { withContainer(names[i]) })
	}
	if len(pods) == 1 {
		withPod(pods[0])
		return
	}
	names := make([]string, len(pods))
	for i, pod := range pods {
		names[i] = pod.Name + "  " + pod.ShortStatus()
	}
	a.choose("Pod", names, func(i int) { withPod(pods[i]) })
}

// kubeDescribe shows kubectl's describe output for the current entry.
func (a *App) kubeDescribe(p *Panel) {
	k := p.Kube
	level := p.kubeLevel()
	var ref kube.Ref
	switch {
	case level.Level == kubeRoot && p.kubeCurrent() != nil:
		ref = kube.Ref{GVR: namespaceGVR, Name: p.kubeCurrent().Name}
	case level.Level == kubeTypeFolder && p.kubeCurrent() != nil:
		ref = p.kubeCurrent().Ref()
	case level.Level == kubeContainers && level.Object != nil:
		ref = level.Object.Ref()
	default:
		a.Status = "Describe: choose a namespace, an object or a pod"
		return
	}
	title := k.label() + "/describe/" + ref.String()
	a.backgroundTask("Describing "+ref.Name, func(o *fsops.Operation) (func(), func(), error) {
		text, err := k.client.Describe(o.Context(), ref)
		if err != nil {
			return nil, nil, err
		}
		return func() { a.showText(title, text) }, nil, nil
	})
	if a.task != nil {
		a.task.reportError = true
	}
}

// kubeRunInContainer runs a command line typed at the prompt inside the
// container shown by the panel, in the panel's folder, with a TTY in the
// terminal area (5.5). There is no persistent shell: each command starts
// fresh, which is why cd must come from the panel.
func (a *App) kubeRunInContainer(p *Panel, r kubePath, command string) {
	k := p.Kube
	ref := kube.ContainerRef{Namespace: r.Namespace, Pod: r.Object.Name, Container: r.Container}
	probe, err := k.probe(backgroundContext{}, ref)
	if err != nil {
		a.error(err)
		return
	}
	if probe.Shell == "" {
		a.Status = "No shell in this container; commands cannot run"
		return
	}
	argv := []string{probe.Shell, "-c", "cd " + terminal.Quote(r.FSPath) + " && " + command}
	a.kubeStartExec(p, ref, argv, "pod:"+ref.Pod+"/"+ref.Container+" "+command)
}

// kubeShell opens an interactive shell in the container under the cursor or
// shown by the panel ("Open shell in container").
func (a *App) kubeShell(p *Panel) {
	k := p.Kube
	r := p.kubeLevel()
	var ref kube.ContainerRef
	switch r.Level {
	case kubeContainerFS:
		ref = kube.ContainerRef{Namespace: r.Namespace, Pod: r.Object.Name, Container: r.Container}
	case kubeContainers:
		if len(p.Entries) == 0 || p.Entries[p.Cursor].Name == ".." {
			a.Status = "Choose a container"
			return
		}
		ref = kube.ContainerRef{Namespace: r.Namespace, Pod: r.Object.Name, Container: containerName(p.Entries[p.Cursor].Name)}
	default:
		a.Status = "Open shell: enter a pod's container first"
		return
	}
	probe, err := k.probe(backgroundContext{}, ref)
	if err != nil {
		a.error(err)
		return
	}
	if probe.Shell == "" {
		a.Status = "No shell in this container"
		return
	}
	a.kubeStartExec(p, ref, []string{probe.Shell}, "pod:"+ref.Pod+"/"+ref.Container+" "+probe.Shell)
}

// kubeStartExec attaches an exec stream to the terminal area; when it ends
// the container panel is listed again.
func (a *App) kubeStartExec(p *Panel, ref kube.ContainerRef, argv []string, label string) {
	k := p.Kube
	index := a.Active
	stream, err := kube.StartExec(context.Background(), k.exec(), ref, argv, a.Cols, max(1, a.Rows-1))
	if err != nil {
		a.error(err)
		return
	}
	a.startForeign(stream, label, func() {
		if q := &a.Panels[index]; q.Kube == k {
			a.error(q.Load(q.Path))
		}
	})
}

// kubeFollowLogs streams a container's log into the terminal area (Alt+F3).
func (a *App) kubeFollowLogs(p *Panel, pod *kube.Object, container string) {
	k := p.Kube
	tail := int64(a.Settings.KubeLogsTail)
	if tail <= 0 {
		tail = 1000
	}
	stream := newLogStream(context.Background(), func(ctx context.Context) (io.ReadCloser, error) {
		return k.client.Logs(ctx, pod.Namespace, pod.Name, kube.LogOptions{Container: container, TailLines: tail, Follow: true})
	})
	a.startForeign(stream, "logs "+pod.Namespace+"/"+pod.Name+"/"+container, nil)
}
