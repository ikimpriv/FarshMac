package ui

import (
	"farshmac/internal/kube"
	"farshmac/internal/podtoast"
	"os"
	"path"
)

type storageFileInfo struct {
	os.FileInfo
	label  string
	column string
	mount  bool
}

func classifyContainerEntries(files []Entry, directory string, mounts []kube.StorageMount, known bool) {
	for i := range files {
		e := &files[i]
		if e.Info == nil || e.Name == ".." {
			continue
		}
		filename := path.Join(directory, e.Name)
		label, column, mount := "Storage unknown", "Unknown", false
		declared, declaredHere := kube.StorageAt(mounts, filename)
		if known {
			label, column = "Container filesystem (pod spec)", "Container FS"
			if declaredHere {
				label, column = declared.Label(), storageColumn(declared)
				mount = path.Clean(filename) == declared.Path
			}
		}
		actual, haveActual := e.Info.(podtoast.Entry)
		if haveActual && actual.Mount != nil {
			// The manifest is sufficient for declared mounts, even with an
			// older agent or an incomplete runtime mount view.
			if observed, ok := kube.StorageAt(mounts, actual.Mount.Path); ok && observed.Path == actual.Mount.Path {
				label, column = observed.Label(), storageColumn(observed)
				mount = mount || actual.MountPoint
			} else if actual.Mount.Path != "/" && (!declaredHere || actual.MountPoint) {
				label, column = "Mounted filesystem · "+actual.Mount.Filesystem, "FS: "+actual.Mount.Filesystem
				mount = actual.MountPoint
			} else if !declaredHere && actual.Mount.Path == "/" {
				label, column = "Container filesystem · writable layer is ephemeral", "Container FS"
			}
		}

		if e.Info.Mode()&os.ModeSymlink != 0 {
			label += " · symlink location"
			mount = false
		}
		e.Info = storageFileInfo{FileInfo: e.Info, label: label, column: column, mount: mount}
	}
}

func storageColumn(m kube.StorageMount) string {
	kind := m.Kind
	switch kind {
	case "Persistent volume":
		kind = "PVC"
	case "Ephemeral · emptyDir":
		kind = "emptyDir"
	case "Memory · emptyDir (tmpfs)":
		kind = "emptyDir RAM"
	case "Ephemeral CSI volume":
		kind = "CSI ephemeral"
	case "Host path":
		kind = "hostPath"
	case "Projected volume":
		kind = "Projected"
	case "Downward API":
		kind = "downwardAPI"
	case "NFS volume":
		kind = "NFS"
	}
	name := m.Volume
	if m.Claim != "" {
		name = m.Claim
	}
	if name != "" {
		kind += ": " + name
	}
	if m.ReadOnly {
		kind += " (ro)"
	}
	return kind
}
