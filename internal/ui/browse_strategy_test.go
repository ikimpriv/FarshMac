package ui

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"farshmac/internal/fsops"
	"farshmac/internal/kube"
	"farshmac/internal/podtoast"
	"farshmac/internal/remotefs"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type fakeBrowse struct {
	entries  map[string][]Entry
	read     []string
	boundary string
}

func (b *fakeBrowse) ReadDir(path string, visit func(Entry) error) error {
	b.read = append(b.read, path)
	for _, e := range b.entries[path] {
		if err := visit(e); err != nil {
			return err
		}
	}
	return nil
}
func (b *fakeBrowse) Descend(path string, e Entry) bool { return e.Dir && path != b.boundary }
func (b *fakeBrowse) Open(string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("needle")), nil
}
func (b *fakeBrowse) Close() error { return nil }

func TestBrowseBoundariesBudgetsAndCancellation(t *testing.T) {
	fixture := func() *fakeBrowse {
		return &fakeBrowse{boundary: "/remote", entries: map[string][]Entry{"/": {{Name: "remote", Dir: true}, {Name: "local", Dir: true}}, "/remote": {{Name: "never"}}, "/local": {{Name: "found"}}}}
	}
	o := fsops.NewOperation("test")
	defer o.Cancel()
	b := fixture()
	var found []string
	stats, err := walkBrowse("/", b, o, browseOptions{Recursive: true}, func(path string, e Entry) error { found = append(found, path); return nil })
	if err != nil || stats.Boundaries != 1 || strings.Contains(strings.Join(b.read, ","), "/remote") || len(found) != 3 {
		t.Fatalf("unsafe traversal: %+v %v %v", stats, b.read, err)
	}
	b = fixture()
	stats, err = walkBrowse("/", b, o, browseOptions{Recursive: false}, func(string, Entry) error { return nil })
	if err != nil || len(b.read) != 1 {
		t.Fatal("nonrecursive search descended")
	}
	b = fixture()
	stats, err = walkBrowse("/", b, o, browseOptions{Recursive: true, MaxEntries: 1}, func(string, Entry) error { return nil })
	if err != nil || !stats.Limited {
		t.Fatal("limit not enforced")
	}
	o.Cancel()
	b = fixture()
	_, err = walkBrowse("/", b, o, browseOptions{}, func(string, Entry) error { return nil })
	if err == nil || len(b.read) != 0 {
		t.Fatal("canceled search read the source")
	}
}
func TestSearchAndTreeSkipNestedNetworkMount(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote")
	os.Mkdir(remote, 0700)
	os.WriteFile(filepath.Join(remote, "never"), nil, 0600)
	o := fsops.NewOperation("test")
	defer o.Cancel()
	b := &localBrowse{o: o, boundaries: map[string]bool{remote: true}}
	var found []string
	_, err := walkBrowse(root, b, o, browseOptions{Recursive: true}, func(path string, e Entry) error { found = append(found, path); return nil })
	if err != nil || len(found) != 1 || found[0] != remote {
		t.Fatalf("crossed network mount: %v %v", found, err)
	}
	opts := scanOptions()
	opts.browse = func(*fsops.Operation) (browseStrategy, error) { return b, nil }
	r := scanTree(root, opts)
	if r.err != nil || len(r.entries) != 1 || r.entries[0].Name != "remote" {
		t.Fatalf("tree crossed boundary: %+v", r)
	}
}
func TestKubeBrowseNeverResolvesPodChildren(t *testing.T) {
	o := fsops.NewOperation("test")
	defer o.Cancel()
	b := &kubeBrowse{o: o} // nil client: any resolve/network access would panic.
	pod := kubeObjectEntry(&kube.Object{Name: "app", GVR: schema.GroupVersionResource{Resource: "pods"}}, true)
	if b.Descend("/ns/pods/app", pod) {
		t.Fatal("pod marked traversable")
	}
	if err := b.ReadDir("/ns/deployments/app/replicasets/replica/pods/pod", func(Entry) error { t.Fatal("pod contents enumerated"); return nil }); err != nil {
		t.Fatal(err)
	}
	a, _ := kubeApp(t)
	source := a.Panels[0]
	source.Path = "/shop"
	strategy, err := source.browse(o)
	if err != nil {
		t.Fatal(err)
	}
	defer strategy.Close()
	pods := 0
	stats, err := walkBrowse(source.Path, strategy, o, browseOptions{Recursive: true, MaxEntries: 10000}, func(_ string, e Entry) error {
		if ke, ok := e.Info.(*kubeEntry); ok && isPodObject(ke.object) {
			pods++
		}
		return nil
	})
	if err != nil || pods == 0 || stats.Boundaries == 0 {
		t.Fatalf("catalog search failed: pods=%d stats=%+v err=%v", pods, stats, err)
	}
	source.Kube.mu.Lock()
	n := len(source.Kube.pods)
	source.Kube.mu.Unlock()
	if n != 0 {
		t.Fatal("search fetched pod summaries")
	}
}
func TestRemoteBrowseNetworkMountPolicy(t *testing.T) {
	b := &remoteBrowse{}
	info := podtoast.Entry{Filename: "data", FileMode: os.ModeDir, MountPoint: true, Mount: &podtoast.MountLocation{Filesystem: "nfs4"}}
	if b.Descend("/data", Entry{Name: "data", Dir: true, Info: info}) {
		t.Fatal("entered network mount")
	}
	info.Mount.Filesystem = "ext4"
	if !b.Descend("/data", Entry{Name: "data", Dir: true, Info: info}) {
		t.Fatal("blocked local mount")
	}
}
func TestBrowseResultLimitStopsImmediately(t *testing.T) {
	b := &fakeBrowse{entries: map[string][]Entry{"/": {{Name: "first"}, {Name: "second"}}}}
	o := fsops.NewOperation("test")
	defer o.Cancel()
	count := 0
	_, err := walkBrowse("/", b, o, browseOptions{}, func(string, Entry) error { count++; return fs.SkipAll })
	if err != nil || count != 1 {
		t.Fatal("result limit not respected")
	}
}

