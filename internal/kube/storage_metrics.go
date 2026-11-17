package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

type storageStats struct {
	Time time.Time `json:"time"`
	Used *uint64   `json:"usedBytes"`
}
type storageContainer struct {
	Name string       `json:"name"`
	Root storageStats `json:"rootfs"`
	Logs storageStats `json:"logs"`
}
type storagePod struct {
	Ref        struct{ Name, Namespace, UID string } `json:"podRef"`
	Ephemeral  storageStats                          `json:"ephemeral-storage"`
	Containers []storageContainer                    `json:"containers"`
	Volumes    []struct {
		Name string `json:"name"`
		storageStats
	} `json:"volume"`
}

// StorageMetrics uses kubelet accounting. statfs of an overlay mount is NOT
// container usage, and metrics-server does not report ephemeral storage.
func (c *Client) StorageMetrics(ctx context.Context, ref ContainerRef, uid string) ([]byte, error) {
	c.storageCacheMu.Lock()
	defer c.storageCacheMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := ref.Namespace + "/" + ref.Pod + "/" + uid
	if key == c.storageCacheKey && time.Since(c.storageCacheAt) < 10*time.Second {
		return c.storageCacheData, c.storageCacheErr
	}
	data, err := c.fetchStorageMetrics(ctx, ref, uid)
	if ctx.Err() == nil {
		c.storageCacheKey, c.storageCacheData, c.storageCacheErr, c.storageCacheAt = key, data, err, time.Now()
	}
	return data, err
}
func (c *Client) fetchStorageMetrics(ctx context.Context, ref ContainerRef, uid string) ([]byte, error) {
	pod, err := c.Pod(ctx, ref.Namespace, ref.Pod)
	if err != nil {
		return nil, err
	}
	if string(pod.UID) != uid {
		return nil, fmt.Errorf("pod was replaced")
	}
	if pod.Spec.NodeName == "" {
		return nil, fmt.Errorf("pod is not scheduled")
	}
	stream, err := c.core.CoreV1().RESTClient().Get().Resource("nodes").Name(pod.Spec.NodeName).SubResource("proxy").Suffix("stats", "summary").Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("ephemeral storage requires kubelet stats via nodes/proxy: %w", err)
	}
	defer stream.Close()
	var summary struct {
		Pods []storagePod `json:"pods"`
	}
	if err = json.NewDecoder(io.LimitReader(stream, 16<<20)).Decode(&summary); err != nil {
		return nil, fmt.Errorf("kubelet storage summary: %w", err)
	}
	agents := map[string]bool{}
	for _, ec := range pod.Spec.EphemeralContainers {
		if isPodToastCandidate(ec) {
			agents[ec.Name] = true
		}
	}
	for _, p := range summary.Pods {
		if p.Ref.UID == uid && p.Ref.Name == ref.Pod && p.Ref.Namespace == ref.Namespace {
			data := formatStorageMetrics(p, agents, time.Now())
			if len(data) == 0 {
				return nil, fmt.Errorf("kubelet storage statistics are missing or stale")
			}
			return data, nil
		}
	}
	return nil, fmt.Errorf("kubelet has no storage statistics for this pod UID")
}
func formatStorageMetrics(p storagePod, agents map[string]bool, now time.Time) []byte {
	var b strings.Builder
	declared := map[string]bool{}
	valid := func(s storageStats) bool {
		return s.Used != nil && !s.Time.IsZero() && now.Sub(s.Time) <= 5*time.Minute && s.Time.Sub(now) <= time.Minute
	}
	write := func(name, labels string, n uint64) {
		if !declared[name] {
			fmt.Fprintf(&b, "# TYPE %s gauge\n", name)
			declared[name] = true
		}
		fmt.Fprintf(&b, "%s{%s} %d\n", name, labels, n)
	}
	if valid(p.Ephemeral) {
		write("kubernetes_pod_ephemeral_storage_used_bytes", `scope="pod"`, *p.Ephemeral.Used)
	}
	for _, c := range p.Containers {
		if agents[c.Name] {
			continue
		}
		labels := fmt.Sprintf("container=%q", c.Name)
		if valid(c.Root) {
			write("kubernetes_container_writable_layer_used_bytes", labels, *c.Root.Used)
		}
		if valid(c.Logs) {
			write("kubernetes_container_logs_used_bytes", labels, *c.Logs.Used)
		}
		if valid(c.Root) && valid(c.Logs) {
			write("kubernetes_container_ephemeral_storage_used_bytes", labels, *c.Root.Used+*c.Logs.Used)
		}
	}
	for _, v := range p.Volumes {
		if valid(v.storageStats) {
			write("kubernetes_pod_volume_used_bytes", fmt.Sprintf("volume=%q,scope=\"pod\"", v.Name), *v.Used)
		}
	}
	return []byte(b.String())
}
