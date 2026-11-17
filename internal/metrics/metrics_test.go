package metrics

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func TestParserMetricTypes(t *testing.T) {
	body := `# HELP requests Requests handled
# TYPE requests counter
requests_total{route="/a",note="a\\b\"c"} 12 # {trace_id="abc"} 1
# TYPE temperature gauge
temperature 23
# TYPE latency histogram
latency_bucket{le="0.5"} 2
latency_bucket{le="1"} 4
latency_bucket{le="+Inf"} 5
latency_sum 3
latency_count 5
# TYPE duration summary
duration{quantile="0.95"} 0.8
duration_sum 4
duration_count 10
# EOF
`
	samples, err := Parse([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, s := range samples {
		kinds[s.Name] = s.Kind
	}
	for name, kind := range map[string]string{"requests_total": "counter", "temperature": "gauge", "latency_bucket": "histogram bucket", "latency_count": "counter", "duration": "summary quantile"} {
		if kinds[name] != kind {
			t.Fatalf("%s: %s", name, kinds[name])
		}
	}
	a := Sample{Name: "x", Labels: map[string]string{"a": "1", "b": "2"}}
	b := Sample{Name: "x", Labels: map[string]string{"b": "2", "a": "1"}}
	if a.Key() != b.Key() {
		t.Fatal("unstable labels")
	}
	if _, err = Parse([]byte("<html>no metrics</html>")); err == nil {
		t.Fatal("accepted HTML")
	}
	if _, err = Parse([]byte(strings.Repeat("x", MaxBody+1))); err == nil {
		t.Fatal("unbounded body")
	}
}
func bareSession() *Session {
	return &Session{series: map[string]*Series{}, selected: map[string]bool{}, interval: 2 * time.Second, status: []EndpointStatus{{ID: "ep", Enabled: true}}}
}
func TestRatesResetsGapsAndBounds(t *testing.T) {
	s := bareSession()
	now := time.Now()
	sample := Sample{Name: "requests", Kind: "counter", Value: 10}
	s.ingest(0, []Sample{sample}, nil, now)
	sample.Value = 14
	s.ingest(0, []Sample{sample}, nil, now.Add(2*time.Second))
	ser := s.series[identity("ep", sample)]
	if *ser.Points[1].Rate != 2 || ser.Points[1].Delta == nil || *ser.Points[1].Delta != 4 {
		t.Fatal(ser.Points)
	}
	sample.Value = 3
	s.ingest(0, []Sample{sample}, nil, now.Add(4*time.Second))
	if *ser.Points[2].Rate != 1.5 || *ser.Points[2].Delta != 3 {
		t.Fatal("reset")
	}
	s.ingest(0, nil, errors.New("offline"), now.Add(6*time.Second))
	sample.Value = 7
	s.ingest(0, []Sample{sample}, nil, now.Add(8*time.Second))
	if ser.Points[4].Rate != nil || ser.Points[4].Delta != nil {
		t.Fatal("rate across gap")
	}
	sample.Value = math.NaN()
	s.ingest(0, []Sample{sample}, nil, now.Add(10*time.Second))
	if ser.Points[5].Value != nil {
		t.Fatal("NaN must be missing")
	}
	for i := 0; i < MaxPoints+20; i++ {
		s.ingest(0, []Sample{sample}, nil, now.Add(time.Duration(i+6)*2*time.Second))
	}
	if len(ser.Points) != MaxPoints {
		t.Fatal(len(ser.Points))
	}
}
func TestHistogramIntervalQuantiles(t *testing.T) {
	s := bareSession()
	samples := []Sample{{Name: "latency_bucket", Kind: "histogram bucket", Labels: map[string]string{"le": "1"}, Value: 0}, {Name: "latency_bucket", Kind: "histogram bucket", Labels: map[string]string{"le": "2"}, Value: 0}, {Name: "latency_bucket", Kind: "histogram bucket", Labels: map[string]string{"le": "+Inf"}, Value: 0}}
	now := time.Now()
	s.ingest(0, samples, nil, now)
	samples[0].Value = 5
	samples[1].Value = 10
	samples[2].Value = 10
	s.ingest(0, samples, nil, now.Add(2*time.Second))
	key := identity("ep", Sample{Name: "latency_quantile", Labels: map[string]string{"quantile": "0.5"}})
	ser := s.series[key]
	if ser == nil || ser.Points[1].Value == nil || *ser.Points[1].Value != 1 {
		t.Fatal(ser)
	}
}
func TestCollectorCancellationAndIndependentPacket(t *testing.T) {
	entered := make(chan struct{})
	clean := make(chan struct{})
	s := NewSession("pod", []Target{{ID: "ep", Name: "endpoint", Fetch: func(ctx context.Context) ([]byte, error) { close(entered); <-ctx.Done(); return nil, ctx.Err() }}}, nil, func() { close(clean) })
	<-entered
	s.Close()
	select {
	case <-clean:
	case <-time.After(time.Second):
		t.Fatal("scrape did not cancel")
	}
}

func TestPauseAndDisableDiscardInflightSamples(t *testing.T) {
	s := bareSession()
	now := time.Now()
	sample := Sample{Name: "requests", Kind: "counter", Value: 10}
	s.ingest(0, []Sample{sample}, nil, now)
	s.Pause(true)
	sample.Value = 12
	s.ingest(0, []Sample{sample}, nil, now.Add(time.Second))
	ser := s.series[identity("ep", sample)]
	if len(ser.Points) != 1 {
		t.Fatal("ingested paused request")
	}
	s.Pause(false)
	s.ingest(0, []Sample{sample}, nil, now.Add(2*time.Second))
	if ser.Points[1].Rate != nil {
		t.Fatal("rate bridged pause")
	}
	s.Enable("ep", false)
	s.ingest(0, []Sample{sample}, nil, now.Add(3*time.Second))
	if len(ser.Points) != 2 {
		t.Fatal("ingested disabled endpoint")
	}
}

func TestCollectorAttachesTargetScopeAndFiltersContainer(t *testing.T) {
	s := NewSession("scoped", []Target{{ID: "ns/pod/ep", Labels: map[string]string{"farshmac_namespace": "ns", "farshmac_pod": "pod"}, ContainerFilter: "app", Fetch: func(context.Context) ([]byte, error) {
		return []byte("cpu{container=\"app\"} 1\ncpu{container=\"other\"} 2\n"), nil
	}}}, nil, nil)
	defer s.Close()
	deadline := time.After(time.Second)
	for {
		packet := s.Packet(true)
		if len(packet.Catalog) > 0 {
			if len(packet.Catalog) != 1 || packet.Catalog[0].Labels["farshmac_container"] != "app" || packet.Catalog[0].Labels["farshmac_pod"] != "pod" {
				t.Fatal(packet.Catalog)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("no scoped samples")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestBasicContainerMetricsSurviveSeriesLimit(t *testing.T) {
	s := &Session{series: map[string]*Series{}, selected: map[string]bool{}, interval: time.Second}
	now := time.Now()
	for i := 0; i < MaxSeries; i++ {
		s.append("large", Sample{Name: "histogram_bucket", Labels: map[string]string{"bucket": fmt.Sprint(i)}, Value: 1}, now)
	}
	basic := s.append("podtoast", Sample{Name: "podtoast_cpu_usage_seconds_total", Kind: "counter", Value: 100}, now)
	if basic == nil || len(s.series) != MaxSeries {
		t.Fatal("CPU metric lost to high cardinality")
	}
	basic = s.append("podtoast", Sample{Name: "podtoast_cpu_usage_seconds_total", Kind: "counter", Value: 103}, now.Add(2*time.Second))
	if basic.Points[1].Rate == nil || *basic.Points[1].Rate != 1.5 {
		t.Fatal("CPU cores scaled incorrectly")
	}
}