func TestRemoteBrowseContentAndArchiveNames(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "folder"), 0700)
	os.WriteFile(filepath.Join(root, "folder", "match.txt"), []byte("a needle here"), 0600)
	o := fsops.NewOperation("remote")
	defer o.Cancel()
	b := &remoteBrowse{FS: remotefs.Local{}, o: o}
	matches := 0
	_, err := walkBrowse(root, b, o, browseOptions{Recursive: true}, func(path string, e Entry) error {
		if e.Dir {
			return nil
		}
		r, err := b.Open(path)
		if err != nil {
			return err
		}
		defer r.Close()
		found, err := readerContains(r, "needle", true, o)
		if found {
			matches++
		}
		return err
	})
	if err != nil || matches != 1 {
		t.Fatalf("remote contents search: %d %v", matches, err)
	}
	idx := &fsops.ArchiveIndex{Path: filepath.Join(root, "test.zip"), Children: map[string][]string{".": {"folder"}, "folder": {"folder/match.txt"}}, Members: map[string]fsops.ArchiveMember{"folder": {Path: "folder", FileMode: os.ModeDir}, "folder/match.txt": {Path: "folder/match.txt"}}}
	archive := &archiveBrowse{index: idx, o: o}
	matches = 0
	_, err = walkBrowse(idx.Path, archive, o, browseOptions{Recursive: true}, func(_ string, e Entry) error {
		if e.Name == "match.txt" {
			matches++
		}
		return nil
	})
	if err != nil || matches != 1 {
		t.Fatalf("archive names search: %d %v", matches, err)
	}
}

func TestDockerBrowseDoesNotOpenContainerOrVolume(t *testing.T) {
	o := fsops.NewOperation("test")
	defer o.Cancel()
	b := &dockerBrowse{o: o}
	for _, path := range []string{"/Containers/app", "/Compose/project/app", "/Volumes/data"} {
		if b.Descend(path, Entry{Dir: true}) {
			t.Fatalf("crossed %s", path)
		}
		if err := b.ReadDir(path, func(Entry) error { t.Fatal("enumerated container or volume files"); return nil }); err != nil {
			t.Fatal(err)
		}
	}
}

func TestKubeCatalogCanonicalPaths(t *testing.T) {
	a, _ := kubeApp(t)
	source := a.Panels[0]
	source.Path = "/shop"
	o := fsops.NewOperation("search")
	defer o.Cancel()
	b, err := source.browse(o)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	identities := map[string]string{}
	pods := 0
	_, err = walkBrowse(source.Path, b, o, browseOptions{Recursive: true}, func(path string, e Entry) error {
		ke, ok := e.Info.(*kubeEntry)
		if !ok || ke.object == nil {
			return nil
		}
		obj := ke.object
		id := string(obj.UID)
		if previous, ok := identities[id]; ok {
			t.Fatalf("duplicate object: %s and %s", previous, path)
		}
		identities[id] = path
		if len(kubeSegments(path)) != 3 {
			t.Fatalf("secondary hierarchy: %s", path)
		}
		if isPodObject(obj) {
			pods++
		}
		return nil
	})
	if err != nil || pods == 0 {
		t.Fatalf("missing pods: %d %v", pods, err)
	}
}
