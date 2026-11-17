//go:build !linux

package podtoast

import (
	"context"
	"time"
)

func (s *Server) snapshot(context.Context) Snapshot {
	unavailable := Section{Unavailable: "Linux /proc and cgroup are required"}
	return Snapshot{At: time.Now(), Processes: unavailable, Resources: unavailable, Storage: unavailable, Network: unavailable}
}

func processMetricTotals(Section) (count, rss, cpu float64, ok bool) { return 0, 0, 0, false }
