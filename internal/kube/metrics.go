package kube

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"farshmac/internal/metrics"
	"farshmac/internal/podtoast"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type MetricsEndpoint struct {
	ID        string     `json:"id"`
	Owner     string     `json:"owner,omitempty"`
	Source    string     `json:"source"`
	Port      int        `json:"port"`
	Scheme    string     `json:"scheme"`
	Path      string     `json:"path"`
	Params    url.Values `json:"-"`
	Container string     `json:"container,omitempty"`
	Target    string     `json:"-"`
	RuntimeID string     `json:"-"`
	Shared    bool       `json:"-"`
	Error     string     `json:"error,omitempty"`
}
type MetricsDiscovery struct {
	ContainerCount int
	PodUID         string
	Endpoints      []MetricsEndpoint
	Warnings       []string
}

func endpointPort(pod *corev1.Pod, value any) int {
	switch v := value.(type) {
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		if n, e := strconv.Atoi(v); e == nil {
			return n
		}
		for _, c := range pod.Spec.Containers {
			for _, p := range c.Ports {
				if p.Name == v && p.Protocol != corev1.ProtocolUDP {
					return int(p.ContainerPort)
				}
			}
		}
	}
	return 0
}
func metricPath(p string) bool {
	u, e := url.Parse(p)
	return e == nil && strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") && u.Scheme == "" && u.Host == "" && u.RawQuery == "" && u.Fragment == "" && !strings.Contains(p, "..") && len(p) <= 1024
}

