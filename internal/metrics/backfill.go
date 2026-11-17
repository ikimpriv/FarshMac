package metrics

import (
	"sort"
	"time"
)

// Build history independently, then merge it before any live samples already
// received. A transient history error must not permanently discard old points.
func (s *Session) backfillHistory(index int, target Target, frames []HistoryFrame, now time.Time) {
	s.mu.Lock()
	interval := s.interval
	s.mu.Unlock()
	temp := &Session{targets: []Target{target}, status: []EndpointStatus{{ID: target.ID, Enabled: true}}, series: map[string]*Series{}, selected: map[string]bool{}, interval: interval}
	sort.Slice(frames, func(i, j int) bool { return frames[i].At.Before(frames[j].At) })
	if len(frames) > MaxPoints {
		frames = frames[len(frames)-MaxPoints:]
	}
	for _, frame := range frames {
		if !frame.At.Before(now) || now.Sub(frame.At) > 10*time.Minute {
			continue
		}
		if samples, err := Parse(frame.Body); err == nil {
			temp.ingest(0, filterTargetSamples(target, samples), nil, frame.At)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.paused || !s.status[index].Enabled {
		return
	}
	for id, old := range temp.series {
		current := s.series[id]
		if current == nil {
			if len(s.series) >= MaxSeries {
				continue
			}
			s.series[id] = old
			s.catalogRevision++
			continue
		}
		var points []Point
		for _, p := range old.Points {
			if len(current.Points) == 0 || p.Time < current.Points[0].Time {
				points = append(points, p)
			}
		}
		if len(points) == 0 {
			continue
		}
		points = append(points, current.Points...)
		if len(points) > MaxPoints {
			points = points[len(points)-MaxPoints:]
		}
		for i := range points {
			points[i].Delta = nil
			points[i].Rate = nil
			if i == 0 || points[i].Value == nil || points[i-1].Value == nil {
				continue
			}
			dt := time.Duration(points[i].Time-points[i-1].Time) * time.Millisecond
			if dt <= 0 || dt > 3*interval+5*time.Second {
				continue
			}
			delta := *points[i].Value - *points[i-1].Value
			if delta < 0 && (current.Kind == "counter" || current.Kind == "histogram bucket") {
				delta = *points[i].Value
			}
			points[i].Delta = finite(delta)
			points[i].Rate = finite(delta / dt.Seconds())
		}
		current.Points = points
	}
	s.revision++
}
