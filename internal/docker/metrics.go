package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func parseQuantity(s string) (float64, error) {
	s = strings.TrimSpace(s)
	for _, suffix := range []struct {
		unit  string
		scale float64
	}{{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}, {"GB", 1e9}, {"MB", 1e6}, {"kB", 1e3}, {"KB", 1e3}, {"B", 1}} {
		if strings.HasSuffix(s, suffix.unit) {
			v, e := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(s, suffix.unit)), 64)
			return v * suffix.scale, e
		}
	}
	return strconv.ParseFloat(s, 64)
}
func (c Client) Metrics(ctx context.Context, id string) ([]byte, error) {
	data, err := c.Run(ctx, "stats", "--no-stream", "--format", "{{json .}}", id)
	if err != nil {
		return nil, err
	}
	var values struct{ CPUPerc, MemUsage, NetIO, BlockIO, PIDs string }
	if err = json.Unmarshal(data, &values); err != nil {
		return nil, err
	}
	cpu, err := strconv.ParseFloat(strings.TrimSuffix(values.CPUPerc, "%"), 64)
	if err != nil {
		return nil, err
	}
	memory, err := parseQuantity(strings.Split(values.MemUsage, "/")[0])
	if err != nil {
		return nil, err
	}
	var out strings.Builder
	fmt.Fprintf(&out, "# TYPE docker_container_cpu_usage_cores gauge\ndocker_container_cpu_usage_cores %g\n# TYPE container_memory_working_set_bytes gauge\ncontainer_memory_working_set_bytes %g\n", cpu/100, memory)
	for _, item := range []struct{ text, a, b string }{{values.NetIO, "receive", "transmit"}, {values.BlockIO, "read", "write"}} {
		parts := strings.Split(item.text, "/")
		if len(parts) != 2 {
			continue
		}
		for i, name := range []string{item.a, item.b} {
			value, e := parseQuantity(parts[i])
			if e != nil {
				continue
			}
			metric := "docker_container_" + name + "_bytes_total"
			fmt.Fprintf(&out, "# TYPE %s counter\n%s %g\n", metric, metric, value)
		}
	}
	if pids, e := strconv.ParseFloat(values.PIDs, 64); e == nil {
		fmt.Fprintf(&out, "# TYPE docker_container_processes gauge\ndocker_container_processes %g\n", pids)
	}
	return []byte(out.String()), nil
}