// DiscoverMetrics only enumerates the chosen pod. Probes never scan other hosts.
func (c *Client) DiscoverMetrics(ctx context.Context, ref ContainerRef) (MetricsDiscovery, error) {
	pod, err := c.Pod(ctx, ref.Namespace, ref.Pod)
	if err != nil {
		return MetricsDiscovery{}, err
	}
	out := MetricsDiscovery{PodUID: string(pod.UID), ContainerCount: len(pod.Spec.Containers)}
	for _, ctr := range pod.Spec.InitContainers {
		if ctr.RestartPolicy != nil && *ctr.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			out.ContainerCount++
		}
	}
	seen := map[string]int{}
	add := func(e MetricsEndpoint) {
		if len(out.Endpoints) >= 32 {
			return
		}
		if e.Container == "" && (e.Port < 1 || e.Port > 65535 || !metricPath(e.Path) || (e.Scheme != "http" && e.Scheme != "https")) {
			out.Warnings = append(out.Warnings, "Invalid endpoint in "+e.Source)
			return
		}
		if e.Container == "" {
			for _, ctr := range pod.Spec.Containers {
				for _, port := range ctr.Ports {
					if int(port.ContainerPort) == e.Port {
						if e.Owner != "" && e.Owner != ctr.Name {
							e.Owner = "pod"
						} else if e.Owner == "" {
							e.Owner = ctr.Name
						}
					}
				}
			}
			if e.Owner == "pod" {
				e.Owner = ""
			}
		} else {
			e.Owner = e.Target
		}
		e.ID = fmt.Sprintf("%s:%d%s?%s", e.Scheme, e.Port, e.Path, e.Params.Encode())
		if e.Container != "" {
			e.ID = "podtoast:" + e.Container + ":" + e.Target
		}
		if i, ok := seen[e.ID]; ok {
			out.Endpoints[i].Source += "; " + e.Source
			return
		}
		seen[e.ID] = len(out.Endpoints)
		out.Endpoints = append(out.Endpoints, e)
	}
	a := pod.Annotations
	if a["prometheus.io/scrape"] == "true" {
		port := endpointPort(pod, a["prometheus.io/port"])
		if port == 0 {
			out.Warnings = append(out.Warnings, "prometheus.io/scrape has no valid prometheus.io/port")
		}
		scheme := a["prometheus.io/scheme"]
		if scheme == "" {
			scheme = "http"
		}
		p := a["prometheus.io/path"]
		if p == "" {
			p = "/metrics"
		}
		if port != 0 {
			add(MetricsEndpoint{Port: port, Path: p, Scheme: scheme, Source: "pod annotations"})
		}
	}
	gvr := schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "podmonitors"}
	list, err := c.dyn.Resource(gvr).List(ctx, metav1.ListOptions{Limit: 500})
	if apierrors.IsForbidden(err) {
		out.Warnings = append(out.Warnings, "Cannot list PodMonitors across namespaces; checking this namespace only")
		list, err = c.dyn.Resource(gvr).Namespace(ref.Namespace).List(ctx, metav1.ListOptions{Limit: 500})
	}
	if err != nil && !apierrors.IsNotFound(err) {
		out.Warnings = append(out.Warnings, "PodMonitor discovery: "+err.Error())
	}
	if err == nil {
		if list.GetContinue() != "" {
			out.Warnings = append(out.Warnings, "PodMonitor discovery limited to first 500 resources")
		}
		for _, monitor := range list.Items {
			spec, _, _ := unstructured.NestedMap(monitor.Object, "spec")
			ns, _, _ := unstructured.NestedMap(spec, "namespaceSelector")
			anyNS, _ := ns["any"].(bool)
			matchNS, _, _ := unstructured.NestedStringSlice(ns, "matchNames")
			eligible := anyNS || len(matchNS) == 0 && monitor.GetNamespace() == ref.Namespace
			for _, n := range matchNS {
				eligible = eligible || n == ref.Namespace
			}
			if !eligible {
				continue
			}
			selector, exists, _ := unstructured.NestedMap(spec, "selector")
			if !exists {
				continue
			}
			var sel metav1.LabelSelector
			if runtime.DefaultUnstructuredConverter.FromUnstructured(selector, &sel) != nil {
				continue
			}
			matcher, e := metav1.LabelSelectorAsSelector(&sel)
			if e != nil || !matcher.Matches(klabels.Set(pod.Labels)) {
				continue
			}
			endpoints, _, _ := unstructured.NestedSlice(spec, "podMetricsEndpoints")
			for _, raw := range endpoints {
				m, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				source := "PodMonitor " + monitor.GetNamespace() + "/" + monitor.GetName()
				port := endpointPort(pod, m["port"])
				if port == 0 {
					port = endpointPort(pod, m["portNumber"])
				}
				if port == 0 {
					port = endpointPort(pod, m["targetPort"])
				}
				scheme, _ := m["scheme"].(string)
				if scheme == "" {
					scheme = "http"
				}
				p, _ := m["path"].(string)
				if p == "" {
					p = "/metrics"
				}
				ep := MetricsEndpoint{Port: port, Scheme: scheme, Path: p, Source: source, Params: url.Values{}}
				for _, key := range []string{"authorization", "basicAuth", "oauth2", "bearerTokenSecret", "tlsConfig", "relabelings"} {
					if v, exists := m[key]; exists && v != nil {
						ep.Error = "Requires PodMonitor " + key + " configuration; not applied by direct pod scraping"
					}
				}
				params, _, _ := unstructured.NestedMap(m, "params")
				for k, raw := range params {
					if vs, ok := raw.([]any); ok {
						for _, v := range vs {
							if text, ok := v.(string); ok {
								ep.Params.Add(k, text)
							}
						}
					}
				}
				add(ep)
			}
		}
	}
	// Named ports and exporter/metrics containers supply conservative candidates.
	for _, container := range pod.Spec.Containers {
		for _, p := range container.Ports {
			name := strings.ToLower(p.Name + " " + container.Name)
			if p.Protocol == corev1.ProtocolUDP || !(strings.Contains(name, "metric") || strings.Contains(name, "prometheus") || strings.Contains(name, "exporter")) {
				continue
			}
			for _, path := range []string{"/metrics", "/actuator/prometheus", "/q/metrics"} {
				add(MetricsEndpoint{Port: int(p.ContainerPort), Scheme: "http", Path: path, Source: "probe " + container.Name + "/" + p.Name})
			}
		}
	}
	// Only reuse an explicitly installed agent, never deploy one from discovery.
	for _, ec := range pod.Spec.EphemeralContainers {
		if !isPodToastCandidate(ec) {
			continue
		}
		running := false
		for _, st := range pod.Status.EphemeralContainerStatuses {
			if st.Name == ec.Name && st.State.Running != nil {
				running = true
			}
		}
		if !running {
			continue
		}
		shared := pod.Spec.ShareProcessNamespace != nil && *pod.Spec.ShareProcessNamespace
		for _, st := range pod.Status.ContainerStatuses {
			if st.State.Running == nil || (!podToastSharedManagement(pod, ec) && st.Name != ec.TargetContainerName) {
				continue
			}
			preferred, ok := RunningPodToast(pod, st.Name)
			if !ok || preferred.Name != ec.Name {
				continue
			}
			_, cid, _ := strings.Cut(st.ContainerID, "://")
			if cid != "" && !pod.Spec.HostPID && !pod.Spec.HostNetwork {
				add(MetricsEndpoint{Container: ec.Name, Target: st.Name, RuntimeID: cid, Shared: shared, Source: "Container / " + st.Name, Path: "/metrics"})
			}
		}
	}
	sort.SliceStable(out.Endpoints, func(i, j int) bool { return out.Endpoints[i].Source < out.Endpoints[j].Source })
	return out, nil
}

