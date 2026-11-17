package metrics

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const MaxSeries = 1024
const MaxPoints = 300
const MaxSelected = 48

type HistoryFrame struct {
	At   time.Time
	Body []byte
}

type Target struct {
	ID, Name            string
	PodToast, SharedPID bool
	Labels              map[string]string
	ContainerFilter     string
	ContainerFilters    []string
	Fetch               func(context.Context) ([]byte, error)
	History             func(context.Context) ([]HistoryFrame, error)
}
type EndpointStatus struct {
	History  string `json:"history,omitempty"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Error    string `json:"error,omitempty"`
	Samples  int    `json:"samples"`
	At       int64  `json:"at"`
	Enabled  bool   `json:"enabled"`
	failures int
	next     time.Time
}
type Descriptor struct {
	ID     string            `json:"id"`
	Target string            `json:"target"`
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels,omitempty"`
	Kind   string            `json:"kind"`
	Help   string            `json:"help,omitempty"`
	Unit   string            `json:"unit,omitempty"`
}
type Point struct {
	Time  int64    `json:"t"`
	Value *float64 `json:"v"`
	Rate  *float64 `json:"r"`
	Delta *float64 `json:"d"`
}
type Series struct {
	source string
	Descriptor
	Points   []Point `json:"points"`
	previous float64
	at       time.Time
	seen     time.Time
	valid    bool
}
type Reference struct {
	Descriptor
	Resource string  `json:"resource"`
	Bound    string  `json:"bound"`
	Value    float64 `json:"value"`
}
type Packet struct {
	References []Reference      `json:"references"`
	Session    string           `json:"session"`
	Title      string           `json:"title"`
	Catalog    []Descriptor     `json:"catalog,omitempty"`
	Series     []Series         `json:"series"`
	Endpoints  []EndpointStatus `json:"endpoints"`
	Warnings   []string         `json:"warnings,omitempty"`
	Paused     bool             `json:"paused"`
	Interval   int              `json:"interval"`
	Limit      bool             `json:"limit"`
	Layout     string           `json:"layout,omitempty"`
	Detached   bool             `json:"detached"`
	Revision   uint64           `json:"revision"`
}
type Session struct {
	showReferences            bool
	id                        string
	mu                        sync.Mutex
	title                     string
	targets                   []Target
	status                    []EndpointStatus
	warnings                  []string
	series                    map[string]*Series
	selected                  map[string]bool
	interval                  time.Duration
	paused                    bool
	limit                     bool
	layout                    string
	revision, catalogRevision uint64
	sent, sentCatalog         uint64
	cancel                    context.CancelFunc
	cycleCancel               context.CancelFunc
	done                      chan struct{}
}

func NewSession(title string, targets []Target, warnings []string, cleanup func()) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{id: rand.Text(), title: title, targets: targets, warnings: warnings, series: map[string]*Series{}, selected: map[string]bool{}, interval: 2 * time.Second, cancel: cancel, done: make(chan struct{}), revision: 1, catalogRevision: 1}
	for _, t := range targets {
		s.status = append(s.status, EndpointStatus{ID: t.ID, Name: t.Name, Enabled: true})
	}
	go func() {
		defer close(s.done)
		if cleanup != nil {
			defer cleanup()
		}
		s.run(ctx)
	}()
	return s
}
func (s *Session) Close()                { s.cancel() }
func (s *Session) Done() <-chan struct{} { return s.done }
func (s *Session) Configure(keys []string, layout string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.selected = map[string]bool{}
	for _, id := range keys {
		if len(s.selected) < MaxSelected {
			s.selected[id] = true
		}
	}
	if len(layout) < 64<<10 {
		s.layout = layout
	}
	s.revision++
}
func (s *Session) Pause(paused bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = paused
	if paused {
		for _, ser := range s.series {
			ser.valid = false
		}
	}
	if paused && s.cycleCancel != nil {
		s.cycleCancel()
	}
	s.revision++
}
func (s *Session) Interval(seconds int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interval = time.Duration(max(2, min(60, seconds))) * time.Second
	s.revision++
}
func (s *Session) Enable(id string, enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.status {
		if s.status[i].ID == id {
			s.status[i].Enabled = enabled
			s.status[i].next = time.Time{}
			if !enabled {
				for _, ser := range s.series {
					if ser.Target == id || ser.source == id {
						ser.valid = false
					}
				}
			}
		}
	}
	s.revision++
}
func (s *Session) Force() { s.mu.Lock(); defer s.mu.Unlock(); s.sent = 0; s.sentCatalog = 0 }
func (s *Session) Packet(force bool) *Packet {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !force && s.sent == s.revision {
		return nil
	}
	p := &Packet{Session: s.id, Title: s.title, Endpoints: append([]EndpointStatus(nil), s.status...), Warnings: append([]string(nil), s.warnings...), Paused: s.paused, Interval: int(s.interval / time.Second), Limit: s.limit, Revision: s.revision, Series: []Series{}, Layout: s.layout}
	keys := make([]string, 0, len(s.series))
	for id := range s.series {
		keys = append(keys, id)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := s.series[keys[i]], s.series[keys[j]]
		if a.Name == b.Name {
			return keys[i] < keys[j]
		}
		return a.Name < b.Name
	})
	catalog := force || s.sentCatalog != s.catalogRevision
	for _, id := range keys {
		ser := s.series[id]
		if resource, bound := ReferenceKind(ser.Name); s.showReferences && resource != "" && len(ser.Points) > 0 {
			v := ser.Points[len(ser.Points)-1].Value
			if v != nil && *v > 0 && !math.IsInf(*v, 0) && !math.IsNaN(*v) && time.Since(ser.seen) < 30*time.Second {
				p.References = append(p.References, Reference{Descriptor: ser.Descriptor, Resource: resource, Bound: bound, Value: *v})
			}
		}
		if catalog {
			p.Catalog = append(p.Catalog, ser.Descriptor)
		}
		if s.selected[id] {
			copy := *ser
			copy.Points = append([]Point(nil), ser.Points...)
			p.Series = append(p.Series, copy)
		}
	}
	s.sent = s.revision
	s.sentCatalog = s.catalogRevision
	return p
}
func identity(target string, sample Sample) string {
	sum := sha256.Sum256([]byte(target + "\x00" + sample.Key()))
	return fmt.Sprintf("%x", sum[:16])
}

