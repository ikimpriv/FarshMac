package kube

import (
	corev1 "k8s.io/api/core/v1"
	"path"
	"strings"
)

// StorageMount describes a pod-declared mount, not filesystem-wide usage.
type StorageMount struct {
	Path, Kind, Volume, Claim string
	ReadOnly                  bool
}

func (m StorageMount) Label() string {
	label := m.Kind
	if m.Claim != "" {
		label += " · PVC " + m.Claim
	} else if m.Volume != "" {
		label += " · " + m.Volume
	}
	if m.ReadOnly {
		label += " · read-only"
	}
	return label
}
func containerStorage(pod *corev1.Pod) map[string][]StorageMount {
	volumes := map[string]corev1.Volume{}
	for _, v := range pod.Spec.Volumes {
		volumes[v.Name] = v
	}
	result := map[string][]StorageMount{}
	add := func(name string, mounts []corev1.VolumeMount) {
		result[name] = []StorageMount{}
		for _, m := range mounts {
			v, ok := volumes[m.Name]
			if !ok {
				continue
			}
			kind, claim := "Mounted volume", ""
			switch {
			case v.PersistentVolumeClaim != nil:
				kind, claim = "Persistent volume", v.PersistentVolumeClaim.ClaimName
			case v.EmptyDir != nil:
				kind = "Ephemeral · emptyDir"
				if v.EmptyDir.Medium == corev1.StorageMediumMemory {
					kind = "Memory · emptyDir (tmpfs)"
				}
			case v.Ephemeral != nil:
				kind, claim = "Ephemeral PVC", pod.Name+"-"+v.Name
			case v.CSI != nil:
				kind = "Ephemeral CSI volume"
			case v.HostPath != nil:
				kind = "Host path"
			case v.Secret != nil:
				kind = "Secret"
			case v.ConfigMap != nil:
				kind = "ConfigMap"
			case v.Projected != nil:
				kind = "Projected volume"
			case v.DownwardAPI != nil:
				kind = "Downward API"
			case v.NFS != nil:
				kind = "NFS volume"
			}
			result[name] = append(result[name], StorageMount{Path: path.Clean("/" + m.MountPath), Kind: kind, Volume: m.Name, Claim: claim, ReadOnly: m.ReadOnly})
		}
	}
	for _, c := range pod.Spec.Containers {
		add(c.Name, c.VolumeMounts)
	}
	for _, c := range pod.Spec.InitContainers {
		add(c.Name, c.VolumeMounts)
	}
	for _, c := range pod.Spec.EphemeralContainers {
		add(c.Name, c.VolumeMounts)
	}
	return result
}
func StorageAt(mounts []StorageMount, filename string) (StorageMount, bool) {
	filename = path.Clean("/" + filename)
	var best StorageMount
	found := false
	for _, m := range mounts {
		if filename == m.Path || m.Path == "/" || strings.HasPrefix(filename, m.Path+"/") {
			if !found || len(m.Path) > len(best.Path) {
				best = m
				found = true
			}
		}
	}
	return best, found
}