// MetricsReader owns its clients independently of panels. Close cancels streams.
type MetricsReader struct {
	Client *Client
	Ref    ContainerRef
	UID    string
	mu     sync.Mutex
	toasts map[string]*podtoast.Client
}

func (r *MetricsReader) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.toasts {
		c.Close()
	}
	r.toasts = nil
	r.Client.Close()
}
func (r *MetricsReader) Scrape(ctx context.Context, ep MetricsEndpoint) ([]byte, error) {
	if ep.Error != "" {
		return nil, fmt.Errorf("%s", ep.Error)
	}
	pod, err := r.Client.Pod(ctx, r.Ref.Namespace, r.Ref.Pod)
	if err != nil {
		return nil, err
	}
	if string(pod.UID) != r.UID {
		return nil, fmt.Errorf("pod was replaced; rediscover endpoints")
	}
	if ep.Container != "" {
		r.mu.Lock()
		client := r.toasts[ep.ID]
		r.mu.Unlock()
		preferred, ok := RunningPodToast(pod, ep.Target)
		if !ok {
			return nil, ErrNoPodToast
		}
		if client == nil || !client.Healthy() || client.AgentName != preferred.Name {
			ref := r.Ref
			ref.Container = ep.Target
			client, err = r.Client.connectPodToast(ctx, ref, preferred.Name, ep.RuntimeID, ep.Shared)
			if err != nil {
				return nil, err
			}
			client.AgentName = preferred.Name
			client.ImageRef = preferred.Image
			r.mu.Lock()
			if r.toasts == nil {
				r.toasts = map[string]*podtoast.Client{}
			}
			if old := r.toasts[ep.ID]; old != nil {
				old.Drain()
			}
			r.toasts[ep.ID] = client
			r.mu.Unlock()
		}
		return client.Metrics(ctx)
	}
	// The API server proxies only this pod/port. No cluster credentials go to the exporter.
	name := fmt.Sprintf("%s:%s:%d", ep.Scheme, r.Ref.Pod, ep.Port)
	req := r.Client.raw.Get().AbsPath("/api/v1/namespaces/"+url.PathEscape(r.Ref.Namespace)+"/pods/"+url.PathEscape(name)+"/proxy"+ep.Path).SetHeader("Accept", "text/plain; version=0.0.4")
	for k, vs := range ep.Params {
		for _, v := range vs {
			req.Param(k, v)
		}
	}
	stream, err := req.Stream(ctx)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	b, err := io.ReadAll(io.LimitReader(stream, metrics.MaxBody+1))
	if err != nil {
		return nil, err
	}
	if len(b) > metrics.MaxBody {
		return nil, fmt.Errorf("metrics exceed 2 MiB")
	}
	return b, nil
}

// History is read after the initial scrape establishes the target-bound agent.
func (r *MetricsReader) History(ctx context.Context, ep MetricsEndpoint) ([]metrics.HistoryFrame, error) {
	r.mu.Lock()
	client := r.toasts[ep.ID]
	r.mu.Unlock()
	if client == nil {
		return nil, nil
	}
	history, err := client.MetricsHistory(ctx)
	if err != nil {
		return nil, err
	}
	frames := make([]metrics.HistoryFrame, 0, len(history.Frames))
	for _, f := range history.Frames {
		frames = append(frames, metrics.HistoryFrame{At: time.UnixMilli(f.At), Body: []byte(f.Body)})
	}
	return frames, nil
}
