package ui

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"farshmac/internal/fsops"
	"farshmac/internal/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Copying into a Kubernetes panel applies manifests to the namespace shown
// there (spec 5.3, phase 3): local YAML/JSON files and folders of them
// (recursively, as an export produced them), or objects selected in another
// Kubernetes panel, whose manifests are exported clean. Every object gets
// the target namespace, whatever its manifest says, so a set exported from
// one namespace lands in another unchanged otherwise.

// manifestFile is one local file to apply, named for messages.
type manifestFile struct{ Name, Path string }

// kubeApplyTarget reports whether target accepts manifests and in which namespace.
func kubeApplyTarget(target *Panel) (ns string, ok bool) {
	if target.Kube == nil {
		return "", false
	}
	level := target.kubeLevel()
	switch level.Level {
	case kubeNamespace, kubeTypeFolder, kubeObject:
		return level.Namespace, level.Namespace != ""
	}
	return "", false
}

// localManifests lists the YAML/JSON files of the selection, walking folders.
func localManifests(paths []string) ([]manifestFile, error) {
	var files []manifestFile
	isManifest := func(name string) bool {
		switch strings.ToLower(filepath.Ext(name)) {
		case ".yaml", ".yml", ".json":
			return true
		}
		return false
	}
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			files = append(files, manifestFile{Name: filepath.Base(p), Path: p})
			continue
		}
		root := p
		err = filepath.WalkDir(root, func(fp string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !isManifest(d.Name()) {
				return nil
			}
			rel, _ := filepath.Rel(filepath.Dir(root), fp)
			files = append(files, manifestFile{Name: filepath.ToSlash(rel), Path: fp})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}

// kubeApplyDialog handles F5 with a Kubernetes namespace as the destination:
// confirms, then applies every manifest of the selection there.
func (a *App) kubeApplyDialog(source, target *Panel) bool {
	ns, ok := kubeApplyTarget(target)
	if !ok {
		a.Status = "Choose a namespace (or a folder inside one) in the cluster panel to apply manifests"
		return true
	}
	k := target.Kube
	var files []manifestFile
	var objects []exportItem
	var err error
	label := ""
	switch {
	case source.Kube != nil:
		objects, err = a.kubeExportPlan(source, source.kubeLevel())
		label = fmt.Sprintf("Apply %d object(s)", len(objects))
	case source.kind() == backendLocal:
		files, err = localManifests(source.Selected())
		label = fmt.Sprintf("Apply %d manifest file(s)", len(files))
	default:
		a.Status = "Manifests are applied from a local folder or another cluster panel"
		return true
	}
	if err != nil {
		a.error(err)
		return true
	}
	if len(files)+len(objects) == 0 {
		a.Status = "No manifests selected (YAML or JSON files, or folders of them)"
		return true
	}
	sourceClient := (*kube.Client)(nil)
	if source.Kube != nil {
		sourceClient = source.Kube.client
	}
	a.form(label+" to "+k.label()+" / "+ns, []Field{
		{Label: "Namespace", Value: ns},
		{Label: "Force fields owned by other managers (kubectl, controllers)", Toggle: true, Checked: false},
	}, func(fields []Field) {
		target := strings.TrimSpace(fields[0].Value)
		if target == "" {
			a.error(fmt.Errorf("enter a namespace"))
			return
		}
		force := fields[1].Checked
		a.workProgress(label, func(o *fsops.Operation) error {
			applied := 0
			apply := func(name string, obj *unstructured.Unstructured) error {
				return o.Attempt("apply", name, "", func() error {
					if obj.GetName() == "" {
						return fmt.Errorf("%s: the manifest has no name", name)
					}
					if _, err := k.client.ApplyWith(o.Context(), obj, target, force); err != nil {
						return fmt.Errorf("%s: %w", name, err)
					}
					applied++
					return nil
				})
			}
			for _, f := range files {
				if err := o.Check(); err != nil {
					return err
				}
				o.Add(f.Name, 0)
				data, err := os.ReadFile(f.Path)
				if err != nil {
					return err
				}
				docs, err := kube.Decode(data)
				if err != nil {
					return fmt.Errorf("%s: %w", f.Name, err)
				}
				for i, obj := range docs {
					name := f.Name
					if len(docs) > 1 {
						name = fmt.Sprintf("%s (document %d)", f.Name, i+1)
					}
					if err := fsops.IgnoreSkipped(apply(name, obj)); err != nil {
						return err
					}
				}
				o.Complete(f.Name)
			}
			for _, it := range objects {
				if err := o.Check(); err != nil {
					return err
				}
				name := path.Join(it.obj.Namespace, it.file)
				o.Add(name, 0)
				live, err := sourceClient.Get(o.Context(), it.obj.Ref())
				if err != nil {
					return fmt.Errorf("%s: %w", name, err)
				}
				clean, err := kube.ExportYAMLMode(live, kube.ExportClean)
				if err != nil {
					return err
				}
				docs, err := kube.Decode(clean)
				if err != nil || len(docs) != 1 {
					return fmt.Errorf("%s: cannot re-read the exported manifest", name)
				}
				if err := fsops.IgnoreSkipped(apply(name, docs[0])); err != nil {
					return err
				}
				o.Complete(name)
			}
			k.noteChanged(target)
			if applied == 0 {
				return fmt.Errorf("nothing applied")
			}
			return nil
		})
	})
	a.Form.Primary = "Apply"
	return true
}
