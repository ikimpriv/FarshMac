package podtoast

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const metricsTimeout = 5 * time.Second

// MetricsHandler exposes only read-only metrics, never file or probe endpoints.
func (s *Server) MetricsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" && r.URL.Path != "/metrics/history" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", 405)
			return
		}
		if s.CheckTarget != nil {
			if err := s.CheckTarget(); err != nil {
				http.Error(w, err.Error(), 410)
				return
			}
		}
		if r.URL.Path == "/metrics/history" {
			h := MetricsHistory{}
			if s.History != nil {
				var err error
				h, err = s.History()
				if err != nil {
					http.Error(w, err.Error(), 503)
					return
				}
			}
			reply(w, h)
			return
		}
		// Do not queue expensive snapshots behind concurrent scrapers.
		if !s.metricsMu.TryLock() {
			http.Error(w, "scrape already in progress", 503)
			return
		}
		defer s.metricsMu.Unlock()
		ctx, cancel := context.WithTimeout(r.Context(), metricsTimeout)
		defer cancel()
		snap := s.snapshot(ctx)
		if ctx.Err() != nil {
			http.Error(w, "scrape cancelled or timed out", 503)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		writeMetrics(w, snap)
	})
}

func writeMetrics(w io.Writer, s Snapshot) {
	emit := func(name, kind, help string, value float64) {
		fmt.Fprintf(w, "# HELP podtoast_%s %s\n# TYPE podtoast_%s %s\npodtoast_%s %s\n", name, help, name, kind, name, strconv.FormatFloat(value, 'g', -1, 64))
	}
	for _, item := range []struct {
		name    string
		section Section
	}{{"processes", s.Processes}, {"resources", s.Resources}, {"storage", s.Storage}, {"network", s.Network}} {
		available := 0.0
		if item.section.Unavailable == "" && item.section.Value != nil {
			available = 1
		}
		emit(item.name+"_available", "gauge", "Whether this observation section is available.", available)
	}
	if count, rss, cpu, ok := processMetricTotals(s.Processes); ok {
		emit("visible_processes", "gauge", "Processes visible in the target PID namespace; may include other containers.", count)
		emit("visible_process_rss_bytes", "gauge", "Sum of visible process RSS; not cgroup memory usage.", rss)
		emit("visible_process_cpu_seconds", "gauge", "Sum of CPU time of currently visible processes; decreases when processes exit.", cpu)
	}
	// No timestamps are attached to samples: the scraper owns sample timestamps.
	if resources, ok := s.Resources.Value.(map[string]any); ok {
		sectionText := func(key string) string { v, _ := resources[key].(Section); t, _ := v.Value.(string); return t }
		cpu := strings.Fields(sectionText("cpu.stat"))
		cpuNames := map[string]string{"usage_usec": "cpu_usage_seconds_total", "user_usec": "cpu_user_seconds_total", "system_usec": "cpu_system_seconds_total", "throttled_usec": "cpu_throttled_seconds_total", "nr_throttled": "cpu_throttled_periods_total", "nr_periods": "cpu_periods_total"}
		for i := 0; i+1 < len(cpu); i += 2 {
			if name, ok := cpuNames[cpu[i]]; ok {
				if v, e := strconv.ParseFloat(cpu[i+1], 64); e == nil {
					if strings.HasSuffix(cpu[i], "_usec") {
						v /= 1e6
					}
					emit(name, "counter", "Verified target cgroup CPU counter.", v)
				}
			}
		}
		for _, item := range []struct{ file, name string }{{"memory.current", "memory_usage_bytes"}, {"memory.max", "memory_limit_bytes"}, {"memory.peak", "memory_peak_bytes"}, {"memory.swap.current", "memory_swap_bytes"}, {"pids.current", "tasks"}, {"pids.max", "tasks_limit"}} {
			if v, e := strconv.ParseFloat(sectionText(item.file), 64); e == nil {
				emit(item.name, "gauge", "Verified target cgroup value; unlimited or unavailable limits are omitted.", v)
			}
		}
		// Keep total usage separate from working set: file cache is charged to
		// the cgroup, but inactive file pages are excluded from working set.
		memory := map[string]float64{}
		fields := strings.Fields(sectionText("memory.stat"))
		for i := 0; i+1 < len(fields); i += 2 {
			if v, err := strconv.ParseUint(fields[i+1], 10, 64); err == nil {
				memory[fields[i]] = float64(v)
			}
		}
		if v, ok := memory["anon"]; ok {
			emit("memory_anonymous_bytes", "gauge", "Anonymous application memory in the target cgroup; excludes file cache.", v)
		}
		if v, ok := memory["file"]; ok {
			emit("memory_file_cache_bytes", "gauge", "Target cgroup file cache, including tmpfs and shared memory.", v)
		}
		if inactive, ok := memory["inactive_file"]; ok {
			if total, err := strconv.ParseUint(sectionText("memory.current"), 10, 64); err == nil {
				emit("memory_working_set_bytes", "gauge", "Target cgroup total minus inactive file cache; includes active cache and kernel memory.", max(0, float64(total)-inactive))
			}
		}
		quota := strings.Fields(sectionText("cpu.max"))
		if len(quota) == 2 {
			q, e := strconv.ParseFloat(quota[0], 64)
			p, e2 := strconv.ParseFloat(quota[1], 64)
			if e == nil && e2 == nil && p > 0 {
				emit("cpu_limit_cores", "gauge", "Verified target cgroup CPU quota in cores.", q/p)
			}
		}
		events := strings.Fields(sectionText("memory.events"))
		for i := 0; i+1 < len(events); i += 2 {
			if events[i] == "oom" || events[i] == "oom_kill" {
				if v, e := strconv.ParseFloat(events[i+1], 64); e == nil {
					emit("memory_"+events[i]+"_total", "counter", "Verified target cgroup memory event count.", v)
				}
			}
		}
	}
	if storage, ok := s.Storage.Value.(map[string]any); ok && s.Storage.Unavailable == "" {
		for _, item := range []struct{ key, name string }{{"capacityBytes", "filesystem_capacity_bytes"}, {"freeBytes", "filesystem_available_bytes"}, {"usedBytes", "filesystem_used_bytes"}, {"inodes", "filesystem_inodes"}, {"freeInodes", "filesystem_free_inodes"}} {
			if v, ok := storage[item.key].(uint64); ok {
				emit(item.name, "gauge", "Root filesystem value; may be shared with other containers.", float64(v))
			}
		}
	}
	if network, ok := s.Network.Value.(map[string]any); ok {
		dev, _ := network["dev"].(Section)
		text, _ := dev.Value.(string)
		rx, tx := uint64(0), uint64(0)
		found := false
		for _, line := range strings.Split(text, "\n") {
			_, data, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			fields := strings.Fields(data)
			if len(fields) < 16 {
				continue
			}
			r, e := strconv.ParseUint(fields[0], 10, 64)
			t, e2 := strconv.ParseUint(fields[8], 10, 64)
			if e == nil && e2 == nil {
				rx += r
				tx += t
				found = true
			}
		}
		if found {
			emit("pod_network_receive_bytes_total", "counter", "Pod network bytes received across interfaces including loopback.", float64(rx))
			emit("pod_network_transmit_bytes_total", "counter", "Pod network bytes transmitted across interfaces including loopback.", float64(tx))
		}
	}
}
