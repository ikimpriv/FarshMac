package ui

import (
	"context"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"farshmac/internal/kube"
	"farshmac/internal/remotefs"
)

// prefixFS maps the panel's virtual paths (/ns/…/pods/pod/ctr/etc) to paths
// inside the container (/etc), so transfers, deletion and documents work on
// virtual paths unchanged.
type prefixFS struct {
	remotefs.FS
	prefix string
}

func (f prefixFS) strip(p string) string {
	rel := strings.TrimPrefix(path.Clean(p), f.prefix)
	if rel == "" {
		return "/"
	}
	return rel
}
func (f prefixFS) ReadDir(p string) ([]os.FileInfo, error) { return f.FS.ReadDir(f.strip(p)) }
func (f prefixFS) Lstat(p string) (os.FileInfo, error)     { return f.FS.Lstat(f.strip(p)) }
func (f prefixFS) Stat(p string) (os.FileInfo, error)      { return f.FS.Stat(f.strip(p)) }
func (f prefixFS) Open(p string) (io.ReadCloser, error)    { return f.FS.Open(f.strip(p)) }
func (f prefixFS) Create(p string) (io.WriteCloser, error) { return f.FS.Create(f.strip(p)) }
func (f prefixFS) Mkdir(p string) error                    { return f.FS.Mkdir(f.strip(p)) }
func (f prefixFS) Remove(p string, dir bool) error         { return f.FS.Remove(f.strip(p), dir) }
func (f prefixFS) Rename(a, b string) error                { return f.FS.Rename(f.strip(a), f.strip(b)) }
func (f prefixFS) Chmod(p string, m os.FileMode) error     { return f.FS.Chmod(f.strip(p), m) }
func (f prefixFS) Chtimes(p string, t time.Time) error     { return f.FS.Chtimes(f.strip(p), t) }
func (f prefixFS) Readlink(p string) (string, error)       { return f.FS.Readlink(f.strip(p)) }
func (f prefixFS) Symlink(target, p string) error          { return f.FS.Symlink(target, f.strip(p)) }

// containerOpener connects to one container's file system (G3). The probe
// (shell, tar, lister) is cached per container for the panel's lifetime.
type containerOpener struct {
	k      *kubePanel
	ref    kube.ContainerRef
	prefix string
}

func (c *containerOpener) Open(ctx context.Context) (remotefs.FS, error) {
	probe, err := c.k.probe(ctx, c.ref)
	if err != nil {
		return nil, err
	}
	fs, err := kube.OpenContainerFS(ctx, c.k.exec(), c.ref, probe)
	if err != nil {
		return nil, err
	}
	return prefixFS{FS: fs, prefix: c.prefix}, nil
}
func (c *containerOpener) Identity() string {
	return c.k.label() + "/" + c.ref.Namespace + "/" + c.ref.Pod + "/" + c.ref.Container
}

func (k *kubePanel) exec() kube.Executor {
	if k.executor != nil {
		return k.executor
	}
	return k.client.Executor()
}
func (k *kubePanel) probe(ctx context.Context, ref kube.ContainerRef) (kube.Probe, error) {
	key := ref.Namespace + "/" + ref.Pod + "/" + ref.Container
	k.mu.Lock()
	cached, ok := k.probes[key]
	k.mu.Unlock()
	if ok {
		return cached, nil
	}
	probe, err := kube.ProbeContainer(ctx, k.exec(), ref)
	if err != nil {
		return kube.Probe{}, err
	}
	k.mu.Lock()
	k.probes[key] = probe
	k.mu.Unlock()
	return probe, nil
}

// container is the opener of the container a resolved path points into.
func (r kubePath) container(k *kubePanel) *containerOpener {
	return &containerOpener{k: k, ref: kube.ContainerRef{Namespace: r.Namespace, Pod: r.Object.Name, Container: r.Container}, prefix: r.Prefix}
}

// containerName strips the " (init)" / " (ephemeral)" marker of a containers
// listing entry.
func containerName(entry string) string {
	name, _, _ := strings.Cut(entry, " (")
	return name
}
