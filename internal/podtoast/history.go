package podtoast

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

const HistoryFrames = 120 // ten minutes at five-second intervals
const HistoryBytes = 1 << 20
const HistoryInterval = 5 * time.Second

type HistoryFrame struct {
	At   int64  `json:"at"`
	Body string `json:"body"`
}
type MetricsHistory struct {
	Frames []HistoryFrame `json:"frames"`
}

func (h *MetricsHistory) Append(at time.Time, body string) {
	if len(body) > 32<<10 {
		return
	}
	h.Frames = append(h.Frames, HistoryFrame{At: at.UnixMilli(), Body: body})
	bytes := 0
	start := len(h.Frames)
	for start > 0 && len(h.Frames)-start < HistoryFrames {
		f := h.Frames[start-1]
		if bytes+len(f.Body) > HistoryBytes || at.UnixMilli()-f.At > int64(10*time.Minute/time.Millisecond) {
			break
		}
		bytes += len(f.Body)
		start--
	}
	if start > 0 {
		n := copy(h.Frames, h.Frames[start:])
		clear(h.Frames[n:])
		h.Frames = h.Frames[:n]
	}
}
func (c *Client) MetricsHistory(ctx context.Context) (MetricsHistory, error) {
	r, err := c.request(ctx, "GET", "/metrics/history", nil)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return MetricsHistory{}, fmt.Errorf("agent has no metrics history; update PodToast")
		}
		return MetricsHistory{}, err
	}
	defer r.Body.Close()
	if r.StatusCode == 404 {
		return MetricsHistory{}, nil
	} // older agents remain compatible
	if r.StatusCode != 200 {
		return MetricsHistory{}, fmt.Errorf("metrics history: HTTP %d", r.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, 2*HistoryBytes+1))
	if err != nil {
		return MetricsHistory{}, err
	}
	if len(b) > 2*HistoryBytes {
		return MetricsHistory{}, fmt.Errorf("metrics history exceeds limit")
	}
	var h MetricsHistory
	err = json.Unmarshal(b, &h)
	if len(h.Frames) > HistoryFrames {
		return MetricsHistory{}, fmt.Errorf("metrics history has too many samples")
	}
	return h, err
}
