package kube

import (
	"encoding/json"
	"farshmac/internal/metrics"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
	"testing"
	"time"
)

func TestStorageMountClassification(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod"}}
	pod.Spec.Volumes = []corev1.Volume{
		{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "database"}}},
		{Name: "cache", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: "ram", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory}}},
	}
	pod.Spec.Containers = []corev1.Container{{Name: "app", VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/data"}, {Name: "cache", MountPath: "/data/cache"}, {Name: "ram", MountPath: "/memory"}}}}
	mounts := PodSummaryOf(pod).Storage["app"]
	for filename, want := range map[string]string{"/data/file": "Persistent volume", "/data/cache/log": "Ephemeral · emptyDir", "/memory/file": "Memory · emptyDir (tmpfs)"} {
		m, ok := StorageAt(mounts, filename)
		if !ok || m.Kind != want {
			t.Fatalf("%s: %+v", filename, m)
		}
	}
	if _, ok := StorageAt(mounts, "/database/file"); ok {
		t.Fatal("matched a sibling prefix")
	}
}
func TestStorageMetricsAccountingAndMissingValues(t *testing.T) {
	now := time.Now()
	stamp := now.Format(time.RFC3339Nano)
	data := `{"podRef":{"uid":"uid","name":"pod","namespace":"ns"},"ephemeral-storage":{"time":"` + stamp + `","usedBytes":999},"containers":[{"name":"app","rootfs":{"time":"` + stamp + `","usedBytes":100},"logs":{"time":"` + stamp + `","usedBytes":20}},{"name":"other","rootfs":{"time":"` + stamp + `","usedBytes":0},"logs":{"time":"` + stamp + `","usedBytes":0}},{"name":"partial","rootfs":{"time":"` + stamp + `","usedBytes":1}},{"name":"agent","rootfs":{"time":"` + stamp + `","usedBytes":10},"logs":{"time":"` + stamp + `","usedBytes":5}}],"volume":[{"name":"shared","time":"` + stamp + `","usedBytes":400}]}`
	var pod storagePod
	if err := json.Unmarshal([]byte(data), &pod); err != nil {
		t.Fatal(err)
	}
	body := formatStorageMetrics(pod, map[string]bool{"agent": true}, now)
	samples, err := metrics.Parse(body)
	if err != nil {
		t.Fatal(err, string(body))
	}
	found := map[string]float64{}
	for _, s := range samples {
		found[s.Name+"/"+s.Labels["container"]] = s.Value
	}
	if found["kubernetes_container_ephemeral_storage_used_bytes/app"] != 120 || found["kubernetes_pod_ephemeral_storage_used_bytes/"] != 999 {
		t.Fatal(string(body))
	}
	if _, ok := found["kubernetes_container_ephemeral_storage_used_bytes/partial"]; ok {
		t.Fatal("fabricated partial sum")
	}
	if _, ok := found["kubernetes_container_ephemeral_storage_used_bytes/other"]; !ok {
		t.Fatal("lost measured zero")
	}
	if strings.Contains(string(body), `container="agent"`) {
		t.Fatal("agent attributed to target")
	}
	if len(formatStorageMetrics(pod, nil, now.Add(10*time.Minute))) != 0 {
		t.Fatal("stale stats accepted")
	}
}