// Protect basic container telemetry from large histogram/cardinality endpoints.
func basicPriority(name string) int {
	if resource, _ := ReferenceKind(name); resource != "" {
		return 3
	}
	switch name {
	case "docker_container_cpu_usage_cores", "podtoast_cpu_usage_seconds_total", "podtoast_memory_usage_bytes", "podtoast_memory_working_set_bytes", "podtoast_memory_anonymous_bytes", "podtoast_memory_file_cache_bytes":
		return 3
	case "container_cpu_usage_seconds_total", "container_memory_working_set_bytes", "container_ephemeral_storage_used_bytes":
		return 2
	case "kubernetes_container_cpu_usage_cores", "kubernetes_container_memory_usage_bytes", "ephemeral_storage_used_bytes", "kubernetes_container_ephemeral_storage_used_bytes":
		return 1
	}
	return 0
}
func (s *Session) append(target string, sample Sample, now time.Time) *Series {
	id := identity(target, sample)
	ser := s.series[id]
	if ser == nil {
		if len(s.series) >= MaxSeries {
			s.limit = true
			victim := ""
			priority := basicPriority(sample.Name)
			for key, existing := range s.series {
				if !s.selected[key] && basicPriority(existing.Name) < priority {
					victim = key
					break
				}
			}
			if victim == "" {
				return nil
			}
			delete(s.series, victim)
			s.catalogRevision++
		}
		ser = &Series{Descriptor: Descriptor{ID: id, Target: target, Name: sample.Name, Labels: sample.Labels, Kind: sample.Kind, Help: sample.Help, Unit: sample.Unit}}
		s.series[id] = ser
		s.catalogRevision++
	}
	if !ser.at.IsZero() && !now.After(ser.at) {
		return nil
	}
	v := finite(sample.Value)
	p := Point{Time: now.UnixMilli(), Value: v}
	if v != nil && ser.valid && now.After(ser.at) && now.Sub(ser.at) <= 3*s.interval+5*time.Second {
		delta := sample.Value - ser.previous
		if sample.Kind == "counter" || sample.Kind == "histogram bucket" {
			if delta < 0 {
				delta = sample.Value
			}
		}
		p.Delta = finite(delta)
		p.Rate = finite(delta / now.Sub(ser.at).Seconds())
	}
	ser.valid = v != nil
	ser.previous = sample.Value
	ser.at = now
	ser.seen = now
	if len(ser.Points) == MaxPoints {
		copy(ser.Points, ser.Points[1:])
		ser.Points[len(ser.Points)-1] = p
	} else {
		ser.Points = append(ser.Points, p)
	}
	return ser
}
func (s *Session) ingest(index int, samples []Sample, err error, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := &s.status[index]
	if s.paused || !st.Enabled {
		return
	}
	st.At = now.UnixMilli()
	st.Samples = len(samples)
	target := st.ID
	if err != nil {
		st.Error = err.Error()
		if len(st.Error) > 1024 {
			st.Error = st.Error[:1024]
		}
		st.failures++
		st.next = now.Add(time.Duration(min(60, 5*st.failures)) * time.Second)
	} else {
		st.Error = ""
		st.failures = 0
		st.next = time.Time{}
	}
	present := map[string]bool{}
	type bucket struct{ bound, value float64 }
	type histogram struct {
		sample  Sample
		buckets []bucket
		valid   bool
	}
	groups := map[string]*histogram{}
	if err == nil {
		for _, sample := range samples {
			seriesTarget := target
			if index < len(s.targets) && s.targets[index].PodToast {
				seriesTarget, sample = podToastSeries(s.targets[index], sample)
				if old := s.series[identity(seriesTarget, sample)]; old != nil && old.source != target {
					// Keep one producer per logical measurement. Never sum overlapping
					// observations or derive rates across different producers.
					healthy := false
					for _, endpoint := range s.status {
						if endpoint.ID == old.source {
							healthy = endpoint.Enabled && endpoint.Error == "" && now.Sub(old.seen) <= 3*s.interval+5*time.Second
							break
						}
					}
					if healthy {
						continue
					}
					old.valid = false
				}
			}
			ser := s.append(seriesTarget, sample, now)
			if ser == nil {
				continue
			}
			ser.source = target
			present[ser.ID] = true
			if sample.Kind == "histogram bucket" {
				bound, e := strconv.ParseFloat(sample.Labels["le"], 64)
				if e != nil {
					continue
				}
				base := sample
				base.Name = strings.TrimSuffix(base.Name, "_bucket")
				base.Labels = map[string]string{}
				for k, v := range sample.Labels {
					if k != "le" {
						base.Labels[k] = v
					}
				}
				key := base.Key()
				g := groups[key]
				if g == nil {
					g = &histogram{sample: base, valid: true}
					groups[key] = g
				}
				p := ser.Points[len(ser.Points)-1]
				if p.Rate == nil {
					g.valid = false
				} else {
					g.buckets = append(g.buckets, bucket{bound, *p.Rate})
				}
			}
		}
		for _, g := range groups {
			sort.Slice(g.buckets, func(i, j int) bool { return g.buckets[i].bound < g.buckets[j].bound })
			for _, q := range []float64{.5, .95, .99} {
				value := math.NaN()
				bs := g.buckets
				if g.valid && len(bs) >= 2 && math.IsInf(bs[len(bs)-1].bound, 1) && bs[len(bs)-1].value > 0 {
					rank := q * bs[len(bs)-1].value
					previous, lower := 0.0, 0.0
					for i, b := range bs {
						if b.value < previous {
							break
						}
						if b.value >= rank {
							if math.IsInf(b.bound, 1) {
								value = bs[i-1].bound
							} else if b.value > previous {
								value = lower + (b.bound-lower)*(rank-previous)/(b.value-previous)
							}
							break
						}
						previous = b.value
						lower = b.bound
					}
				}
				sample := g.sample
				sample.Name += "_quantile"
				sample.Kind = "histogram quantile"
				sample.Labels = map[string]string{}
				for k, v := range g.sample.Labels {
					sample.Labels[k] = v
				}
				sample.Labels["quantile"] = strconv.FormatFloat(q, 'g', -1, 64)
				sample.Value = value
				sample.Help = "Estimated interval quantile from classic cumulative bucket rates. " + sample.Help
				if ser := s.append(target, sample, now); ser != nil {
					present[ser.ID] = true
				}
			}
		}
	}
	for id, ser := range s.series {
		if (ser.Target != target && ser.source != target) || present[id] {
			continue
		}
		ser.valid = false
		if !s.selected[id] && now.Sub(ser.seen) > 10*time.Minute {
			delete(s.series, id)
			s.catalogRevision++
			continue
		}
		p := Point{Time: now.UnixMilli()}
		if len(ser.Points) == MaxPoints {
			copy(ser.Points, ser.Points[1:])
			ser.Points[len(ser.Points)-1] = p
		} else {
			ser.Points = append(ser.Points, p)
		}
	}
	s.revision++
}
func (s *Session) run(ctx context.Context) {
	historyLoaded := make([]bool, len(s.targets))
	historyAttempts := make([]int, len(s.targets))
	for {
		if ctx.Err() != nil {
			return
		}
		cycle, cancel := context.WithCancel(ctx)
		s.mu.Lock()
		s.cycleCancel = cancel
		paused := s.paused
		interval := s.interval
		statuses := append([]EndpointStatus(nil), s.status...)
		s.mu.Unlock()
		if !paused {
			var wg sync.WaitGroup
			sem := make(chan struct{}, 4)
			for i, t := range s.targets {
				if !statuses[i].Enabled || time.Now().Before(statuses[i].next) {
					continue
				}
				select {
				case sem <- struct{}{}:
				case <-cycle.Done():
					break
				}
				if cycle.Err() != nil {
					break
				}
				wg.Add(1)
				go func(i int, t Target) {
					defer wg.Done()
					defer func() { <-sem }()
					request, stop := context.WithTimeout(cycle, 5*time.Second)
					defer stop()
					body, err := t.Fetch(request)
					fetchedAt := time.Now()
					var samples []Sample
					if err == nil {
						samples, err = Parse(body)
						samples = filterTargetSamples(t, samples)
					}
					if !historyLoaded[i] && t.History != nil && err == nil {
						historyAttempts[i]++
						history, e := t.History(request)
						if e == nil && len(history) > 0 {
							s.backfillHistory(i, t, history, fetchedAt)
							historyLoaded[i] = true
						} else if historyAttempts[i] >= 3 {
							historyLoaded[i] = true
						}
						s.mu.Lock()
						s.status[i].History = ""
						if e != nil {
							s.status[i].History = "History unavailable · " + e.Error()
						}
						if e == nil && len(history) == 0 {
							s.status[i].History = "No retained history yet"
						}
						s.mu.Unlock()
					}
					if cycle.Err() == nil {
						s.ingest(i, samples, err, fetchedAt)
					}
				}(i, t)
			}
			wg.Wait()
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// podToastSeries identifies the measured resource, not the agent exporting it.
// Filesystem totals lack a filesystem ID in older agents: retain them as
// per-target observations, never infer identity from coincidentally equal values.
func podToastSeries(t Target, sample Sample) (string, Sample) {
	labels := map[string]string{}
	for k, v := range sample.Labels {
		labels[k] = v
	}
	for k, v := range t.Labels {
		if v != "" {
			labels[k] = v
		}
	}
	scope := "container"
	if strings.HasPrefix(sample.Name, "podtoast_pod_network_") || sample.Name == "podtoast_network_available" {
		scope = "pod"
	}
	if t.SharedPID && (strings.HasPrefix(sample.Name, "podtoast_visible_process") || sample.Name == "podtoast_processes_available") {
		scope = "pod"
	}
	if scope == "pod" {
		delete(labels, "farshmac_container")
		delete(labels, "container")
	}
	labels["farshmac_scope"] = scope
	sample.Labels = labels
	return "podtoast/" + t.Labels["farshmac_namespace"] + "/" + t.Labels["farshmac_pod"] + "/" + scope + "/" + labels["farshmac_container"], sample
}

func filterTargetSamples(t Target, samples []Sample) []Sample {
	filtered := samples[:0]
	for _, sample := range samples {
		container := t.Labels["farshmac_container"]
		if container == "" {
			container = sample.Labels["container"]
		}
		allowed := len(t.ContainerFilters) == 0
		for _, name := range t.ContainerFilters {
			allowed = allowed || container == name
		}
		if !allowed || t.ContainerFilter != "" && container != t.ContainerFilter {
			continue
		}
		if sample.Labels == nil {
			sample.Labels = map[string]string{}
		}
		for k, v := range t.Labels {
			if v != "" {
				sample.Labels[k] = v
			}
		}
		if container != "" {
			sample.Labels["farshmac_container"] = container
		}
		filtered = append(filtered, sample)
	}
	return filtered
}

// ReferenceKind recognizes only explicit container budgets, never filesystem capacity.
func ReferenceKind(name string) (string, string) {
	for _, bound := range []string{"request", "limit"} {
		for _, resource := range []string{"cpu_cores", "memory_bytes", "ephemeral_storage_bytes"} {
			if name == "farshmac_container_"+bound+"_"+resource {
				return resource, bound
			}
		}
	}
	switch name {
	case "podtoast_cpu_limit_cores":
		return "cpu_cores", "limit"
	case "podtoast_memory_limit_bytes":
		return "memory_bytes", "limit"
	}
	return "", ""
}

// SetReferenceVisibility uses the monitored selection, not successful scrapes:
// a missing endpoint must not make a multi-container selection appear singular.
func (s *Session) SetReferenceVisibility(singleContainer bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.showReferences = singleContainer
	s.revision++
}
