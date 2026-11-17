package metrics

import (
	"errors"
	"testing"
	"time"
)

func TestOverlappingPodToastMetrics(t *testing.T) {
	s := bareSession()
	for _, pair := range [][3]string{{"a", "pod", "app"}, {"b", "pod", "sidecar"}, {"c", "pod", "app"}, {"d", "other", "app"}} {
		s.targets = append(s.targets, Target{ID: pair[0], PodToast: true, SharedPID: true, Labels: map[string]string{"farshmac_namespace": "ns", "farshmac_pod": pair[1], "farshmac_container": pair[2]}})
	}
	s.status = nil
	for _, target := range s.targets {
		s.status = append(s.status, EndpointStatus{ID: target.ID, Enabled: true})
	}
	samples := []Sample{{Name: "podtoast_pod_network_receive_bytes_total", Kind: "counter", Value: 100}, {Name: "podtoast_cpu_usage_seconds_total", Kind: "counter", Value: 10}, {Name: "podtoast_visible_processes", Kind: "gauge", Value: 4}}
	now := time.Now()
	for i := range s.targets {
		s.ingest(i, samples, nil, now)
	}
	counts := map[string]int{}
	for _, ser := range s.series {
		counts[ser.Name]++
		if len(ser.Points) != 1 {
			t.Fatalf("duplicate observations: %+v", ser)
		}
		if ser.Name == samples[0].Name && ser.Labels["farshmac_container"] != "" {
			t.Fatal("pod network attributed to a container")
		}
	}
	if counts[samples[0].Name] != 2 || counts[samples[1].Name] != 3 || counts[samples[2].Name] != 2 {
		t.Fatal(counts)
	}
	s.ingest(0, nil, errors.New("offline"), now.Add(time.Second))
	s.ingest(1, samples, nil, now.Add(2*time.Second))
	key, sample := podToastSeries(s.targets[1], samples[0])
	ser := s.series[identity(key, sample)]
	if ser.source != "b" || ser.Points[len(ser.Points)-1].Rate != nil {
		t.Fatal("failover must keep one series and reset rate", ser)
	}
	s.ingest(0, samples, nil, now.Add(3*time.Second))
	if ser.source != "b" {
		t.Fatal("recovered duplicate stole the series")
	}
	s.Enable("b", false)
	s.ingest(2, samples, nil, now.Add(4*time.Second))
	if ser.source != "c" {
		t.Fatal("disabled producer prevented fallback")
	}
}

func TestPrivatePIDMetricsStaySeparate(t *testing.T) {
	a := Target{PodToast: true, Labels: map[string]string{"farshmac_namespace": "ns", "farshmac_pod": "pod", "farshmac_container": "a"}}
	b := a
	b.Labels = map[string]string{"farshmac_namespace": "ns", "farshmac_pod": "pod", "farshmac_container": "b"}
	for _, name := range []string{"podtoast_visible_processes", "podtoast_cpu_usage_seconds_total", "podtoast_filesystem_used_bytes"} {
		ak, as := podToastSeries(a, Sample{Name: name})
		bk, bs := podToastSeries(b, Sample{Name: name})
		if identity(ak, as) == identity(bk, bs) {
			t.Fatal("distinct target observations collapsed", name)
		}
	}
}
