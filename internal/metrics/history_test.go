package metrics

import (
	"context"
	"testing"
	"time"
)

func TestHistorySeedsCounterRatesAndFilters(t *testing.T) {
	now := time.Now()
	s := NewSession("history", []Target{{ID: "agent", Labels: map[string]string{"farshmac_pod": "pod"}, ContainerFilters: []string{"app"}, Fetch: func(context.Context) ([]byte, error) {
		return []byte("# TYPE cpu_total counter\ncpu_total{container=\"app\"} 30\ncpu_total{container=\"other\"} 100\n"), nil
	}, History: func(context.Context) ([]HistoryFrame, error) {
		return []HistoryFrame{{At: now.Add(-10 * time.Second), Body: []byte("# TYPE cpu_total counter\ncpu_total{container=\"app\"} 10\n")}, {At: now.Add(-5 * time.Second), Body: []byte("# TYPE cpu_total counter\ncpu_total{container=\"app\"} 20\n")}}, nil
	}}}, nil, nil)
	defer s.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		if len(s.series) == 1 {
			for _, ser := range s.series {
				if len(ser.Points) >= 3 {
					if ser.Points[1].Rate == nil || *ser.Points[1].Rate != 2 {
						t.Fatal(ser.Points)
					}
					s.mu.Unlock()
					return
				}
			}
		}
		s.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatal("history not seeded")
}

func TestHistoryBackfillAfterLiveSample(t *testing.T) {
	now := time.Now()
	target := Target{ID: "agent"}
	s := &Session{targets: []Target{target}, status: []EndpointStatus{{ID: "agent", Enabled: true}}, series: map[string]*Series{}, selected: map[string]bool{}, interval: 2 * time.Second}
	s.ingest(0, []Sample{{Name: "cpu_total", Kind: "counter", Value: 30}}, nil, now)
	s.backfillHistory(0, target, []HistoryFrame{
		{At: now.Add(-10 * time.Second), Body: []byte("# TYPE cpu_total counter\ncpu_total 10\n")},
		{At: now.Add(-5 * time.Second), Body: []byte("# TYPE cpu_total counter\ncpu_total 20\n")},
	}, now)
	for _, series := range s.series {
		if len(series.Points) != 3 || series.Points[2].Rate == nil || *series.Points[2].Rate != 2 {
			t.Fatalf("%+v", series.Points)
		}
	}
}
