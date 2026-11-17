//go:build linux

package podtoast

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

func trafficOverview(ctx context.Context) *TrafficOverview {
	return readTrafficOverview(ctx, "/proc/net")
}
func readTrafficOverview(ctx context.Context, root string) *TrafficOverview {
	out := &TrafficOverview{Sockets: []TrafficSocket{}}
	for _, protocol := range []string{"tcp", "tcp6", "udp", "udp6"} {
		if ctx.Err() != nil {
			out.Warnings = append(out.Warnings, ctx.Err().Error())
			break
		}
		data, err := socketTable(filepath.Join(root, protocol))
		if err != nil {
			out.Warnings = append(out.Warnings, protocol+": "+err.Error())
			continue
		}
		table := data.(map[string]any)
		if truncated, _ := table["truncated"].(bool); truncated {
			out.Truncated = true
		}
		for i, s := range table["items"].([]Socket) {
			if i >= 1024 {
				out.Truncated = true
				break
			}
			state := s.State
			if strings.HasPrefix(protocol, "udp") && state == "CLOSE" {
				state = "UNCONNECTED"
			}
			out.Sockets = append(out.Sockets, TrafficSocket{protocol, s.Local, s.Remote, state, s.Inode})
		}
	}
	text, err := smallFile(filepath.Join(root, "dev"))
	if err != nil {
		out.Warnings = append(out.Warnings, "interface counters: "+err.Error())
		return out
	}
	valid := false
	for _, line := range strings.Split(text, "\n") {
		name, values, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(name) == "lo" {
			continue
		}
		f := strings.Fields(values)
		if len(f) < 16 {
			out.Warnings = append(out.Warnings, "incomplete interface counters")
			continue
		}
		rx, e1 := strconv.ParseUint(f[0], 10, 64)
		tx, e2 := strconv.ParseUint(f[8], 10, 64)
		if e1 != nil || e2 != nil {
			out.Warnings = append(out.Warnings, fmt.Sprintf("invalid counters for %s", strings.TrimSpace(name)))
			continue
		}
		out.Received += rx
		out.Sent += tx
		valid = true
	}
	out.CountersValid = valid
	if !valid {
		out.Warnings = append(out.Warnings, "No non-loopback interface counters available")
	}
	return out
}
