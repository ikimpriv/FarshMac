package ui

import (
	"encoding/json"
	"encoding/xml"
	"farshmac/internal/docker"
	"farshmac/internal/kube"
	"farshmac/internal/kube/kubetest"
	"farshmac/internal/metrics"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

func TestQuickMetricsSourcesAndChart(t *testing.T) {
	d := func(id, name, container string) metrics.Descriptor {
		return metrics.Descriptor{ID: id, Name: name, Labels: map[string]string{"farshmac_namespace": "ns", "farshmac_pod": "pod", "farshmac_container": container}}
	}
	catalog := []metrics.Descriptor{d("a", "podtoast_cpu_usage_seconds_total", "app"), d("b", "kubernetes_container_cpu_usage_cores", "app"), d("c", "podtoast_cpu_usage_seconds_total", "worker")}
	ids := quickSeriesIDs(catalog)
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "c" {
		t.Fatal(ids)
	}
	v := 0.4
	c := &compactMetricsPanel{enabled: [3]bool{true, true, true}, packet: &metrics.Packet{Series: []metrics.Series{{Descriptor: catalog[0], Points: []metrics.Point{{Time: 100000, Value: &v}, {Time: 105000, Value: &v}}}}}}
	for _, theme := range []string{"Classic", "Dark", "Light"} {
		svg := quickMetricsSVG(c, theme, 480, 480)
		var root struct{ XMLName xml.Name }
		if err := xml.Unmarshal([]byte(svg), &root); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(svg, "<path") || strings.Contains(svg, "NaN") {
			t.Fatal(svg)
		}
	}
}
func TestQuickMetricsRefGrouping(t *testing.T) {
	refs, filters := quickMetricRefs([]kube.ContainerRef{{Namespace: "ns", Pod: "p", Container: "a"}, {Namespace: "ns", Pod: "p", Container: "b"}})
	if len(refs) != 1 || len(filters["ns/p"]) != 2 {
		t.Fatal(refs, filters)
	}
	_, filters = quickMetricRefs(append(refs, kube.ContainerRef{Namespace: "ns", Pod: "p", Container: "a"}))
	if len(filters) != 0 {
		t.Fatal(filters)
	}
}
func TestQuickMetricsHotkeyAndOverlay(t *testing.T) {
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	session := metrics.NewSession("fixture", nil, nil, nil)
	a.startCompactMetrics(session, "fixture")
	p := a.imagePreview()
	if p == nil || !p.Stretch || !strings.HasPrefix(p.Data, "data:image/svg+xml;base64,") {
		t.Fatal(p)
	}
	a.Form = &Form{Title: "Context menu", Choice: []string{"Action"}}
	a.dirty = true
	u := a.Poll()
	if u.Preview == nil || len(u.Preview.Occlusions) != 2 {
		t.Fatal("missing dialog and shadow occlusion", u.Preview)
	}
	if a.imagePreview() == nil {
		t.Fatal("dialog hides metrics")
	}
	a.Form = nil
	if !a.farPanelKey(Message{Key: "Q", Ctrl: true, Shift: true}) || a.compactMetrics != nil {
		t.Fatal("hotkey failed to restore panel")
	}
	select {
	case <-session.Done():
	case <-time.After(time.Second):
		t.Fatal("collector did not stop")
	}
}

func TestQuickMetricsVisualFixture(t *testing.T) {
	output := os.Getenv("FARSHMAC_QUICK_METRICS_PREVIEW")
	if output == "" {
		t.Skip("optional visual fixture")
	}
	c := &compactMetricsPanel{enabled: [3]bool{true, true, true}, packet: &metrics.Packet{}}
	for group, name := range []string{"docker_container_cpu_usage_cores", "container_memory_working_set_bytes", "container_ephemeral_storage_used_bytes"} {
		for j, container := range []string{"api", "worker"} {
			s := metrics.Series{Descriptor: metrics.Descriptor{ID: fmt.Sprint(group, j), Name: name, Kind: "gauge", Labels: map[string]string{"farshmac_pod": "studio", "farshmac_container": container}}}
			for i := 0; i < 120; i++ {
				v := 0.5 + float64(j)*0.25 + 0.15*math.Sin(float64(i)/8)
				if group > 0 {
					v *= float64((group + 1) * 256 << 20)
				}
				s.Points = append(s.Points, metrics.Point{Time: int64(i) * 2500, Value: &v})
			}
			c.packet.Series = append(c.packet.Series, s)
			if j == 0 {
				resource := []string{"cpu_cores", "memory_bytes", "ephemeral_storage_bytes"}[group]
				value := 1.0
				if group > 0 {
					value = float64((group + 1) * 512 << 20)
				}
				c.packet.References = append(c.packet.References, metrics.Reference{Descriptor: s.Descriptor, Resource: resource, Bound: "limit", Value: value}, metrics.Reference{Descriptor: s.Descriptor, Resource: resource, Bound: "request", Value: value / 2})
			}
		}
	}
	if strings.HasSuffix(output, ".json") {
		a, err := New(t.TempDir(), t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		a.Cols, a.Rows = 160, 44
		a.Settings.ColorTheme = os.Getenv("FARSHMAC_QUICK_METRICS_THEME")
		a.Panels[0].Path = "/Containers"
		a.Panels[0].Docker = &dockerPanel{client: docker.Client{Context: "demo"}}
		a.Panels[0].Entries = []Entry{dockerCatalogEntry("..", true), dockerCatalogEntry("api", true), dockerCatalogEntry("worker", true), dockerCatalogEntry("database", true)}
		a.Panels[0].Cursor = 1
		a.Panels[0].Marked = map[string]bool{"api": true, "worker": true}
		a.Panels[1].View = "Quick view"
		c.index = 1
		c.title = "Docker / demo · api + worker · DEMO DATA"
		if os.Getenv("FARSHMAC_QUICK_METRICS_SINGLE") == "1" {
			c.enabled = [3]bool{true, false, false}
		}
		a.compactMetrics = c
		a.Status = "Demo data · Ctrl+Shift+Q Quick Live Metrics · Tab then 1/2/3 toggles charts"
		u := Update{Mode: "panels", Grid: a.Draw().ANSI(), Cols: a.Cols, Rows: a.Rows, FontSize: 14, Preview: a.imagePreview()}
		a.compactMetrics = nil
		b, err := json.Marshal(u)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(output, b, 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := os.WriteFile(output, []byte(quickMetricsSVG(c, "Classic", 560, 640)), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestQuickMetricsMarkedContainers(t *testing.T) {
	a, api := kubeApp(t)
	defer a.Close()
	podWithContainers(api, "multi", "app", "worker")
	p := &a.Panels[0]
	p.Kube.invalidate("")
	a.error(p.Load("/shop/pods/multi"))
	waitNetwork(t, a)
	p.Marked = map[string]bool{"app": true, "worker": true}
	selectEntry(t, p, "..")
	refs := metricSelection(p)
	if len(refs) != 2 {
		t.Fatal(refs)
	}
	names := map[string]bool{}
	for _, r := range refs {
		names[r.Container] = true
	}
	if !names["app"] || !names["worker"] {
		t.Fatal(refs)
	}
}

func TestQuickMetricsReferenceScope(t *testing.T) {
	d := metrics.Descriptor{Name: "kubernetes_container_cpu_usage_cores", Labels: map[string]string{"farshmac_container": "app"}}
	v := 0.2
	panel := &compactMetricsPanel{enabled: [3]bool{true}, packet: &metrics.Packet{Series: []metrics.Series{{Descriptor: d, Points: []metrics.Point{{Time: 1000, Value: &v}}}}, References: []metrics.Reference{
		{Descriptor: d, Resource: "cpu_cores", Bound: "request", Value: 0.5},
		{Descriptor: d, Resource: "cpu_cores", Bound: "limit", Value: 2},
		{Descriptor: metrics.Descriptor{Labels: map[string]string{"farshmac_container": "other"}}, Resource: "cpu_cores", Bound: "limit", Value: 100},
	}}}
	svg := quickMetricsSVG(panel, "Classic", 600, 400)
	for _, want := range []string{"Request 0.5 cores", "Limit 2 cores", `stroke-dasharray="0.1 4"`, `stroke="#55dd77"`, `stroke="#ff6666"`, `stroke-opacity="0.18"`} {
		if !strings.Contains(svg, want) {
			t.Fatalf("missing %s", want)
		}
	}
	if strings.Count(svg, `class="time-tick"`) != 6 || !strings.Contains(svg, "−5 min") || !strings.Contains(svg, ">Latest</text>") {
		t.Fatal("missing labeled time scale")
	}
	if strings.Count(svg, `class="value-tick"`) != 5 || !strings.Contains(svg, `class="plot-frame"`) {
		t.Fatal("missing quarter-scale axis or frame")
	}
	panel.packet.Series = append(panel.packet.Series, metrics.Series{Descriptor: metrics.Descriptor{Name: d.Name, Labels: map[string]string{"farshmac_container": "second"}}})
	if strings.Contains(quickMetricsSVG(panel, "Classic", 600, 400), "stroke-dasharray") {
		t.Fatal("multi-container chart exposes bounds")
	}
	if strings.Contains(svg, "100 cores") {
		t.Fatal("unrelated container bound rendered")
	}
}

func TestQuickMemorySelection(t *testing.T) {
	names := []string{"podtoast_memory_working_set_bytes", "podtoast_memory_anonymous_bytes", "podtoast_memory_file_cache_bytes", "podtoast_memory_usage_bytes"}
	var catalog []metrics.Descriptor
	for _, name := range names {
		catalog = append(catalog, metrics.Descriptor{ID: name, Name: name, Target: "app"})
	}
	for mode, name := range names {
		ids := quickSeriesIDsForMemory(catalog, mode)
		if len(ids) != 1 || ids[0] != name {
			t.Fatalf("mode %d: %v", mode, ids)
		}
	}
	if ids := quickSeriesIDs(catalog[3:]); len(ids) != 0 {
		t.Fatal("total substituted for missing working set", ids)
	}
	catalog = append(catalog, metrics.Descriptor{ID: "fallback", Name: "kubernetes_container_memory_usage_bytes", Target: "app"})
	if ids := quickSeriesIDs(catalog[3:]); len(ids) != 1 || ids[0] != "fallback" {
		t.Fatal(ids)
	}
}

func TestQuickNodeCapacityAndDevices(t *testing.T) {
	labels := map[string]string{"farshmac_pod": "Node worker · pods"}
	d := metrics.Descriptor{Name: "kubernetes_container_cpu_usage_cores", Kind: "gauge", Labels: labels}
	value := 2.0
	panel := &compactMetricsPanel{enabled: [3]bool{true, true, false}, packet: &metrics.Packet{Series: []metrics.Series{{Descriptor: d, Points: []metrics.Point{{Time: time.Now().UnixMilli(), Value: &value}}}}}}
	panel.packet.References = []metrics.Reference{
		{Descriptor: metrics.Descriptor{Name: "farshmac_node_capacity_cpu_cores", Labels: labels}, Resource: "cpu_cores", Bound: "capacity", Value: 8},
		{Descriptor: metrics.Descriptor{Name: "farshmac_node_allocatable_cpu_cores", Labels: labels}, Resource: "cpu_cores", Bound: "allocatable", Value: 7},
	}
	deviceLabels := map[string]string{"farshmac_pod": "Node worker · pods", "device": "nvidia.com/gpu"}
	for _, r := range []struct {
		bound string
		value float64
	}{{"allocated", 0}, {"allocatable", 4}, {"capacity", 8}} {
		panel.packet.References = append(panel.packet.References, metrics.Reference{Descriptor: metrics.Descriptor{Labels: deviceLabels}, Resource: "device", Bound: r.bound, Value: r.value})
	}
	for _, theme := range []string{"Classic", "Dark", "Light"} {
		svg := quickMetricsSVG(panel, theme, 800, 640)
		for _, want := range []string{"28.6% allocatable", "Allocatable 7 cores", "nvidia.com/gpu", "0 / 4 allocated", "device-allocation"} {
			if !strings.Contains(svg, want) {
				t.Fatal(want, svg)
			}
		}
		var root struct{ XMLName xml.Name }
		if err := xml.Unmarshal([]byte(svg), &root); err != nil {
			t.Fatal(err)
		}
	}
}

func TestQuickNodeMetricsLaunch(t *testing.T) {
	a, f := kubeApp(t)
	f.Add("/api/v1/nodes", kubetest.Obj("Node", "", "worker", "worker-uid", nil, nil, nil))
	a.error(a.Panels[0].Load("/@cluster/nodes"))
	waitNetwork(t, a)
	selectEntry(t, &a.Panels[0], "worker")
	a.key(Message{Key: "q", Ctrl: true, Shift: true})
	waitWork(t, a)
	if a.compactMetrics == nil || a.compactMetrics.title != "Nodes · pod usage" {
		t.Fatal("node monitor did not open", a.Status)
	}
	if a.compactMetrics.enabled[2] {
		t.Fatal("storage must not masquerade as node usage")
	}
	packet := a.compactMetrics.session.Packet(true)
	if len(packet.Endpoints) != 2 {
		t.Fatal("usage and capacity must be independent", packet.Endpoints)
	}
}

func TestNodeAllocatableScale(t *testing.T) {
	for _, tc := range []struct {
		usage, allocatable, capacity, want float64
		bytes                              bool
	}{
		{3.12, 12, 12, 12, false},
		{42.8, 163.5, 163.6, 163.5, true},
		{13, 12, 16, 20, false},
	} {
		refs := []metrics.Reference{{Bound: "allocatable", Value: tc.allocatable}, {Bound: "capacity", Value: tc.capacity}}
		maximum, shown := quickMetricScale(tc.usage, refs, tc.bytes)
		if maximum != tc.want || len(shown) != 1 || shown[0].Bound != "allocatable" {
			t.Fatal(tc, maximum, shown)
		}
	}
}
