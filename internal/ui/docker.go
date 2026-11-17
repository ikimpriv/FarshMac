package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"farshmac/internal/docker"
	"farshmac/internal/fsops"
	"farshmac/internal/kube"
	"farshmac/internal/podtoast"
	"farshmac/internal/remotefs"
	"farshmac/internal/shellsession"
	"farshmac/internal/terminal"
)

type dockerPanel struct {
	client     docker.Client
	mu         sync.Mutex
	containers []docker.Container
	probes     map[string]kube.Probe
	helpers    map[string]string
	mounts     map[string][]kube.StorageMount
}

func (d *dockerPanel) refresh(ctx context.Context) error {
	list, err := d.client.Containers(ctx)
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.containers = list
	running := map[string]bool{}
	for _, c := range list {
		running[c.ID] = c.State == "running"
	}
	for target, id := range d.helpers {
		if !running[id] {
			delete(d.helpers, target)
		}
	}
	if d.helpers == nil {
		d.helpers = map[string]string{}
	}
	for _, c := range list {
		if c.State != "running" {
			continue
		}
		if target := c.Label("io.farshmac.podtoast.target"); target != "" {
			d.helpers[target] = c.ID
		}
		if volume := c.Label("io.farshmac.podtoast.volume"); volume != "" {
			d.helpers["volume:"+volume] = c.ID
		}
	}
	d.mu.Unlock()
	return nil
}
func (d *dockerPanel) location(p string) (docker.Container, string, string, bool) {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	count := 2
	if len(parts) > 0 && parts[0] == "Compose" {
		count = 3
	}
	if len(parts) < count {
		return docker.Container{}, "", "", false
	}
	if parts[0] == "Volumes" {
		d.mu.Lock()
		helper := d.helpers["volume:"+parts[1]]
		d.mu.Unlock()
		if helper != "" {
			return docker.Container{ID: helper, Names: parts[1], State: "running"}, "/Volumes/" + parts[1], "/" + strings.Join(parts[2:], "/"), true
		}
		return docker.Container{}, "", "", false
	}
	if parts[0] != "Compose" && parts[0] != "Containers" {
		return docker.Container{}, "", "", false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range d.containers {
		if c.Names != parts[count-1] {
			continue
		}
		project := c.Label("com.docker.compose.project")
		if parts[0] == "Compose" && project != parts[1] || parts[0] == "Containers" && project != "" {
			continue
		}
		return c, "/" + strings.Join(parts[:count], "/"), "/" + strings.Join(parts[count:], "/"), true
	}
	return docker.Container{}, "", "", false
}
func (d *dockerPanel) helper(id string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.helpers[id]
}
func (d *dockerPanel) probe(ctx context.Context, id string) (kube.Probe, error) {
	d.mu.Lock()
	p, ok := d.probes[id]
	d.mu.Unlock()
	if ok {
		return p, nil
	}
	p, err := kube.ProbeContainer(ctx, d.client, kube.ContainerRef{Container: id})
	if err == nil {
		d.mu.Lock()
		if d.probes == nil {
			d.probes = map[string]kube.Probe{}
		}
		d.probes[id] = p
		d.mu.Unlock()
	}
	return p, err
}

type dockerOpener struct {
	d          *dockerPanel
	id, prefix string
}

func (o *dockerOpener) Identity() string { return "docker:" + o.d.client.Context + ":" + o.id }
func (o *dockerOpener) Open(ctx context.Context) (remotefs.FS, error) {
	execution, root := o.id, ""
	if strings.HasPrefix(o.prefix, "/Volumes/") {
		root = "/volume"
	}
	if helper := o.d.helper(o.id); helper != "" {
		agent, err := o.d.client.Agent(ctx, helper, o.id)
		if err != nil {
			return nil, fmt.Errorf("PodToast: %w", err)
		}
		return prefixFS{FS: dockerAgentFS{FS: agent.FS(ctx), agent: agent}, prefix: o.prefix, identity: o.Identity()}, nil
	}
	probe, err := o.d.probe(ctx, execution)
	if err != nil {
		return nil, fmt.Errorf("%w; F11 → Attach PodToast toolbox for distroless containers", err)
	}
	f, err := kube.OpenContainerFS(ctx, o.d.client, kube.ContainerRef{Container: execution}, probe)
	if err != nil {
		return nil, err
	}
	return prefixFS{FS: f, prefix: o.prefix, identity: o.Identity(), root: root}, nil
}

type dockerBackend struct{ remoteBackend }

func (dockerBackend) Kind() backendKind                        { return backendDocker }
func (dockerBackend) Label(p *Panel) string                    { return "docker:" + p.Docker.client.Context }
func (dockerBackend) SizeSource(p *Panel) string               { return dockerBackend{}.Label(p) }
func (dockerBackend) HistoryEntry(p *Panel, abs string) string { return dockerBackend{}.Label(p) + abs }
func (dockerBackend) InfoPlugin(p *Panel) string               { return dockerBackend{}.Label(p) + p.Path }
func (dockerBackend) Caps(p *Panel) panelCaps {
	if _, _, _, ok := p.Docker.location(p.Path); ok {
		return remoteBackend{}.Caps(p)
	}
	return capQuickView
}
func (dockerBackend) Endpoint(p *Panel) remotefs.Endpoint {
	c, prefix, _, ok := p.Docker.location(p.Path)
	if !ok {
		return remotefs.Endpoint{}
	}
	return remotefs.Endpoint{Opener: &dockerOpener{p.Docker, c.ID, prefix}}
}
func (dockerBackend) DocumentSource(p *Panel, name string) documentSource {
	endpoint := p.endpoint()
	if endpoint.Opener == nil {
		return nil
	}
	return &remoteSource{Opener: endpoint.Opener, Label: p.label(), Path: name}
}
func (dockerBackend) Enter(a *App, p *Panel) bool {
	if len(p.Entries) == 0 {
		return true
	}
	if p.Entries[p.Cursor].Dir {
		a.error(p.Load(p.Current()))
	} else {
		a.dockerInspectPath(p)
	}
	return true
}
func (dockerBackend) Open(a *App, p *Panel, edit bool) bool {
	if p.can(capEdit) {
		a.openRemoteDocument(edit)
	} else {
		a.dockerInspectPath(p)
	}
	return true
}
func (dockerBackend) Move(a *App, p *Panel) bool {
	if p.can(capRename) {
		a.transferDialogTo(true, false)
	}
	return true
}
func (dockerBackend) CommandPrompt(p *Panel) string { return p.label() + p.Path }
func (dockerBackend) Execute(a *App, p *Panel, command string) bool {
	if !a.showDockerTerminal() {
		return true
	}
	_, _, cwd, _ := p.Docker.location(p.Path)
	a.queueContextCommand(p, cwd, command)
	return true
}
func (p *Panel) loadDocker(target string) error {
	ctx := context.Background()
	if p.readOperation != nil {
		ctx = p.readOperation.Context()
	}
	d := p.Docker
	c, _, cwd, files := d.location(target)
	var entries []Entry
	if files {
		if c.State != "running" {
			return fmt.Errorf("container %s is %s", c.Names, c.State)
		}
		copy := *p
		copy.Path = target
		f, err := copy.endpoint().Open(ctx)
		if err != nil {
			return err
		}
		defer f.Close()
		op := p.readOperation
		if op == nil {
			op = fsops.NewOperation("Reading Docker folder")
			defer op.Cancel()
		}
		list, size, err := p.listFS(f, target, op)
		if err != nil {
			return err
		}
		mounts, known := d.storageMounts(ctx, c.ID)
		if strings.HasPrefix(target, "/Volumes/") {
			mounts = []kube.StorageMount{{Path: "/", Kind: "Docker volume", Volume: c.Names}}
			known = true
		}
		classifyContainerEntries(list, cwd, mounts, known)
		for i := range list {
			if info, ok := list[i].Info.(storageFileInfo); ok {
				info.label = strings.ReplaceAll(info.label, "(pod spec)", "(Docker inspect)")
				list[i].Info = info
			}
		}
		entries = list
		p.TotalBytes = size
	} else {
		if err := d.refresh(ctx); err != nil {
			return err
		}
		add := func(name string, dir bool) { entries = append(entries, dockerCatalogEntry(name, dir)) }
		parts := strings.Split(strings.Trim(target, "/"), "/")
		switch {
		case target == "/":
			for _, name := range []string{"Compose", "Containers", "Volumes", "Networks", "Images"} {
				add(name, true)
			}
		case target == "/Volumes" || target == "/Networks":
			kind := "volume"
			if target == "/Networks" {
				kind = "network"
			}
			names, err := d.client.Names(ctx, kind)
			if err != nil {
				return err
			}
			for _, name := range names {
				add(name, target == "/Volumes")
			}
		case target == "/Images":
			b, err := d.client.Run(ctx, "image", "ls", "--format", "{{.ID}}")
			if err != nil {
				return err
			}
			seen := map[string]bool{}
			for _, id := range strings.Fields(string(b)) {
				if !seen[id] {
					add(id, false)
					seen[id] = true
				}
			}
		case len(parts) >= 2 && parts[0] == "Volumes":
			return fmt.Errorf("Attach a volume toolbox with F11 before browsing this volume")
		case parts[0] == "Compose" || parts[0] == "Containers":
			d.mu.Lock()
			list := append([]docker.Container(nil), d.containers...)
			d.mu.Unlock()
			seen := map[string]bool{}
			for _, c := range list {
				if c.Label("io.farshmac.podtoast.target") != "" || c.Label("io.farshmac.podtoast.volume") != "" || c.Label("io.farshmac.podtoast.network") != "" {
					continue
				}
				project := c.Label("com.docker.compose.project")
				if target == "/Containers" && project == "" {
					add(c.Names, true)
				}
				if target == "/Compose" && project != "" && !seen[project] {
					add(project, true)
					seen[project] = true
				}
				if len(parts) == 2 && parts[0] == "Compose" && parts[1] == project {
					add(c.Names, true)
				}
			}
		default:
			return fmt.Errorf("Docker location no longer exists")
		}
	}
	state := p.beginLoad(target)
	p.sortEntries(entries)
	p.installEntries(entries, state)
	p.ItemCount = len(entries)
	p.InfoText = p.label()
	return nil
}
func (a *App) dockerConnections(index int) {
	a.backgroundTask("Docker contexts", func(o *fsops.Operation) (func(), func(), error) {
		kube.EnsureLoginPath("")
		names, err := (docker.Client{}).Names(o.Context(), "context")
		return func() { a.choose("Docker context", names, func(i int) { a.connectDocker(index, names[i]) }) }, nil, err
	})
}
func (a *App) connectDocker(index int, name string) {
	requested := a.requestedRestore
	a.requestedRestore = nil
	a.rememberConnection(index, false)
	d := &dockerPanel{client: docker.Client{Context: name}, helpers: map[string]string{}}
	a.backgroundTask("Connecting to Docker", func(o *fsops.Operation) (func(), func(), error) {
		kube.EnsureLoginPath("")
		err := d.refresh(o.Context())
		if err != nil {
			return nil, nil, err
		}
		d.mu.Lock()
		for _, c := range d.containers {
			if id := c.Label("io.farshmac.podtoast.target"); id != "" && c.State == "running" {
				d.helpers[id] = c.ID
			}
		}
		for _, c := range d.containers {
			if volume := c.Label("io.farshmac.podtoast.volume"); volume != "" && c.State == "running" {
				d.helpers["volume:"+volume] = c.ID
			}
		}
		d.mu.Unlock()
		return func() {
			p := &a.Panels[index]
			local := p.filesystemPath()
			p.cancelPanelLoad()
			p.cancelTree()
			p.Kube, p.Remote, p.Archive = nil, nil, nil
			p.Docker = d
			p.RemoteLocalPath = local
			p.Async = true
			p.Hidden = false
			p.View = "Full"
			p.states = nil
			p.Marked = map[string]bool{}
			a.Active = index
			a.error(p.Load("/"))
			a.restoreConnection(index, requested)
		}, nil, nil
	})
}
func (a *App) dockerInspectPath(p *Panel) {
	if p.can(capEdit) {
		a.openRemoteDocument(false)
		return
	}
	target := p.Current()
	if len(p.Entries) == 0 || p.Entries[p.Cursor].Name == ".." {
		target = p.Path
	}
	d := p.Docker
	a.backgroundTask("Docker details", func(o *fsops.Operation) (func(), func(), error) {
		text, err := d.describe(o.Context(), target)
		return func() { a.showText("Docker details", text) }, nil, err
	})
}
func (a *App) dockerOutput(p *Panel, label string, args ...string) {
	client := p.Docker.client
	a.backgroundTask(label, func(o *fsops.Operation) (func(), func(), error) {
		b, err := client.Run(o.Context(), args...)
		if json.Valid(b) {
			var obj any
			_ = json.Unmarshal(b, &obj)
			b, _ = json.MarshalIndent(obj, "", "  ")
		}
		return func() { a.showText(label, string(b)) }, nil, err
	})
}
func (a *App) showDockerTerminal() bool {
	p := &a.Panels[a.Active]
	if p.Docker == nil {
		return false
	}
	c, _, cwd, ok := p.Docker.location(p.Path)
	key := a.panelShellContext().ID
	a.activateTerminal(key)
	if a.Foreign != nil {
		a.TermVisible = true
		return true
	}
	if !ok {
		a.pending = append(a.pending, []byte("Select a running container to open a shell.\r\n")...)
		a.TermVisible = true
		return true
	}
	managed, err := shellsession.NewRemote(a.panelShellContext(), time.Now())
	if err != nil {
		a.error(err)
		return true
	}
	d := p.Docker
	volumeContext := strings.HasPrefix(p.Path, "/Volumes/")
	cols, rows := a.Cols, max(1, a.Rows-1)
	stream := newDeferredTerminal(cols, rows, func(ctx context.Context) (foreignStream, error) {
		execution, root, label := c.ID, "", c.Names
		if volumeContext {
			root = "/volume"
			label += " | volume toolbox"
		}
		if helper := d.helper(c.ID); helper != "" {
			execution, root, label = helper, "/proc/1/root", c.Names+" | PodToast"
		}
		probe, err := d.probe(ctx, execution)
		if err != nil {
			return nil, err
		}
		script := "unset TARGET_ROOT; "
		if root != "" {
			script = "export TARGET_ROOT=" + terminal.Quote(root) + "; "
		}
		script += "cd " + terminal.Quote(path.Join(root, cwd)) + " || exit; " + managed.Launch(probe.Shell, label)
		return kube.StartExec(ctx, d.client, kube.ContainerRef{Container: execution}, []string{probe.Shell, "-c", script}, cols, rows)
	})
	a.installForeign(stream, "Docker "+c.Names, nil)
	a.Shells.SetRemote(managed, stream)
	return true
}
func (a *App) dockerContextItems(p *Panel) []menuItem {
	if items, handled := a.dockerObjectItems(p); handled {
		return items
	}
	c, _, _, ok := p.Docker.location(p.Path)
	if !ok {
		c, _, _, ok = p.Docker.location(p.Current())
	}
	if !ok {
		return nil
	}
	items := []menuItem{
		{"Live metrics…", "", func() {
			selected := []docker.Container{}
			for _, name := range p.Selected() {
				if item, _, _, found := p.Docker.location(name); found {
					selected = append(selected, item)
				}
			}
			if len(selected) == 0 {
				selected = []docker.Container{c}
			}
			a.dockerMetrics(p.Docker, selected, false)
		}},
		{"Traffic monitoring / faults…", "", func() { a.dockerTraffic(p, c) }},
		{"PodToast diagnostics", "", func() { a.dockerDiagnostics(p, c) }},
		{"Processes", "", func() { a.dockerOutput(p, "Processes", "top", c.ID, "-eo", "pid,ppid,user,%cpu,%mem,args") }},
		{"Logs", "", func() { a.dockerOutput(p, "Logs", "logs", "--tail", "2000", c.ID) }},
		{"Attach PodToast toolbox…", "", func() { a.attachDockerToast(p, c) }},
	}
	filteredItems := items[:0]
	for _, item := range items {
		if item.Label == "PodToast diagnostics" && p.Docker.helper(c.ID) == "" {
			continue
		}
		if item.Label == "Attach PodToast toolbox…" && p.Docker.helper(c.ID) != "" {
			continue
		}
		filteredItems = append(filteredItems, item)
	}
	items = filteredItems
	if c.State != "running" {
		filtered := []menuItem{}
		for _, item := range items {
			if item.Label == "Logs" {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	items = append(items,
		menuItem{"Filesystem changes", "", func() { a.dockerOutput(p, "Filesystem changes (A: added, C: changed, D: deleted)", "diff", c.ID) }},
		menuItem{"Published ports", "", func() { a.dockerOutput(p, "Published ports", "port", c.ID) }},
		menuItem{"Resource limits / restart policy…", "", func() { a.dockerUpdateContainer(p, c) }},
	)
	for _, verb := range dockerStateActions(c.State) {
		items = append(items, menuItem{dockerActionLabel(verb) + " container…", "", func() { a.dockerConfirm(p, verb+" "+c.Names, []string{verb, c.ID}) }})
	}
	if c.State == "running" || c.State == "paused" {
		items = append(items, menuItem{"Send signal…", "", func() { a.dockerSignal(p, c) }})
	}
	return items
}
func (a *App) attachDockerToast(p *Panel, c docker.Container) {
	a.prompt("Attach PodToast toolbox", "Image (shares target PID and network namespaces; adds SYS_PTRACE)", defaultPodToastImage, func(image string) {
		if strings.TrimSpace(image) == "" {
			return
		}
		d := p.Docker
		a.backgroundTask("Attaching PodToast toolbox", func(o *fsops.Operation) (func(), func(), error) {
			if err := d.refresh(o.Context()); err != nil {
				return nil, nil, err
			}
			if existing := d.helper(c.ID); existing != "" {
				return func() { a.Status = "Using existing PodToast toolbox" }, nil, nil
			}
			b, err := d.client.Run(o.Context(), "run", "-d", "--label", "io.farshmac.podtoast.target="+c.ID, "--pid=container:"+c.ID, "--network=container:"+c.ID, "--cap-add=SYS_PTRACE", image, "idle")
			if err != nil {
				return nil, nil, err
			}
			id := strings.TrimSpace(string(b))
			d.mu.Lock()
			d.helpers[c.ID] = id
			d.mu.Unlock()
			return func() { a.Status = "PodToast attached to " + c.Names; a.refresh() }, nil, nil
		})
	})
}

func (dockerBackend) Copy(a *App, p *Panel) bool {
	if !p.can(capEdit) {
		a.Status = "Enter a container filesystem before copying files"
		return true
	}
	return false
}
func (dockerBackend) Mkdir(a *App, p *Panel) bool {
	if p.can(capMkdir) {
		a.remoteMkdir()
	}
	return true
}
func (dockerBackend) Delete(a *App, p *Panel, _ bool) bool {
	if p.can(capDelete) {
		a.remoteDelete()
	}
	return true
}

type dockerAgentFS struct {
	remotefs.FS
	agent *podtoast.Client
}

func (f dockerAgentFS) Close() error { return f.agent.Close() }
func (f dockerAgentFS) CopyFile(a, b, previous string) error {
	return f.FS.(interface {
		CopyFile(string, string, string) error
	}).CopyFile(a, b, previous)
}
func (o *dockerOpener) PathFrom(other remotefs.Opener, name string) (string, error) {
	target, ok := other.(*dockerOpener)
	if !ok || o.Identity() != target.Identity() {
		return "", fmt.Errorf("different Docker containers")
	}
	clean := path.Clean(name)
	if clean != target.prefix && !strings.HasPrefix(clean, target.prefix+"/") {
		return "", fmt.Errorf("outside container")
	}
	return path.Join(o.prefix, strings.TrimPrefix(clean, target.prefix)), nil
}
func (o *dockerOpener) CopyWithin(ctx context.Context, source string, other remotefs.Opener, destination, previous string) error {
	dst, err := o.PathFrom(other, destination)
	if err != nil {
		return err
	}
	if previous != "" {
		previous, err = o.PathFrom(other, previous)
		if err != nil {
			return err
		}
	}
	f, err := o.Open(ctx)
	if err != nil {
		return err
	}
	defer f.Close()
	prefix := f.(prefixFS)
	if previous != "" {
		previous = prefix.strip(previous)
	}
	copier, ok := prefix.FS.(interface {
		CopyFile(string, string, string) error
	})
	if !ok {
		return fmt.Errorf("internal Docker copy unavailable")
	}
	return copier.CopyFile(prefix.strip(source), prefix.strip(dst), previous)
}
func (a *App) attachDockerVolume(p *Panel, volume string) {
	a.prompt("Volume toolbox", "Image (mounts the selected volume read/write)", defaultPodToastImage, func(image string) {
		if strings.TrimSpace(image) == "" {
			return
		}
		d := p.Docker
		a.backgroundTask("Opening Docker volume", func(o *fsops.Operation) (func(), func(), error) {
			if _, err := d.client.Run(o.Context(), "volume", "inspect", volume); err != nil {
				return nil, nil, err
			}
			id := d.helper("volume:" + volume)
			if id == "" {
				b, err := d.client.Run(o.Context(), "run", "-d", "--network=none", "--label", "io.farshmac.podtoast.volume="+volume, "--mount", "type=volume,source="+volume+",target=/volume", image, "idle")
				if err != nil {
					return nil, nil, err
				}
				id = strings.TrimSpace(string(b))
				d.mu.Lock()
				d.helpers["volume:"+volume] = id
				d.mu.Unlock()
			}
			return func() { a.error(p.Load("/Volumes/" + volume)) }, nil, nil
		})
	})
}

// Catalog rows are virtual, but every row still supplies the FileInfo contract
// used by sorting, status and alternative panel views.
type dockerCatalogInfo struct {
	name string
	dir  bool
}

func (i dockerCatalogInfo) Name() string { return i.name }
func (i dockerCatalogInfo) Size() int64  { return 0 }
func (i dockerCatalogInfo) Mode() os.FileMode {
	if i.dir {
		return os.ModeDir | 0555
	}
	return 0444
}
func (i dockerCatalogInfo) ModTime() time.Time { return time.Time{} }
func (i dockerCatalogInfo) IsDir() bool        { return i.dir }
func (i dockerCatalogInfo) Sys() any           { return nil }
func dockerCatalogEntry(name string, dir bool) Entry {
	return Entry{Name: name, Dir: dir, Info: dockerCatalogInfo{name, dir}}
}
