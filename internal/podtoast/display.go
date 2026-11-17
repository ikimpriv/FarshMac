package podtoast

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// MonitorText accepts decoded JSON snapshots and remains bounded regardless
// of process/socket count. Full data is available separately as a snapshot.
func MonitorText(s Snapshot) string { return monitorText(s, nil) }
func monitorText(s Snapshot, rates map[float64]float64) string {
	var out strings.Builder
	fmt.Fprintf(&out, "PodToast · %s · target PID %d · Esc / Ctrl+C stops\n\n", s.At.Format("15:04:05"), s.TargetPID)
	if s.Resources.Unavailable != "" {
		fmt.Fprintf(&out, "Resources: %s\n", s.Resources.Unavailable)
	} else if m, ok := s.Resources.Value.(map[string]any); ok {
		for _, name := range []string{"cpu.stat", "cpu.max", "memory.current", "memory.max", "pids.current", "memory.events"} {
			if section, ok := m[name].(map[string]any); ok {
				if value, ok := section["value"].(string); ok {
					fmt.Fprintf(&out, "%-15s %s\n", name, strings.ReplaceAll(value, "\n", " · "))
				}
			}
		}
	}
	if m, ok := s.Storage.Value.(map[string]any); ok {
		fmt.Fprintf(&out, "Storage /: used %.1f MiB · available %.1f MiB (shared filesystem capacity)\n", number(m["usedBytes"])/(1<<20), number(m["freeBytes"])/(1<<20))
	} else {
		fmt.Fprintf(&out, "Storage: %s\n", s.Storage.Unavailable)
	}
	out.WriteString("\nProcesses — visible PID namespace; CPU%: 100% = 1 core; TIME(s): lifetime CPU\n    PID    PPID    CPU%    TIME(s)    RSS MiB THR STATE NAME\n")
	if m, ok := s.Processes.Value.(map[string]any); ok {
		if list, ok := m["items"].([]any); ok {
			list = append([]any(nil), list...)
			if rates != nil {
				sort.SliceStable(list, func(i, j int) bool {
					a, _ := list[i].(map[string]any)
					b, _ := list[j].(map[string]any)
					return rates[number(a["pid"])] > rates[number(b["pid"])]
				})
			}
			for _, item := range list[:min(16, len(list))] {
				if p, ok := item.(map[string]any); ok {
					cpu := "—"
					if value, ok := rates[number(p["pid"])]; ok {
						cpu = fmt.Sprintf("%.1f", value)
					}
					fmt.Fprintf(&out, "%7.0f %7.0f %7s %10.2f %10.1f %3.0f %-5s %.60s\n", number(p["pid"]), number(p["parent"]), cpu, number(p["cpuSeconds"]), number(p["rssBytes"])/(1<<20), number(p["threads"]), safeText(fmt.Sprint(p["state"])), safeText(fmt.Sprint(p["name"])))
				}
			}
			fmt.Fprintf(&out, "%d processes (up to 16 shown; full details in PodToast snapshot)\n", len(list))
		}
	} else {
		fmt.Fprintf(&out, "%s\n", s.Processes.Unavailable)
	}
	out.WriteString("\nNetwork — shared by pod containers\n")
	if m, ok := s.Network.Value.(map[string]any); ok {
		for _, name := range []string{"tcp", "tcp6", "udp", "udp6"} {
			if sec, ok := m[name].(map[string]any); ok {
				if v, ok := sec["value"].(map[string]any); ok {
					list, _ := v["items"].([]any)
					fmt.Fprintf(&out, "%s: %d sockets  ", name, len(list))
				}
			}
		}
	}
	out.WriteString("\n")
	return out.String()
}
func number(v any) float64 { n, _ := v.(float64); return n }
func safeText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, s)
}

// MonitorFormatter computes rates from successive samples without storing a
// time series. CPU 100% means one fully occupied core, not the entire node.
type processCPUPoint struct{ start, seconds float64 }
type MonitorFormatter struct {
	processAt time.Time
	processes map[float64]processCPUPoint
	previous  time.Time
	usage     float64
	have      bool
}

func (f *MonitorFormatter) Format(s Snapshot) string {
	rate := "CPU: sampling…"
	if resources, ok := s.Resources.Value.(map[string]any); ok {
		if u, ok := resources["usage"].(map[string]any); ok {
			if use, ok := u["usage_usec"].(float64); ok {
				if f.have && s.At.After(f.previous) && use >= f.usage {
					rate = fmt.Sprintf("CPU %.2f%% (100%% = 1 core)", (use-f.usage)/s.At.Sub(f.previous).Seconds()/10000)
				}
				f.previous, f.usage, f.have = s.At, use, true
				if memory, ok := u["memory.current"].(float64); ok {
					rate += fmt.Sprintf(" · memory %.1f MiB", memory/(1<<20))
				}
			}
		}
	}
	rates := map[float64]float64{}
	next := map[float64]processCPUPoint{}
	if section, ok := s.Processes.Value.(map[string]any); ok {
		if list, ok := section["items"].([]any); ok {
			dt := s.At.Sub(f.processAt).Seconds()
			for _, raw := range list {
				p, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				pid := number(p["pid"])
				point := processCPUPoint{number(p["startTicks"]), number(p["cpuSeconds"])}
				if old, ok := f.processes[pid]; ok && old.start == point.start && point.seconds >= old.seconds && dt > 0 && dt <= 30 {
					rates[pid] = 100 * (point.seconds - old.seconds) / dt
				}
				if len(next) < 16384 {
					next[pid] = point
				}
			}
		}
	}
	f.processes = next
	f.processAt = s.At
	return rate + "\n" + monitorText(s, rates)
}
